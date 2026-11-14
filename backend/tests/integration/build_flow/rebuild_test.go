package build_flow_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type rebuildEnv struct {
	repos    coretest.CoreRepositories
	ownerID  uuid.UUID
	builds   *service.BuildService
	registry *coremocks.ImageRegistryAPI
	docker   *coremocks.BuildImageCache
	cfg      coretest.StaticConfig
}

func newRebuildEnv(t *testing.T) *rebuildEnv {
	t.Helper()
	repos := coretest.OpenCoreRepositories(t)
	ownerID := uuid.New()
	repos.SetCustomQuota(t, ownerID, model.UserQuota{CPUMillicores: 4000, RAMMB: 4096, DiskMB: 100})
	cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
	cfg.Config.RegistryCleanupIntervalSeconds = 1
	hostMetrics := coremocks.NewHostMetricsProvider(t)
	hostMetrics.EXPECT().GetTotalMemory().Return(int64(1<<40), nil).Maybe()
	hostMetrics.EXPECT().GetLogicalCPUs().Return(int64(64), nil).Maybe()
	disk := coremocks.NewHostDiskMetricsProvider(t)
	disk.EXPECT().GetDiskUsage(mock.Anything).Return(model.HostDiskStats{FreeBytes: 1 << 40}, nil).Maybe()
	// The Registry mock answers only for manifests a test pushed, and accepts
	// only the manifest deletions a test expects.
	registry := coremocks.NewImageRegistryAPI(t)
	docker := coremocks.NewBuildImageCache(t)
	builds, err := service.NewBuildService(service.BuildServiceDeps{
		Repository:    repos.Builds,
		RegistryAuth:  repos.RegistryAuthorization(t),
		Images:        repos.Images,
		Registry:      registry,
		Docker:        docker,
		Quotas:        repos.UserQuotas(t, cfg),
		Logger:        coretest.DiscardLogger(),
		Containers:    repos.Containers,
		HostMetrics:   hostMetrics,
		VolumeRepo:    repos.Volumes,
		Config:        cfg,
		StagedObjects: repos.Staged,
		DiskMetrics:   disk,
		HostDiskPath:  "/",
	})
	require.NoError(t, err)
	return &rebuildEnv{repos: repos, ownerID: ownerID, builds: builds, registry: registry, docker: docker, cfg: cfg}
}

type claimedBuild struct {
	buildID     uuid.UUID
	imageID     uuid.UUID
	destination string // repository:tag
	digest      string
}

// claimAndPush queues a build of tag, claims it as Builder does and pushes an
// image of sizeMB to the claim's destination.
func (e *rebuildEnv) claimAndPush(t *testing.T, tag string, sizeMB int64) claimedBuild {
	t.Helper()
	ctx := coretest.UserContext(e.ownerID)
	buildID, imageID, err := e.builds.CreateBuildJob(ctx, tag, "archives/"+uuid.NewString(), "logs/"+uuid.NewString(), "", "Dockerfile", nil, "")
	require.NoError(t, err)
	claimed, started, err := e.builds.StartBuildRecord(context.Background(), buildID)
	require.NoError(t, err)
	require.True(t, started)
	// The destination names the Registry authority, then repository:tag.
	destination, ok := strings.CutPrefix(claimed.RegistryCredential.Destination, "registry.test/")
	require.True(t, ok, claimed.RegistryCredential.Destination)
	digest := "sha256:" + uuid.NewString()
	repository, tag := splitReference(destination)
	e.registry.EXPECT().GetImageSizeAndDigest(mock.Anything, repository, tag).Return(sizeMB<<20, digest, nil).Maybe()
	return claimedBuild{buildID: buildID, imageID: imageID, destination: destination, digest: digest}
}

func (e *rebuildEnv) succeed(t *testing.T, b claimedBuild) model.Image {
	t.Helper()
	require.NoError(t, e.builds.CompleteBuildRecord(context.Background(), b.buildID, b.imageID, model.BuildStatusSuccess, 0))
	img, err := e.repos.Images.GetByID(context.Background(), b.imageID)
	require.NoError(t, err)
	require.Equal(t, model.ImageStatusAvailable, img.Status)
	return img
}

func (e *rebuildEnv) reservedDiskMB(t *testing.T) int64 {
	t.Helper()
	used, err := e.repos.Images.GetUserUsedDiskSpace(context.Background(), e.ownerID)
	require.NoError(t, err)
	return used
}

// runRegistryCleanup ends every fence, then runs the production cleanup
// worker until no cleanup job is left.
func (e *rebuildEnv) runRegistryCleanup(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := e.repos.DB.Exec(ctx, `UPDATE registry_build_credentials SET last_issued_at=last_issued_at-interval '1 hour'`)
	require.NoError(t, err)
	_, err = e.repos.DB.Exec(ctx, `UPDATE registry_cleanup_jobs SET not_before=not_before-interval '1 hour'`)
	require.NoError(t, err)
	worker, err := service.NewRegistryCleanupWorker(service.RegistryCleanupWorkerDeps{
		Repository: e.repos.Grants, Registry: e.registry, Config: e.cfg, Logger: coretest.DiscardLogger(),
	})
	require.NoError(t, err)
	runCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() { worker.Run(runCtx) })
	defer func() {
		cancel()
		wg.Wait()
	}()
	require.Eventually(t, func() bool {
		return countTable(t, e.repos.DB, "registry_cleanup_jobs") == 0
	}, 15*time.Second, 50*time.Millisecond)
}

