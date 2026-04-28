package service

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
)

func TestEventWorkerClosedStreamBacksOff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dockerAPI := &closedEventDockerFake{}
	worker := NewEventWorker(&eventRepoFake{}, nil, dockerAPI, &eventRebalancerFake{}, nil, staticConfig{}, slog.Default())

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

	if calls := dockerAPI.calls.Load(); calls > 2 {
		t.Fatalf("ListenEvents calls = %d, want at most 2", calls)
	}
}

func TestEventWorkerMarksMissingContainerOnSync(t *testing.T) {
	ctx := context.Background()
	containerID := uuid.New()
	repo := &eventRepoFake{
		containers: []model.Container{{
			ID:       containerID,
			DockerID: "missing-docker",
			Status:   model.ContainerStatusRunning,
		}},
	}
	dockerAPI := &closedEventDockerFake{inspectContainerErr: cerrdefs.ErrNotFound}
	worker := NewEventWorker(repo, nil, dockerAPI, &eventRebalancerFake{}, nil, staticConfig{}, slog.Default())

	worker.syncState(ctx)

	if got := repo.markedStatus[containerID]; got != model.ContainerStatusMissing {
		t.Fatalf("marked status = %q, want %q", got, model.ContainerStatusMissing)
	}
}

func TestEventWorkerMarksMissingVolumeOnSync(t *testing.T) {
	ctx := context.Background()
	volumeID := uuid.New()
	volumeRepo := &eventVolumeRepoFake{
		volumes: []model.Volume{{
			ID:         volumeID,
			DockerName: "missing-volume",
			Status:     model.VolumeStatusAvailable,
		}},
	}
	dockerAPI := &closedEventDockerFake{inspectVolumeErr: cerrdefs.ErrNotFound}
	worker := NewEventWorker(&eventRepoFake{}, volumeRepo, dockerAPI, &eventRebalancerFake{}, nil, staticConfig{}, slog.Default())

	worker.syncState(ctx)

	if got := volumeRepo.markedStatus[volumeID]; got != model.VolumeStatusMissing {
		t.Fatalf("marked status = %q, want %q", got, model.VolumeStatusMissing)
	}
}

type eventRepoFake struct {
	containers   []model.Container
	markedStatus map[uuid.UUID]string
}

func (f *eventRepoFake) UpdateStatusByDockerID(ctx context.Context, dockerID string, status string) error {
	return nil
}

func (f *eventRepoFake) GetByDockerID(ctx context.Context, dockerID string) (model.Container, error) {
	return model.Container{}, nil
}

func (f *eventRepoFake) GetNonExited(ctx context.Context) ([]model.Container, error) {
	return f.containers, nil
}

func (f *eventRepoFake) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	return nil
}

func (f *eventRepoFake) MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error {
	if f.markedStatus == nil {
		f.markedStatus = make(map[uuid.UUID]string)
	}
	f.markedStatus[id] = status
	return nil
}

type closedEventDockerFake struct {
	calls               atomic.Int32
	inspectContainerErr error
	inspectVolumeErr    error
}

func (f *closedEventDockerFake) ListenEvents(ctx context.Context) (<-chan model.ContainerEvent, <-chan error) {
	f.calls.Add(1)
	eventCh := make(chan model.ContainerEvent)
	errCh := make(chan error)
	close(eventCh)
	close(errCh)
	return eventCh, errCh
}

func (f *closedEventDockerFake) InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error) {
	return model.ContainerInspection{}, f.inspectContainerErr
}

func (f *closedEventDockerFake) InspectVolume(ctx context.Context, volumeName string) (model.VolumeInspection, error) {
	return model.VolumeInspection{}, f.inspectVolumeErr
}

type eventVolumeRepoFake struct {
	volumes      []model.Volume
	markedStatus map[uuid.UUID]string
}

func (f *eventVolumeRepoFake) GetReconcileCandidates(ctx context.Context) ([]model.Volume, error) {
	return f.volumes, nil
}

func (f *eventVolumeRepoFake) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	return nil
}

func (f *eventVolumeRepoFake) MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error {
	if f.markedStatus == nil {
		f.markedStatus = make(map[uuid.UUID]string)
	}
	f.markedStatus[id] = status
	return nil
}

type eventRebalancerFake struct {
	requests atomic.Int32
}

func (f *eventRebalancerFake) RequestRebalance() {
	f.requests.Add(1)
}
