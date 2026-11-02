package build_flow_test

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/tests/mocks/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/tests/testutil/coretest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBuildQueueResidenceDoesNotConsumeExecutionOrReservations(t *testing.T) {
	for _, claimPath := range []string{"build service", "registry grant"} {
		t.Run(claimPath, func(t *testing.T) {
			ctx := context.Background()
			repos := coretest.OpenCoreRepositories(t)
			owner := uuid.New()
			builds := newBuildService(t, repos, owner, map[string][]byte{}, false)
			result, err := builds.CreateBuildFromArchive(coretest.UserContext(owner), service.BuildArchiveInput{Tag: "timing:latest", ArchiveName: "source.zip", Archive: strings.NewReader("archive")})
			require.NoError(t, err)
			queued, err := repos.Builds.GetByID(ctx, result.BuildID)
			require.NoError(t, err)
			require.False(t, queued.CreatedAt.IsZero())
			require.True(t, queued.StartedAt.IsZero())
			// Model a prolonged broker/worker outage without waiting in wall-clock time.
			_, err = repos.DB.ExecContext(ctx, `UPDATE builds SET created_at=clock_timestamp()-interval '24 hours' WHERE id=$1`, result.BuildID)
			require.NoError(t, err)
			cutoff := time.Now().Add(-30 * time.Minute)
			stale, err := repos.Builds.GetStaleBuilds(ctx, cutoff)
			require.NoError(t, err)
			require.Empty(t, stale)
			reserved, err := repos.Staged.ActiveBytesByOwner(ctx, owner)
			require.NoError(t, err)
			require.Positive(t, reserved)
			count, err := repos.Builds.CountActive(ctx)
			require.NoError(t, err)
			require.Equal(t, 1, count)

			beforeClaim := time.Now()
			var started model.Build
			var claimed bool
			if claimPath == "registry grant" {
				hash := sha256.Sum256([]byte("build-secret"))
				started, claimed, err = repos.Grants.ClaimBuild(ctx, result.BuildID, hash[:], 10*time.Minute)
			} else {
				started, claimed, err = builds.StartBuildRecord(ctx, result.BuildID)
			}
			require.NoError(t, err)
			require.True(t, claimed)
			require.WithinDuration(t, time.Now(), started.StartedAt, 5*time.Second)
			require.False(t, started.StartedAt.Before(beforeClaim.Add(-time.Second)))
			require.True(t, started.CreatedAt.Before(cutoff))
			persisted, err := repos.Builds.GetByID(ctx, result.BuildID)
			require.NoError(t, err)
			require.True(t, persisted.StartedAt.Equal(started.StartedAt))
			stale, err = repos.Builds.GetStaleBuilds(ctx, cutoff)
			require.NoError(t, err)
			require.Empty(t, stale, "queue residence must not make fresh execution stale")
			again, claimed, err := builds.StartBuildRecord(ctx, result.BuildID)
			require.NoError(t, err)
			require.False(t, claimed)
			require.True(t, again.StartedAt.Equal(started.StartedAt))
			count, err = repos.Builds.CountActive(ctx)
			require.NoError(t, err)
			require.Equal(t, 1, count)
			stillReserved, err := repos.Staged.ActiveBytesByOwner(ctx, owner)
			require.NoError(t, err)
			require.Equal(t, reserved, stillReserved)

			// Genuine stale execution is recovered through the actual GC/service path.
			_, err = repos.DB.ExecContext(ctx, `UPDATE builds SET started_at=clock_timestamp()-interval '31 minutes' WHERE id=$1`, result.BuildID)
			require.NoError(t, err)
			stale, err = repos.Builds.GetStaleBuilds(ctx, cutoff)
			require.NoError(t, err)
			require.Len(t, stale, 1)
			require.Equal(t, result.BuildID, stale[0].ID)
			runBuildGC(t, builds, repos.Builds)
			completed, err := repos.Builds.GetByID(ctx, result.BuildID)
			require.NoError(t, err)
			require.Equal(t, model.BuildStatusFailedTimeout, completed.Status)
			require.NotNil(t, completed.FinishedAt)
			_, err = repos.Images.GetByID(ctx, completed.ImageID)
			require.ErrorIs(t, err, apperrors.ErrNotFound)
			count, err = repos.Builds.CountActive(ctx)
			require.NoError(t, err)
			require.Zero(t, count)
			reserved, err = repos.Staged.ActiveBytesByOwner(ctx, owner)
			require.NoError(t, err)
			require.Zero(t, reserved)
			require.NoError(t, builds.CompleteBuildRecord(ctx, completed.ID, completed.ImageID, model.BuildStatusSuccess, 0))
			completed, err = repos.Builds.GetByID(ctx, completed.ID)
			require.NoError(t, err)
			require.Equal(t, model.BuildStatusFailedTimeout, completed.Status)
		})
	}
}

