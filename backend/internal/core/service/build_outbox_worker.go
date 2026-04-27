package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type BuildOutboxRepository interface {
	LeasePendingBuildOutbox(ctx context.Context, limit int) ([]model.BuildQueueOutbox, error)
	MarkBuildOutboxPublished(ctx context.Context, id uuid.UUID) error
	MarkBuildOutboxPending(ctx context.Context, id uuid.UUID, cause error) error
}

type BuildQueuePublisher interface {
	Publish(ctx context.Context, exchange, routingKey string, body []byte) error
	Close() error
}

type BuildOutboxWorker struct {
	repo      BuildOutboxRepository
	publisher BuildQueuePublisher
	interval  time.Duration
	batchSize int
	logger    *slog.Logger
}

func NewBuildOutboxWorker(repo BuildOutboxRepository, publisher BuildQueuePublisher, interval time.Duration, batchSize int, logger *slog.Logger) *BuildOutboxWorker {
	if interval <= 0 {
		interval = time.Second
	}
	if batchSize <= 0 {
		batchSize = 10
	}
	return &BuildOutboxWorker{
		repo:      repo,
		publisher: publisher,
		interval:  interval,
		batchSize: batchSize,
		logger:    logging.WithComponent(logger, "build_outbox_worker"),
	}
}

func (w *BuildOutboxWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "build outbox worker started", "interval", w.interval.String(), "batch_size", w.batchSize)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	w.publishBatch(ctx)
	for {
		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "build outbox worker stopped")
			return
		case <-ticker.C:
			w.publishBatch(ctx)
		}
	}
}

func (w *BuildOutboxWorker) publishBatch(ctx context.Context) {
	items, err := w.repo.LeasePendingBuildOutbox(ctx, w.batchSize)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to lease build outbox messages", "error", err)
		return
	}

	for _, item := range items {
		if err := w.publisher.Publish(ctx, item.Exchange, item.RoutingKey, item.Payload); err != nil {
			w.logger.ErrorContext(ctx, "failed to publish build queue message", "build_id", item.BuildID, "outbox_id", item.ID, "error", err)
			_ = w.repo.MarkBuildOutboxPending(ctx, item.ID, err)
			continue
		}
		if err := w.repo.MarkBuildOutboxPublished(ctx, item.ID); err != nil {
			w.logger.ErrorContext(ctx, "failed to mark build outbox message published", "build_id", item.BuildID, "outbox_id", item.ID, "error", err)
		}
	}
}
