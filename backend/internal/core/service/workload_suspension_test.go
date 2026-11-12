package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
)

// suspensionEnv is a workload suspension over the container store fake. The
// owner has no builds or deployments unless a test scripts them.
type suspensionEnv struct {
	store              *containerStore
	builds             *coremocks.WorkloadSuspensionBuildRepository
	buildCanceler      *coremocks.WorkloadSuspensionBuildCanceler
	deployments        *coremocks.WorkloadSuspensionDeploymentRepository
	deploymentCanceler *coremocks.WorkloadSuspensionDeploymentCanceler
	svc                *service.WorkloadSuspension
}

func newSuspensionEnv(t *testing.T) *suspensionEnv {
	t.Helper()
	env := &suspensionEnv{
		store:              newContainerStore(),
		builds:             coremocks.NewWorkloadSuspensionBuildRepository(t),
		buildCanceler:      coremocks.NewWorkloadSuspensionBuildCanceler(t),
		deployments:        coremocks.NewWorkloadSuspensionDeploymentRepository(t),
		deploymentCanceler: coremocks.NewWorkloadSuspensionDeploymentCanceler(t),
	}
	svc, err := service.NewWorkloadSuspension(service.WorkloadSuspensionDeps{
		Containers:         env.store,
		Builds:             env.builds,
		BuildCanceler:      env.buildCanceler,
		Deployments:        env.deployments,
		DeploymentCanceler: env.deploymentCanceler,
		Logger:             slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)
	env.svc = svc
	return env
}

func TestNewWorkloadSuspensionRejectsMissingDependencies(t *testing.T) {
	valid := func(t *testing.T) service.WorkloadSuspensionDeps {
		return service.WorkloadSuspensionDeps{
			Containers:         newContainerStore(),
			Builds:             coremocks.NewWorkloadSuspensionBuildRepository(t),
			BuildCanceler:      coremocks.NewWorkloadSuspensionBuildCanceler(t),
			Deployments:        coremocks.NewWorkloadSuspensionDeploymentRepository(t),
			DeploymentCanceler: coremocks.NewWorkloadSuspensionDeploymentCanceler(t),
		}
	}
	depstest.RequireRejectsMissing(t, valid, service.NewWorkloadSuspension, map[string]string{
		"Containers":         "containers",
		"Builds":             "builds",
		"BuildCanceler":      "build canceler",
		"Deployments":        "deployments",
		"DeploymentCanceler": "deployment canceler",
	})
}

// noBuildsOrDeployments scripts an owner without builds or deployments.
func (env *suspensionEnv) noBuildsOrDeployments(ownerID uuid.UUID) {
	env.builds.EXPECT().ListActiveByOwner(mock.Anything, ownerID).Return(nil, nil)
	env.deployments.EXPECT().ListCancelableComposeDeploymentJobsByOwner(mock.Anything, ownerID).Return(nil, nil)
}

func suspensionAdminContext() context.Context {
	return accessscope.WithAdminScope(context.Background(), uuid.New(), "root", "admin")
}

// ownedContainer records a container of ownerID with a Docker container.
func ownedContainer(t *testing.T, store *containerStore, ownerID uuid.UUID, status, desired string) model.Container {
	t.Helper()
	c := model.Container{
		ID:                    uuid.New(),
		OwnerID:               ownerID,
		Name:                  "web",
		ImageTag:              "nginx:latest",
		DockerID:              "docker-" + uuid.NewString()[:12],
		DockerGeneration:      1,
		Status:                status,
		DesiredStatus:         desired,
		BaseMemoryReservation: 256 << 20,
		BaseCPUReservation:    250,
	}
	saveContainer(t, store, c)
	return c
}

// queuedStops reads back every queued lifecycle message, keyed by container.
func queuedStops(t *testing.T, store *containerStore) map[uuid.UUID]queue.ContainerLifecycleMessage {
	t.Helper()
	outbox, err := store.LeasePendingContainerOutbox(context.Background(), 100)
	require.NoError(t, err)
	stops := make(map[uuid.UUID]queue.ContainerLifecycleMessage, len(outbox))
	for _, item := range outbox {
		var msg queue.ContainerLifecycleMessage
		require.NoError(t, json.Unmarshal(item.Payload, &msg))
		stops[item.ContainerID] = msg
		require.NoError(t, store.MarkContainerOutboxPublished(context.Background(), item.ID))
	}
	return stops
}

func TestSuspendingAnOwnerQueuesADeactivationStopForEachContainerThatReservesCapacity(t *testing.T) {
	env := newSuspensionEnv(t)
	ownerID := uuid.New()
	running := ownedContainer(t, env.store, ownerID, model.ContainerStatusRunning, model.ContainerStatusRunning)
	crashed := ownedContainer(t, env.store, ownerID, model.ContainerStatusError, model.ContainerStatusRunning)
	stopped := ownedContainer(t, env.store, ownerID, model.ContainerStatusExited, model.ContainerStatusExited)
	other := ownedContainer(t, env.store, uuid.New(), model.ContainerStatusRunning, model.ContainerStatusRunning)
	env.noBuildsOrDeployments(ownerID)

	got, err := env.svc.SuspendUserWorkloads(suspensionAdminContext(), ownerID)

	require.NoError(t, err)
	require.Equal(t, model.WorkloadSuspension{StoppedContainers: 2}, got)
	stops := queuedStops(t, env.store)
	require.Len(t, stops, 2, "neither the stopped container nor another owner's is stopped")
	for _, c := range []model.Container{running, crashed} {
		msg, ok := stops[c.ID]
		require.True(t, ok, "container %s is stopped", c.ID)
		require.Equal(t, model.OperationStop, msg.Operation)
		require.Equal(t, service.AccountDeactivatedStopReason, msg.Reason)
		require.Equal(t, c.Status, msg.PreviousStatus)
		row, err := env.store.GetByID(context.Background(), c.ID)
		require.NoError(t, err)
		require.Equal(t, model.ContainerStatusStopping, row.Status)
		require.Equal(t, model.ContainerStatusExited, row.DesiredStatus)
	}
	for _, c := range []model.Container{stopped, other} {
		row, err := env.store.GetByID(context.Background(), c.ID)
		require.NoError(t, err)
		require.Equal(t, c.Status, row.Status, "container %s is untouched", c.ID)
	}
}

func TestSuspendingSkipsAContainerWithAnActiveOperationUntilItFinishes(t *testing.T) {
	ctx := context.Background()
	env := newSuspensionEnv(t)
	ownerID := uuid.New()
	running := ownedContainer(t, env.store, ownerID, model.ContainerStatusRunning, model.ContainerStatusRunning)
	starting := ownedContainer(t, env.store, ownerID, model.ContainerStatusExited, model.ContainerStatusExited)
	start := model.ResourceOperation{ID: uuid.New(), ResourceType: model.ResourceTypeContainer, ResourceID: starting.ID, OwnerID: ownerID, Operation: model.OperationStart, Status: model.OperationStatusPending}
	require.NoError(t, env.store.QueueContainerOperation(ctx, starting.ID, model.ContainerStatusStarting, model.ContainerStatusRunning, start,
		model.ContainerLifecycleOutbox{ID: uuid.New(), OperationID: start.ID, ContainerID: starting.ID, Payload: []byte(`{}`)}))
	require.Len(t, queuedStops(t, env.store), 1, "only the start is queued")
	env.builds.EXPECT().ListActiveByOwner(mock.Anything, ownerID).Return(nil, nil).Twice()
	env.deployments.EXPECT().ListCancelableComposeDeploymentJobsByOwner(mock.Anything, ownerID).Return(nil, nil).Twice()

	first, err := env.svc.SuspendUserWorkloads(suspensionAdminContext(), ownerID)

	require.NoError(t, err)
	require.Equal(t, model.WorkloadSuspension{StoppedContainers: 1, SkippedContainers: 1}, first)
	stops := queuedStops(t, env.store)
	require.Len(t, stops, 1)
	require.Contains(t, stops, running.ID)

	// The start finishes; the second call stops the started container and
	// leaves the one already stopping alone.
	_, claimed, err := env.store.ClaimPendingOperation(ctx, start.ID, 0)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, env.store.UpdateStatus(ctx, starting.ID, model.ContainerStatusRunning))
	require.NoError(t, env.store.CompleteOperation(ctx, start.ID, model.OperationStatusDone, nil))

	second, err := env.svc.SuspendUserWorkloads(suspensionAdminContext(), ownerID)

	require.NoError(t, err)
	require.Equal(t, model.WorkloadSuspension{StoppedContainers: 1}, second)
	stops = queuedStops(t, env.store)
	require.Len(t, stops, 1, "nothing is stopped twice")
	require.Equal(t, service.AccountDeactivatedStopReason, stops[starting.ID].Reason)
}

