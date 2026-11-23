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
	require.EqualValues(t, 42, img.SizeBytes)
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
	require.NoError(t, volumeRepo.Save(ctx, model.Volume{ID: volumeID, OwnerID: ownerID, ProjectID: &projectID, Name: "data", DockerName: "vol_data", Status: model.VolumeStatusAvailable, SizeBytes: 1 << 20}))
	require.NoError(t, volumeRepo.LinkProjectVolume(ctx, projectID, volumeID))
	require.NoError(t, containerRepo.SaveWithMountsOperationAndOutbox(ctx, model.Container{
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
	}, []model.VolumeMount{{ContainerID: containerID, VolumeID: volumeID, MountPath: "/data", IsReadOnly: true}}, model.ResourceOperation{}, model.ContainerLifecycleOutbox{}, false, false))
	cpuReserved, err := containerRepo.GetTotalSystemReservedCPU(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 250, cpuReserved)

	inUse, err := volumeRepo.IsVolumeInUse(ctx, volumeID)
	require.NoError(t, err)
	require.True(t, inUse)
	require.NoError(t, volumeRepo.UpdateUsage(ctx, volumeID, 2048))
	reserved, err := volumeRepo.GetUserReservedVolumeBytes(ctx, ownerID)
	require.NoError(t, err)
	require.EqualValues(t, 1<<20, reserved, "a volume within its size reserves its size")
	freeID := uuid.New()
	require.NoError(t, volumeRepo.Save(ctx, model.Volume{ID: freeID, OwnerID: ownerID, Name: "free", DockerName: "vol_free", Status: model.VolumeStatusAvailable, SizeBytes: 1 << 20}))
	listed, total, err := volumeRepo.List(ctx, model.ListOptions{OwnerID: &ownerID, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	usedBy := map[string][]model.VolumeUser{}
	for _, v := range listed {
		usedBy[v.Name] = v.UsedBy
	}
	require.Equal(t, []model.VolumeUser{{ContainerID: containerID, Name: "web"}}, usedBy["data"], "a listed volume names the containers mounting it")
	require.Empty(t, usedBy["free"], "a volume no container mounts names none")
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
	listedBuilds, _, err := builds.List(ctx, model.ListOptions{OwnerID: &ownerID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, listedBuilds, 1)
	require.True(t, listedBuilds[0].StartedAt.IsZero())
}

// Container create resolves a tag with one lookup that prefers the available
// row, so a queued or running rebuild of the tag never hides it.
func TestImageRepositoryFindByOwnerAndTagPrefersTheAvailableRow(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	imageRepo := repository.NewImageRepository(db)
	ownerID := uuid.New()
	save := func(tag, status string) model.Image {
		t.Helper()
		img := model.Image{ID: uuid.New(), OwnerID: ownerID, Tag: tag, Status: status}
		img.RegistryTag = img.ID.String()
		require.NoError(t, imageRepo.Save(ctx, img))
		return img
	}

	available := save("app:latest", model.ImageStatusAvailable)
	save("app:latest", model.ImageStatusBuilding)
	building := save("queued:v1", model.ImageStatusBuilding)
	save("broken:v1", model.ImageStatusMissing)
	broken := save("broken:v1", model.ImageStatusBuilding)
	missing := save("gone:v1", model.ImageStatusMissing)
	// Another owner's available row never answers for this owner.
	require.NoError(t, imageRepo.Save(ctx, model.Image{ID: uuid.New(), OwnerID: uuid.New(), RegistryTag: "x", Tag: "other:v1", Status: model.ImageStatusAvailable}))

	for _, tc := range []struct {
		tag    string
		wantID uuid.UUID
		found  bool
	}{
		{tag: "app:latest", wantID: available.ID, found: true},
		{tag: "queued:v1", wantID: building.ID, found: true},
		{tag: "broken:v1", wantID: broken.ID, found: true},
		{tag: "gone:v1", wantID: missing.ID, found: true},
		{tag: "other:v1"},
		{tag: "nginx:latest"},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			img, found, err := imageRepo.FindByOwnerAndTag(ctx, ownerID, tc.tag)
			require.NoError(t, err)
			require.Equal(t, tc.found, found)
			if found {
				require.Equal(t, tc.wantID, img.ID)
				require.Equal(t, tc.wantID.String(), img.RegistryTag)
			}
		})
	}
}

// The shared-manifest check lists the available images of one Registry
// repository and records a missing digest once, never replacing one.
func TestImageRepositoryListsAvailableImagesOfARepositoryAndRecordsDigests(t *testing.T) {
	ctx := context.Background()
	db := dbtest.OpenCorePostgres(t)
	imageRepo := repository.NewImageRepository(db)
	ownerID := uuid.New()
	save := func(owner uuid.UUID, tag, status string, digest *string) model.Image {
		t.Helper()
		img := model.Image{ID: uuid.New(), OwnerID: owner, Tag: tag, Status: status, Digest: digest}
		img.RegistryTag = img.ID.String()
		require.NoError(t, imageRepo.Save(ctx, img))
		return img
	}
	recorded := "sha256:recorded"
	v1 := save(ownerID, "app:v1", model.ImageStatusAvailable, &recorded)
	v2 := save(ownerID, "app:v2", model.ImageStatusAvailable, nil)
	save(ownerID, "app:v3", model.ImageStatusBuilding, nil)
	save(ownerID, "other:v1", model.ImageStatusAvailable, nil)
	save(uuid.New(), "app:v1", model.ImageStatusAvailable, nil)

	listed, err := imageRepo.ListAvailableInRepository(ctx, ownerID.String()+"_app")
	require.NoError(t, err)
	var ids []uuid.UUID
	for _, img := range listed {
		ids = append(ids, img.ID)
	}
	require.ElementsMatch(t, []uuid.UUID{v1.ID, v2.ID}, ids)

	require.NoError(t, imageRepo.RecordDigest(ctx, v2.ID, "sha256:resolved"))
	require.NoError(t, imageRepo.RecordDigest(ctx, v1.ID, "sha256:other"))
	require.NoError(t, imageRepo.RecordDigest(ctx, uuid.New(), "sha256:gone"))
	got, err := imageRepo.GetByID(ctx, v2.ID)
	require.NoError(t, err)
	require.Equal(t, "sha256:resolved", *got.Digest)
	got, err = imageRepo.GetByID(ctx, v1.ID)
	require.NoError(t, err)
	require.Equal(t, recorded, *got.Digest, "a recorded digest is never replaced")
}
