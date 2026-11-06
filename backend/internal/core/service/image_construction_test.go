package service_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestImageServiceRejectsMissingDependencies(t *testing.T) {
	depstest.RequireRejectsMissing(t, imageServiceDeps, service.NewImageService, map[string]string{
		"Repository":   "repository",
		"Docker":       "docker",
		"Registry":     "registry",
		"Containers":   "containers",
		"Config":       "config",
		"Lifecycle":    "lifecycle",
		"RegistryAuth": "registry authorization",
	})
}

func TestImageServiceAcceptsRequiredDependencies(t *testing.T) {
	svc, err := service.NewImageService(imageServiceDeps(t))
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func imageServiceDeps(t *testing.T) service.ImageServiceDeps {
	t.Helper()
	return service.ImageServiceDeps{
		Repository:   coremocks.NewImageRepository(t),
		Docker:       coremocks.NewImageDockerAPI(t),
		Registry:     coremocks.NewImageRegistryAPI(t),
		Containers:   coremocks.NewImageContainerRepository(t),
		Config:       testConfig(),
		Lifecycle:    coremocks.NewImageLifecycleRepository(t),
		RegistryAuth: unlockedRepositoryFence(t),
	}
}

// unlockedRepositoryFence admits every Registry deletion without waiting.
func unlockedRepositoryFence(t *testing.T) *coremocks.ImageRepositoryFencer {
	t.Helper()
	fence := coremocks.NewImageRepositoryFencer(t)
	fence.EXPECT().FenceAndLockRepository(mock.Anything, mock.Anything).Return(func() {}, nil).Maybe()
	return fence
}

var _ service.ImageRepository = (*repository.ImageRepository)(nil)
var _ service.ImageLifecycleRepository = (*repository.ResourceLifecycleRepository)(nil)
var _ service.ImageRepositoryFencer = (*service.RegistryAuthorization)(nil)