func TestSuspendingCountsAFailedStopAndGoesOn(t *testing.T) {
	env := newSuspensionEnv(t)
	ownerID := uuid.New()
	ownedContainer(t, env.store, ownerID, model.ContainerStatusRunning, model.ContainerStatusRunning)
	ownedContainer(t, env.store, ownerID, model.ContainerStatusRunning, model.ContainerStatusRunning)
	env.noBuildsOrDeployments(ownerID)
	env.store.SetFaults(coretest.ContainerStoreFaults{QueueContainerOperation: coretest.FailNext(1, errStoreWrite)})

	got, err := env.svc.SuspendUserWorkloads(suspensionAdminContext(), ownerID)

	require.NoError(t, err)
	require.Equal(t, model.WorkloadSuspension{StoppedContainers: 1, Failed: 1}, got)
	require.Len(t, queuedStops(t, env.store), 1)
}

func TestSuspendingCancelsTheOwnersActiveBuildsAndDeployments(t *testing.T) {
	env := newSuspensionEnv(t)
	ownerID := uuid.New()
	pending := model.Build{ID: uuid.New(), OwnerID: ownerID, Status: model.BuildStatusPending}
	running := model.Build{ID: uuid.New(), OwnerID: ownerID, Status: model.BuildStatusRunning}
	broken := model.Build{ID: uuid.New(), OwnerID: ownerID, Status: model.BuildStatusRunning}
	finishedMeanwhile := model.Build{ID: uuid.New(), OwnerID: ownerID, Status: model.BuildStatusPending}
	deploying := model.ComposeDeploymentJob{ID: uuid.New(), ProjectID: uuid.New(), OwnerID: ownerID, Status: model.ComposeDeploymentStatusRunning}
	finishedDeployment := model.ComposeDeploymentJob{ID: uuid.New(), ProjectID: uuid.New(), OwnerID: ownerID, Status: model.ComposeDeploymentStatusQueued}
	env.deployments.EXPECT().ListCancelableComposeDeploymentJobsByOwner(mock.Anything, ownerID).Return([]model.ComposeDeploymentJob{deploying, finishedDeployment}, nil)
	env.deploymentCanceler.EXPECT().CancelDeployment(mock.Anything, deploying.ProjectID).Return(nil).Once()
	// A deployment that ended after it was listed is no longer cancelable.
	env.deploymentCanceler.EXPECT().CancelDeployment(mock.Anything, finishedDeployment.ProjectID).
		Return(apperrors.New(apperrors.ErrConflict, "compose deployment is not active")).Once()
	env.builds.EXPECT().ListActiveByOwner(mock.Anything, ownerID).Return([]model.Build{pending, running, broken, finishedMeanwhile}, nil)
	env.buildCanceler.EXPECT().CancelBuildRecord(mock.Anything, pending.ID).Return(nil).Once()
	env.buildCanceler.EXPECT().CancelBuildRecord(mock.Anything, running.ID).Return(nil).Once()
	env.buildCanceler.EXPECT().CancelBuildRecord(mock.Anything, broken.ID).Return(errors.New("registry unavailable")).Once()
	env.buildCanceler.EXPECT().CancelBuildRecord(mock.Anything, finishedMeanwhile.ID).Return(apperrors.ErrNotFound).Once()

	got, err := env.svc.SuspendUserWorkloads(suspensionAdminContext(), ownerID)

	require.NoError(t, err)
	require.Equal(t, model.WorkloadSuspension{CanceledBuilds: 2, CanceledDeployments: 1, Failed: 1}, got)
}

