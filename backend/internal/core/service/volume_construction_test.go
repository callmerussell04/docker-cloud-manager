package service_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/stretchr/testify/require"
)

func TestVolumeServiceRejectsMissingDependencies(t *testing.T) {
	for _, field := range []struct{ field, message string }{
		{"Repository", "repository"},
		{"Docker", "docker"},
		{"Config", "config"},
		{"Lifecycle", "lifecycle"},
		{"Users", "users"},
		{"ImageRepo", "images"},
		{"DiskMetrics", "disk metrics"},
	} {
		for _, typedNil := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/typed_nil=%t", field.field, typedNil), func(t *testing.T) {
				deps := volumeServiceDeps(t)
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
				svc, err := service.NewVolumeService(deps)
				require.ErrorContains(t, err, field.message+" is required")
				require.Nil(t, svc)
			})
		}
	}
}

func TestVolumeServiceAcceptsRequiredDependencies(t *testing.T) {
	svc, err := service.NewVolumeService(volumeServiceDeps(t))
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func volumeServiceDeps(t *testing.T) service.VolumeServiceDeps {
	t.Helper()
	return service.VolumeServiceDeps{
		Repository:  coremocks.NewVolumeRepository(t),
		Docker:      coremocks.NewVolumeDockerAPI(t),
		Config:      staticConfig{},
		Lifecycle:   coremocks.NewVolumeLifecycleRepository(t),
		Users:       coremocks.NewUserInfoProvider(t),
		ImageRepo:   coremocks.NewVolumeImageDiskRepository(t),
		DiskMetrics: coremocks.NewHostDiskMetricsProvider(t),
	}
}

var _ service.VolumeRepository = (*repository.VolumeRepository)(nil)
var _ service.VolumeLifecycleRepository = (*repository.ResourceLifecycleRepository)(nil)

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
	svc, err := service.NewVolumeService(deps)
	require.NoError(t, err)
	return svc
}
