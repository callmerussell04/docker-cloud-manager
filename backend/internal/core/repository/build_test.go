package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func queueTaggedBuild(t *testing.T, db *pgxpool.Pool, owner uuid.UUID, tag string) (buildID, imageID uuid.UUID) {
	t.Helper()
	buildID, imageID = uuid.New(), uuid.New()
	require.NoError(t, repository.NewBuildRepository(db, db).CreateQueuedBuild(context.Background(),
		model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: owner, Tag: tag},
		model.Build{ID: buildID, ImageID: imageID, OwnerID: owner, Status: model.BuildStatusPending},
		model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: "x", RoutingKey: "k", Payload: []byte(`{}`)}))
	return buildID, imageID
}

func TestBuildKeepsItsTagAndImageWhileTheImageExists(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	owner := uuid.New()
	buildID, imageID := queueTaggedBuild(t, db, owner, "app:v1")

	build, err := repository.NewBuildRepository(db, db).GetByID(ctx, buildID)

	require.NoError(t, err)
	require.Equal(t, "app:v1", build.Tag)
	require.Equal(t, imageID, build.ImageID)
}

func TestFailedBuildKeepsItsTagWithoutAnImage(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	builds := repository.NewBuildRepository(db, db)
	owner := uuid.New()
	buildID, imageID := queueTaggedBuild(t, db, owner, "app:v1")

	require.NoError(t, repository.NewImageRepository(db).MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, model.BuildStatusFailed))

	build, err := builds.GetByID(ctx, buildID)
	require.NoError(t, err)
	require.Equal(t, model.BuildStatusFailed, build.Status)
	require.Equal(t, "app:v1", build.Tag)
	require.Equal(t, uuid.Nil, build.ImageID, "the image record is gone, so the build names none")
	listed, total, err := builds.List(ctx, model.ListOptions{OwnerID: &owner})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, "app:v1", listed[0].Tag)
	require.Equal(t, uuid.Nil, listed[0].ImageID)
}

func TestRebuildLeavesTheOlderBuildWithItsTag(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	builds := repository.NewBuildRepository(db, db)
	images := repository.NewImageRepository(db)
	owner := uuid.New()
	firstBuild, firstImage := queueTaggedBuild(t, db, owner, "app:v1")
	_, err := images.UpdateBuildAndImageSizeTx(ctx, firstBuild, firstImage, model.BuildStatusSuccess, 10, nil)
	require.NoError(t, err)
	secondBuild, secondImage := queueTaggedBuild(t, db, owner, "app:v1")

	_, err = images.UpdateBuildAndImageSizeTx(ctx, secondBuild, secondImage, model.BuildStatusSuccess, 12, nil)
	require.NoError(t, err)

	older, err := builds.GetByID(ctx, firstBuild)
	require.NoError(t, err)
	require.Equal(t, "app:v1", older.Tag)
	require.Equal(t, uuid.Nil, older.ImageID, "its image record was replaced")
	newer, err := builds.GetByID(ctx, secondBuild)
	require.NoError(t, err)
	require.Equal(t, secondImage, newer.ImageID)
}

const buildOutboxSecretPayload = `{"build_args": {"NPM_TOKEN": "s3cret"}, "build_id": "b"}`

func queueBuildWithSecretPayload(t *testing.T, db *pgxpool.Pool) (*repository.BuildRepository, uuid.UUID) {
	t.Helper()
	repo := repository.NewBuildRepository(db, db)
	owner, imageID, buildID, outboxID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, repo.CreateQueuedBuild(context.Background(),
		model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: owner, Tag: "app:latest"},
		model.Build{ID: buildID, ImageID: imageID, OwnerID: owner, Status: model.BuildStatusPending},
		model.BuildQueueOutbox{ID: outboxID, BuildID: buildID, Exchange: "x", RoutingKey: "k", Payload: []byte(buildOutboxSecretPayload)}))
	return repo, outboxID
}

func storedBuildOutboxPayload(t *testing.T, db *pgxpool.Pool, outboxID uuid.UUID) string {
	t.Helper()
	var payload string
	require.NoError(t, db.QueryRow(context.Background(), `SELECT payload::text FROM build_queue_outbox WHERE id = $1`, outboxID).Scan(&payload))
	return payload
}

func TestBuildOutboxPayloadIsEmptiedOncePublished(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo, outboxID := queueBuildWithSecretPayload(t, db)
	leased := leaseOneBuildOutbox(t, db)
	require.JSONEq(t, buildOutboxSecretPayload, string(leased.Payload), "the relay publishes the full message")

	require.NoError(t, repo.MarkBuildOutboxPublished(ctx, outboxID))

	require.JSONEq(t, `{}`, storedBuildOutboxPayload(t, db, outboxID))
}

func TestBuildOutboxPayloadIsEmptiedOnceDiscarded(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo, outboxID := queueBuildWithSecretPayload(t, db)
	leaseOneBuildOutbox(t, db)

	require.NoError(t, repo.MarkBuildOutboxDiscarded(ctx, outboxID, errors.New("build is terminal")))

	require.JSONEq(t, `{}`, storedBuildOutboxPayload(t, db, outboxID))
}

func TestBuildOutboxFailedPublishKeepsThePayloadForTheRetry(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo, outboxID := queueBuildWithSecretPayload(t, db)
	leaseOneBuildOutbox(t, db)

	require.NoError(t, repo.MarkBuildOutboxPending(ctx, outboxID, errors.New("broker unavailable")))

	retried := leaseOneBuildOutbox(t, db)
	require.Equal(t, outboxID, retried.ID)
	require.JSONEq(t, buildOutboxSecretPayload, string(retried.Payload))
}

// A published row's payload is gone, so nothing may queue it again: the row
// would publish an empty message.
func TestBuildOutboxPublishedRowNeverReturnsToPending(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	repo, outboxID := queueBuildWithSecretPayload(t, db)
	leaseOneBuildOutbox(t, db)
	require.NoError(t, repo.MarkBuildOutboxPublished(ctx, outboxID))

	err := repo.MarkBuildOutboxPending(ctx, outboxID, errors.New("late failure"))

	require.ErrorIs(t, err, apperrors.ErrNotFound)
	entries, err := repo.LeasePendingBuildOutbox(ctx, 10, 10)
	require.NoError(t, err)
	require.Empty(t, entries)
}
