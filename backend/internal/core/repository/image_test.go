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
