package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/platform/logging"
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
	logger    *slog.Logger
}

func NewTTLWorker(repo TTLContainerRepository, dockerAPI TTLDockerAPI, interval time.Duration, logger *slog.Logger) *TTLWorker {
	return &TTLWorker{
		repo:      repo,
		dockerAPI: dockerAPI,
		interval:  interval,
		logger:    logging.WithComponent(logger, "ttl_worker"),
	}
}

func (w *TTLWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "ttl worker started", "interval", w.interval.String())
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "ttl worker stopped")
			return
		case <-ticker.C:
			w.processExpired(ctx)
		}
	}
}

func (w *TTLWorker) processExpired(ctx context.Context) {
	expiredContainers, err := w.repo.GetExpired(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to fetch expired containers", "error", err)
		return
	}

	for _, c := range expiredContainers {
		if err := w.dockerAPI.StopContainer(ctx, c.DockerID, 10); err != nil {
			w.logger.ErrorContext(ctx, "failed to stop expired container", "container_id", c.ID, "docker_id", c.DockerID, "error", err)
			continue
		}
		w.logger.InfoContext(ctx, "expired container stopped", "container_id", c.ID, "docker_id", c.DockerID)
	}
}
