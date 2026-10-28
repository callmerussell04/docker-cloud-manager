package service_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestContainerCreateRejectsRegistryHostsBeforeAdmission(t *testing.T) {
	for _, image := range []string{"evil.example/app", "localhost/app", "127.0.0.1/app", "registry:5000/app"} {
		t.Run(image, func(t *testing.T) {
			docker := coremocks.NewContainerDockerAPI(t)
			svc := newTestContainerService(t, ContainerServiceDeps{
				Docker:       docker,
				HostDiskPath: "",
				Logger:       slog.Default(),
			})
			id, err := svc.Create(accessscope.WithUserScope(context.Background(), uuid.New(), "", ""), model.ContainerCreateParams{Name: "web", ImageTag: image})
			require.ErrorIs(t, err, apperrors.ErrBadRequest)
			require.Equal(t, uuid.Nil, id)
			docker.AssertNotCalled(t, "PullImage", mock.Anything, mock.Anything)
		})
	}
}

// Resolve through the queued creation use case, including its Docker pull boundary.
func TestQueuedContainerImageSourcePolicy(t *testing.T) {
	owner := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	for _, tc := range []struct {
		name, image, pull             string
		custom, rejected, lookupError bool
	}{
		{name: "implicit Hub", image: "nginx", pull: "nginx:latest"},
		{name: "namespaced Hub", image: "acme/app:v1", pull: "acme/app:v1"},
		{name: "explicit Hub", image: "docker.io/library/nginx", pull: "docker.io/library/nginx:latest"},
		{name: "owned custom", image: "nginx", pull: "registry.internal:5000/11111111-1111-1111-1111-111111111111_nginx:latest", custom: true},
		{name: "explicit owned custom", image: "docker.io/acme/app:v1", pull: "registry.internal:5000/11111111-1111-1111-1111-111111111111_docker.io/acme/app:v1", custom: true},
		{name: "lookup failure cannot select public fallback", image: "nginx", lookupError: true},
		{name: "queued foreign host", image: "evil.example/app", rejected: true},
		{name: "queued localhost", image: "localhost/app", rejected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workerRepo := newContainerRepository(t)
			repo := workerRepo
			images := newContainerImageRepository(t)
			docker := coremocks.NewContainerDockerAPI(t)
			cfg := coremocks.NewConfigManager(t)
			config := containerCreateConfig()
			config.RegistryPublicURL = "registry.internal:5000"
			cfg.EXPECT().Get().Return(config).Maybe()
			svc := newTestContainerService(t, ContainerServiceDeps{
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
				var owned []model.Image
				if tc.custom {
					ownedTag := tc.image
					if ownedTag == "nginx" {
						ownedTag = "nginx:latest"
					}
					owned = []model.Image{{OwnerID: owner, Tag: ownedTag, Status: model.ImageStatusAvailable}}
				}
				var lookupErr error
				if tc.lookupError {
					lookupErr = apperrors.ErrUnavailable
				}
				images.EXPECT().List(mock.Anything, mock.MatchedBy(func(opts model.ListOptions) bool { return opts.OwnerID != nil && *opts.OwnerID == owner })).Return(owned, len(owned), lookupErr).Once()
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
			err := svc.ExecuteQueuedCreate(context.Background(), opID, id)
			if tc.rejected || tc.lookupError {
				if tc.rejected {
					require.ErrorIs(t, err, apperrors.ErrBadRequest)
				} else {
					require.ErrorIs(t, err, apperrors.ErrUnavailable)
				}
				docker.AssertNotCalled(t, "PullImage", mock.Anything, mock.Anything)
				docker.AssertNotCalled(t, "CreateContainer", mock.Anything, mock.Anything)
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
	cfg := coremocks.NewConfigManager(t)
	cfg.EXPECT().Get().Return(containerCreateConfig()).Maybe()
	images := newContainerImageRepository(t)
	svc := newTestContainerService(t, ContainerServiceDeps{
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
	images.EXPECT().List(mock.Anything, mock.Anything).Return(nil, 0, nil).Maybe()
	docker.EXPECT().EnsureUserNetwork(mock.Anything, mock.Anything).Return("network", nil)
	docker.EXPECT().ImageExists(mock.Anything, "nginx:latest").Return(false, nil)
	docker.EXPECT().PullImage(mock.Anything, "nginx:latest").Return(nil).Once()
	docker.EXPECT().CreateContainer(mock.Anything, mock.Anything).Return("", createErr)
	docker.EXPECT().RemoveImage(mock.Anything, "nginx:latest", false).Return(nil).Once()

	err := svc.ExecuteQueuedCreate(context.Background(), opID, id)
	require.ErrorIs(t, err, apperrors.ErrBadRequest)
}
