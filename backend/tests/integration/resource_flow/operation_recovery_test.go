package resource_flow_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errCompletionWrite = errors.New("completion write failed")

// failingCompletion is Core's container repository whose operation
// completion write always fails, as when PostgreSQL is unreachable right
// after a worker finished its Docker step.
type failingCompletion struct {
	*repository.ContainerRepository
}

func (failingCompletion) CompleteOperation(context.Context, uuid.UUID, string, error) error {
	return errCompletionWrite
}

// newFailingCompletionWorker is the fixture's container lifecycle worker over
// failingCompletion.
func newFailingCompletionWorker(t *testing.T, f *coretest.ComposeFixture) *service.ContainerCreateWorker {
	t.Helper()
	hostMetrics := coremocks.NewHostMetricsProvider(t)
	hostMetrics.EXPECT().GetTotalMemory().Return(int64(1<<40), nil).Maybe()
	hostMetrics.EXPECT().GetLogicalCPUs().Return(int64(64), nil).Maybe()
	diskMetrics := coremocks.NewHostDiskMetricsProvider(t)
	diskMetrics.EXPECT().GetDiskUsage(mock.Anything).Return(model.HostDiskStats{TotalBytes: 1 << 41, FreeBytes: 1 << 40}, nil).Maybe()
	containers, err := service.NewContainerService(service.ContainerServiceDeps{
		Repository:   failingCompletion{ContainerRepository: f.Repos.Containers},
		Volumes:      f.Repos.Volumes,
		Images:       f.Repos.Images,
		Docker:       f.ContainerDocker,
		Metrics:      hostMetrics,
		DiskMetrics:  diskMetrics,
		Config:       f.Config,
		Quotas:       f.Repos.UserQuotas(t, f.Config),
		Builds:       f.Repos.Builds,
		HostDiskPath: "/",
		Logger:       coretest.DiscardLogger(),
	})
	require.NoError(t, err)
	return service.NewContainerCreateWorker(containers, f.Config, coretest.DiscardLogger())
}

// deliverContainerMessages plays the relay and broker for every pending
// container message, handing each to worker, and returns the handler errors.
func deliverContainerMessages(t *testing.T, f *coretest.ComposeFixture, worker *service.ContainerCreateWorker) []error {
	t.Helper()
	ctx := context.Background()
	items, err := f.Repos.Containers.LeasePendingContainerOutbox(ctx, 10)
	require.NoError(t, err)
	require.NotEmpty(t, items)
	var errs []error
	for _, item := range items {
		require.NoError(t, f.Repos.Containers.MarkContainerOutboxPublished(ctx, item.ID))
		var msg queue.ContainerLifecycleMessage
		require.NoError(t, json.Unmarshal(item.Payload, &msg))
		errs = append(errs, worker.HandleMessage(ctx, msg))
	}
	return errs
}

// loseContainerMessages plays a relay that published every pending container
// message and a broker that dead-lettered them, and ages the records past the
// lost-delivery bound.
func loseContainerMessages(t *testing.T, f *coretest.ComposeFixture) {
	t.Helper()
	ctx := context.Background()
	items, err := f.Repos.Containers.LeasePendingContainerOutbox(ctx, 10)
	require.NoError(t, err)
	require.NotEmpty(t, items)
	for _, item := range items {
		require.NoError(t, f.Repos.Containers.MarkContainerOutboxPublished(ctx, item.ID))
	}
	_, err = f.Repos.DB.Exec(ctx, `UPDATE container_lifecycle_queue_outbox SET updated_at = NOW() - INTERVAL '6 minutes'`)
	require.NoError(t, err)
}

// expireRunningOperations ages every running operation past the default
// expiry: the 30-minute handler deadline plus five minutes of headroom.
func expireRunningOperations(t *testing.T, f *coretest.ComposeFixture) {
	t.Helper()
	_, err := f.Repos.DB.Exec(context.Background(), `UPDATE resource_operations SET updated_at = NOW() - INTERVAL '36 minutes' WHERE status = 'running'`)
	require.NoError(t, err)
}

func activeOperation(t *testing.T, f *coretest.ComposeFixture, containerID uuid.UUID) model.ResourceOperation {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, f.Repos.DB.QueryRow(context.Background(), `SELECT id FROM resource_operations WHERE resource_id = $1 ORDER BY created_at DESC LIMIT 1`, containerID).Scan(&id))
	op, err := f.Repos.Containers.GetOperationByID(context.Background(), id)
	require.NoError(t, err)
	return op
}

