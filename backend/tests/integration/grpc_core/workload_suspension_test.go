package grpc_core_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coregrpcmocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/grpc"
)

// workloadServer serves the workload API over the Compose fixture, whose
// build service and orchestrator are wired as Core's app wires them.
func workloadServer(t *testing.T) (*coretest.ComposeFixture, *grpc.ClientConn) {
	t.Helper()
	f := coretest.NewComposeFixture(t, coretest.IntegrationConfig())
	suspension, err := service.NewWorkloadSuspension(service.WorkloadSuspensionDeps{
		Containers:         f.Repos.Containers,
		Builds:             f.Repos.Builds,
		BuildCanceler:      f.Builds,
		Deployments:        f.Repos.Projects,
		DeploymentCanceler: f.Orchestrator,
		Logger:             coretest.DiscardLogger(),
	})
	require.NoError(t, err)
	return f, coretest.NewCoreGRPCConn(t, coretest.CoreGRPCRegistration{
		Workloads: suspension,
		Users:     coregrpcmocks.NewMockUserDirectory(t),
	})
}

func queueBuild(t *testing.T, f *coretest.ComposeFixture, ownerID uuid.UUID, status string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	imageID, buildID := uuid.New(), uuid.New()
	require.NoError(t, f.Repos.Builds.CreateQueuedBuild(ctx,
		model.Image{ID: imageID, OwnerID: ownerID, Tag: "app-" + buildID.String()[:8] + ":latest"},
		model.Build{ID: buildID, ImageID: imageID, OwnerID: ownerID, Status: model.BuildStatusPending, LogFilePath: "build-logs/x.log"},
		model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: buildqueue.ExchangeName, RoutingKey: buildqueue.RoutingKey, Payload: []byte(`{}`)},
	))
	if status != model.BuildStatusPending {
		require.NoError(t, f.Repos.Builds.UpdateStatus(ctx, buildID, status))
	}
	return buildID
}

func queueDeployment(t *testing.T, f *coretest.ComposeFixture, ownerID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	projectID, jobID := uuid.New(), uuid.New()
	require.NoError(t, f.Repos.Projects.CreateWithComposeDeploymentJob(context.Background(),
		model.Project{ID: projectID, OwnerID: ownerID, Name: name, Status: model.ProjectStatusBuilding},
		model.ComposeDeploymentJob{ID: jobID, ProjectID: projectID, OwnerID: ownerID, SourceType: model.ComposeSourceTypeUpload, SourceObjectKey: "compose-sources/" + name + ".zip"},
		model.ComposeDeploymentOutbox{ID: uuid.New(), JobID: jobID, Exchange: queue.ComposeExchangeName, RoutingKey: queue.ComposeRoutingKey, Payload: []byte(`{}`)},
	))
	return projectID
}

func saveRunningContainer(t *testing.T, f *coretest.ComposeFixture, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, f.Repos.Containers.Save(context.Background(), model.Container{
		ID: id, OwnerID: ownerID, Name: "web-" + id.String()[:8], ImageTag: "nginx:latest",
		DockerID: "docker-" + id.String()[:12], DockerGeneration: 1, EnvVars: []byte(`{}`),
		Status: model.ContainerStatusRunning, DesiredStatus: model.ContainerStatusRunning,
		BaseMemoryReservation: 256 << 20, BaseCPUReservation: 250,
	}))
	return id
}

func buildStatus(t *testing.T, f *coretest.ComposeFixture, id uuid.UUID) string {
	t.Helper()
	build, err := f.Repos.Builds.GetByID(context.Background(), id)
	require.NoError(t, err)
	return build.Status
}

func projectStatus(t *testing.T, f *coretest.ComposeFixture, id uuid.UUID) string {
	t.Helper()
	project, err := f.Repos.Projects.GetByID(context.Background(), id)
	require.NoError(t, err)
	return project.Status
}

