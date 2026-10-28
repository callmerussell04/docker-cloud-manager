package service

import (
	"context"
	"errors"
	"log/slog"

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
	repo   ResourceOutboxRepository
	cfg    ResourceOutboxConfigProvider
	relay  outboxRelay
	logger *slog.Logger
}

func NewResourceOutboxWorker(repo ResourceOutboxRepository, publisher OutboxPublisher, cfg ResourceOutboxConfigProvider, logger *slog.Logger) *ResourceOutboxWorker {
	logger = logging.WithComponent(logger, "resource_outbox_worker")
	return &ResourceOutboxWorker{
		repo:   repo,
		cfg:    cfg,
		relay:  newOutboxRelay("resource", publisher, logger),
		logger: logger,
	}
}

func (w *ResourceOutboxWorker) Run(ctx context.Context) {
	w.relay.run(ctx, func() int64 { return w.cfg.Get().ContainerCreateOutboxIntervalSeconds }, w.publishBatch)
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
			w.relay.logMarkFailure(ctx, "discarded", item.ID, w.repo.MarkResourceOutboxDiscarded(ctx, item.ID, nil), "resource_type", item.ResourceType, "resource_id", item.ResourceID, "operation_id", item.OperationID)
			continue
		}
		if err != nil {
			if errors.Is(err, apperrors.ErrNotFound) {
				w.relay.logMarkFailure(ctx, "discarded", item.ID, w.repo.MarkResourceOutboxDiscarded(ctx, item.ID, err), "resource_type", item.ResourceType, "resource_id", item.ResourceID, "operation_id", item.OperationID)
				continue
			}
			w.logger.ErrorContext(ctx, "failed to fetch resource operation for outbox message", "operation_id", item.OperationID, "outbox_id", item.ID, "error", err)
			w.relay.logMarkFailure(ctx, "pending", item.ID, w.repo.MarkResourceOutboxPending(ctx, item.ID, err), "resource_type", item.ResourceType, "resource_id", item.ResourceID, "operation_id", item.OperationID)
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
