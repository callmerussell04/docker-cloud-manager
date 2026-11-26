package service_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/stretchr/testify/require"
)

func TestProjectServiceRejectsMissingDependencies(t *testing.T) {
	depstest.RequireRejectsMissing(t, projectServiceDeps, service.NewProjectService, map[string]string{
		"Repository": "repository",
		"Resources":  "resources",
		"Docker":     "docker",
		"Containers": "containers",
		"Volumes":    "volumes",
		"Capacity":   "capacity",
		"Config":     "config",
	})
}

func projectServiceDeps(t *testing.T) service.ProjectServiceDeps {
	t.Helper()
	return service.ProjectServiceDeps{
		Repository: coremocks.NewProjectRepository(t),
		Resources:  coremocks.NewProjectResourceRepository(t),
		Docker:     coremocks.NewProjectDockerAPI(t),
		Containers: coremocks.NewProjectContainerLifecycle(t),
		Volumes:    coremocks.NewProjectVolumeLifecycle(t),
		Capacity:   coremocks.NewCapacityChecker(t),
		Config:     testConfig(),
	}
}

var _ service.ProjectRepository = (*repository.ProjectRepository)(nil)
var _ service.CapacityChecker = (*service.ContainerService)(nil)
var _ service.ProjectContainerLifecycle = (*service.ContainerService)(nil)

// Network cleanup is discovered on Containers; keep production satisfying it.
var _ service.ProjectNetworkCleaner = (*service.ContainerService)(nil)

// Tests that do not exercise a dependency still assemble every mandatory capability.
func newTestProjectService(t *testing.T, deps service.ProjectServiceDeps) *service.ProjectService {
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
	svc, err := service.NewProjectService(deps)
	require.NoError(t, err)
	return svc
}
