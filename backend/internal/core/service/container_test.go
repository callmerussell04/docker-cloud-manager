package service_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newContainerRepository(t *testing.T) *coremocks.ContainerRepository {
	t.Helper()
	repo := coremocks.NewContainerRepository(t)
	repo.EXPECT().AcquireOwnerCapacityLock(mock.Anything, mock.Anything).Return(func() {}, nil).Maybe()
	repo.EXPECT().CountActiveCreateOperationsByOwner(mock.Anything, mock.Anything).Return(0, nil).Maybe()
	repo.EXPECT().HasActiveOperation(mock.Anything, mock.Anything, mock.Anything).Return(false, nil).Maybe()
	return repo
}

func newContainerImageRepository(t *testing.T) *coremocks.ContainerImageRepository {
	t.Helper()
	repo := coremocks.NewContainerImageRepository(t)
	repo.EXPECT().GetUserImageBytes(mock.Anything, mock.Anything).Return(int64(0), nil).Maybe()
	return repo
}

func newTestContainerService(t *testing.T, deps service.ContainerServiceDeps) *service.ContainerService {
	t.Helper()
	if deps.Repository == nil {
		deps.Repository = newContainerRepository(t)
	}
	if deps.Volumes == nil {
		volumes := coremocks.NewContainerVolumeRepository(t)
		volumes.EXPECT().GetUserReservedVolumeBytes(mock.Anything, mock.Anything).Return(int64(0), nil).Maybe()
		deps.Volumes = volumes
	}
	if deps.Images == nil {
		deps.Images = newContainerImageRepository(t)
	}
	if deps.Docker == nil {
		deps.Docker = coremocks.NewContainerDockerAPI(t)
	}
	if deps.Metrics == nil {
		deps.Metrics = coremocks.NewHostMetricsProvider(t)
	}
	if deps.DiskMetrics == nil {
		deps.DiskMetrics = coremocks.NewHostDiskMetricsProvider(t)
	}
	if deps.Config == nil {
		deps.Config = testConfig()
	}
	if deps.Quotas == nil {
		deps.Quotas = testQuotas(t, deps.Config, nil)
	}
	if deps.Builds == nil {
		builds := coremocks.NewActiveSystemBuildCounter(t)
		builds.EXPECT().CountActive(mock.Anything).Return(0, nil).Maybe()
		deps.Builds = builds
	}
	svc, err := service.NewContainerService(deps)
	require.NoError(t, err)
	return svc
}

func validContainerServiceDeps(t *testing.T) service.ContainerServiceDeps {
	t.Helper()
	return service.ContainerServiceDeps{
		Repository:  coremocks.NewContainerRepository(t),
		Volumes:     coremocks.NewContainerVolumeRepository(t),
		Images:      coremocks.NewContainerImageRepository(t),
		Docker:      coremocks.NewContainerDockerAPI(t),
		Metrics:     coremocks.NewHostMetricsProvider(t),
		DiskMetrics: coremocks.NewHostDiskMetricsProvider(t),
		Config:      testConfig(),
		Quotas:      testQuotas(t, nil, nil),
		Builds:      coremocks.NewActiveSystemBuildCounter(t),
	}
}

func TestNewContainerServiceRequiresDependencies(t *testing.T) {
	depstest.RequireRejectsMissing(t, validContainerServiceDeps, service.NewContainerService, map[string]string{
		"Repository":  "repository",
		"Volumes":     "volumes",
		"Images":      "images",
		"Docker":      "docker",
		"Metrics":     "metrics",
		"DiskMetrics": "disk metrics",
		"Config":      "config",
		"Quotas":      "quotas",
		"Builds":      "builds",
	})
}

func TestContainerServiceAdmissionPropagatesLockFailure(t *testing.T) {
	for _, operation := range []string{"create", "start", "check capacity"} {
		t.Run(operation, func(t *testing.T) {
			owner, id := uuid.New(), uuid.New()
			deps := validContainerServiceDeps(t)
			repo := deps.Repository.(*coremocks.ContainerRepository)
			repo.EXPECT().AcquireOwnerCapacityLock(mock.Anything, owner).Return(nil, apperrors.ErrUnavailable).Once()
			if operation == "start" {
				repo.EXPECT().GetByID(mock.Anything, id).Return(model.Container{ID: id, OwnerID: owner, DockerID: "runtime"}, nil)
			}
			deps.Config = testConfig()
			svc, err := service.NewContainerService(deps)
			require.NoError(t, err)
			ctx := accessscope.WithUserScope(context.Background(), owner, "", "")
			switch operation {
			case "create":
				_, err = svc.Create(ctx, model.ContainerCreateParams{Name: "web", ImageTag: "nginx"})
			case "start":
				err = svc.Start(ctx, id)
			case "check capacity":
				err = svc.CheckCapacity(ctx, owner, 128, 250, 0)
			}
			require.ErrorIs(t, err, apperrors.ErrUnavailable)
		})
	}
}

func TestContainerServiceCreateEnforcesQueuedCreateLimit(t *testing.T) {
	owner := uuid.New()
	deps := validContainerServiceDeps(t)
	repo := deps.Repository.(*coremocks.ContainerRepository)
	released := false
	repo.EXPECT().AcquireOwnerCapacityLock(mock.Anything, owner).Return(func() { released = true }, nil).Once()
	repo.EXPECT().CountByOwnerID(mock.Anything, owner).Return(1, nil).Once()
	repo.EXPECT().CountActiveCreateOperationsByOwner(mock.Anything, owner).Return(2, nil).Once()
	cfg := containerCreateConfig()
	cfg.MaxQueuedContainerCreatesPerUser = 2
	configProvider := coretest.StaticConfig{Config: cfg}
	deps.Config = configProvider
	svc, err := service.NewContainerService(deps)
	require.NoError(t, err)
	_, err = svc.Create(accessscope.WithUserScope(context.Background(), owner, "", ""), model.ContainerCreateParams{Name: "web", ImageTag: "nginx"})
	require.ErrorIs(t, err, apperrors.ErrLimitExceeded)
	require.True(t, released)
}

var _ service.ContainerDockerAPI = (*docker.Adapter)(nil)
