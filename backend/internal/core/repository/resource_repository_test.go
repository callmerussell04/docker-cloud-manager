package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/grpcerrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func TestImageRepositoryListAndBuildTransactions(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	buildRepo := repository.NewBuildRepository(db, db)
	imageRepo := repository.NewImageRepository(db)
	ownerID := uuid.New()
	imageID := uuid.New()
	buildID := uuid.New()
	payload, err := json.Marshal(buildqueue.ImageBuildMessage{BuildID: buildID.String(), ImageID: imageID.String(), OwnerID: ownerID.String()})
	require.NoError(t, err)
	require.NoError(t, buildRepo.CreateQueuedBuild(ctx,
		model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: ownerID, Tag: "demo:latest"},
		model.Build{ID: buildID, ImageID: imageID, OwnerID: ownerID, Status: model.BuildStatusRunning, StartedAt: time.Now()},
		model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: buildqueue.ExchangeName, RoutingKey: buildqueue.RoutingKey, Payload: payload},
	))

	replaced, err := imageRepo.UpdateBuildAndImageSizeTx(ctx, buildID, imageID, model.BuildStatusSuccess, 42, nil)
	require.NoError(t, err)
	require.Empty(t, replaced)
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
		model.Image{ID: secondImageID, RegistryTag: secondImageID.String(), OwnerID: ownerID, Tag: "demo-failed:latest"},
		model.Build{ID: secondBuildID, ImageID: secondImageID, OwnerID: ownerID, Status: model.BuildStatusRunning, StartedAt: time.Now()},
		model.BuildQueueOutbox{ID: uuid.New(), BuildID: secondBuildID, Exchange: buildqueue.ExchangeName, RoutingKey: buildqueue.RoutingKey, Payload: payload},
	))
	require.NoError(t, imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, secondBuildID, secondImageID, model.BuildStatusFailed))
	_, err = imageRepo.GetByID(ctx, secondImageID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

func TestContainerRepositoryQueuedCreateOutboxAndClaim(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	containerRepo := repository.NewContainerRepository(db, db)
	ownerID := uuid.New()
	containerID := uuid.New()
	operationID := uuid.New()
	payload, err := json.Marshal(queue.ContainerLifecycleMessage{
		OperationID: operationID.String(),
		ContainerID: containerID.String(),
		OwnerID:     ownerID.String(),
	})
	require.NoError(t, err)

	require.NoError(t, containerRepo.SaveWithMountsOperationAndOutbox(ctx, model.Container{
		ID:                    containerID,
		OwnerID:               ownerID,
		Name:                  "web",
		ImageTag:              "nginx:latest",
		Status:                model.ContainerStatusPending,
		DesiredStatus:         model.ContainerStatusCreated,
		EnvVars:               []byte(`{"PORT":"8080"}`),
		BaseMemoryReservation: 256,
		BaseCPUReservation:    500,
		DockerGeneration:      1,
	}, nil, model.ResourceOperation{
		ID:           operationID,
		ResourceType: model.ResourceTypeContainer,
		ResourceID:   containerID,
		OwnerID:      ownerID,
		Operation:    model.OperationCreate,
		Status:       model.OperationStatusPending,
	}, model.ContainerLifecycleOutbox{
		ID:          uuid.New(),
		OperationID: operationID,
		ContainerID: containerID,
		Exchange:    queue.ContainerExchangeName,
		RoutingKey:  queue.ContainerRoutingKey,
		Payload:     payload,
		Status:      model.ContainerOutboxStatusPending,
	}, false, false))

	items, err := containerRepo.LeasePendingContainerOutbox(ctx, 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, operationID, items[0].OperationID)

	op, claimed, err := containerRepo.ClaimPendingOperation(ctx, operationID, 3)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, model.OperationStatusRunning, op.Status)
	require.Equal(t, 1, op.Attempts)

	got, err := containerRepo.GetByID(ctx, containerID)
	require.NoError(t, err)
	require.EqualValues(t, 500, got.BaseCPUReservation)

	require.NoError(t, containerRepo.MarkContainerOutboxPublished(ctx, items[0].ID))
	require.NoError(t, containerRepo.CompleteOperation(ctx, operationID, model.OperationStatusDone, nil))
	active, err := containerRepo.HasActiveOperation(ctx, model.ResourceTypeContainer, containerID)
	require.NoError(t, err)
	require.False(t, active)
}