func TestSuspendingFailsWhenAWorkloadListCannotBeRead(t *testing.T) {
	env := newSuspensionEnv(t)
	ownerID := uuid.New()
	running := ownedContainer(t, env.store, ownerID, model.ContainerStatusRunning, model.ContainerStatusRunning)
	env.deployments.EXPECT().ListCancelableComposeDeploymentJobsByOwner(mock.Anything, ownerID).Return(nil, errStoreWrite)
	env.builds.EXPECT().ListActiveByOwner(mock.Anything, ownerID).Return(nil, nil)

	_, err := env.svc.SuspendUserWorkloads(suspensionAdminContext(), ownerID)

	require.ErrorIs(t, err, apperrors.ErrUnavailable)
	require.NotContains(t, apperrors.SafeMessage(err), errStoreWrite.Error())
	// The other workloads are still stopped; repeating finishes the rest.
	require.Contains(t, queuedStops(t, env.store), running.ID)
}

func TestSuspendingRequiresAdminScope(t *testing.T) {
	ownerID := uuid.New()
	for name, ctx := range map[string]context.Context{
		"user":     accessscope.WithUserScope(context.Background(), ownerID, "alice", "user"),
		"system":   accessscope.WithSystemScope(context.Background()),
		"no scope": context.Background(),
	} {
		t.Run(name, func(t *testing.T) {
			env := newSuspensionEnv(t)
			ownedContainer(t, env.store, ownerID, model.ContainerStatusRunning, model.ContainerStatusRunning)

			_, err := env.svc.SuspendUserWorkloads(ctx, ownerID)

			require.Error(t, err)
			require.True(t, errors.Is(err, apperrors.ErrForbidden) || errors.Is(err, apperrors.ErrUnauthorized), err)
			require.Empty(t, queuedStops(t, env.store))
		})
	}
}

