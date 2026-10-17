package service_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/stretchr/testify/require"
)

func TestImageServiceRejectsMissingDependencies(t *testing.T) {
	for _, field := range []struct{ field, message string }{
		{"Repository", "repository"},
		{"Docker", "docker"},
		{"Registry", "registry"},
		{"Containers", "containers"},
		{"Config", "config"},
		{"Lifecycle", "lifecycle"},
	} {
		for _, typedNil := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/typed_nil=%t", field.field, typedNil), func(t *testing.T) {
				deps := imageServiceDeps(t)
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
				svc, err := NewImageService(deps)
				require.ErrorContains(t, err, field.message+" is required")
				require.Nil(t, svc)
			})
		}
	}
}

func TestImageServiceAcceptsRequiredDependencies(t *testing.T) {
	svc, err := NewImageService(imageServiceDeps(t))
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func imageServiceDeps(t *testing.T) ImageServiceDeps {
	t.Helper()
	return ImageServiceDeps{
		Repository: coremocks.NewImageRepository(t),
		Docker:     coremocks.NewImageDockerAPI(t),
		Registry:   coremocks.NewImageRegistryAPI(t),
		Containers: coremocks.NewImageContainerRepository(t),
		Config:     staticConfig{},
		Lifecycle:  coremocks.NewImageLifecycleRepository(t),
	}
}

var _ ImageRepository = (*repository.ImageRepository)(nil)
var _ ImageLifecycleRepository = (*repository.ResourceLifecycleRepository)(nil)
