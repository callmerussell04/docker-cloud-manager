package service_test

import (
	"testing"

	builderdocker "github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	"github.com/stretchr/testify/require"
)

func TestBuilderServiceRejectsMissingDependencies(t *testing.T) {
	valid := func(t *testing.T) service.BuilderServiceDeps {
		return newBuilderHarness(t, testBuilderRuntimeConfig()).deps()
	}
	depstest.RequireRejectsMissing(t, valid, service.NewBuilderService, map[string]string{
		"Files":            "files",
		"Workspaces":       "workspaces",
		"Extractor":        "extractor",
		"Docker":           "docker",
		"NetworkAdmission": "network admission",
		"Logs":             "logs",
		"ObjectStore":      "object store",
		"Core":             "core",
		"Results":          "results",
		"Config":           "config",
	})
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
