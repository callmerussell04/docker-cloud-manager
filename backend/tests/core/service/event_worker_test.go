package service_test

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestEventWorkerClosedStreamBacksOff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := coremocks.NewEventContainerRepo(t)
	dockerAPI := coremocks.NewEventDockerAPI(t)
	rebalancer := coremocks.NewContainerRebalancer(t)
	cfg := coremocks.NewEventConfigProvider(t)
	var calls atomic.Int32

	repo.EXPECT().GetNonExited(mock.Anything).Return([]model.Container(nil), nil).Maybe()
	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	dockerAPI.EXPECT().ListenEvents(mock.Anything).RunAndReturn(func(ctx context.Context) (<-chan model.ContainerEvent, <-chan error) {
		calls.Add(1)
		eventCh := make(chan model.ContainerEvent)
		errCh := make(chan error)
		close(eventCh)
		close(errCh)
		return eventCh, errCh
	}).Maybe()
	worker := NewEventWorker(repo, nil, dockerAPI, rebalancer, nil, cfg, slog.Default())

	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event worker did not stop")
	}
	require.LessOrEqual(t, calls.Load(), int32(2))
}

func TestEventWorkerMarksMissingContainerOnSync(t *testing.T) {
	containerID := uuid.New()
	repo := coremocks.NewEventContainerRepo(t)
	dockerAPI := coremocks.NewEventDockerAPI(t)
	rebalancer := coremocks.NewContainerRebalancer(t)
	cfg := coremocks.NewEventConfigProvider(t)
	statusCh := make(chan string, 1)

	repo.EXPECT().GetNonExited(mock.Anything).Return([]model.Container{{
		ID:       containerID,
		DockerID: "missing-docker",
		Status:   model.ContainerStatusRunning,
	}}, nil)
	dockerAPI.EXPECT().InspectContainer(mock.Anything, "missing-docker").Return(model.ContainerInspection{}, cerrdefs.ErrNotFound)
	repo.EXPECT().MarkStatusError(mock.Anything, containerID, model.ContainerStatusMissing, mock.Anything).Run(func(ctx context.Context, id uuid.UUID, status string, cause error) {
		statusCh <- status
	}).Return(nil)
	rebalancer.EXPECT().RequestRebalance().Return().Maybe()
	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	dockerAPI.EXPECT().ListenEvents(mock.Anything).RunAndReturn(closedEventStreams).Maybe()
	worker := NewEventWorker(repo, nil, dockerAPI, rebalancer, nil, cfg, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Run(ctx)

	require.Equal(t, model.ContainerStatusMissing, waitForEventWorkerStatus(t, statusCh))
}

func TestEventWorkerSkipsCreatingContainerWithActiveOperation(t *testing.T) {
	containerID := uuid.New()
	repo := coremocks.NewEventContainerRepo(t)
	dockerAPI := coremocks.NewEventDockerAPI(t)
	rebalancer := coremocks.NewContainerRebalancer(t)
	cfg := coremocks.NewEventConfigProvider(t)

	repo.EXPECT().GetNonExited(mock.Anything).Return([]model.Container{{
		ID:     containerID,
		Status: model.ContainerStatusCreating,
	}}, nil)
	repo.EXPECT().HasActiveOperation(mock.Anything, model.ResourceTypeContainer, containerID).Return(true, nil)
	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	dockerAPI.EXPECT().ListenEvents(mock.Anything).RunAndReturn(closedEventStreams).Maybe()
	worker := NewEventWorker(repo, nil, dockerAPI, rebalancer, nil, cfg, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	go worker.Run(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()

	repo.AssertNotCalled(t, "MarkStatusError", mock.Anything, containerID, model.ContainerStatusMissing, mock.Anything)
	dockerAPI.AssertNotCalled(t, "InspectContainer", mock.Anything, mock.Anything)
}

func TestEventWorkerMarksCreatingContainerWithoutActiveOperationMissing(t *testing.T) {
	containerID := uuid.New()
	repo := coremocks.NewEventContainerRepo(t)
	dockerAPI := coremocks.NewEventDockerAPI(t)
	rebalancer := coremocks.NewContainerRebalancer(t)
	cfg := coremocks.NewEventConfigProvider(t)
	statusCh := make(chan string, 1)

	repo.EXPECT().GetNonExited(mock.Anything).Return([]model.Container{{
		ID:     containerID,
		Status: model.ContainerStatusCreating,
	}}, nil)
	repo.EXPECT().HasActiveOperation(mock.Anything, model.ResourceTypeContainer, containerID).Return(false, nil)
	repo.EXPECT().MarkStatusError(mock.Anything, containerID, model.ContainerStatusMissing, mock.Anything).Run(func(ctx context.Context, id uuid.UUID, status string, cause error) {
		statusCh <- status
	}).Return(nil)
	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	dockerAPI.EXPECT().ListenEvents(mock.Anything).RunAndReturn(closedEventStreams).Maybe()
	worker := NewEventWorker(repo, nil, dockerAPI, rebalancer, nil, cfg, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Run(ctx)

	require.Equal(t, model.ContainerStatusMissing, waitForEventWorkerStatus(t, statusCh))
}

func TestEventWorkerMarksMissingVolumeOnSync(t *testing.T) {
	volumeID := uuid.New()
	containerRepo := coremocks.NewEventContainerRepo(t)
	volumeRepo := coremocks.NewEventVolumeRepo(t)
	dockerAPI := coremocks.NewEventDockerAPI(t)
	rebalancer := coremocks.NewContainerRebalancer(t)
	cfg := coremocks.NewEventConfigProvider(t)
	statusCh := make(chan string, 1)

	containerRepo.EXPECT().GetNonExited(mock.Anything).Return([]model.Container(nil), nil)
	volumeRepo.EXPECT().GetReconcileCandidates(mock.Anything).Return([]model.Volume{{
		ID:         volumeID,
		DockerName: "missing-volume",
		Status:     model.VolumeStatusAvailable,
	}}, nil)
	dockerAPI.EXPECT().InspectVolume(mock.Anything, "missing-volume").Return(model.VolumeInspection{}, cerrdefs.ErrNotFound)
	volumeRepo.EXPECT().MarkStatusError(mock.Anything, volumeID, model.VolumeStatusMissing, mock.Anything).Run(func(ctx context.Context, id uuid.UUID, status string, cause error) {
		statusCh <- status
	}).Return(nil)
	cfg.EXPECT().Get().Return(staticConfig{}.Get()).Maybe()
	dockerAPI.EXPECT().ListenEvents(mock.Anything).RunAndReturn(closedEventStreams).Maybe()
	worker := NewEventWorker(containerRepo, volumeRepo, dockerAPI, rebalancer, nil, cfg, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Run(ctx)

	require.Equal(t, model.VolumeStatusMissing, waitForEventWorkerStatus(t, statusCh))
}

func waitForEventWorkerStatus(t *testing.T, statusCh <-chan string) string {
	t.Helper()
	select {
	case status := <-statusCh:
		return status
	case <-time.After(time.Second):
		t.Fatal("status was not updated")
		return ""
	}
}

func closedEventStreams(ctx context.Context) (<-chan model.ContainerEvent, <-chan error) {
	eventCh := make(chan model.ContainerEvent)
	errCh := make(chan error)
	close(eventCh)
	close(errCh)
	return eventCh, errCh
}
