package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/google/uuid"
)

type EventContainerRepo interface {
	UpdateStatusByDockerID(ctx context.Context, dockerID string, status string) error
	GetNonExited(ctx context.Context) ([]domain.Container, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
}

type EventDockerAPI interface {
	ListenEvents(ctx context.Context) (<-chan events.Message, <-chan error)
	InspectContainer(ctx context.Context, dockerID string) (*container.InspectResponse, error)
}

type ContainerRebalancer interface {
	RebalanceResources(ctx context.Context)
}

type EventWorker struct {
	repo       EventContainerRepo
	dockerAPI  EventDockerAPI
	rebalancer ContainerRebalancer
}

func NewEventWorker(repo EventContainerRepo, dockerAPI EventDockerAPI, rebalancer ContainerRebalancer) *EventWorker {
	return &EventWorker{
		repo:       repo,
		dockerAPI:  dockerAPI,
		rebalancer: rebalancer,
	}
}

// TODO: maybe update so that it also manually checks the state once per some time
func (w *EventWorker) Run(ctx context.Context) {
	w.syncState(ctx)

	msgCh, errCh := w.dockerAPI.ListenEvents(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-errCh:
			continue
		case msg := <-msgCh:
			if msg.Type == "container" {
				switch msg.Action {
				case "start":
					_ = w.repo.UpdateStatusByDockerID(ctx, msg.Actor.ID, domain.ContainerStatusRunning)
					go w.rebalancer.RebalanceResources(context.Background())
				case "die", "stop", "kill", "oom":
					_ = w.repo.UpdateStatusByDockerID(ctx, msg.Actor.ID, domain.ContainerStatusExited)
					go w.rebalancer.RebalanceResources(context.Background())
				}
			}
		}
	}
}

func (w *EventWorker) syncState(ctx context.Context) {
	containers, err := w.repo.GetNonExited(ctx)
	if err != nil {
		return
	}

	changed := false

	for _, c := range containers {
		if c.DockerID == "" {
			_ = w.repo.UpdateStatus(ctx, c.ID, domain.ContainerStatusError)
			continue
		}

		inspect, err := w.dockerAPI.InspectContainer(ctx, c.DockerID)
		if err != nil {
			if cerrdefs.IsNotFound(err) {
				_ = w.repo.UpdateStatus(ctx, c.ID, domain.ContainerStatusExited)
				changed = true
			}
			continue
		}

		expectedStatus := domain.ContainerStatusExited
		if inspect.State.Running {
			expectedStatus = domain.ContainerStatusRunning
		} else if c.Status == domain.ContainerStatusCreated && inspect.State.Status == "created" {
			expectedStatus = domain.ContainerStatusCreated
		}

		if c.Status != expectedStatus {
			_ = w.repo.UpdateStatus(ctx, c.ID, expectedStatus)
			if expectedStatus == domain.ContainerStatusRunning || c.Status == domain.ContainerStatusRunning {
				changed = true
			}
		}
	}

	if changed {
		go w.rebalancer.RebalanceResources(context.Background())
	}
}
