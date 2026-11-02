package service_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	. "github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/tests/mocks/core/service"
	"github.com/stretchr/testify/require"
)

func TestProjectServiceRejectsMissingDependencies(t *testing.T) {
	for _, field := range []struct{ field, message string }{
		{"Repository", "repository"},
		{"Resources", "resources"},
		{"Docker", "docker"},
		{"Containers", "containers"},
		{"Volumes", "volumes"},
		{"Capacity", "capacity"},
		{"Config", "config"},
	} {
		for _, typedNil := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/typed_nil=%t", field.field, typedNil), func(t *testing.T) {
				deps := projectServiceDeps(t)
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
				svc, err := NewProjectService(deps)
				require.ErrorContains(t, err, field.message+" is required")
				require.Nil(t, svc)
			})
		}
	}
}

func TestProjectServiceAcceptsRequiredDependencies(t *testing.T) {
	svc, err := NewProjectService(projectServiceDeps(t))
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func projectServiceDeps(t *testing.T) ProjectServiceDeps {
	t.Helper()
	return ProjectServiceDeps{
		Repository: coremocks.NewProjectRepository(t),
		Resources:  coremocks.NewProjectResourceRepository(t),
		Docker:     coremocks.NewProjectDockerAPI(t),
		Containers: coremocks.NewProjectContainerLifecycle(t),
		Volumes:    coremocks.NewProjectVolumeLifecycle(t),
		Capacity:   coremocks.NewCapacityChecker(t),
		Config:     staticConfig{},
	}
}

var _ ProjectRepository = (*repository.ProjectRepository)(nil)
var _ CapacityChecker = (*ContainerService)(nil)
var _ ProjectContainerLifecycle = (*ContainerService)(nil)

// Network cleanup is discovered on Containers; keep production satisfying it.
var _ ProjectNetworkCleaner = (*ContainerService)(nil)

// Tests that do not exercise a dependency still assemble every mandatory capability.
func newTestProjectService(t *testing.T, deps ProjectServiceDeps) *ProjectService {
	t.Helper()
	defaults := projectServiceDeps(t)
	if deps.Repository == nil {
		deps.Repository = defaults.Repository
	}
	if deps.Resources == nil {
		deps.Resources = defaults.Resources
	}
	if deps.Docker == nil {
		deps.Docker = defaults.Docker
	}
	if deps.Containers == nil {
		deps.Containers = defaults.Containers
	}
	if deps.Volumes == nil {
		deps.Volumes = defaults.Volumes
	}
	if deps.Capacity == nil {
		deps.Capacity = defaults.Capacity
	}
	if deps.Config == nil {
		deps.Config = defaults.Config
	}
	svc, err := NewProjectService(deps)
	require.NoError(t, err)
	return svc
}
