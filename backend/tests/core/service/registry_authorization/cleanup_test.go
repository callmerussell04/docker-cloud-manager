package registry_authorization_test

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

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/coretest"
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
		repo := coremocks.NewRegistryGrantRepository(t)
		registry := coremocks.NewImageRegistryAPI(t)
		job := model.RegistryCleanup{ID: uuid.New(), Repository: "owner_app", Tag: "v1"}
		unlock := func() {}

		// Tick 1: polling fails. Tick 2: the job fails. Tick 3: the job completes.
		repo.EXPECT().PendingRegistryCleanup(mock.Anything).Return(nil, errors.New("database unavailable")).Once()
		repo.EXPECT().PendingRegistryCleanup(mock.Anything).Return([]model.RegistryCleanup{job}, nil).Twice()
		repo.EXPECT().PendingRegistryCleanup(mock.Anything).Return(nil, nil).Maybe()
		repo.EXPECT().LockRegistryRepository(mock.Anything, job.Repository).Return(unlock, nil).Twice()
		repo.EXPECT().RegistryCleanupState(mock.Anything, job, service.RegistryCleanupDelay).Return(model.RegistryCleanupReady, nil).Twice()
		registry.EXPECT().GetImageSizeAndDigest(mock.Anything, job.Repository, job.Tag).Return(0, "", apperrors.ErrUnavailable).Once()
		registry.EXPECT().GetImageSizeAndDigest(mock.Anything, job.Repository, job.Tag).Return(10, "sha256:abc", nil).Once()
		registry.EXPECT().DeleteManifest(mock.Anything, job.Repository, "sha256:abc").Return(nil).Once()
		repo.EXPECT().CompleteRegistryCleanup(mock.Anything, job.ID).Return(nil).Once()

		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		auth := service.NewRegistryAuthorization(repo, nil, nil, "registry:5000")
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Go(func() { auth.RunCleanup(ctx, registry, cleanupConfig, logger) })

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
		repo := coremocks.NewRegistryGrantRepository(t)
		registry := coremocks.NewImageRegistryAPI(t)
		job := model.RegistryCleanup{ID: uuid.New(), Repository: "owner_app", Tag: "v1"}
		repo.EXPECT().PendingRegistryCleanup(mock.Anything).Return([]model.RegistryCleanup{job, job}, nil).Once()
		repo.EXPECT().LockRegistryRepository(mock.Anything, job.Repository).Return(func() {}, nil).Once()
		repo.EXPECT().RegistryCleanupState(mock.Anything, job, service.RegistryCleanupDelay).Return(model.RegistryCleanupReady, nil).Once()
		registry.EXPECT().GetImageSizeAndDigest(mock.Anything, job.Repository, job.Tag).RunAndReturn(func(ctx context.Context, _, _ string) (int64, string, error) {
			<-ctx.Done()
			return 0, "", ctx.Err()
		}).Once()

		var logs bytes.Buffer
		auth := service.NewRegistryAuthorization(repo, nil, nil, "registry:5000")
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Go(func() { auth.RunCleanup(ctx, registry, cleanupConfig, slog.New(slog.NewJSONHandler(&logs, nil))) })
		time.Sleep(6 * time.Second)
		synctest.Wait()
		cancel()
		wg.Wait()
		for _, entry := range decodeLogs(t, &logs) {
			require.NotEqual(t, "ERROR", entry["level"], "shutdown is not a cleanup failure")
		}
	})
}
