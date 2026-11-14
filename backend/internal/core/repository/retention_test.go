package repository_test

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const retentionTestAge = 8 * 24 * time.Hour

func retentionCutoff() time.Time { return time.Now().Add(-7 * 24 * time.Hour) }

func TestRetentionDeletesOnlyOldFinishedOperations(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	retention := repository.NewRetentionRepository(db)
	seed := func(status string, age time.Duration) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := db.Exec(ctx, `
			INSERT INTO resource_operations (id, resource_type, resource_id, owner_id, operation, status, updated_at)
			VALUES ($1, 'volume', $2, $3, 'delete', $4, NOW() - $5::interval)`,
			id, uuid.New(), uuid.New(), status, age.String())
		require.NoError(t, err)
		_, err = db.Exec(ctx, `
			INSERT INTO resource_lifecycle_queue_outbox (operation_id, resource_type, resource_id, exchange, routing_key, payload, status)
			VALUES ($1, 'volume', $2, 'x', 'k', '{}', 'published')`, id, uuid.New())
		require.NoError(t, err)
		return id
	}
	oldDone := seed(model.OperationStatusDone, retentionTestAge)
	oldFailed := seed(model.OperationStatusFailed, retentionTestAge)
	oldPending := seed(model.OperationStatusPending, retentionTestAge)
	oldRunning := seed(model.OperationStatusRunning, retentionTestAge)
	recentDone := seed(model.OperationStatusDone, time.Hour)

	deleted, err := retention.DeleteFinishedOperations(ctx, retentionCutoff(), 100)

	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
	require.ElementsMatch(t, []uuid.UUID{oldPending, oldRunning, recentDone}, retentionIDs(t, db, `SELECT id FROM resource_operations`))
	require.ElementsMatch(t, []uuid.UUID{oldPending, oldRunning, recentDone}, retentionIDs(t, db, `SELECT operation_id FROM resource_lifecycle_queue_outbox`),
		"an operation's outbox row goes with it")
	require.NotContains(t, retentionIDs(t, db, `SELECT id FROM resource_operations`), oldDone)
	require.NotContains(t, retentionIDs(t, db, `SELECT id FROM resource_operations`), oldFailed)
}

func TestRetentionDeletesAtMostTheBatchLimit(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	retention := repository.NewRetentionRepository(db)
	for range 3 {
		_, err := db.Exec(ctx, `
			INSERT INTO resource_operations (id, resource_type, resource_id, owner_id, operation, status, updated_at)
			VALUES ($1, 'volume', $2, $3, 'delete', 'done', NOW() - INTERVAL '8 days')`, uuid.New(), uuid.New(), uuid.New())
		require.NoError(t, err)
	}

	first, err := retention.DeleteFinishedOperations(ctx, retentionCutoff(), 2)
	require.NoError(t, err)
	second, err := retention.DeleteFinishedOperations(ctx, retentionCutoff(), 2)
	require.NoError(t, err)

	require.EqualValues(t, 2, first)
	require.EqualValues(t, 1, second)
}

func TestRetentionDeletesOnlyOldCredentialsOfFinishedBuilds(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	builds := repository.NewBuildRepository(db, db)
	grants := repository.NewRegistryGrantRepository(db, db)
	retention := repository.NewRetentionRepository(db)
	owner := uuid.New()
	// Each build is claimed, which issues its credential, then moved to the
	// status and credential age under test.
	seed := func(name, status string, age time.Duration, deleteBuild bool) uuid.UUID {
		t.Helper()
		imageID, buildID := uuid.New(), uuid.New()
		require.NoError(t, builds.CreateQueuedBuild(ctx,
			model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: owner, Tag: name + ":latest"},
			model.Build{ID: buildID, ImageID: imageID, OwnerID: owner, Status: model.BuildStatusPending},
			model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: "x", RoutingKey: "k", Payload: []byte(`{}`)}))
		hash := sha256.Sum256([]byte(name))
		_, started, err := grants.ClaimBuild(ctx, buildID, hash[:], time.Minute, 10)
		require.NoError(t, err)
		require.True(t, started)
		_, err = db.Exec(ctx, `UPDATE builds SET status = $2 WHERE id = $1`, buildID, status)
		require.NoError(t, err)
		_, err = db.Exec(ctx, `UPDATE registry_build_credentials SET last_issued_at = NOW() - $2::interval WHERE build_id = $1`, buildID, age.String())
		require.NoError(t, err)
		if deleteBuild {
			require.NoError(t, builds.Delete(ctx, buildID))
		}
		return buildID
	}
	seed("old-success", model.BuildStatusSuccess, retentionTestAge, false)
	seed("old-deleted", model.BuildStatusFailed, retentionTestAge, true)
	oldRunning := seed("old-running", model.BuildStatusRunning, retentionTestAge, false)
	recentSuccess := seed("recent-success", model.BuildStatusSuccess, time.Hour, false)

	deleted, err := retention.DeleteSpentBuildCredentials(ctx, retentionCutoff(), 100)

	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
	require.ElementsMatch(t, []uuid.UUID{oldRunning, recentSuccess}, retentionIDs(t, db, `SELECT build_id FROM registry_build_credentials`))
}

func TestRetentionDeletesOnlyOldReleasedStagedObjects(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	retention := repository.NewRetentionRepository(db)
	seed := func(status string, age time.Duration) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := db.Exec(ctx, `
			INSERT INTO staged_object_reservations (id, owner_id, object_key, kind, bytes_reserved, status, updated_at, released_at)
			VALUES ($1, $2, $3, 'build_archive', 1, $4::text, NOW() - $5::interval,
				CASE WHEN $4::text = 'released' THEN NOW() - $5::interval END)`,
			id, uuid.New(), "staged/"+id.String(), status, age.String())
		require.NoError(t, err)
		return id
	}
	seed(model.StagedObjectStatusReleased, retentionTestAge)
	oldActive := seed(model.StagedObjectStatusActive, retentionTestAge)
	recentReleased := seed(model.StagedObjectStatusReleased, time.Hour)

	deleted, err := retention.DeleteReleasedStagedObjects(ctx, retentionCutoff(), 100)

	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.ElementsMatch(t, []uuid.UUID{oldActive, recentReleased}, retentionIDs(t, db, `SELECT id FROM staged_object_reservations`))
}

func retentionIDs(t *testing.T, db *pgxpool.Pool, query string) []uuid.UUID {
	t.Helper()
	rows, err := db.Query(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	return ids
}
