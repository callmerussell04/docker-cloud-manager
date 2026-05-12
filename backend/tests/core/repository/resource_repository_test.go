package repository_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestImageRepositoryListAndBuildTransactions(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	buildRepo := repository.NewBuildRepository(db)
	imageRepo := repository.NewImageRepository(db)
	ownerID := uuid.New()
	imageID := uuid.New()
	buildID := uuid.New()
	payload, err := json.Marshal(buildqueue.ImageBuildMessage{BuildID: buildID.String(), ImageID: imageID.String(), OwnerID: ownerID.String()})
	require.NoError(t, err)
	require.NoError(t, buildRepo.CreateQueuedBuild(ctx,
		model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo:latest"},
		model.Build{ID: buildID, ImageID: imageID, OwnerID: ownerID, Status: model.BuildStatusRunning, StartedAt: time.Now()},
		model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: buildqueue.ExchangeName, RoutingKey: buildqueue.RoutingKey, Payload: payload},
	))

	require.NoError(t, imageRepo.UpdateBuildAndImageSizeTx(ctx, buildID, imageID, model.BuildStatusSuccess, 42))
	img, err := imageRepo.GetByID(ctx, imageID)
	require.NoError(t, err)
	require.Equal(t, 42, img.SizeMB)
	require.Equal(t, model.ImageStatusAvailable, img.Status)
	build, err := buildRepo.GetByID(ctx, buildID)
	require.NoError(t, err)
	require.Equal(t, model.BuildStatusSuccess, build.Status)
	require.NotNil(t, build.FinishedAt)

	images, total, err := imageRepo.List(ctx, model.ListOptions{OwnerID: &ownerID, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, images, 1)

	secondImageID := uuid.New()
	secondBuildID := uuid.New()
	require.NoError(t, buildRepo.CreateQueuedBuild(ctx,
		model.Image{ID: secondImageID, OwnerID: ownerID, Tag: "demo-failed:latest"},
		model.Build{ID: secondBuildID, ImageID: secondImageID, OwnerID: ownerID, Status: model.BuildStatusRunning, StartedAt: time.Now()},
		model.BuildQueueOutbox{ID: uuid.New(), BuildID: secondBuildID, Exchange: buildqueue.ExchangeName, RoutingKey: buildqueue.RoutingKey, Payload: payload},
	))
	require.NoError(t, imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, secondBuildID, secondImageID, model.BuildStatusFailed))
	_, err = imageRepo.GetByID(ctx, secondImageID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

func TestVolumeRepositoryMountUsageAndProjectFilters(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	projectRepo := repository.NewProjectRepository(db)
	containerRepo := repository.NewContainerRepository(db)
	volumeRepo := repository.NewVolumeRepository(db)
	ownerID := uuid.New()
	projectID := uuid.New()
	containerID := uuid.New()
	volumeID := uuid.New()
	require.NoError(t, projectRepo.Save(ctx, model.Project{ID: projectID, OwnerID: ownerID, Name: "demo", Status: model.ProjectStatusRunning}))
	require.NoError(t, containerRepo.Save(ctx, model.Container{
		ID:                    containerID,
		OwnerID:               ownerID,
		ProjectID:             &projectID,
		DockerID:              "docker-" + containerID.String()[:8],
		Name:                  "web",
		ImageTag:              "nginx:latest",
		Status:                model.ContainerStatusRunning,
		DesiredStatus:         model.ContainerStatusRunning,
		BaseMemoryReservation: 128,
	}))
	require.NoError(t, volumeRepo.Save(ctx, model.Volume{ID: volumeID, OwnerID: ownerID, ProjectID: &projectID, DockerName: "vol_data", Status: model.VolumeStatusAvailable}))
	require.NoError(t, volumeRepo.SaveMounts(ctx, []model.VolumeMount{{ContainerID: containerID, VolumeID: volumeID, MountPath: "/data", IsReadOnly: true}}))

	inUse, err := volumeRepo.IsVolumeInUse(ctx, volumeID)
	require.NoError(t, err)
	require.True(t, inUse)
	require.NoError(t, volumeRepo.UpdateUsage(ctx, volumeID, 2048))
	used, err := volumeRepo.GetUserUsedVolumeBytes(ctx, ownerID)
	require.NoError(t, err)
	require.EqualValues(t, 2048, used)
	volumes, err := volumeRepo.GetByProjectID(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, volumes, 1)
	require.EqualValues(t, 2048, volumes[0].UsedBytes)
}

func TestContainerRepositoryOperationsAndConflict(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	containerRepo := repository.NewContainerRepository(db)
	ownerID := uuid.New()
	containerID := uuid.New()
	op := model.ResourceOperation{
		ID:           uuid.New(),
		ResourceType: model.ResourceTypeContainer,
		ResourceID:   containerID,
		OwnerID:      ownerID,
		Operation:    model.OperationCreate,
		Status:       model.OperationStatusPending,
	}
	require.NoError(t, containerRepo.SaveWithMountsAndOperation(ctx, model.Container{
		ID:                    containerID,
		OwnerID:               ownerID,
		DockerID:              "docker-" + containerID.String()[:8],
		Name:                  "web",
		ImageTag:              "nginx:latest",
		Status:                model.ContainerStatusCreating,
		DesiredStatus:         model.ContainerStatusRunning,
		BaseMemoryReservation: 256,
	}, nil, op, true, true))

	err := containerRepo.CreateOperation(ctx, model.ResourceOperation{
		ID:           uuid.New(),
		ResourceType: model.ResourceTypeContainer,
		ResourceID:   containerID,
		OwnerID:      ownerID,
		Operation:    model.OperationStart,
		Status:       model.OperationStatusPending,
	})
	require.ErrorIs(t, err, apperrors.ErrConflict)

	require.NoError(t, containerRepo.CompleteLatestOperation(ctx, model.ResourceTypeContainer, containerID, model.OperationStatusDone, nil))
	require.NoError(t, containerRepo.CreateOperationAndSetDesired(ctx, containerID, model.ContainerStatusExited, model.ResourceOperation{
		ID:           uuid.New(),
		ResourceType: model.ResourceTypeContainer,
		ResourceID:   containerID,
		OwnerID:      ownerID,
		Operation:    model.OperationStop,
		Status:       model.OperationStatusPending,
	}))
	container, err := containerRepo.GetByID(ctx, containerID)
	require.NoError(t, err)
	require.Equal(t, model.ContainerStatusExited, container.DesiredStatus)
}
