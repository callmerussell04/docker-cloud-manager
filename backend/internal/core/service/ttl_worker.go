package service

import (
	"context"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
)

type TTLContainerRepository interface {
	GetExpired(ctx context.Context) ([]model.Container, error)
}

type TTLDockerAPI interface {
	StopContainer(ctx context.Context, dockerID string, timeout int) error
}

type TTLWorker struct {
	repo      TTLContainerRepository
	dockerAPI TTLDockerAPI
	interval  time.Duration
}

func NewTTLWorker(repo TTLContainerRepository, dockerAPI TTLDockerAPI, interval time.Duration) *TTLWorker {
	return &TTLWorker{
		repo:      repo,
		dockerAPI: dockerAPI,
		interval:  interval,
	}
}

func (w *TTLWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processExpired(ctx)
		}
	}
}

func (w *TTLWorker) processExpired(ctx context.Context) {
	expiredContainers, err := w.repo.GetExpired(ctx)
	if err != nil {
		return
	}

	for _, c := range expiredContainers {
		_ = w.dockerAPI.StopContainer(ctx, c.DockerID, 10)
	}
}
