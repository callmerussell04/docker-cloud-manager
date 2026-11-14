package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// cleanupConfig ticks every 5 seconds with a 20-second job timeout.
var cleanupConfig = coretest.StaticConfig{Config: coretest.SystemConfig()}

func decodeLogs(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		entry := map[string]any{}
		require.NoError(t, json.Unmarshal(line, &entry))
		entries = append(entries, entry)
	}
	return entries
}

func TestRegistryCleanupSurvivesPollingFailureAndRetriesFailedJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo := coremocks.NewRegistryCleanupRepository(t)
		registry := coremocks.NewImageRegistryAPI(t)
		job := model.RegistryCleanup{ID: uuid.New(), Repository: "owner_app", Tag: "v1"}
		unlock := func() {}

		// Tick 1: polling fails. Tick 2: the job fails. Tick 3: the job completes.
		repo.EXPECT().PendingRegistryCleanup(mock.Anything).Return(nil, errors.New("database unavailable")).Once()
		repo.EXPECT().PendingRegistryCleanup(mock.Anything).Return([]model.RegistryCleanup{job}, nil).Twice()
		repo.EXPECT().PendingRegistryCleanup(mock.Anything).Return(nil, nil).Maybe()
		repo.EXPECT().LockRegistryRepository(mock.Anything, job.Repository).Return(unlock, nil).Twice()
		repo.EXPECT().RegistryCleanupFacts(mock.Anything, job, service.RegistryCleanupDelay).Return(model.RegistryCleanupFacts{}, nil).Twice()
		registry.EXPECT().GetImageSizeAndDigest(mock.Anything, job.Repository, job.Tag).Return(0, "", apperrors.ErrUnavailable).Once()
		registry.EXPECT().GetImageSizeAndDigest(mock.Anything, job.Repository, job.Tag).Return(10, "sha256:abc", nil).Once()
		registry.EXPECT().DeleteManifest(mock.Anything, job.Repository, "sha256:abc").Return(nil).Once()
		repo.EXPECT().CompleteRegistryCleanup(mock.Anything, job.ID).Return(nil).Once()

		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		worker := newCleanupWorker(t, repo, registry, logger)
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Go(func() { worker.Run(ctx) })

		time.Sleep(17 * time.Second)
		synctest.Wait()
		cancel()
		wg.Wait()

		var failures []map[string]any
		for _, entry := range decodeLogs(t, &logs) {
			if entry["level"] == "ERROR" {
				failures = append(failures, entry)
			}
		}
		require.Len(t, failures, 2, "each failure is logged once")
		require.Equal(t, "registry cleanup poll", failures[0]["operation"])
		require.Contains(t, failures[0]["error"], "database unavailable")
		require.Equal(t, "registry cleanup job", failures[1]["operation"])
		require.Equal(t, job.ID.String(), failures[1]["cleanup_id"])
		require.NotEmpty(t, failures[1]["error"])
	})
}

func TestRegistryCleanupStopsQuietlyWhenCanceledDuringAJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo := coremocks.NewRegistryCleanupRepository(t)
		registry := coremocks.NewImageRegistryAPI(t)
		job := model.RegistryCleanup{ID: uuid.New(), Repository: "owner_app", Tag: "v1"}
		repo.EXPECT().PendingRegistryCleanup(mock.Anything).Return([]model.RegistryCleanup{job, job}, nil).Once()
		repo.EXPECT().LockRegistryRepository(mock.Anything, job.Repository).Return(func() {}, nil).Once()
		repo.EXPECT().RegistryCleanupFacts(mock.Anything, job, service.RegistryCleanupDelay).Return(model.RegistryCleanupFacts{}, nil).Once()
		registry.EXPECT().GetImageSizeAndDigest(mock.Anything, job.Repository, job.Tag).RunAndReturn(func(ctx context.Context, _, _ string) (int64, string, error) {
			<-ctx.Done()
			return 0, "", ctx.Err()
		}).Once()

		var logs bytes.Buffer
		worker := newCleanupWorker(t, repo, registry, slog.New(slog.NewJSONHandler(&logs, nil)))
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Go(func() { worker.Run(ctx) })
		time.Sleep(6 * time.Second)
		synctest.Wait()
		cancel()
		wg.Wait()
		for _, entry := range decodeLogs(t, &logs) {
			require.NotEqual(t, "ERROR", entry["level"], "shutdown is not a cleanup failure")
		}
	})
}

