package service_test

import (
	"context"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
	"testing"

	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
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
	repo.EXPECT().GetUserUsedDiskSpace(mock.Anything, mock.Anything).Return(int64(0), nil).Maybe()
	return repo
}

func newTestContainerService(t *testing.T, deps ContainerServiceDeps) *ContainerService {
	t.Helper()
	if deps.Repository == nil {
		deps.Repository = newContainerRepository(t)
	}
	if deps.Volumes == nil {
		volumes := coremocks.NewContainerVolumeRepository(t)
		volumes.EXPECT().GetUserUsedVolumeBytes(mock.Anything, mock.Anything).Return(int64(0), nil).Maybe()
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
		deps.Config = staticConfig{}
	}
	if deps.Users == nil {
		deps.Users = coremocks.NewUserInfoProvider(t)
	}
	if deps.Builds == nil {
		builds := coremocks.NewActiveSystemBuildCounter(t)
		builds.EXPECT().CountActive(mock.Anything).Return(0, nil).Maybe()
		deps.Builds = builds
	}
	svc, err := NewContainerService(deps)
	require.NoError(t, err)
	return svc
}

func validContainerServiceDeps(t *testing.T) ContainerServiceDeps {
	t.Helper()
	return ContainerServiceDeps{
		Repository:  coremocks.NewContainerRepository(t),
		Volumes:     coremocks.NewContainerVolumeRepository(t),
		Images:      coremocks.NewContainerImageRepository(t),
		Docker:      coremocks.NewContainerDockerAPI(t),
		Metrics:     coremocks.NewHostMetricsProvider(t),
		DiskMetrics: coremocks.NewHostDiskMetricsProvider(t),
		Config:      coremocks.NewConfigManager(t),
		Users:       coremocks.NewUserInfoProvider(t),
		Builds:      coremocks.NewActiveSystemBuildCounter(t),
	}
}

func TestNewContainerServiceRequiresDependencies(t *testing.T) {
	for _, tc := range []struct {
		name string
		omit func(*ContainerServiceDeps, bool)
	}{
		{"repository", func(d *ContainerServiceDeps, typed bool) {
			d.Repository = (*coremocks.ContainerRepository)(nil)
			if !typed {
				d.Repository = nil
			}
		}},
		{"volumes", func(d *ContainerServiceDeps, typed bool) {
			d.Volumes = (*coremocks.ContainerVolumeRepository)(nil)
			if !typed {
				d.Volumes = nil
			}
		}},
		{"images", func(d *ContainerServiceDeps, typed bool) {
			d.Images = (*coremocks.ContainerImageRepository)(nil)
			if !typed {
				d.Images = nil
			}
		}},
		{"docker", func(d *ContainerServiceDeps, typed bool) {
			d.Docker = (*coremocks.ContainerDockerAPI)(nil)
			if !typed {
				d.Docker = nil
			}
		}},
		{"metrics", func(d *ContainerServiceDeps, typed bool) {
			d.Metrics = (*coremocks.HostMetricsProvider)(nil)
			if !typed {
				d.Metrics = nil
			}
		}},
		{"disk metrics", func(d *ContainerServiceDeps, typed bool) {
			d.DiskMetrics = (*coremocks.HostDiskMetricsProvider)(nil)
			if !typed {
				d.DiskMetrics = nil
			}
		}},
		{"config", func(d *ContainerServiceDeps, typed bool) {
			d.Config = (*coremocks.ConfigManager)(nil)
			if !typed {
				d.Config = nil
			}
		}},
		{"users", func(d *ContainerServiceDeps, typed bool) {
			d.Users = (*coremocks.UserInfoProvider)(nil)
			if !typed {
				d.Users = nil
			}
		}},
		{"builds", func(d *ContainerServiceDeps, typed bool) {
			d.Builds = (*coremocks.ActiveSystemBuildCounter)(nil)
			if !typed {
				d.Builds = nil
			}
		}},
	} {
		for _, typed := range []bool{false, true} {
			name := "nil"
			if typed {
				name = "typed nil"
			}
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				deps := validContainerServiceDeps(t)
				tc.omit(&deps, typed)
				svc, err := NewContainerService(deps)
				require.Nil(t, svc)
				require.EqualError(t, err, "container service: "+tc.name+" is required")
			})
		}
	}
}

func TestNewContainerServiceAcceptsCompleteDependenciesWithoutOptionalObservers(t *testing.T) {
	svc, err := NewContainerService(validContainerServiceDeps(t))
	require.NoError(t, err)
	require.NotNil(t, svc)
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
			deps.Config = staticConfig{}
			svc, err := NewContainerService(deps)
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
	configProvider := coremocks.NewConfigManager(t)
	configProvider.EXPECT().Get().Return(cfg)
	deps.Config = configProvider
	svc, err := NewContainerService(deps)
	require.NoError(t, err)
	_, err = svc.Create(accessscope.WithUserScope(context.Background(), owner, "", ""), model.ContainerCreateParams{Name: "web", ImageTag: "nginx"})
	require.ErrorIs(t, err, apperrors.ErrLimitExceeded)
	require.True(t, released)
}

var _ ContainerDockerAPI = (*docker.Adapter)(nil)
