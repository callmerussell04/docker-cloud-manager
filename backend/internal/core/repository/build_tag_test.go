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

func TestImageDiskIsCountedInBytes(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	images := repository.NewImageRepository(db)
	owner := uuid.New()
	// Sizes that are not whole megabytes: nothing is rounded or dropped.
	require.NoError(t, images.Save(ctx, model.Image{ID: uuid.New(), OwnerID: owner, Tag: "small:latest", RegistryTag: "a", SizeBytes: 700_000, Status: model.ImageStatusAvailable}))
	require.NoError(t, images.Save(ctx, model.Image{ID: uuid.New(), OwnerID: owner, Tag: "big:latest", RegistryTag: "b", SizeBytes: 5<<20 + 1, Status: model.ImageStatusAvailable}))
	require.NoError(t, images.Save(ctx, model.Image{ID: uuid.New(), OwnerID: owner, Tag: "going:latest", RegistryTag: "c", SizeBytes: 1 << 30, Status: model.ImageStatusDeleting}))
	require.NoError(t, images.Save(ctx, model.Image{ID: uuid.New(), OwnerID: uuid.New(), Tag: "other:latest", RegistryTag: "d", SizeBytes: 9, Status: model.ImageStatusAvailable}))

	used, err := images.GetUserImageBytes(ctx, owner)
	require.NoError(t, err)
	require.EqualValues(t, 700_000+5<<20+1, used, "an image being deleted no longer counts")

	total, err := images.GetTotalImageBytes(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 700_000+5<<20+1+9, total)
}