// expectManifestDeletion accepts the one deletion of b's manifest.
func (e *rebuildEnv) expectManifestDeletion(b claimedBuild) {
	repository, _ := splitReference(b.destination)
	e.registry.EXPECT().DeleteManifest(mock.Anything, repository, b.digest).Return(nil).Once()
}

func splitReference(destination string) (repository, tag string) {
	i := strings.LastIndex(destination, ":")
	return destination[:i], destination[i+1:]
}

func repositoryOf(destination string) string {
	repository, _ := splitReference(destination)
	return repository
}

// A rebuild of an available tag that fails after its push never touches the
// image available under the tag: the old row, its Registry manifest and the
// owner's reserved disk stay exactly as they were, and only the rebuild's own
// manifest is deleted.
func TestFailedRebuildLeavesTheAvailableImageUntouched(t *testing.T) {
	tests := []struct {
		name   string
		sizeMB int64
		fail   func(t *testing.T, e *rebuildEnv, b claimedBuild)
		status string
	}{
		{
			name:   "quota rejected",
			sizeMB: 150,
			fail: func(t *testing.T, e *rebuildEnv, b claimedBuild) {
				require.NoError(t, e.builds.CompleteBuildRecord(context.Background(), b.buildID, b.imageID, model.BuildStatusSuccess, 0))
			},
			status: model.BuildStatusFailedQuotaExceeded,
		},
		{
			name:   "canceled",
			sizeMB: 3,
			fail: func(t *testing.T, e *rebuildEnv, b claimedBuild) {
				require.NoError(t, e.builds.CancelBuildRecord(coretest.UserContext(e.ownerID), b.buildID))
				// Builder's redelivered completion after the cancel changes nothing.
				require.NoError(t, e.builds.CompleteBuildRecord(context.Background(), b.buildID, b.imageID, model.BuildStatusSuccess, 0))
			},
			status: model.BuildStatusCanceled,
		},
		{
			name:   "internal failure",
			sizeMB: 3,
			fail: func(t *testing.T, e *rebuildEnv, b claimedBuild) {
				require.NoError(t, e.builds.CompleteBuildRecord(context.Background(), b.buildID, b.imageID, model.BuildStatusFailedInternal, 0))
			},
			status: model.BuildStatusFailedInternal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			e := newRebuildEnv(t)
			first := e.claimAndPush(t, "app:latest", 10)
			available := e.succeed(t, first)
			require.Equal(t, first.imageID.String(), available.RegistryTag)
			require.NotNil(t, available.Digest)
			reservedBefore := e.reservedDiskMB(t)

			rebuild := e.claimAndPush(t, "app:latest", tc.sizeMB)
			require.Equal(t, repositoryOf(first.destination)+":"+rebuild.imageID.String(), rebuild.destination)
			require.NotEqual(t, first.destination, rebuild.destination)
			tc.fail(t, e, rebuild)

			build, err := e.repos.Builds.GetByID(ctx, rebuild.buildID)
			require.NoError(t, err)
			require.Equal(t, tc.status, build.Status)
			after, err := e.repos.Images.GetByID(ctx, first.imageID)
			require.NoError(t, err)
			require.Equal(t, available, after)
			require.Equal(t, reservedBefore, e.reservedDiskMB(t))

			// Only the rebuild's manifest is deleted; the available image's
			// manifest has no deletion expectation, so deleting it fails the test.
			e.expectManifestDeletion(rebuild)
			e.runRegistryCleanup(t)
		})
	}
}

// A successful rebuild replaces the available image: the replaced row is
// deleted, its Registry manifest is queued for cleanup in the same
// transaction, and its Docker cache entry is removed best effort.
func TestSuccessfulRebuildReplacesTheAvailableImage(t *testing.T) {
	ctx := context.Background()
	e := newRebuildEnv(t)
	first := e.claimAndPush(t, "app:latest", 10)
	e.succeed(t, first)

	rebuild := e.claimAndPush(t, "app:latest", 20)
	e.docker.EXPECT().RemoveImage(mock.Anything, "registry-public.test:5000/"+first.destination, false).Return(apperrors.ErrResourceInUse).Once()
	replacement := e.succeed(t, rebuild)
	require.Equal(t, rebuild.imageID.String(), replacement.RegistryTag)
	require.Equal(t, 20, replacement.SizeMB)
	require.NotNil(t, replacement.Digest)
	require.Equal(t, rebuild.digest, *replacement.Digest)
	_, err := e.repos.Images.GetByID(ctx, first.imageID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	require.Equal(t, int64(20), e.reservedDiskMB(t))

	jobs := cleanupJobs(t, e)
	require.Equal(t, []model.RegistryCleanup{{ID: first.imageID, Repository: repositoryOf(first.destination), Tag: first.imageID.String()}}, jobs)

	// Only the replaced manifest is deleted; the replacement stays pullable.
	e.expectManifestDeletion(first)
	e.runRegistryCleanup(t)
}

// cleanupJobs lists queued Registry cleanup jobs that do not belong to a build
// credential; a successful build's revocation queues none.
func cleanupJobs(t *testing.T, e *rebuildEnv) []model.RegistryCleanup {
	t.Helper()
	rows, err := e.repos.DB.Query(context.Background(), `SELECT id, repository, tag FROM registry_cleanup_jobs ORDER BY not_before`)
	require.NoError(t, err)
	defer rows.Close()
	var jobs []model.RegistryCleanup
	for rows.Next() {
		var job model.RegistryCleanup
		require.NoError(t, rows.Scan(&job.ID, &job.Repository, &job.Tag))
		jobs = append(jobs, job)
	}
	require.NoError(t, rows.Err())
	return jobs
}
