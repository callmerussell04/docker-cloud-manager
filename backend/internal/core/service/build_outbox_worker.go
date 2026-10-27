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
	repo  BuildOutboxRepository
	cfg   BuildOutboxConfigProvider
	relay outboxRelay
}

func NewBuildOutboxWorker(repo BuildOutboxRepository, publisher BuildQueuePublisher, cfg BuildOutboxConfigProvider, logger *slog.Logger) *BuildOutboxWorker {
	return &BuildOutboxWorker{
		repo:  repo,
		cfg:   cfg,
		relay: newOutboxRelay("build", publisher, logger),
	}
}

func (w *BuildOutboxWorker) Run(ctx context.Context) {
	w.relay.schedule(ctx, func() int64 { return w.cfg.Get().BuildOutboxIntervalSeconds }, w.publishBatch)
}

func (w *BuildOutboxWorker) publishBatch(ctx context.Context) {
	items, err := w.repo.LeasePendingBuildOutbox(ctx, w.cfg.Get().BuildOutboxBatchSize)
	if err != nil {
		w.relay.logger.ErrorContext(ctx, "failed to lease build outbox messages", "error", err)
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
			w.relay.logger.ErrorContext(ctx, "failed to fetch build for outbox message", "build_id", item.BuildID, "outbox_id", item.ID, "error", err)
			_ = w.repo.MarkBuildOutboxPending(ctx, item.ID, err)
			continue
		}
		w.relay.publish(ctx, outboxMessage{
			id:         item.ID,
			exchange:   item.Exchange,
			routingKey: item.RoutingKey,
			payload:    item.Payload,
			logAttrs:   []any{"build_id", item.BuildID},
		}, w.repo.MarkBuildOutboxPublished, w.repo.MarkBuildOutboxPending)
	}
}
