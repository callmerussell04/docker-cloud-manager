package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/containerqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type TTLContainerRepository interface {
	GetExpired(ctx context.Context) ([]model.Container, error)
	QueueContainerOperation(ctx context.Context, id uuid.UUID, status string, desiredStatus string, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox) error
}

type TTLConfigProvider interface {
	Get() config.SystemConfig
}

type TTLWorker struct {
	repo   TTLContainerRepository
	cfg    TTLConfigProvider
	logger *slog.Logger
}

func NewTTLWorker(repo TTLContainerRepository, cfg TTLConfigProvider, logger *slog.Logger) *TTLWorker {
	return &TTLWorker{
		repo:   repo,
		cfg:    cfg,
		logger: logging.WithComponent(logger, "ttl_worker"),
	}
}

func (w *TTLWorker) Run(ctx context.Context) {
	w.logger.InfoContext(ctx, "ttl worker started")

	for {
		interval := time.Duration(w.cfg.Get().TTLWorkerIntervalSeconds) * time.Second
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			w.logger.InfoContext(ctx, "ttl worker stopped")
			return
		case <-timer.C:
			w.processExpired(ctx)
		}
	}
}

func (w *TTLWorker) processExpired(ctx context.Context) {
	if w.cfg.Get().ContainerTTLHours <= 0 {
		return
	}
	expiredContainers, err := w.repo.GetExpired(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "failed to fetch expired containers", "error", err)
		return
	}

	for _, c := range expiredContainers {
		if err := w.queueStop(ctx, c); err != nil {
			if errors.Is(err, apperrors.ErrConflict) {
				w.logger.DebugContext(ctx, "expired container already has active operation", "container_id", c.ID)
				continue
			}
			w.logger.ErrorContext(ctx, "failed to queue expired container stop", "container_id", c.ID, "error", err)
			continue
		}
		w.logger.InfoContext(ctx, "expired container stop queued", "container_id", c.ID)
	}
}

func (w *TTLWorker) queueStop(ctx context.Context, c model.Container) error {
	op := model.ResourceOperation{
		ID:           uuid.New(),
		ResourceType: model.ResourceTypeContainer,
		ResourceID:   c.ID,
		OwnerID:      c.OwnerID,
		Operation:    model.OperationStop,
		Status:       model.OperationStatusPending,
	}
	msg := containerqueue.LifecycleMessage{
		OperationID:    op.ID.String(),
		ContainerID:    c.ID.String(),
		OwnerID:        c.OwnerID.String(),
		Operation:      model.OperationStop,
		RequestID:      logging.RequestIDFromContext(ctx),
		CreatedAt:      time.Now().Unix(),
		PreviousStatus: c.Status,
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal ttl stop message: %w", err)
	}
	outbox := model.ContainerLifecycleOutbox{
		ID:          uuid.New(),
		OperationID: op.ID,
		ContainerID: c.ID,
		Exchange:    containerqueue.ExchangeName,
		RoutingKey:  containerqueue.RoutingKey,
		Payload:     payload,
		Status:      model.ContainerOutboxStatusPending,
	}
	return w.repo.QueueContainerOperation(ctx, c.ID, model.ContainerStatusStopping, model.ContainerStatusExited, op, outbox)
}
