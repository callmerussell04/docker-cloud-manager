package service_test

import (
	"fmt"
	"reflect"
	"testing"

	builderdocker "github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/service"
	"github.com/stretchr/testify/require"
)

func TestBuilderServiceRejectsMissingDependencies(t *testing.T) {
	fields := []struct{ field, message string }{
		{"Files", "files"}, {"Workspaces", "workspaces"}, {"Extractor", "extractor"},
		{"Docker", "docker"}, {"NetworkAdmission", "network admission"},
		{"Logs", "logs"}, {"ObjectStore", "object store"}, {"Core", "core"},
		{"Results", "results"}, {"Config", "config"},
	}
	for _, field := range fields {
		for _, typedNil := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/typed_nil=%t", field.field, typedNil), func(t *testing.T) {
				deps := newBuilderHarness(t, testBuilderRuntimeConfig()).deps()
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
				svc, err := service.NewBuilderService(deps)
				require.ErrorContains(t, err, field.message+" is required")
				require.Nil(t, svc)
			})
		}
	}
}

func TestBuilderServiceAcceptsRequiredDependenciesWithoutLogger(t *testing.T) {
	deps := newBuilderHarness(t, testBuilderRuntimeConfig()).deps()
	deps.Logger = nil
	svc, err := service.NewBuilderService(deps)
	require.NoError(t, err)
	require.NotNil(t, svc)
}

var _ service.DockerAPI = (*builderdocker.Adapter)(nil)
var _ service.BuildNetworkAdmission = (*builderdocker.Adapter)(nil)
