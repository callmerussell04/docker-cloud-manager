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

type ContainerOutboxRepository interface {
	LeasePendingContainerOutbox(ctx context.Context, limit int) ([]model.ContainerLifecycleOutbox, error)
	GetByID(ctx context.Context, id uuid.UUID) (model.Container, error)
	GetOperationByID(ctx context.Context, id uuid.UUID) (model.ResourceOperation, error)
	MarkContainerOutboxPublished(ctx context.Context, id uuid.UUID) error
	MarkContainerOutboxPending(ctx context.Context, id uuid.UUID, cause error) error
	MarkContainerOutboxDiscarded(ctx context.Context, id uuid.UUID, cause error) error
}

type ContainerOutboxConfigProvider interface {
	Get() config.SystemConfig
}

type ContainerOutboxWorker struct {
	repo  ContainerOutboxRepository
	cfg   ContainerOutboxConfigProvider
	relay outboxRelay
}

func NewContainerOutboxWorker(repo ContainerOutboxRepository, publisher BuildQueuePublisher, cfg ContainerOutboxConfigProvider, logger *slog.Logger) *ContainerOutboxWorker {
	return &ContainerOutboxWorker{
		repo:  repo,
		cfg:   cfg,
		relay: newOutboxRelay("container", publisher, logger),
	}
}

func (w *ContainerOutboxWorker) Run(ctx context.Context) {
	w.relay.schedule(ctx, func() int64 { return w.cfg.Get().ContainerCreateOutboxIntervalSeconds }, w.publishBatch)
}

func (w *ContainerOutboxWorker) publishBatch(ctx context.Context) {
	items, err := w.repo.LeasePendingContainerOutbox(ctx, w.cfg.Get().ContainerCreateOutboxBatchSize)
	if err != nil {
		w.relay.logger.ErrorContext(ctx, "failed to lease container outbox messages", "error", err)
		return
	}
	for _, item := range items {
		op, err := w.repo.GetOperationByID(ctx, item.OperationID)
		if err == nil && op.Status != model.OperationStatusPending {
			_ = w.repo.MarkContainerOutboxDiscarded(ctx, item.ID, nil)
			continue
		}
		if err != nil {
			if errors.Is(err, apperrors.ErrNotFound) {
				_ = w.repo.MarkContainerOutboxDiscarded(ctx, item.ID, err)
				continue
			}
			w.relay.logger.ErrorContext(ctx, "failed to fetch container operation for outbox message", "operation_id", item.OperationID, "outbox_id", item.ID, "error", err)
			_ = w.repo.MarkContainerOutboxPending(ctx, item.ID, err)
			continue
		}
		if _, err := w.repo.GetByID(ctx, item.ContainerID); err != nil {
			if errors.Is(err, apperrors.ErrNotFound) {
				_ = w.repo.MarkContainerOutboxDiscarded(ctx, item.ID, err)
				continue
			}
			w.relay.logger.ErrorContext(ctx, "failed to fetch container for outbox message", "container_id", item.ContainerID, "outbox_id", item.ID, "error", err)
			_ = w.repo.MarkContainerOutboxPending(ctx, item.ID, err)
			continue
		}
		w.relay.publish(ctx, outboxMessage{
			id:         item.ID,
			exchange:   item.Exchange,
			routingKey: item.RoutingKey,
			payload:    item.Payload,
			logAttrs:   []any{"container_id", item.ContainerID, "operation_id", item.OperationID},
		}, w.repo.MarkContainerOutboxPublished, w.repo.MarkContainerOutboxPending)
	}
}
