package compose_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	compose "github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/stretchr/testify/require"
)

func TestComposeDeploymentCoordinatorRejectsMissingDependencies(t *testing.T) {
	for _, field := range []struct{ field, message string }{
		{"Deployments", "deployments"},
		{"Builds", "builds"},
		{"Builder", "builder"},
		{"Containers", "containers"},
		{"ProjectContainers", "project containers"},
		{"Volumes", "volumes"},
		{"Docker", "docker"},
		{"Images", "images"},
		{"Sources", "sources"},
		{"Config", "config"},
	} {
		for _, typedNil := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/typed_nil=%t", field.field, typedNil), func(t *testing.T) {
				deps := newCoordinatorEnv(model.ComposeDeploymentJob{}).deps()
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
				coordinator, err := compose.NewComposeDeploymentCoordinator(deps)
				require.ErrorContains(t, err, field.message+" is required")
				require.Nil(t, coordinator)
			})
		}
	}
}

func TestComposeDeploymentCoordinatorAcceptsWithoutOptionalAudit(t *testing.T) {
	deps := newCoordinatorEnv(model.ComposeDeploymentJob{}).deps()
	deps.Auditor = nil
	deps.Users = nil
	coordinator, err := compose.NewComposeDeploymentCoordinator(deps)
	require.NoError(t, err)
	require.NotNil(t, coordinator)
}

// Production adapters keep satisfying the coordinator's durable contracts.
var _ compose.DeploymentStore = (*repository.ProjectRepository)(nil)
var _ compose.ProjectRepository = (*repository.ProjectRepository)(nil)
var _ compose.VolumeService = (*service.VolumeService)(nil)
var _ compose.ContainerService = (*service.ContainerService)(nil)
var _ compose.ComposeSourceReleaser = (*compose.Orchestrator)(nil)
