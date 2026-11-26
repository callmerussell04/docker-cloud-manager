package repository_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

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

func TestImageListNamesTheContainersUsingEachImage(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	images := repository.NewImageRepository(db)
	containers := repository.NewContainerRepository(db, db)
	owner := uuid.New()
	other := uuid.New()
	usedID := uuid.New()
	freeID := uuid.New()
	require.NoError(t, images.Save(ctx, model.Image{ID: usedID, OwnerID: owner, Tag: "shop:v1", RegistryTag: "a", Status: model.ImageStatusAvailable}))
	require.NoError(t, images.Save(ctx, model.Image{ID: freeID, OwnerID: owner, Tag: "shop:v2", RegistryTag: "b", Status: model.ImageStatusAvailable}))
	save := func(ownerID uuid.UUID, name string) uuid.UUID {
		id := uuid.New()
		require.NoError(t, containers.SaveWithMountsOperationAndOutbox(ctx, model.Container{
			ID:            id,
			OwnerID:       ownerID,
			DockerID:      "docker-" + id.String()[:8],
			Name:          name,
			ImageTag:      "shop:v1",
			Status:        model.ContainerStatusRunning,
			DesiredStatus: model.ContainerStatusRunning,
		}, nil, model.ResourceOperation{}, model.ContainerLifecycleOutbox{}, false, false))
		return id
	}
	web := save(owner, "web")
	api := save(owner, "api")
	save(other, "theirs")

	listed, _, err := images.List(ctx, model.ListOptions{OwnerID: &owner, Limit: 10})
	require.NoError(t, err)
	usedBy := map[string][]model.ContainerRef{}
	for _, img := range listed {
		usedBy[img.Tag] = img.UsedBy
	}
	require.Equal(t, []model.ContainerRef{{ContainerID: api, Name: "api"}, {ContainerID: web, Name: "web"}}, usedBy["shop:v1"],
		"an image names its owner's containers created from its tag, in name order, and not another user's")
	require.Empty(t, usedBy["shop:v2"], "an image no container uses names none")
}
