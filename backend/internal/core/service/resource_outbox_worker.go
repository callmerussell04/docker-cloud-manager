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
	repo  ResourceOutboxRepository
	cfg   ResourceOutboxConfigProvider
	relay outboxRelay
}

func NewResourceOutboxWorker(repo ResourceOutboxRepository, publisher BuildQueuePublisher, cfg ResourceOutboxConfigProvider, logger *slog.Logger) *ResourceOutboxWorker {
	return &ResourceOutboxWorker{
		repo:  repo,
		cfg:   cfg,
		relay: newOutboxRelay("resource", publisher, logger),
	}
}

func (w *ResourceOutboxWorker) Run(ctx context.Context) {
	w.relay.schedule(ctx, func() int64 { return w.cfg.Get().ContainerCreateOutboxIntervalSeconds }, w.publishBatch)
}

func (w *ResourceOutboxWorker) publishBatch(ctx context.Context) {
	items, err := w.repo.LeasePendingResourceOutbox(ctx, w.cfg.Get().ContainerCreateOutboxBatchSize)
	if err != nil {
		w.relay.logger.ErrorContext(ctx, "failed to lease resource outbox messages", "error", err)
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
			w.relay.logger.ErrorContext(ctx, "failed to fetch resource operation for outbox message", "operation_id", item.OperationID, "outbox_id", item.ID, "error", err)
			_ = w.repo.MarkResourceOutboxPending(ctx, item.ID, err)
			continue
		}
		w.relay.publish(ctx, outboxMessage{
			id:         item.ID,
			exchange:   item.Exchange,
			routingKey: item.RoutingKey,
			payload:    item.Payload,
			logAttrs:   []any{"resource_type", item.ResourceType, "resource_id", item.ResourceID, "operation_id", item.OperationID},
		}, w.repo.MarkResourceOutboxPublished, w.repo.MarkResourceOutboxPending)
	}
}
