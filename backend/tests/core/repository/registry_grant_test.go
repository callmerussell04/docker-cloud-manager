package repository_test

import (
	"context"
	"crypto/sha256"
	"sync"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRegistryGrantClaimIsExclusiveAndRevocationSurvivesRestart(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)
	ctx := context.Background()
	builds := repository.NewBuildRepository(db)
	grants := repository.NewRegistryGrantRepository(db)
	owner, imageID, buildID := uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, builds.CreateQueuedBuild(ctx, model.Image{ID: imageID, OwnerID: owner, Tag: "destination:latest"}, model.Build{ID: buildID, ImageID: imageID, OwnerID: owner, Status: model.BuildStatusPending, StartedAt: time.Now()}, model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: "test", RoutingKey: "test", Payload: []byte(`{}`)}))
	hashes := [][32]byte{sha256.Sum256([]byte("worker-one-secret")), sha256.Sum256([]byte("worker-two-secret"))}
	type result struct {
		worker  int
		started bool
		err     error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := range hashes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, started, err := grants.ClaimBuild(ctx, buildID, hashes[i][:], time.Minute)
			results <- result{i, started, err}
		}(i)
	}
	wg.Wait()
	close(results)
	winner := -1
	for result := range results {
		require.NoError(t, result.err)
		if result.started {
			require.Equal(t, -1, winner)
			winner = result.worker
		}
	}
	require.NotEqual(t, -1, winner)
	principal, err := grants.AuthorizeCredential(ctx, hashes[winner][:], time.Now())
	require.NoError(t, err)
	require.Equal(t, owner, principal.OwnerID)
	require.Equal(t, owner.String()+"_destination", principal.Repository)
	_, err = grants.AuthorizeCredential(ctx, hashes[1-winner][:], time.Now())
	require.ErrorIs(t, err, apperrors.ErrForbidden)
	_, err = grants.AuthorizeCredential(ctx, hashes[winner][:], time.Now().Add(2*time.Minute))
	require.ErrorIs(t, err, apperrors.ErrForbidden)
	require.NoError(t, grants.RevokeBuild(ctx, buildID, true))
	restarted := repository.NewRegistryGrantRepository(db)
	_, err = restarted.AuthorizeCredential(ctx, hashes[winner][:], time.Now())
	require.ErrorIs(t, err, apperrors.ErrForbidden)
	until, err := restarted.FenceRepository(ctx, principal.Repository)
	require.NoError(t, err)
	require.True(t, until.After(time.Now().Add(179*time.Second)), "cleanup must wait for token skew and admitted writes")
	require.NoError(t, builds.Delete(ctx, buildID))
	require.NoError(t, repository.NewImageRepository(db).Delete(ctx, imageID))
	exists, err := restarted.RepositoryExists(ctx, principal.Repository)
	require.NoError(t, err)
	require.True(t, exists, "durable cleanup authority must outlive resource rows")
	jobs, err := restarted.PendingRegistryCleanup(ctx)
	require.NoError(t, err)
	require.Empty(t, jobs, "cleanup cannot run before the fence")
}

func TestRegistryCleanupCannotRaceReplacementClaim(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)
	ctx := context.Background()
	builds := repository.NewBuildRepository(db)
	grants := repository.NewRegistryGrantRepository(db)
	owner := uuid.New()
	queue := func(tag string) uuid.UUID {
		imageID, buildID := uuid.New(), uuid.New()
		require.NoError(t, builds.CreateQueuedBuild(ctx, model.Image{ID: imageID, OwnerID: owner, Tag: tag}, model.Build{ID: buildID, ImageID: imageID, OwnerID: owner, Status: model.BuildStatusPending, StartedAt: time.Now()}, model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: "test", RoutingKey: "test", Payload: []byte(`{}`)}))
		return buildID
	}
	oldID := queue("replacement:latest")
	oldHash := sha256.Sum256([]byte("old-secret"))
	old, started, err := grants.ClaimBuild(ctx, oldID, oldHash[:], time.Minute)
	require.NoError(t, err)
	require.True(t, started)
	require.NoError(t, grants.RevokeBuild(ctx, oldID, true))
	require.NoError(t, repository.NewImageRepository(db).MarkBuildFailedAndDeleteImageTx(ctx, oldID, old.ImageID, model.BuildStatusCanceled))
	_, err = db.ExecContext(ctx, `UPDATE registry_build_credentials SET last_issued_at=clock_timestamp()-interval '5 minutes' WHERE build_id=$1`, oldID)
	require.NoError(t, err)
	repo := owner.String() + "_replacement"
	job := model.RegistryCleanup{ID: oldID, Repository: repo, Tag: "latest"}
	replacementID := queue("replacement:v2")
	unlock, err := grants.LockRegistryRepository(ctx, repo)
	require.NoError(t, err)
	nextHash := sha256.Sum256([]byte("replacement-secret"))
	_, started, err = grants.ClaimBuild(ctx, replacementID, nextHash[:], time.Minute)
	require.ErrorIs(t, err, apperrors.ErrUnavailable)
	require.False(t, started)
	unlock()
	_, started, err = grants.ClaimBuild(ctx, replacementID, nextHash[:], time.Minute)
	require.NoError(t, err)
	require.True(t, started)
	state, err := grants.RegistryCleanupState(ctx, job)
	require.NoError(t, err)
	require.Equal(t, model.RegistryCleanupPending, state, "repository-wide grants must keep sibling-tag publishers ahead of cleanup")
}
