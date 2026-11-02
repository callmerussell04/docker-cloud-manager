package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
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
	repo   ComposeOutboxRepository
	cfg    ComposeOutboxConfigProvider
	relay  outboxRelay
	logger *slog.Logger
}

func NewComposeOutboxWorker(repo ComposeOutboxRepository, publisher OutboxPublisher, cfg ComposeOutboxConfigProvider, logger *slog.Logger) *ComposeOutboxWorker {
	logger = logging.WithComponent(logger, "compose_outbox_worker")
	return &ComposeOutboxWorker{
		repo:   repo,
		cfg:    cfg,
		relay:  newOutboxRelay("compose", publisher, logger),
		logger: logger,
	}
}

func (w *ComposeOutboxWorker) Run(ctx context.Context) {
	w.relay.run(ctx, func() int64 { return w.cfg.Get().ComposeOutboxIntervalSeconds }, w.publishBatch)
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
			w.relay.logMarkFailure(ctx, "discarded", item.ID, w.repo.MarkComposeOutboxDiscarded(ctx, item.ID, nil), "job_id", item.JobID)
			continue
		}
		if err != nil {
			if errors.Is(err, apperrors.ErrNotFound) {
				w.relay.logMarkFailure(ctx, "discarded", item.ID, w.repo.MarkComposeOutboxDiscarded(ctx, item.ID, err), "job_id", item.JobID)
				continue
			}
			w.logger.ErrorContext(ctx, "failed to fetch compose deployment job for outbox message", "job_id", item.JobID, "outbox_id", item.ID, "error", err)
			w.relay.logMarkFailure(ctx, "pending", item.ID, w.repo.MarkComposeOutboxPending(ctx, item.ID, err), "job_id", item.JobID)
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