func TestVolumeRepositoryMountUsageAndProjectFilters(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	projectRepo := repository.NewProjectRepository(db)
	containerRepo := repository.NewContainerRepository(db, db)
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
		BaseCPUReservation:    250,
	}))
	cpuReserved, err := containerRepo.GetTotalSystemReservedCPU(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 250, cpuReserved)
	require.NoError(t, volumeRepo.Save(ctx, model.Volume{ID: volumeID, OwnerID: ownerID, ProjectID: &projectID, Name: "data", DockerName: "vol_data", Status: model.VolumeStatusAvailable, SizeBytes: 1 << 20}))
	require.NoError(t, volumeRepo.LinkProjectVolume(ctx, projectID, volumeID))
	require.NoError(t, volumeRepo.SaveMounts(ctx, []model.VolumeMount{{ContainerID: containerID, VolumeID: volumeID, MountPath: "/data", IsReadOnly: true}}))

	inUse, err := volumeRepo.IsVolumeInUse(ctx, volumeID)
	require.NoError(t, err)
	require.True(t, inUse)
	require.NoError(t, volumeRepo.UpdateUsage(ctx, volumeID, 2048))
	reserved, err := volumeRepo.GetUserReservedVolumeBytes(ctx, ownerID)
	require.NoError(t, err)
	require.EqualValues(t, 1<<20, reserved, "a volume within its size reserves its size")
	volumes, err := volumeRepo.GetByProjectID(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, volumes, 1)
	require.Equal(t, "data", volumes[0].Name)
	require.EqualValues(t, 2048, volumes[0].UsedBytes)

	require.NoError(t, projectRepo.Delete(ctx, projectID))
	volumes, err = volumeRepo.GetByProjectID(ctx, projectID)
	require.NoError(t, err)
	require.Empty(t, volumes)
	preservedVolume, err := volumeRepo.GetByID(ctx, volumeID)
	require.NoError(t, err)
	require.Nil(t, preservedVolume.ProjectID)
}

