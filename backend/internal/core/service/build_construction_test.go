package service_test

import (
	"fmt"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"reflect"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/stretchr/testify/mock"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/stretchr/testify/require"
)

func TestBuildServiceRejectsMissingDependencies(t *testing.T) {
	fields := []struct{ field, message string }{
		{"Repository", "repository"}, {"Images", "images"}, {"Registry", "registry"},
		{"Users", "users"}, {"Config", "config"}, {"VolumeRepo", "volumes"},
		{"StagedObjects", "staged objects"}, {"Containers", "containers"},
		{"DiskMetrics", "disk metrics"}, {"HostMetrics", "host metrics"},
		{"RegistryAuth", "registry authorization"},
	}
	for _, field := range fields {
		for _, typedNil := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/typed_nil=%t", field.field, typedNil), func(t *testing.T) {
				deps := buildServiceDeps(t)
				value := reflect.ValueOf(&deps).Elem().FieldByName(field.field)
				if typedNil {
					concrete := reflect.TypeOf(value.Interface())
					if concrete.Kind() != reflect.Pointer {
						concrete = reflect.PointerTo(concrete)
					}
					value.Set(reflect.Zero(concrete))
				} else {
					value.SetZero()
				}
				svc, err := service.NewBuildService(deps)
				require.ErrorContains(t, err, field.message+" is required")
				require.Nil(t, svc)
			})
		}
	}
}

func TestBuildServiceAcceptsRequiredDependenciesWithoutOptionalIntegrations(t *testing.T) {
	svc, err := service.NewBuildService(buildServiceDeps(t))
	require.NoError(t, err)
	require.NotNil(t, svc)
}

var _ service.BuildRepository = (*repository.BuildRepository)(nil)
var _ service.BuildContainerReservationRepository = (*repository.ContainerRepository)(nil)
var _ service.BuildObjectStore = (*objectstorage.LazyStorage)(nil)
var _ service.BuildRegistryAuthorizer = (*service.RegistryAuthorization)(nil)

func newBuildRepository(t *testing.T) *coremocks.BuildRepository {
	t.Helper()
	repo := coremocks.NewBuildRepository(t)
	repo.EXPECT().AcquireCapacityLock(mock.Anything).Return(func() {}, nil).Maybe()
	repo.EXPECT().CountActiveByOwner(mock.Anything, mock.Anything).Return(0, nil).Maybe()
	repo.EXPECT().CountActive(mock.Anything).Return(0, nil).Maybe()
	return repo
}

func buildServiceDeps(t *testing.T) service.BuildServiceDeps {
	t.Helper()
	volumes := coremocks.NewBuildVolumeDiskRepository(t)
	volumes.EXPECT().GetUserUsedVolumeBytes(mock.Anything, mock.Anything).Return(int64(0), nil).Maybe()
	staged := coremocks.NewStagedObjectRepository(t)
	staged.EXPECT().Reserve(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	staged.EXPECT().Release(mock.Anything, mock.Anything).Return(nil).Maybe()
	staged.EXPECT().ActiveBytesByOwner(mock.Anything, mock.Anything).Return(int64(0), nil).Maybe()
	containers := coremocks.NewBuildContainerReservationRepository(t)
	containers.EXPECT().GetTotalSystemReservedMemory(mock.Anything).Return(int64(0), nil).Maybe()
	containers.EXPECT().GetTotalSystemReservedCPU(mock.Anything).Return(int64(0), nil).Maybe()
	metrics := coremocks.NewHostMetricsProvider(t)
	metrics.EXPECT().GetTotalMemory().Return(int64(1<<40), nil).Maybe()
	metrics.EXPECT().GetLogicalCPUs().Return(int64(64), nil).Maybe()
	return service.BuildServiceDeps{
		Repository:    newBuildRepository(t),
		Images:        coremocks.NewBuildImageRepository(t),
		Registry:      coremocks.NewImageRegistryAPI(t),
		Users:         coremocks.NewUserInfoProvider(t),
		Config:        coretest.StaticConfig{Config: coretest.IntegrationConfig()},
		VolumeRepo:    volumes,
		StagedObjects: staged,
		Containers:    containers,
		DiskMetrics:   coremocks.NewHostDiskMetricsProvider(t),
		HostMetrics:   metrics,
		RegistryAuth:  revokingRegistryAuthorizer(t),
	}
}

// revokingRegistryAuthorizer accepts every credential revocation.
func revokingRegistryAuthorizer(t *testing.T) *coremocks.BuildRegistryAuthorizer {
	t.Helper()
	auth := coremocks.NewBuildRegistryAuthorizer(t)
	auth.EXPECT().RevokeBuild(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	return auth
}

func newTestBuildService(t *testing.T, deps service.BuildServiceDeps) *service.BuildService {
	t.Helper()
	defaults := buildServiceDeps(t)
	if deps.Config == nil {
		deps.Config = defaults.Config
	}
	if deps.VolumeRepo == nil {
		deps.VolumeRepo = defaults.VolumeRepo
	}
	if deps.StagedObjects == nil {
		deps.StagedObjects = defaults.StagedObjects
	}
	if deps.Containers == nil {
		deps.Containers = defaults.Containers
	}
	if deps.DiskMetrics == nil {
		deps.DiskMetrics = defaults.DiskMetrics
	}
	if deps.HostMetrics == nil {
		deps.HostMetrics = defaults.HostMetrics
	}
	if deps.RegistryAuth == nil {
		deps.RegistryAuth = defaults.RegistryAuth
	}
	svc, err := service.NewBuildService(deps)
	require.NoError(t, err)
	return svc
}