// An administrator suspends a user: their running container is stopped, their
// active builds and deployment are canceled, finished work and other users'
// workloads are untouched, and a second call does nothing more.
func TestAdministratorSuspendsAUsersWorkloads(t *testing.T) {
	f, conn := workloadServer(t)
	client := coreapi.NewWorkloadAPIClient(conn)
	ctx := context.Background()
	ownerID, otherID := uuid.New(), uuid.New()
	container := saveRunningContainer(t, f, ownerID)
	otherContainer := saveRunningContainer(t, f, otherID)
	pending := queueBuild(t, f, ownerID, model.BuildStatusPending)
	running := queueBuild(t, f, ownerID, model.BuildStatusRunning)
	finished := queueBuild(t, f, ownerID, model.BuildStatusSuccess)
	otherBuild := queueBuild(t, f, otherID, model.BuildStatusRunning)
	deploying := queueDeployment(t, f, ownerID, "deploying")
	otherDeployment := queueDeployment(t, f, otherID, "other")
	done := queueDeployment(t, f, ownerID, "done")
	require.NoError(t, f.Repos.Projects.UpdateStatus(ctx, done, model.ProjectStatusRunning, nil))
	doneJob, err := f.Repos.Projects.GetActiveComposeDeploymentJobByProjectID(ctx, done)
	require.NoError(t, err)
	require.NoError(t, f.Repos.Projects.CompleteComposeDeploymentJob(ctx, doneJob.ID, model.ComposeDeploymentStatusSucceeded, nil))

	got, err := client.SuspendUserWorkloads(adminGRPCContext(), &coreapi.SuspendUserWorkloadsRequest{UserId: ownerID.String()})

	require.NoError(t, err)
	require.Equal(t, &coreapi.SuspendUserWorkloadsResponse{StoppedContainers: 1, CanceledBuilds: 2, CanceledDeployments: 1}, suspensionCounts(got))
	stopping, err := f.Repos.Containers.GetByID(ctx, container)
	require.NoError(t, err)
	require.Equal(t, model.ContainerStatusStopping, stopping.Status)
	require.Equal(t, model.ContainerStatusExited, stopping.DesiredStatus)
	require.Equal(t, model.BuildStatusCanceled, buildStatus(t, f, pending))
	require.Equal(t, model.BuildStatusCanceled, buildStatus(t, f, running))
	require.Equal(t, model.BuildStatusSuccess, buildStatus(t, f, finished))
	require.Equal(t, model.ProjectStatusCanceling, projectStatus(t, f, deploying))
	require.Equal(t, model.ProjectStatusRunning, projectStatus(t, f, done))

	other, err := f.Repos.Containers.GetByID(ctx, otherContainer)
	require.NoError(t, err)
	require.Equal(t, model.ContainerStatusRunning, other.Status)
	require.Equal(t, model.BuildStatusRunning, buildStatus(t, f, otherBuild))
	require.Equal(t, model.ProjectStatusBuilding, projectStatus(t, f, otherDeployment))

	again, err := client.SuspendUserWorkloads(adminGRPCContext(), &coreapi.SuspendUserWorkloadsRequest{UserId: ownerID.String()})
	require.NoError(t, err)
	require.Equal(t, &coreapi.SuspendUserWorkloadsResponse{}, suspensionCounts(again), "nothing is done twice")
}

// Only Gateway may suspend, and only in admin scope: Builder's credential is
// outside its policy, and user and system scope are refused.
func TestSuspendingWorkloadsNeedsGatewayInAdminScope(t *testing.T) {
	f, conn := workloadServer(t)
	client := coreapi.NewWorkloadAPIClient(conn)
	ownerID := uuid.New()
	container := saveRunningContainer(t, f, ownerID)
	for name, ctx := range map[string]context.Context{
		"user scope":   coretest.InternalGRPCContext(context.Background(), coretest.GatewayCredential, accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID, Username: "alice", Role: "user"}),
		"system scope": coretest.InternalGRPCContext(context.Background(), coretest.GatewayCredential, accessscope.Scope{Kind: accessscope.KindSystem}),
		"builder":      coretest.InternalGRPCContext(context.Background(), coretest.BuilderCredential, accessscope.Scope{Kind: accessscope.KindSystem}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := client.SuspendUserWorkloads(ctx, &coreapi.SuspendUserWorkloadsRequest{UserId: ownerID.String()})
			require.Equal(t, codes.PermissionDenied, status.Code(err))
		})
	}
	running, err := f.Repos.Containers.GetByID(context.Background(), container)
	require.NoError(t, err)
	require.Equal(t, model.ContainerStatusRunning, running.Status)
}

// suspensionCounts drops protobuf internals so responses compare by value.
func suspensionCounts(resp *coreapi.SuspendUserWorkloadsResponse) *coreapi.SuspendUserWorkloadsResponse {
	return &coreapi.SuspendUserWorkloadsResponse{
		StoppedContainers:   resp.GetStoppedContainers(),
		SkippedContainers:   resp.GetSkippedContainers(),
		CanceledBuilds:      resp.GetCanceledBuilds(),
		CanceledDeployments: resp.GetCanceledDeployments(),
		Failed:              resp.GetFailed(),
	}
}