// The queued stop records the deactivation reason; the owner's next start
// clears it.
func TestDeactivationStopRecordsItsReasonUntilTheNextStart(t *testing.T) {
	ctx := context.Background()
	env := newSuspensionEnv(t)
	ownerID := uuid.New()
	c := ownedContainer(t, env.store, ownerID, model.ContainerStatusRunning, model.ContainerStatusRunning)
	env.noBuildsOrDeployments(ownerID)
	_, err := env.svc.SuspendUserWorkloads(suspensionAdminContext(), ownerID)
	require.NoError(t, err)
	msg := queuedStops(t, env.store)[c.ID]

	w := roomyAdmissionWorld()
	cfg := coretest.StaticConfig{Config: w.config()}
	docker := coremocks.NewContainerDockerAPI(t)
	docker.EXPECT().StopContainer(mock.Anything, c.DockerID, mock.Anything).Return(nil).Once()
	images := coremocks.NewContainerImageRepository(t)
	images.EXPECT().GetUserUsedDiskSpace(mock.Anything, ownerID).Return(int64(0), nil).Maybe()
	images.EXPECT().List(mock.Anything, mock.Anything).Return(nil, 0, nil).Maybe()
	builds := coremocks.NewActiveSystemBuildCounter(t)
	builds.EXPECT().CountActive(mock.Anything).Return(0, nil).Maybe()
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:  env.store,
		Images:      images,
		Docker:      docker,
		Metrics:     admissionHostMetrics(t),
		DiskMetrics: admissionDiskMetrics(t, w),
		Config:      cfg,
		Quotas:      w.quotas(t, ownerID, cfg),
		Builds:      builds,
		Logger:      slog.New(slog.DiscardHandler),
	})
	require.NoError(t, service.NewContainerCreateWorker(svc, cfg, slog.New(slog.DiscardHandler)).HandleMessage(ctx, msg))

	stopped, err := env.store.GetByID(ctx, c.ID)
	require.NoError(t, err)
	require.Equal(t, model.ContainerStatusExited, stopped.Status)
	require.NotNil(t, stopped.LastError)
	require.Equal(t, service.AccountDeactivatedStopReason, *stopped.LastError)

	require.NoError(t, svc.Start(accessscope.WithAdminScope(ctx, uuid.New(), "root", "admin"), c.ID))

	started, err := env.store.GetByID(ctx, c.ID)
	require.NoError(t, err)
	require.Equal(t, model.ContainerStatusStarting, started.Status)
	require.Nil(t, started.LastError)
}
