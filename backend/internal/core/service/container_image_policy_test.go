package service_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestContainerCreateRejectsRegistryHostsBeforeAdmission(t *testing.T) {
	for _, image := range []string{"evil.example/app", "localhost/app", "127.0.0.1/app", "registry:5000/app"} {
		t.Run(image, func(t *testing.T) {
			docker := coremocks.NewContainerDockerAPI(t)
			svc := newTestContainerService(t, service.ContainerServiceDeps{
				Docker:       docker,
				HostDiskPath: "",
				Logger:       slog.Default(),
			})
			id, err := svc.Create(accessscope.WithUserScope(context.Background(), uuid.New(), "", ""), model.ContainerCreateParams{Name: "web", ImageTag: image})
			require.ErrorIs(t, err, apperrors.ErrBadRequest)
			require.Equal(t, uuid.Nil, id)
		})
	}
}

// Resolve through the queued creation use case, including its Docker pull boundary.
func TestQueuedContainerImageSourcePolicy(t *testing.T) {
	owner := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	for _, tc := range []struct {
		name, image, pull             string
		registryTag                   string
		custom, rejected, lookupError bool
	}{
		{name: "implicit Hub", image: "nginx", pull: "nginx:latest"},
		{name: "namespaced Hub", image: "acme/app:v1", pull: "acme/app:v1"},
		{name: "explicit Hub", image: "docker.io/library/nginx", pull: "docker.io/library/nginx:latest"},
		// An owned image is pulled by its record's Registry tag: a version tag
		// for an image built before per-record tags, its image ID otherwise.
		{name: "owned custom", image: "nginx", registryTag: "latest", pull: "registry.internal:5000/11111111-1111-1111-1111-111111111111_nginx:latest", custom: true},
		{name: "explicit owned custom", image: "docker.io/acme/app:v1", registryTag: "22222222-2222-2222-2222-222222222222", pull: "registry.internal:5000/11111111-1111-1111-1111-111111111111_docker.io/acme/app:22222222-2222-2222-2222-222222222222", custom: true},
		{name: "lookup failure cannot select public fallback", image: "nginx", lookupError: true},
		{name: "queued foreign host", image: "evil.example/app", rejected: true},
		{name: "queued localhost", image: "localhost/app", rejected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workerRepo := newContainerRepository(t)
			repo := workerRepo
			images := newContainerImageRepository(t)
			docker := coremocks.NewContainerDockerAPI(t)
			config := containerCreateConfig()
			config.RegistryPublicURL = "registry.internal:5000"
			cfg := coretest.StaticConfig{Config: config}
			svc := newTestContainerService(t, service.ContainerServiceDeps{
				Repository:   repo,
				Images:       images,
				Docker:       docker,
				Config:       cfg,
				HostDiskPath: "",
				Logger:       slog.Default(),
			})
			id, opID := uuid.New(), uuid.New()
			op := model.ResourceOperation{ID: opID, ResourceID: id, ResourceType: model.ResourceTypeContainer, Operation: model.OperationCreate, Status: model.OperationStatusRunning}
			workerRepo.EXPECT().ClaimPendingOperation(mock.Anything, opID, mock.Anything).Return(op, true, nil)
			workerRepo.EXPECT().UpdateStatus(mock.Anything, id, model.ContainerStatusCreating).Return(nil)
			workerRepo.EXPECT().GetByID(mock.Anything, id).Return(model.Container{ID: id, OwnerID: owner, Name: "web", ImageTag: tc.image}, nil)
			workerRepo.EXPECT().GetMountsByContainerID(mock.Anything, id).Return(nil, nil).Maybe()
			workerRepo.EXPECT().GetOperationByID(mock.Anything, opID).Return(op, nil).Maybe()
			docker.EXPECT().EnsureUserNetwork(mock.Anything, mock.Anything).Return("network", nil).Maybe()
			if !tc.rejected {
				normalized := tc.image
				switch normalized {
				case "nginx", "docker.io/library/nginx":
					normalized += ":latest"
				}
				var owned model.Image
				if tc.custom {
					owned = model.Image{OwnerID: owner, Tag: normalized, RegistryTag: tc.registryTag, Status: model.ImageStatusAvailable}
				}
				var lookupErr error
				if tc.lookupError {
					lookupErr = apperrors.ErrUnavailable
				}
				images.EXPECT().FindByOwnerAndTag(mock.Anything, owner, normalized).Return(owned, tc.custom, lookupErr).Once()
			}
			if tc.rejected || tc.lookupError {
				workerRepo.EXPECT().MarkStatusError(mock.Anything, id, model.ContainerStatusError, mock.Anything).Return(nil)
				workerRepo.EXPECT().CompleteOperation(mock.Anything, opID, model.OperationStatusFailed, mock.Anything).Return(nil)
				repo.EXPECT().CountByOwnerID(mock.Anything, owner).Return(1, nil).Maybe()
			} else {
				if !tc.custom {
					docker.EXPECT().ImageExists(mock.Anything, tc.pull).Return(false, nil)
				}
				docker.EXPECT().PullImage(mock.Anything, tc.pull).Return(nil).Once()
				docker.EXPECT().CreateContainer(mock.Anything, mock.MatchedBy(func(spec model.ContainerRuntimeSpec) bool { return spec.ImageName == tc.pull })).Return("docker-id", nil)
				workerRepo.EXPECT().UpdateDockerIDAndStatus(mock.Anything, id, "docker-id", model.ContainerStatusCreated).Return(nil)
				workerRepo.EXPECT().CompleteOperation(mock.Anything, opID, model.OperationStatusDone, nil).Return(nil)
			}
			err := handleQueuedCreate(svc, opID, id)
			if tc.rejected || tc.lookupError {
				if tc.rejected {
					require.ErrorIs(t, err, apperrors.ErrBadRequest)
				} else {
					require.ErrorIs(t, err, apperrors.ErrUnavailable)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestQueuedContainerCreateRemovesPulledPublicImageAfterFailure(t *testing.T) {
	owner := uuid.New()
	workerRepo := newContainerRepository(t)
	docker := coremocks.NewContainerDockerAPI(t)
	cfg := coretest.StaticConfig{Config: containerCreateConfig()}
	images := newContainerImageRepository(t)
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository: workerRepo,
		Images:     images,
		Docker:     docker,
		Config:     cfg,
		Logger:     slog.Default(),
	})
	id, opID := uuid.New(), uuid.New()
	op := model.ResourceOperation{ID: opID, ResourceID: id, ResourceType: model.ResourceTypeContainer, Operation: model.OperationCreate, Status: model.OperationStatusRunning}
	createErr := apperrors.New(apperrors.ErrBadRequest, "invalid container spec")
	workerRepo.EXPECT().ClaimPendingOperation(mock.Anything, opID, mock.Anything).Return(op, true, nil)
	workerRepo.EXPECT().UpdateStatus(mock.Anything, id, model.ContainerStatusCreating).Return(nil)
	workerRepo.EXPECT().GetByID(mock.Anything, id).Return(model.Container{ID: id, OwnerID: owner, Name: "web", ImageTag: "nginx"}, nil)
	workerRepo.EXPECT().GetMountsByContainerID(mock.Anything, id).Return(nil, nil).Maybe()
	workerRepo.EXPECT().GetOperationByID(mock.Anything, opID).Return(op, nil).Maybe()
	workerRepo.EXPECT().MarkStatusError(mock.Anything, id, model.ContainerStatusError, mock.Anything).Return(nil)
	workerRepo.EXPECT().CompleteOperation(mock.Anything, opID, model.OperationStatusFailed, mock.Anything).Return(nil)
	workerRepo.EXPECT().CountByOwnerID(mock.Anything, owner).Return(1, nil).Maybe()
	images.EXPECT().FindByOwnerAndTag(mock.Anything, mock.Anything, mock.Anything).Return(model.Image{}, false, nil).Maybe()
	docker.EXPECT().EnsureUserNetwork(mock.Anything, mock.Anything).Return("network", nil)
	docker.EXPECT().ImageExists(mock.Anything, "nginx:latest").Return(false, nil)
	docker.EXPECT().PullImage(mock.Anything, "nginx:latest").Return(nil).Once()
	docker.EXPECT().CreateContainer(mock.Anything, mock.Anything).Return("", createErr)
	docker.EXPECT().RemoveImage(mock.Anything, "nginx:latest", false).Return(nil).Once()

	err := handleQueuedCreate(svc, opID, id)
	require.ErrorIs(t, err, apperrors.ErrBadRequest)
}

// handleQueuedCreate delivers the create message for operationID to the
// container lifecycle queue's handler.
func handleQueuedCreate(svc *service.ContainerService, operationID, containerID uuid.UUID) error {
	cfg := containerCreateConfig()
	worker := service.NewContainerCreateWorker(svc, coretest.StaticConfig{Config: cfg}, slog.New(slog.DiscardHandler))
	return worker.HandleMessage(context.Background(), queue.ContainerLifecycleMessage{
		OperationID: operationID.String(),
		ContainerID: containerID.String(),
		Operation:   model.OperationCreate,
	})
}

// While a tag is rebuilt, container create resolves it to the image that is
// still available under it; a tag with only a build in progress is a
// conflict, and any other record keeps the unavailable error. The request-time
// check and the queued create share this resolution.
func TestContainerCreateResolvesATagUnderRebuild(t *testing.T) {
	owner := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	availableID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	for _, tc := range []struct {
		name        string
		record      model.Image
		found       bool
		wantMessage string
		pull        string
	}{
		{
			name:   "available row beside a newer build",
			record: model.Image{ID: availableID, OwnerID: owner, Tag: "app:latest", RegistryTag: availableID.String(), Status: model.ImageStatusAvailable},
			found:  true,
			pull:   "registry.internal:5000/11111111-1111-1111-1111-111111111111_app:" + availableID.String(),
		},
		{
			name:        "only a build in progress",
			record:      model.Image{ID: uuid.New(), OwnerID: owner, Tag: "app:latest", Status: model.ImageStatusBuilding},
			found:       true,
			wantMessage: "image is still being built",
		},
		{
			name:        "missing record",
			record:      model.Image{ID: uuid.New(), OwnerID: owner, Tag: "app:latest", Status: model.ImageStatusMissing},
			found:       true,
			wantMessage: "image is missing in Docker and can only be deleted",
		},
		{
			name:        "error record",
			record:      model.Image{ID: uuid.New(), OwnerID: owner, Tag: "app:latest", Status: model.ImageStatusError},
			found:       true,
			wantMessage: "image is missing in Docker and can only be deleted",
		},
	} {
		t.Run(tc.name+"/request", func(t *testing.T) {
			images := newContainerImageRepository(t)
			images.EXPECT().FindByOwnerAndTag(mock.Anything, owner, "app:latest").Return(tc.record, tc.found, nil).Once()
			w := roomyAdmissionWorld()
			config := w.config()
			config.RegistryPublicURL = "registry.internal:5000"
			cfg := coretest.StaticConfig{Config: config}
			svc := newTestContainerService(t, service.ContainerServiceDeps{
				Repository:  newContainerStore(),
				Images:      images,
				Metrics:     admissionHostMetrics(t),
				DiskMetrics: admissionDiskMetrics(t, w),
				Config:      cfg,
				Quotas:      w.quotas(t, owner, cfg),
				Logger:      slog.New(slog.DiscardHandler),
			})
			id, err := svc.Create(accessscope.WithUserScope(context.Background(), owner, "", ""), model.ContainerCreateParams{Name: "web", ImageTag: "app"})
			if tc.wantMessage == "" {
				require.NoError(t, err)
				require.NotEqual(t, uuid.Nil, id)
				return
			}
			require.ErrorIs(t, err, apperrors.ErrConflict)
			require.Equal(t, tc.wantMessage, apperrors.SafeMessage(err))
			require.Equal(t, uuid.Nil, id)
		})
		t.Run(tc.name+"/queued", func(t *testing.T) {
			workerRepo := newContainerRepository(t)
			images := newContainerImageRepository(t)
			images.EXPECT().FindByOwnerAndTag(mock.Anything, owner, "app:latest").Return(tc.record, tc.found, nil).Once()
			docker := coremocks.NewContainerDockerAPI(t)
			config := containerCreateConfig()
			config.RegistryPublicURL = "registry.internal:5000"
			svc := newTestContainerService(t, service.ContainerServiceDeps{
				Repository: workerRepo,
				Images:     images,
				Docker:     docker,
				Config:     coretest.StaticConfig{Config: config},
				Logger:     slog.Default(),
			})
			id, opID := uuid.New(), uuid.New()
			op := model.ResourceOperation{ID: opID, ResourceID: id, ResourceType: model.ResourceTypeContainer, Operation: model.OperationCreate, Status: model.OperationStatusRunning}
			workerRepo.EXPECT().ClaimPendingOperation(mock.Anything, opID, mock.Anything).Return(op, true, nil)
			workerRepo.EXPECT().UpdateStatus(mock.Anything, id, model.ContainerStatusCreating).Return(nil)
			workerRepo.EXPECT().GetByID(mock.Anything, id).Return(model.Container{ID: id, OwnerID: owner, Name: "web", ImageTag: "app:latest"}, nil)
			workerRepo.EXPECT().GetMountsByContainerID(mock.Anything, id).Return(nil, nil).Maybe()
			workerRepo.EXPECT().GetOperationByID(mock.Anything, opID).Return(op, nil).Maybe()
			workerRepo.EXPECT().CountByOwnerID(mock.Anything, owner).Return(1, nil).Maybe()
			docker.EXPECT().EnsureUserNetwork(mock.Anything, mock.Anything).Return("network", nil)
			if tc.wantMessage == "" {
				docker.EXPECT().PullImage(mock.Anything, tc.pull).Return(nil).Once()
				docker.EXPECT().CreateContainer(mock.Anything, mock.MatchedBy(func(spec model.ContainerRuntimeSpec) bool { return spec.ImageName == tc.pull })).Return("docker-id", nil)
				workerRepo.EXPECT().UpdateDockerIDAndStatus(mock.Anything, id, "docker-id", model.ContainerStatusCreated).Return(nil)
				workerRepo.EXPECT().CompleteOperation(mock.Anything, opID, model.OperationStatusDone, nil).Return(nil)
				require.NoError(t, handleQueuedCreate(svc, opID, id))
				return
			}
			docker.EXPECT().RemoveNetwork(mock.Anything, mock.Anything).Return(nil).Maybe()
			workerRepo.EXPECT().MarkStatusError(mock.Anything, id, model.ContainerStatusError, mock.Anything).Return(nil)
			workerRepo.EXPECT().CompleteOperation(mock.Anything, opID, model.OperationStatusFailed, mock.Anything).Return(nil)
			err := handleQueuedCreate(svc, opID, id)
			require.ErrorIs(t, err, apperrors.ErrConflict)
			require.Equal(t, tc.wantMessage, apperrors.SafeMessage(err))
		})
	}
}

func TestContainerCreateRejectsTwoMountsAtOnePath(t *testing.T) {
	for _, tc := range []struct{ name, first, second string }{
		{name: "same path", first: "/data", second: "/data"},
		{name: "same path with a trailing slash", first: "/data", second: "/data/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// No store, volume or Docker call is expected: the request is refused before any of them.
			svc := newTestContainerService(t, service.ContainerServiceDeps{HostDiskPath: "", Logger: slog.Default()})

			id, err := svc.Create(accessscope.WithUserScope(context.Background(), uuid.New(), "", ""), model.ContainerCreateParams{
				Name: "web", ImageTag: "nginx:latest",
				VolumeMounts: []model.VolumeMountParams{
					{VolumeID: uuid.New(), MountPath: tc.first},
					{VolumeID: uuid.New(), MountPath: tc.second},
				},
			})

			require.ErrorIs(t, err, apperrors.ErrBadRequest)
			require.ErrorContains(t, err, "mount path is used more than once")
			require.Equal(t, uuid.Nil, id)
		})
	}
}