// runCleanupTick runs the cleanup worker through its first 5-second tick and returns
// the error-level log entries.
func runCleanupTick(t *testing.T, repo *coremocks.RegistryCleanupRepository, registry *coremocks.ImageRegistryAPI) []map[string]any {
	t.Helper()
	var logs bytes.Buffer
	worker := newCleanupWorker(t, repo, registry, slog.New(slog.NewJSONHandler(&logs, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { worker.Run(ctx) })
	time.Sleep(6 * time.Second)
	synctest.Wait()
	cancel()
	wg.Wait()
	var failures []map[string]any
	for _, entry := range decodeLogs(t, &logs) {
		if entry["level"] == "ERROR" {
			failures = append(failures, entry)
		}
	}
	return failures
}

func TestRegistryCleanupSeparatesAbsentManifestFromUpstreamFailure(t *testing.T) {
	tests := []struct {
		name             string
		digest           string
		lookupErr        error
		expectedComplete bool
	}{
		{name: "absent manifest completes the job", lookupErr: apperrors.ErrNotFound, expectedComplete: true},
		{name: "missing digest keeps the job", digest: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				repo := coremocks.NewRegistryCleanupRepository(t)
				registry := coremocks.NewImageRegistryAPI(t)
				job := model.RegistryCleanup{ID: uuid.New(), Repository: "owner_app", Tag: "v1"}
				repo.EXPECT().PendingRegistryCleanup(mock.Anything).Return([]model.RegistryCleanup{job}, nil).Once()
				repo.EXPECT().LockRegistryRepository(mock.Anything, job.Repository).Return(func() {}, nil).Once()
				repo.EXPECT().RegistryCleanupFacts(mock.Anything, job, service.RegistryCleanupDelay).Return(model.RegistryCleanupFacts{}, nil).Once()
				registry.EXPECT().GetImageSizeAndDigest(mock.Anything, job.Repository, job.Tag).Return(10, tc.digest, tc.lookupErr).Once()
				if tc.expectedComplete {
					repo.EXPECT().CompleteRegistryCleanup(mock.Anything, job.ID).Return(nil).Once()
				}

				failures := runCleanupTick(t, repo, registry)
				if tc.expectedComplete {
					require.Empty(t, failures)
					return
				}
				require.Len(t, failures, 1)
				require.Equal(t, job.ID.String(), failures[0]["cleanup_id"])
			})
		})
	}
}

// newCleanupWorker builds a cleanup worker whose repositories hold no other
// available image, so no manifest is shared.
func newCleanupWorker(t *testing.T, repo *coremocks.RegistryCleanupRepository, registry *coremocks.ImageRegistryAPI, logger *slog.Logger) *service.RegistryCleanupWorker {
	t.Helper()
	images := coremocks.NewManifestSharingRepository(t)
	images.EXPECT().ListAvailableInRepository(mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	return newCleanupWorkerWithImages(t, repo, images, registry, logger)
}

func newCleanupWorkerWithImages(t *testing.T, repo *coremocks.RegistryCleanupRepository, images *coremocks.ManifestSharingRepository, registry *coremocks.ImageRegistryAPI, logger *slog.Logger) *service.RegistryCleanupWorker {
	t.Helper()
	worker, err := service.NewRegistryCleanupWorker(service.RegistryCleanupWorkerDeps{Repository: repo, Images: images, Registry: registry, Config: cleanupConfig, Logger: logger})
	require.NoError(t, err)
	return worker
}

// The Registry deletes a manifest by digest, removing every tag on it, so a
// cleanup job deletes its manifest only when no available image of the
// repository uses the same digest. An available row without a recorded
// digest has it resolved and recorded first; a failed resolution postpones
// the job.
func TestRegistryCleanupKeepsAManifestAnAvailableImageShares(t *testing.T) {
	digest := func(s string) *string { return &s }
	tests := []struct {
		name      string
		available []model.Image
		resolve   map[string]error // Registry tags resolved for rows without a digest
		resolved  map[string]string
		recorded  map[string]string // registry tag -> digest recorded
		deletes   bool
		complete  bool
	}{
		{name: "shared recorded digest completes without deleting",
			available: []model.Image{{RegistryTag: "other", Digest: digest("sha256:other")}, {RegistryTag: "twin", Digest: digest("sha256:job")}},
			complete:  true},
		{name: "different digests delete",
			available: []model.Image{{RegistryTag: "other", Digest: digest("sha256:other")}},
			deletes:   true, complete: true},
		{name: "unrecorded shared digest is recorded and keeps the manifest",
			available: []model.Image{{RegistryTag: "legacy"}},
			resolve:   map[string]error{"legacy": nil}, resolved: map[string]string{"legacy": "sha256:job"},
			recorded: map[string]string{"legacy": "sha256:job"}, complete: true},
		{name: "unrecorded different digest is recorded and the manifest deleted",
			available: []model.Image{{RegistryTag: "legacy"}},
			resolve:   map[string]error{"legacy": nil}, resolved: map[string]string{"legacy": "sha256:legacy"},
			recorded: map[string]string{"legacy": "sha256:legacy"}, deletes: true, complete: true},
		{name: "unrecorded row with no manifest shares nothing",
			available: []model.Image{{RegistryTag: "gone"}},
			resolve:   map[string]error{"gone": apperrors.ErrNotFound},
			deletes:   true, complete: true},
		{name: "failed resolution postpones the job",
			available: []model.Image{{RegistryTag: "legacy"}},
			resolve:   map[string]error{"legacy": apperrors.ErrUnavailable}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				repo := coremocks.NewRegistryCleanupRepository(t)
				images := coremocks.NewManifestSharingRepository(t)
				registry := coremocks.NewImageRegistryAPI(t)
				job := model.RegistryCleanup{ID: uuid.New(), Repository: "owner_app", Tag: "v1"}
				for i := range tc.available {
					tc.available[i].ID = uuid.New()
					tc.available[i].Status = model.ImageStatusAvailable
				}
				repo.EXPECT().PendingRegistryCleanup(mock.Anything).Return([]model.RegistryCleanup{job}, nil).Once()
				repo.EXPECT().PendingRegistryCleanup(mock.Anything).Return(nil, nil).Maybe()
				repo.EXPECT().LockRegistryRepository(mock.Anything, job.Repository).Return(func() {}, nil).Once()
				repo.EXPECT().RegistryCleanupFacts(mock.Anything, job, service.RegistryCleanupDelay).Return(model.RegistryCleanupFacts{}, nil).Once()
				registry.EXPECT().GetImageSizeAndDigest(mock.Anything, job.Repository, job.Tag).Return(10, "sha256:job", nil).Once()
				images.EXPECT().ListAvailableInRepository(mock.Anything, job.Repository).Return(tc.available, nil).Once()
				for tag, err := range tc.resolve {
					registry.EXPECT().GetImageSizeAndDigest(mock.Anything, job.Repository, tag).Return(10, tc.resolved[tag], err).Once()
				}
				for _, img := range tc.available {
					if d, ok := tc.recorded[img.RegistryTag]; ok {
						images.EXPECT().RecordDigest(mock.Anything, img.ID, d).Return(nil).Once()
					}
				}
				if tc.deletes {
					registry.EXPECT().DeleteManifest(mock.Anything, job.Repository, "sha256:job").Return(nil).Once()
				}
				if tc.complete {
					repo.EXPECT().CompleteRegistryCleanup(mock.Anything, job.ID).Return(nil).Once()
				}

				var logs bytes.Buffer
				worker := newCleanupWorkerWithImages(t, repo, images, registry, slog.New(slog.NewJSONHandler(&logs, nil)))
				ctx, cancel := context.WithCancel(context.Background())
				var wg sync.WaitGroup
				wg.Go(func() { worker.Run(ctx) })
				time.Sleep(6 * time.Second)
				synctest.Wait()
				cancel()
				wg.Wait()
				var failures int
				for _, entry := range decodeLogs(t, &logs) {
					if entry["level"] == "ERROR" {
						failures++
					}
				}
				if tc.complete {
					require.Zero(t, failures)
				} else {
					require.Equal(t, 1, failures, "the postponed job is logged and stays queued")
				}
			})
		})
	}
}

