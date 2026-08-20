package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type ComposeOutboxRepository interface {
	LeasePendingComposeOutbox(ctx context.Context, limit int) ([]model.ComposeDeploymentOutbox, error)
	GetComposeDeploymentJob(ctx context.Context, id uuid.UUID) (model.ComposeDeploymentJob, error)
	MarkComposeOutboxPublished(ctx context.Context, id uuid.UUID) error
	MarkComposeOutboxPending(ctx context.Context, id uuid.UUID, cause error) error
	MarkComposeOutboxDiscarded(ctx context.Context, id uuid.UUID, cause error) error
}

type ComposeOutboxConfigProvider interface {
	Get() config.SystemConfig
}

type ComposeOutboxWorker struct {
	repo      ComposeOutboxRepository
	publisher BuildQueuePublisher
	cfg       ComposeOutboxConfigProvider
	logger    *slog.Logger
}

func NewComposeOutboxWorker(repo ComposeOutboxRepository, publisher BuildQueuePublisher, cfg ComposeOutboxConfigProvider, logger *slog.Logger) *ComposeOutboxWorker {
	return &ComposeOutboxWorker{
		repo:      repo,
		publisher: publisher,
		cfg:       cfg,
		logger:    logging.WithComponent(logger, "compose_outbox_worker"),
	}
}

func (w *ComposeOutboxWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "compose outbox worker started")
	w.publishBatch(ctx)
	for {
		interval := time.Duration(w.cfg.Get().ComposeOutboxIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = time.Second
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			w.logger.InfoContext(ctx, "compose outbox worker stopped")
			return
		case <-timer.C:
			w.publishBatch(ctx)
		}
	}
}

func (w *ComposeOutboxWorker) publishBatch(ctx context.Context) {
	items, err := w.repo.LeasePendingComposeOutbox(ctx, w.cfg.Get().ComposeOutboxBatchSize)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to lease compose outbox messages", "error", err)
		return
	}
	for _, item := range items {
		job, err := w.repo.GetComposeDeploymentJob(ctx, item.JobID)
		if err == nil && model.IsComposeDeploymentTerminalStatus(job.Status) {
			_ = w.repo.MarkComposeOutboxDiscarded(ctx, item.ID, nil)
			continue
		}
		if err != nil {
			if errors.Is(err, apperrors.ErrNotFound) {
				_ = w.repo.MarkComposeOutboxDiscarded(ctx, item.ID, err)
				continue
			}
			w.logger.ErrorContext(ctx, "failed to fetch compose deployment job for outbox message", "job_id", item.JobID, "outbox_id", item.ID, "error", err)
			_ = w.repo.MarkComposeOutboxPending(ctx, item.ID, err)
			continue
		}
		if err := w.publisher.Publish(ctx, item.Exchange, item.RoutingKey, item.Payload); err != nil {
			w.logger.ErrorContext(ctx, "failed to publish compose queue message", "job_id", item.JobID, "outbox_id", item.ID, "error", err)
			_ = w.repo.MarkComposeOutboxPending(ctx, item.ID, err)
			continue
		}
		if err := w.repo.MarkComposeOutboxPublished(ctx, item.ID); err != nil {
			w.logger.ErrorContext(ctx, "failed to mark compose outbox message published", "job_id", item.JobID, "outbox_id", item.ID, "error", err)
		}
	}
}
