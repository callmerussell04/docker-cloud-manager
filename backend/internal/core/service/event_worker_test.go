package service

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/google/uuid"
)

func TestEventWorkerClosedStreamBacksOff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dockerAPI := &closedEventDockerFake{}
	worker := NewEventWorker(&eventRepoFake{}, dockerAPI, &eventRebalancerFake{}, slog.Default())

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

type eventRepoFake struct{}

func (f *eventRepoFake) UpdateStatusByDockerID(ctx context.Context, dockerID string, status string) error {
	return nil
}

func (f *eventRepoFake) GetNonExited(ctx context.Context) ([]model.Container, error) {
	return nil, nil
}

func (f *eventRepoFake) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	return nil
}

type closedEventDockerFake struct {
	calls atomic.Int32
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
	return model.ContainerInspection{}, nil
}

type eventRebalancerFake struct {
	requests atomic.Int32
}

func (f *eventRebalancerFake) RequestRebalance() {
	f.requests.Add(1)
}
