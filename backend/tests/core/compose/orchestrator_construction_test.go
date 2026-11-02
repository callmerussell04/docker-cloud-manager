package compose_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	composemocks "github.com/callmerussell04/docker-cloud-manager/backend/tests/mocks/core/service/compose"
	"github.com/stretchr/testify/require"
)

func TestOrchestratorRejectsMissingAdmissionDependencies(t *testing.T) {
	for _, field := range []struct{ field, message string }{
		{"Projects", "projects"},
		{"DeploymentCounter", "deployment counter"},
		{"Capacity", "capacity"},
		{"Config", "config"},
	} {
		for _, typedNil := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/typed_nil=%t", field.field, typedNil), func(t *testing.T) {
				deps, _ := composeOrchestratorDeps(t, composemocks.NewProjectRepository(t), composeTestConfig())
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
				orchestrator, err := compose.NewOrchestrator(deps)
				require.ErrorContains(t, err, field.message+" is required")
				require.Nil(t, orchestrator)
			})
		}
	}
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

// Production adapters keep satisfying the orchestrator's admission contracts.
var _ compose.ActiveComposeDeploymentCounter = (*repository.ProjectRepository)(nil)
var _ compose.CapacityChecker = (*service.ContainerService)(nil)
var _ compose.BuildJobCreator = (*service.BuildService)(nil)
var _ compose.ObjectStorage = (*objectstorage.LazyStorage)(nil)
