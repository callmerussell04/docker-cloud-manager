package compose_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	composemocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service/compose"
	"github.com/stretchr/testify/require"
)

func TestOrchestratorRejectsMissingAdmissionDependencies(t *testing.T) {
	valid := func(t *testing.T) compose.OrchestratorDeps {
		deps, _ := composeOrchestratorDeps(t, composemocks.NewProjectRepository(t), composeTestConfig())
		return deps
	}
	depstest.RequireRejectsMissing(t, valid, compose.NewOrchestrator, map[string]string{
		"Projects":          "projects",
		"DeploymentCounter": "deployment counter",
		"Capacity":          "capacity",
		"Config":            "config",
	})
}

func TestOrchestratorAcceptsRequiredDependenciesWithoutOptionalIntegrations(t *testing.T) {
	deps, _ := composeOrchestratorDeps(t, composemocks.NewProjectRepository(t), composeTestConfig())
	orchestrator, err := compose.NewOrchestrator(compose.OrchestratorDeps{
		Projects:          deps.Projects,
		DeploymentCounter: deps.DeploymentCounter,
		Capacity:          deps.Capacity,
		Config:            deps.Config,
	})
	require.NoError(t, err)
	require.NotNil(t, orchestrator)
}

// TestOrchestratorRequiresTheStagingLockWithStagedObjects pins that Compose
// cannot stage sources without the owner capacity lock and the reserved disk
// reader the one staged disk rule needs.
func TestOrchestratorRequiresTheStagingLockWithStagedObjects(t *testing.T) {
	valid := func(t *testing.T) compose.OrchestratorDeps {
		deps, _ := composeOrchestratorDeps(t, composemocks.NewProjectRepository(t), composeTestConfig())
		deps.StagedObjects = composemocks.NewStagedObjectRepository(t)
		deps.CapacityLock = composemocks.NewCapacityLock(t)
		deps.ReservedDisk = composemocks.NewReservedDiskReader(t)
		return deps
	}
	depstest.RequireRejectsMissing(t, valid, compose.NewOrchestrator, map[string]string{
		"CapacityLock": "capacity lock",
		"ReservedDisk": "reserved disk reader",
	})

	orchestrator, err := compose.NewOrchestrator(valid(t))
	require.NoError(t, err)
	require.NotNil(t, orchestrator)
}

// Production adapters keep satisfying the orchestrator's admission contracts.
var _ compose.ActiveComposeDeploymentCounter = (*repository.ProjectRepository)(nil)
var _ compose.CapacityChecker = (*service.ComposePlanAdmission)(nil)
var _ compose.BuildJobCreator = (*service.BuildService)(nil)
var _ compose.ObjectStorage = (*objectstorage.LazyStorage)(nil)
var _ compose.CapacityLock = (*repository.ContainerRepository)(nil)
var _ compose.ReservedDiskReader = service.ReservedDiskReader{}
var _ compose.StagedObjectRepository = (*repository.StagedObjectRepository)(nil)