func TestContainerRepositoryOperationsAndConflict(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	containerRepo := repository.NewContainerRepository(db, db)
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

func TestResourceLifecycleRepositoryNamesOperationAndKeepsCause(t *testing.T) {
	db := dbtest.OpenCorePostgres(t)
	repo := repository.NewResourceLifecycleRepository(db)
	volumeID := uuid.New()
	op := model.ResourceOperation{ID: uuid.New(), ResourceType: model.ResourceTypeVolume, ResourceID: volumeID, OwnerID: uuid.New(), Operation: model.OperationDelete, Status: model.OperationStatusPending}
	outbox := model.ResourceLifecycleOutbox{ID: uuid.New(), OperationID: op.ID, Exchange: "test", RoutingKey: "test", Payload: []byte(`{}`)}

	err := repo.QueueVolumeDelete(context.Background(), volumeID, op, outbox)

	require.ErrorIs(t, err, apperrors.ErrNotFound)
	require.ErrorContains(t, err, "resource queue volume delete")
	require.Equal(t, apperrors.ErrNotFound.Error(), apperrors.SafeMessage(err))
	require.Equal(t, "not_found", apperrors.Code(err))
	require.Equal(t, codes.NotFound, grpcerrors.Code(err))
	require.Equal(t, http.StatusNotFound, httpresponse.Status(err))
}

func TestRepositoriesRoundTripNullsArraysAndConstraintErrors(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	volumes := repository.NewVolumeRepository(db)
	projects := repository.NewProjectRepository(db)
	ownerID := uuid.New()

	plain := model.Volume{ID: uuid.New(), OwnerID: ownerID, Name: "plain", DockerName: "vol_plain", SizeBytes: 1 << 20}
	require.NoError(t, volumes.Save(ctx, plain))
	got, err := volumes.GetByID(ctx, plain.ID)
	require.NoError(t, err)
	require.Nil(t, got.ProjectID)
	require.Nil(t, got.LastError)
	require.Nil(t, got.LastObservedAt)
	require.Equal(t, model.VolumeStatusAvailable, got.Status)
	require.Equal(t, time.UTC, got.CreatedAt.Location(), "persisted instants must not pick up the host zone")

	require.NoError(t, volumes.MarkStatusError(ctx, plain.ID, model.VolumeStatusError, errors.New("disk gone")))
	got, err = volumes.GetByID(ctx, plain.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastError)
	require.Equal(t, "disk gone", *got.LastError)
	require.NotNil(t, got.LastObservedAt)
	require.NoError(t, volumes.MarkStatusError(ctx, plain.ID, model.VolumeStatusAvailable, nil))
	got, err = volumes.GetByID(ctx, plain.ID)
	require.NoError(t, err)
	require.Nil(t, got.LastError)

	projectID := uuid.New()
	require.NoError(t, projects.Save(ctx, model.Project{ID: projectID, OwnerID: ownerID, Name: "linked", Status: model.ProjectStatusRunning}))
	linked := model.Volume{ID: uuid.New(), OwnerID: ownerID, ProjectID: &projectID, Name: "linked", DockerName: "vol_linked", SizeBytes: 1 << 20}
	require.NoError(t, volumes.Save(ctx, linked))
	got, err = volumes.GetByID(ctx, linked.ID)
	require.NoError(t, err)
	require.Equal(t, &projectID, got.ProjectID)

	duplicate := plain
	duplicate.ID = uuid.New()
	require.ErrorIs(t, volumes.Save(ctx, duplicate), apperrors.ErrAlreadyExists)
	_, err = volumes.GetByID(ctx, uuid.New())
	require.ErrorIs(t, err, apperrors.ErrNotFound)

	require.NoError(t, projects.Save(ctx, model.Project{ID: uuid.New(), OwnerID: ownerID, Name: "pending", Status: model.ProjectStatusPending}))
	listed, err := projects.ListByStatuses(ctx, []string{model.ProjectStatusRunning, model.ProjectStatusCanceled}, 10)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, projectID, listed[0].ID)

	// A pending build has NULL started_at, finished_at and project columns; every
	// read path maps them to zero values rather than dereferencing NULL.
	builds := repository.NewBuildRepository(db, db)
	imageID, buildID := uuid.New(), uuid.New()
	require.NoError(t, builds.CreateQueuedBuild(ctx,
		model.Image{ID: imageID, RegistryTag: imageID.String(), OwnerID: ownerID, Tag: "pending:latest"},
		model.Build{ID: buildID, ImageID: imageID, OwnerID: ownerID, Status: model.BuildStatusPending},
		model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: buildqueue.ExchangeName, RoutingKey: buildqueue.RoutingKey, Payload: []byte(`{}`)},
	))
	pending, err := builds.GetByID(ctx, buildID)
	require.NoError(t, err)
	require.True(t, pending.StartedAt.IsZero())
	require.Nil(t, pending.FinishedAt)
	require.Nil(t, pending.ProjectID)
	require.Empty(t, pending.ProjectServiceName)
	byImage, err := builds.GetByImageID(ctx, imageID)
	require.NoError(t, err)
	require.Len(t, byImage, 1)
	require.True(t, byImage[0].StartedAt.IsZero())
	listedBuilds, _, err := builds.List(ctx, model.ListOptions{OwnerID: &ownerID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, listedBuilds, 1)
	require.True(t, listedBuilds[0].StartedAt.IsZero())
}
