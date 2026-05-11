package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
)

type TTLContainerRepository interface {
	GetExpired(ctx context.Context) ([]model.Container, error)
	SetDesiredStatus(ctx context.Context, id uuid.UUID, desiredStatus string) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error
}

type TTLDockerAPI interface {
	StopContainer(ctx context.Context, dockerID string, timeout int) error
}

type TTLConfigProvider interface {
	Get() config.SystemConfig
}

type TTLWorker struct {
	repo      TTLContainerRepository
	dockerAPI TTLDockerAPI
	cfg       TTLConfigProvider
	logger    *slog.Logger
}

func NewTTLWorker(repo TTLContainerRepository, dockerAPI TTLDockerAPI, cfg TTLConfigProvider, logger *slog.Logger) *TTLWorker {
	return &TTLWorker{
		repo:      repo,
		dockerAPI: dockerAPI,
		cfg:       cfg,
		logger:    logging.WithComponent(logger, "ttl_worker"),
	}
}

func (w *TTLWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "ttl worker started")

	for {
		interval := time.Duration(w.cfg.Get().TTLWorkerIntervalSeconds) * time.Second
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			w.logger.InfoContext(ctx, "ttl worker stopped")
			return
		case <-timer.C:
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

	stopTimeout := w.cfg.Get().ContainerStopTimeout
	for _, c := range expiredContainers {
		if err := w.repo.SetDesiredStatus(ctx, c.ID, model.ContainerStatusExited); err != nil {
			w.logger.WarnContext(ctx, "failed to update expired container desired status", "container_id", c.ID, "error", err)
		}
		if err := w.dockerAPI.StopContainer(ctx, c.DockerID, stopTimeout); err != nil {
			if errors.Is(err, cerrdefs.ErrNotFound) {
				_ = w.repo.MarkStatusError(ctx, c.ID, model.ContainerStatusMissing, err)
				continue
			}
			w.logger.ErrorContext(ctx, "failed to stop expired container", "container_id", c.ID, "docker_id", c.DockerID, "error", err)
			continue
		}
		if err := w.repo.UpdateStatus(ctx, c.ID, model.ContainerStatusExited); err != nil {
			w.logger.WarnContext(ctx, "failed to update expired container status", "container_id", c.ID, "error", err)
		}
		w.logger.InfoContext(ctx, "expired container stopped", "container_id", c.ID, "docker_id", c.DockerID)
	}
}
