package compose_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
)

func TestDeploymentCoordinatorRejectsMissingDependencies(t *testing.T) {
	valid := func(*testing.T) compose.DeploymentCoordinatorDeps {
		return newCoordinatorEnv(model.ComposeDeploymentJob{}).deps()
	}
	depstest.RequireRejectsMissing(t, valid, compose.NewDeploymentCoordinator, map[string]string{
		"Deployments":       "deployments",
		"Builds":            "builds",
		"Builder":           "builder",
		"Containers":        "containers",
		"ProjectContainers": "project containers",
		"Volumes":           "volumes",
		"Docker":            "docker",
		"Images":            "images",
		"Sources":           "sources",
		"Config":            "config",
	})
}

// Production adapters keep satisfying the coordinator's durable contracts.
var _ compose.DeploymentStore = (*repository.ProjectRepository)(nil)
var _ compose.ProjectRepository = (*repository.ProjectRepository)(nil)
var _ compose.VolumeService = (*service.VolumeService)(nil)
var _ compose.ContainerService = (*service.ContainerService)(nil)
var _ compose.SourceReleaser = (*compose.Orchestrator)(nil)
