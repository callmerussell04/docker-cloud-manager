package service_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestVolumeServiceRejectsMissingDependencies(t *testing.T) {
	depstest.RequireRejectsMissing(t, volumeServiceDeps, service.NewVolumeService, map[string]string{
		"Repository":   "repository",
		"Docker":       "docker",
		"Config":       "config",
		"Lifecycle":    "lifecycle",
		"Users":        "users",
		"ImageRepo":    "images",
		"DiskMetrics":  "disk metrics",
		"CapacityLock": "capacity lock",
		"SizeMode":     "volume size mode",
	})
}

func TestVolumeServiceAcceptsRequiredDependencies(t *testing.T) {
	svc, err := service.NewVolumeService(volumeServiceDeps(t))
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func volumeServiceDeps(t *testing.T) service.VolumeServiceDeps {
	t.Helper()
	return service.VolumeServiceDeps{
		Repository:   coremocks.NewVolumeRepository(t),
		Docker:       coremocks.NewVolumeDockerAPI(t),
		Config:       testConfig(),
		Lifecycle:    coremocks.NewVolumeLifecycleRepository(t),
		Users:        coremocks.NewUserInfoProvider(t),
		ImageRepo:    coremocks.NewVolumeImageDiskRepository(t),
		DiskMetrics:  coremocks.NewHostDiskMetricsProvider(t),
		CapacityLock: coremocks.NewVolumeCapacityLock(t),
		SizeMode:     coremocks.NewVolumeSizeModeReader(t),
	}
}

var _ service.VolumeRepository = (*repository.VolumeRepository)(nil)
var _ service.VolumeLifecycleRepository = (*repository.ResourceLifecycleRepository)(nil)
var _ service.VolumeCapacityLock = (*repository.ContainerRepository)(nil)
var _ service.VolumeSizeModeReader = (*service.VolumeSizeEnforcement)(nil)

// Tests unrelated to admission still assemble every mandatory capability.
func newTestVolumeService(t *testing.T, deps service.VolumeServiceDeps) *service.VolumeService {
	t.Helper()
	if deps.Users == nil {
		deps.Users = coremocks.NewUserInfoProvider(t)
	}
	if deps.ImageRepo == nil {
		deps.ImageRepo = coremocks.NewVolumeImageDiskRepository(t)
	}
	if deps.DiskMetrics == nil {
		deps.DiskMetrics = coremocks.NewHostDiskMetricsProvider(t)
	}
	if deps.CapacityLock == nil {
		lock := coremocks.NewVolumeCapacityLock(t)
		lock.EXPECT().AcquireOwnerCapacityLock(mock.Anything, mock.Anything).Return(func() {}, nil).Maybe()
		deps.CapacityLock = lock
	}
	if deps.SizeMode == nil {
		deps.SizeMode = coretest.MeasuredVolumeSizeMode(t)
	}
	svc, err := service.NewVolumeService(deps)
	require.NoError(t, err)
	return svc
}
