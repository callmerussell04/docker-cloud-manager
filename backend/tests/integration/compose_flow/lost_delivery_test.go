package compose_flow_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// loseComposeDelivery plays a relay that published the deployment message and
// a broker that then lost it, and ages the record past the lost-delivery
// bound. It returns the queued job.
func loseComposeDelivery(t *testing.T, f *coretest.ComposeFixture, name string) model.ComposeDeploymentJob {
	t.Helper()
	ctx := context.Background()
	projectID, err := f.Orchestrator.StartDeployment(f.OwnerContext(), name, "compose.zip", "", bytes.NewReader(coretest.ComposeZip(t, map[string]string{
		"docker-compose.yml": webCompose,
	})))
	require.NoError(t, err)
	job, err := f.Repos.Projects.GetActiveComposeDeploymentJobByProjectID(ctx, projectID)
	require.NoError(t, err)
	items, err := f.Repos.Projects.LeasePendingComposeOutbox(ctx, 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.NoError(t, f.Repos.Projects.MarkComposeOutboxPublished(ctx, items[0].ID))
	_, err = f.Repos.DB.Exec(ctx, `UPDATE compose_deployment_queue_outbox SET updated_at = NOW() - INTERVAL '36 minutes' WHERE job_id = $1`, job.ID)
	require.NoError(t, err)
	return job
}

func requireNoComposeMessagePending(t *testing.T, f *coretest.ComposeFixture) {
	t.Helper()
	items, err := f.Repos.Projects.LeasePendingComposeOutbox(context.Background(), 10)
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestComposeDeploymentWhoseMessageWasLostRunsAfterRecovery(t *testing.T) {
	f := coretest.NewComposeFixture(t, coretest.IntegrationConfig())
	f.ContainerDocker.EXPECT().EnsureUserNetwork(mock.Anything, mock.Anything).Return("network", nil).Once()
	f.ContainerDocker.EXPECT().ImageExists(mock.Anything, mock.Anything).Return(true, nil).Once()
	f.ContainerDocker.EXPECT().CreateContainer(mock.Anything, mock.Anything).Return("docker-web", nil).Once()
	f.ContainerDocker.EXPECT().UpdateContainerResources(mock.Anything, "docker-web", mock.Anything).Return(nil).Once()
	f.ContainerDocker.EXPECT().StartContainer(mock.Anything, "docker-web").Return(nil).Once()
	job := loseComposeDelivery(t, f, "stack")

	// Nothing looks at a queued job whose message is gone.
	f.Tick(t)
	requireNoComposeMessagePending(t, f)
	requireDeployment(t, f, job.ID, model.ComposeDeploymentStatusQueued, "", model.ProjectStatusBuilding)

	f.RecoverOperations(t)

	require.NoError(t, f.Deliver(t))
	requireDeployment(t, f, job.ID, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStageCreating, model.ProjectStatusDeploying)
	f.Tick(t)
	require.Equal(t, 1, f.RunContainerOperations(t))
	f.Tick(t)
	f.Tick(t)
	require.Equal(t, 1, f.RunContainerOperations(t))
	f.Tick(t)
	requireDeployment(t, f, job.ID, model.ComposeDeploymentStatusSucceeded, model.ComposeDeploymentStageStarting, model.ProjectStatusRunning)
}

func TestCanceledComposeDeploymentWhoseMessageWasLostIsCanceledAfterRecovery(t *testing.T) {
	ctx := context.Background()
	f := coretest.NewComposeFixture(t, coretest.IntegrationConfig())
	job := loseComposeDelivery(t, f, "cancel-demo")
	// A queued job is canceled when its message is delivered.
	require.NoError(t, f.Repos.Projects.RequestComposeDeploymentCancel(ctx, job.ProjectID))
	f.Tick(t)
	requireNoComposeMessagePending(t, f)
	requireDeployment(t, f, job.ID, model.ComposeDeploymentStatusCanceling, "", model.ProjectStatusBuilding)

	f.RecoverOperations(t)

	require.NoError(t, f.Deliver(t))
	requireDeployment(t, f, job.ID, model.ComposeDeploymentStatusCanceled, "", model.ProjectStatusCanceled)
	require.False(t, f.Objects.Has(job.SourceObjectKey))
	// Recovery has nothing left to republish.
	f.RecoverOperations(t)
	requireNoComposeMessagePending(t, f)
}
