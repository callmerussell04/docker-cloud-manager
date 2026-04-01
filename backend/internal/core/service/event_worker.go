package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/docker/docker/api/types/events"
)

type EventContainerRepo interface {
	UpdateStatusByDockerID(ctx context.Context, dockerID string, status string) error
}

type EventDockerAPI interface {
	ListenEvents(ctx context.Context) (<-chan events.Message, <-chan error)
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

func (w *EventWorker) Run(ctx context.Context) {
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