func runBuildGC(t *testing.T, builds *service.BuildService, repo service.GCBuildRepo) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	docker := coremocks.NewGCDockerAPI(t)
	docker.EXPECT().PruneSystem(mock.Anything).Return(nil).Once()
	// The second config read happens after the build cleanup pass.
	cfg := coremocks.NewGCConfigProvider(t)
	snapshot := coretest.IntegrationConfig()
	snapshot.RegistryContainerName = ""
	snapshot.StaleBuildTimeoutMinutes = 30
	cfg.EXPECT().Get().Return(snapshot).Once()
	cfg.EXPECT().Get().Run(cancel).Return(snapshot).Once()
	service.NewGCWorker(docker, builds, repo, nil, nil, cfg, coretest.DiscardLogger()).Run(ctx)
}

func TestBuildTimeoutPreservesCanceledAndTerminalState(t *testing.T) {
	for _, state := range []string{"canceled before claim", "canceled after stale selection", "success after stale selection"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			repos := coretest.OpenCoreRepositories(t)
			owner := uuid.New()
			objects := map[string][]byte{}
			builds := newBuildService(t, repos, owner, objects, false)
			result, err := builds.CreateBuildFromArchive(coretest.UserContext(owner), service.BuildArchiveInput{Tag: "terminal:latest", ArchiveName: "source.zip", Archive: strings.NewReader("archive")})
			require.NoError(t, err)
			build, err := repos.Builds.GetByID(ctx, result.BuildID)
			require.NoError(t, err)
			if state == "canceled before claim" {
				require.NoError(t, builds.CancelBuildRecord(coretest.UserContext(owner), build.ID))
				canceled, claimed, err := builds.StartBuildRecord(ctx, build.ID)
				require.NoError(t, err)
				require.False(t, claimed)
				require.True(t, canceled.StartedAt.IsZero())
			} else {
				_, claimed, err := builds.StartBuildRecord(ctx, build.ID)
				require.NoError(t, err)
				require.True(t, claimed)
				_, err = repos.DB.ExecContext(ctx, `UPDATE builds SET started_at=clock_timestamp()-interval '31 minutes' WHERE id=$1`, build.ID)
				require.NoError(t, err)
				stale, err := repos.Builds.GetStaleBuilds(ctx, time.Now().Add(-30*time.Minute))
				require.NoError(t, err)
				require.Len(t, stale, 1)
				if state == "canceled after stale selection" {
					require.NoError(t, builds.CancelBuildRecord(coretest.UserContext(owner), build.ID))
				} else {
					require.NoError(t, builds.CompleteBuildRecord(ctx, build.ID, build.ImageID, model.BuildStatusSuccess, 0))
				}
			}
			terminal, err := repos.Builds.GetByID(ctx, build.ID)
			require.NoError(t, err)
			require.NotNil(t, terminal.FinishedAt)
			// Simulate a GC pass whose stale snapshot preceded cancellation/completion.
			require.NoError(t, builds.CompleteBuildRecord(ctx, build.ID, build.ImageID, model.BuildStatusFailedTimeout, 0))
			after, err := repos.Builds.GetByID(ctx, build.ID)
			require.NoError(t, err)
			require.Equal(t, terminal.Status, after.Status)
			require.True(t, terminal.FinishedAt.Equal(*after.FinishedAt))
			if state == "success after stale selection" {
				image, err := repos.Images.GetByID(ctx, build.ImageID)
				require.NoError(t, err)
				require.Equal(t, model.ImageStatusAvailable, image.Status)
			} else {
				require.Equal(t, model.BuildStatusCanceled, after.Status)
				require.NoError(t, builds.CompleteBuildRecord(ctx, build.ID, build.ImageID, model.BuildStatusSuccess, 0))
				after, err = repos.Builds.GetByID(ctx, build.ID)
				require.NoError(t, err)
				require.Equal(t, model.BuildStatusCanceled, after.Status)
			}
			runBuildGC(t, builds, repos.Builds)
			stale, err := repos.Builds.GetStaleBuilds(ctx, time.Now())
			require.NoError(t, err)
			require.Empty(t, stale)
			count, err := repos.Builds.CountActive(ctx)
			require.NoError(t, err)
			require.Zero(t, count)
			reserved, err := repos.Staged.ActiveBytesByOwner(ctx, owner)
			require.NoError(t, err)
			require.Zero(t, reserved)
			require.NotContains(t, objects, build.ArchiveObjectKey)
		})
	}
}