func TestRegistryCleanupDecidesJobsFromLockedFacts(t *testing.T) {
	tests := []struct {
		name     string
		facts    model.RegistryCleanupFacts
		deletes  bool
		complete bool
	}{
		{name: "active publisher postpones", facts: model.RegistryCleanupFacts{PublisherActive: true, TagAvailable: true}},
		{name: "token inside fence postpones", facts: model.RegistryCleanupFacts{TokenMayWrite: true}},
		{name: "available tag completes without deleting", facts: model.RegistryCleanupFacts{TagAvailable: true}, complete: true},
		{name: "quiet repository deletes then completes", deletes: true, complete: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				repo := coremocks.NewRegistryCleanupRepository(t)
				registry := coremocks.NewImageRegistryAPI(t)
				job := model.RegistryCleanup{ID: uuid.New(), Repository: "owner_app", Tag: "v1"}
				repo.EXPECT().PendingRegistryCleanup(mock.Anything).Return([]model.RegistryCleanup{job}, nil).Once()
				repo.EXPECT().LockRegistryRepository(mock.Anything, job.Repository).Return(func() {}, nil).Once()
				repo.EXPECT().RegistryCleanupFacts(mock.Anything, job, service.RegistryCleanupDelay).Return(tc.facts, nil).Once()
				if tc.deletes {
					registry.EXPECT().GetImageSizeAndDigest(mock.Anything, job.Repository, job.Tag).Return(10, "sha256:abc", nil).Once()
					registry.EXPECT().DeleteManifest(mock.Anything, job.Repository, "sha256:abc").Return(nil).Once()
				}
				if tc.complete {
					repo.EXPECT().CompleteRegistryCleanup(mock.Anything, job.ID).Return(nil).Once()
				}

				require.Empty(t, runCleanupTick(t, repo, registry))
			})
		})
	}
}

func TestRegistryCleanupWorkerRejectsMissingDependencies(t *testing.T) {
	valid := func(t *testing.T) service.RegistryCleanupWorkerDeps {
		return service.RegistryCleanupWorkerDeps{
			Repository: coremocks.NewRegistryCleanupRepository(t),
			Images:     coremocks.NewManifestSharingRepository(t),
			Registry:   coremocks.NewImageRegistryAPI(t),
			Config:     cleanupConfig,
		}
	}
	depstest.RequireRejectsMissing(t, valid, service.NewRegistryCleanupWorker, map[string]string{
		"Repository": "repository",
		"Images":     "images",
		"Registry":   "registry",
		"Config":     "config",
	})
}
