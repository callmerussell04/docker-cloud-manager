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

type ResourceOutboxRepository interface {
	LeasePendingResourceOutbox(ctx context.Context, limit int) ([]model.ResourceLifecycleOutbox, error)
	GetOperationByID(ctx context.Context, id uuid.UUID) (model.ResourceOperation, error)
	MarkResourceOutboxPublished(ctx context.Context, id uuid.UUID) error
	MarkResourceOutboxPending(ctx context.Context, id uuid.UUID, cause error) error
	MarkResourceOutboxDiscarded(ctx context.Context, id uuid.UUID, cause error) error
}

type ResourceOutboxConfigProvider interface {
	Get() config.SystemConfig
}

type ResourceOutboxWorker struct {
	repo      ResourceOutboxRepository
	publisher BuildQueuePublisher
	cfg       ResourceOutboxConfigProvider
	logger    *slog.Logger
}

func NewResourceOutboxWorker(repo ResourceOutboxRepository, publisher BuildQueuePublisher, cfg ResourceOutboxConfigProvider, logger *slog.Logger) *ResourceOutboxWorker {
	return &ResourceOutboxWorker{
		repo:      repo,
		publisher: publisher,
		cfg:       cfg,
		logger:    logging.WithComponent(logger, "resource_outbox_worker"),
	}
}

func (w *ResourceOutboxWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "resource outbox worker started")
	w.publishBatch(ctx)
	for {
		interval := time.Duration(w.cfg.Get().ContainerCreateOutboxIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = time.Second
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			w.logger.InfoContext(ctx, "resource outbox worker stopped")
			return
		case <-timer.C:
			w.publishBatch(ctx)
		}
	}
}

func (w *ResourceOutboxWorker) publishBatch(ctx context.Context) {
	items, err := w.repo.LeasePendingResourceOutbox(ctx, w.cfg.Get().ContainerCreateOutboxBatchSize)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to lease resource outbox messages", "error", err)
		return
	}
	for _, item := range items {
		op, err := w.repo.GetOperationByID(ctx, item.OperationID)
		if err == nil && op.Status != model.OperationStatusPending {
			_ = w.repo.MarkResourceOutboxDiscarded(ctx, item.ID, nil)
			continue
		}
		if err != nil {
			if errors.Is(err, apperrors.ErrNotFound) {
				_ = w.repo.MarkResourceOutboxDiscarded(ctx, item.ID, err)
				continue
			}
			w.logger.ErrorContext(ctx, "failed to fetch resource operation for outbox message", "operation_id", item.OperationID, "outbox_id", item.ID, "error", err)
			_ = w.repo.MarkResourceOutboxPending(ctx, item.ID, err)
			continue
		}
		if err := w.publisher.Publish(ctx, item.Exchange, item.RoutingKey, item.Payload); err != nil {
			w.logger.ErrorContext(ctx, "failed to publish resource queue message", "resource_type", item.ResourceType, "resource_id", item.ResourceID, "operation_id", item.OperationID, "outbox_id", item.ID, "error", err)
			_ = w.repo.MarkResourceOutboxPending(ctx, item.ID, err)
			continue
		}
		if err := w.repo.MarkResourceOutboxPublished(ctx, item.ID); err != nil {
			w.logger.ErrorContext(ctx, "failed to mark resource outbox message published", "resource_type", item.ResourceType, "resource_id", item.ResourceID, "operation_id", item.OperationID, "outbox_id", item.ID, "error", err)
		}
	}
}
