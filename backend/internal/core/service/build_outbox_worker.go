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

type BuildOutboxRepository interface {
	LeasePendingBuildOutbox(ctx context.Context, limit int) ([]model.BuildQueueOutbox, error)
	GetByID(ctx context.Context, id uuid.UUID) (model.Build, error)
	MarkBuildOutboxPublished(ctx context.Context, id uuid.UUID) error
	MarkBuildOutboxPending(ctx context.Context, id uuid.UUID, cause error) error
	MarkBuildOutboxDiscarded(ctx context.Context, id uuid.UUID, cause error) error
}

type BuildQueuePublisher interface {
	Publish(ctx context.Context, exchange, routingKey string, body []byte) error
	Close() error
}

type BuildOutboxConfigProvider interface {
	Get() config.SystemConfig
}

type BuildOutboxWorker struct {
	repo      BuildOutboxRepository
	publisher BuildQueuePublisher
	cfg       BuildOutboxConfigProvider
	logger    *slog.Logger
}

func NewBuildOutboxWorker(repo BuildOutboxRepository, publisher BuildQueuePublisher, cfg BuildOutboxConfigProvider, logger *slog.Logger) *BuildOutboxWorker {
	return &BuildOutboxWorker{
		repo:      repo,
		publisher: publisher,
		cfg:       cfg,
		logger:    logging.WithComponent(logger, "build_outbox_worker"),
	}
}

func (w *BuildOutboxWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "build outbox worker started")

	w.publishBatch(ctx)
	for {
		interval := time.Duration(w.cfg.Get().BuildOutboxIntervalSeconds) * time.Second
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			w.logger.InfoContext(ctx, "build outbox worker stopped")
			return
		case <-timer.C:
			w.publishBatch(ctx)
		}
	}
}

func (w *BuildOutboxWorker) publishBatch(ctx context.Context) {
	items, err := w.repo.LeasePendingBuildOutbox(ctx, w.cfg.Get().BuildOutboxBatchSize)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to lease build outbox messages", "error", err)
		return
	}

	for _, item := range items {
		build, err := w.repo.GetByID(ctx, item.BuildID)
		if err == nil && model.IsBuildTerminalStatus(build.Status) {
			_ = w.repo.MarkBuildOutboxDiscarded(ctx, item.ID, nil)
			continue
		}
		if err != nil {
			if errors.Is(err, apperrors.ErrNotFound) {
				_ = w.repo.MarkBuildOutboxDiscarded(ctx, item.ID, err)
				continue
			}
			w.logger.ErrorContext(ctx, "failed to fetch build for outbox message", "build_id", item.BuildID, "outbox_id", item.ID, "error", err)
			_ = w.repo.MarkBuildOutboxPending(ctx, item.ID, err)
			continue
		}
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