func TestContainerLeftRunningByAFailedCompletionWriteIsUsableAfterRecovery(t *testing.T) {
	ctx := context.Background()
	f := coretest.NewComposeFixture(t, coretest.IntegrationConfig())
	f.ContainerDocker.EXPECT().EnsureUserNetwork(mock.Anything, mock.Anything).Return("network", nil).Once()
	f.ContainerDocker.EXPECT().ImageExists(mock.Anything, "nginx:latest").Return(true, nil).Once()
	f.ContainerDocker.EXPECT().CreateContainer(mock.Anything, mock.Anything).Return("docker-web", nil).Once()
	containerID, err := f.Containers.Create(f.OwnerContext(), model.ContainerCreateParams{Name: "web", ImageTag: "nginx:latest"})
	require.NoError(t, err)

	// The create's Docker work and record succeed; its completion write fails.
	errs := deliverContainerMessages(t, f, newFailingCompletionWorker(t, f))
	require.Len(t, errs, 1)
	require.ErrorIs(t, errs[0], errCompletionWrite)
	create := activeOperation(t, f, containerID)
	require.Equal(t, model.OperationStatusRunning, create.Status)
	require.ErrorIs(t, f.Containers.Start(f.OwnerContext(), containerID), apperrors.ErrConflict, "the stuck create locks the container")

	expireRunningOperations(t, f)
	f.RecoverOperations(t)

	// The create is delivered again and completes without Docker work.
	require.Equal(t, model.OperationStatusPending, activeOperation(t, f, containerID).Status)
	require.Equal(t, 1, f.RunContainerOperations(t))
	create, err = f.Repos.Containers.GetOperationByID(ctx, create.ID)
	require.NoError(t, err)
	require.Equal(t, model.OperationStatusDone, create.Status)

	// The container accepts start, stop and delete again.
	f.ContainerDocker.EXPECT().UpdateContainerResources(mock.Anything, "docker-web", mock.Anything).Return(nil).Once()
	f.ContainerDocker.EXPECT().StartContainer(mock.Anything, "docker-web").Return(nil).Once()
	require.NoError(t, f.Containers.Start(f.OwnerContext(), containerID))
	require.Equal(t, 1, f.RunContainerOperations(t))
	f.ContainerDocker.EXPECT().StopContainer(mock.Anything, "docker-web", mock.Anything).Return(nil).Once()
	require.NoError(t, f.Containers.Stop(f.OwnerContext(), containerID))
	require.Equal(t, 1, f.RunContainerOperations(t))
	f.ContainerDocker.EXPECT().InspectContainer(mock.Anything, "docker-web").Return(model.ContainerInspection{}, nil).Once()
	f.ContainerDocker.EXPECT().RemoveContainer(mock.Anything, "docker-web", true).Return(nil).Once()
	f.ContainerDocker.EXPECT().RemoveNetwork(mock.Anything, mock.Anything).Return(nil).Maybe()
	require.NoError(t, f.Containers.Delete(f.OwnerContext(), containerID))
	require.Equal(t, 1, f.RunContainerOperations(t))
	_, err = f.Repos.Containers.GetByID(ctx, containerID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

func TestContainerStartWhoseMessageWasDeadLetteredRunsAfterRecovery(t *testing.T) {
	ctx := context.Background()
	f := coretest.NewComposeFixture(t, coretest.IntegrationConfig())
	c := model.Container{
		ID: uuid.New(), OwnerID: f.OwnerID, DockerID: "docker-web", Name: "web", ImageTag: "nginx:latest",
		Status: model.ContainerStatusExited, DesiredStatus: model.ContainerStatusExited, EnvVars: []byte(`{}`),
		BaseMemoryReservation: 256 << 20, BaseCPUReservation: 250, DockerGeneration: 1,
	}
	require.NoError(t, f.Repos.Containers.Save(ctx, c))
	require.NoError(t, f.Containers.Start(f.OwnerContext(), c.ID))
	loseContainerMessages(t, f)

	// Nothing publishes it again: the start stays pending and the container locked.
	require.Equal(t, model.OperationStatusPending, activeOperation(t, f, c.ID).Status)
	pending, err := f.Repos.Containers.LeasePendingContainerOutbox(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, pending)
	require.ErrorIs(t, f.Containers.Stop(f.OwnerContext(), c.ID), apperrors.ErrConflict)

	f.RecoverOperations(t)

	f.ContainerDocker.EXPECT().UpdateContainerResources(mock.Anything, "docker-web", mock.Anything).Return(nil).Once()
	f.ContainerDocker.EXPECT().StartContainer(mock.Anything, "docker-web").Return(nil).Once()
	require.Equal(t, 1, f.RunContainerOperations(t))
	require.Equal(t, model.OperationStatusDone, activeOperation(t, f, c.ID).Status)
	got, err := f.Repos.Containers.GetByID(ctx, c.ID)
	require.NoError(t, err)
	require.Equal(t, model.ContainerStatusRunning, got.Status)
}
