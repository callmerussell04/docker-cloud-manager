package repository_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// queuedBuild is one build accepted into the queue, waiting in PostgreSQL
// until its outbox row is released.
type queuedBuild struct {
	buildID  uuid.UUID
	outboxID uuid.UUID
}

// queueBuilds queues n builds for owner, oldest first. Each is its own
// transaction, so their creation times are ordered.
func queueBuilds(t *testing.T, db *pgxpool.Pool, owner uuid.UUID, n int) []queuedBuild {
	t.Helper()
	repo := repository.NewBuildRepository(db, db)
	builds := make([]queuedBuild, 0, n)
	for range n {
		imageID, b := uuid.New(), queuedBuild{buildID: uuid.New(), outboxID: uuid.New()}
		require.NoError(t, repo.CreateQueuedBuild(context.Background(),
			model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: owner, Tag: "app:" + b.buildID.String()},
			model.Build{ID: b.buildID, ImageID: imageID, OwnerID: owner, Status: model.BuildStatusPending},
			model.BuildQueueOutbox{ID: b.outboxID, BuildID: b.buildID, Exchange: "x", RoutingKey: "k", Payload: []byte(`{}`)}))
		builds = append(builds, b)
	}
	return builds
}

func outboxIDs(builds ...queuedBuild) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(builds))
	for _, b := range builds {
		ids = append(ids, b.outboxID)
	}
	return ids
}

func leasedIDs(entries []model.BuildQueueOutbox) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	return ids
}

// release leases with the given batch size and per-owner cap and publishes
// every leased row, as the relay does after the broker confirms.
func release(t *testing.T, repo *repository.BuildRepository, batch, perOwner int) []uuid.UUID {
	t.Helper()
	ctx := context.Background()
	entries, err := repo.LeasePendingBuildOutbox(ctx, batch, perOwner)
	require.NoError(t, err)
	for _, e := range entries {
		require.NoError(t, repo.MarkBuildOutboxPublished(ctx, e.ID))
	}
	return leasedIDs(entries)
}

func TestBuildOutboxReleasesAnOwnersBuildsUpToTheCapOldestFirstWithinOneBatch(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewBuildRepository(db, db)
	queued := queueBuilds(t, db, uuid.New(), 5)

	entries, err := repo.LeasePendingBuildOutbox(context.Background(), 10, 3)

	require.NoError(t, err)
	require.Equal(t, outboxIDs(queued[:3]...), leasedIDs(entries), "an owner alone fills every build slot, oldest first, and no more within one batch")
	again, err := repo.LeasePendingBuildOutbox(context.Background(), 10, 3)
	require.NoError(t, err)
	require.Empty(t, again, "builds being published hold their owner's places")
}

func TestBuildOutboxReleasesAnotherOwnersBuildBehindOnlyTheReleasedOnes(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewBuildRepository(db, db)
	busy := queueBuilds(t, db, uuid.New(), 10)
	require.Equal(t, outboxIDs(busy[:2]...), release(t, repo, 10, 2))

	other := queueBuilds(t, db, uuid.New(), 1)

	require.Equal(t, outboxIDs(other...), release(t, repo, 10, 2), "the second owner's new build is released at once")
	require.Empty(t, release(t, repo, 10, 2), "the first owner is at its cap")

	require.NoError(t, repo.UpdateStatus(context.Background(), busy[0].buildID, model.BuildStatusRunning))
	require.Empty(t, release(t, repo, 10, 2), "a running build still holds its place")
	require.NoError(t, repo.UpdateStatus(context.Background(), busy[0].buildID, model.BuildStatusSuccess))
	require.Equal(t, outboxIDs(busy[2]), release(t, repo, 10, 2), "a finished build frees its owner's next place")
}

func TestBuildOutboxLeasesABuildCanceledBeforeReleaseSoTheRelayDiscardsIt(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewBuildRepository(db, db)
	queued := queueBuilds(t, db, uuid.New(), 2)
	require.Equal(t, outboxIDs(queued[0]), release(t, repo, 10, 1))
	require.NoError(t, repo.UpdateStatus(context.Background(), queued[1].buildID, model.BuildStatusCanceled))

	entries, err := repo.LeasePendingBuildOutbox(context.Background(), 10, 1)

	require.NoError(t, err)
	require.Equal(t, outboxIDs(queued[1]), leasedIDs(entries), "a canceled build takes no place, even at the cap")
}

func TestBuildOutboxUnclaimedReleasedBuildHoldsAPlaceOfItsOwnOwnerOnly(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewBuildRepository(db, db)
	first := queueBuilds(t, db, uuid.New(), 2)
	require.Equal(t, outboxIDs(first[0]), release(t, repo, 10, 1), "released and never claimed: it stays pending")
	other := queueBuilds(t, db, uuid.New(), 1)

	require.Equal(t, outboxIDs(other...), release(t, repo, 10, 1))
	require.Empty(t, release(t, repo, 10, 1))
}

func TestBuildOutboxStaleLeaseDoesNotHoldItsOwnPlace(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewBuildRepository(db, db)
	queued := queueBuilds(t, db, uuid.New(), 2)
	entries, err := repo.LeasePendingBuildOutbox(ctx, 10, 1)
	require.NoError(t, err)
	require.Equal(t, outboxIDs(queued[0]), leasedIDs(entries))
	_, err = db.Exec(ctx, `UPDATE build_queue_outbox SET updated_at = NOW() - INTERVAL '2 minutes' WHERE id = $1`, queued[0].outboxID)
	require.NoError(t, err)

	entries, err = repo.LeasePendingBuildOutbox(ctx, 10, 1)

	require.NoError(t, err)
	require.Equal(t, outboxIDs(queued[0]), leasedIDs(entries), "a lease the relay abandoned is retried in its turn")
}

func TestBuildOutboxInterleavesTwoOwnersPassByPass(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewBuildRepository(db, db)
	first := queueBuilds(t, db, uuid.New(), 3)
	second := queueBuilds(t, db, uuid.New(), 3)

	for pass := range 3 {
		require.Equal(t, outboxIDs(first[pass], second[pass]), release(t, repo, 10, 1), "pass %d releases one build of each owner, oldest first", pass)
		require.NoError(t, repo.UpdateStatus(ctx, first[pass].buildID, model.BuildStatusSuccess))
		require.NoError(t, repo.UpdateStatus(ctx, second[pass].buildID, model.BuildStatusSuccess))
	}
	require.Empty(t, release(t, repo, 10, 1))
}
