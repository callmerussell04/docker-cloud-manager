package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
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
	repo  ComposeOutboxRepository
	cfg   ComposeOutboxConfigProvider
	relay outboxRelay
}

func NewComposeOutboxWorker(repo ComposeOutboxRepository, publisher BuildQueuePublisher, cfg ComposeOutboxConfigProvider, logger *slog.Logger) *ComposeOutboxWorker {
	return &ComposeOutboxWorker{
		repo:  repo,
		cfg:   cfg,
		relay: newOutboxRelay("compose", publisher, logger),
	}
}

func (w *ComposeOutboxWorker) Run(ctx context.Context) {
	w.relay.schedule(ctx, func() int64 { return w.cfg.Get().ComposeOutboxIntervalSeconds }, w.publishBatch)
}

func (w *ComposeOutboxWorker) publishBatch(ctx context.Context) {
	items, err := w.repo.LeasePendingComposeOutbox(ctx, w.cfg.Get().ComposeOutboxBatchSize)
	if err != nil {
		w.relay.logger.ErrorContext(ctx, "failed to lease compose outbox messages", "error", err)
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
			w.relay.logger.ErrorContext(ctx, "failed to fetch compose deployment job for outbox message", "job_id", item.JobID, "outbox_id", item.ID, "error", err)
			_ = w.repo.MarkComposeOutboxPending(ctx, item.ID, err)
			continue
		}
		w.relay.publish(ctx, outboxMessage{
			id:         item.ID,
			exchange:   item.Exchange,
			routingKey: item.RoutingKey,
			payload:    item.Payload,
			logAttrs:   []any{"job_id", item.JobID},
		}, w.repo.MarkComposeOutboxPublished, w.repo.MarkComposeOutboxPending)
	}
}
