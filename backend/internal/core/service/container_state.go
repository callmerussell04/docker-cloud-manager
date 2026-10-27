package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/containerqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
)

const containerStateWriteTimeout = 10 * time.Second

func (s *ContainerService) acquireOwnerCapacityLock(ctx context.Context, ownerID uuid.UUID) (func(), error) {
	return s.repo.AcquireOwnerCapacityLock(ctx, ownerID)
}

func (s *ContainerService) markContainerError(ctx context.Context, containerID uuid.UUID, status string, cause error) {
	stateRepo := s.repo
	writeCtx, cancel := detachedContainerStateContext(ctx)
	defer cancel()
	if err := stateRepo.MarkStatusError(writeCtx, containerID, status, normalizeContainerRuntimeError(cause)); err != nil {
		s.logger.WarnContext(ctx, "failed to mark container error", "container_id", containerID, "status", status, "error", err)
	}
}

func (s *ContainerService) markVolumeMissing(ctx context.Context, volumeID uuid.UUID) {
	stateRepo := s.volumeRepo
	if err := stateRepo.MarkStatusError(ctx, volumeID, model.VolumeStatusMissing, resourceMissingError("volume")); err != nil {
		s.logger.WarnContext(ctx, "failed to mark volume missing", "volume_id", volumeID, "error", err)
	}
}

func (s *ContainerService) markImageMissing(ctx context.Context, imageID uuid.UUID) {
	stateRepo := s.imageRepo
	if err := stateRepo.MarkStatusError(ctx, imageID, model.ImageStatusMissing, resourceMissingError("image")); err != nil {
		s.logger.WarnContext(ctx, "failed to mark image missing", "image_id", imageID, "error", err)
	}
}

func (s *ContainerService) queueContainerOperation(ctx context.Context, c model.Container, operation, busyStatus, desiredStatus string, msg containerqueue.LifecycleMessage) error {
	queueRepo := s.repo
	op := model.ResourceOperation{
		ID:           uuid.New(),
		ResourceType: model.ResourceTypeContainer,
		ResourceID:   c.ID,
		OwnerID:      c.OwnerID,
		Operation:    operation,
		Status:       model.OperationStatusPending,
	}
	msg.OperationID = op.ID.String()
	msg.ContainerID = c.ID.String()
	msg.OwnerID = c.OwnerID.String()
	msg.Operation = operation
	msg.RequestID = logging.RequestIDFromContext(ctx)
	msg.CreatedAt = time.Now().Unix()
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal container lifecycle message: %w", err)
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
	if err := queueRepo.QueueContainerOperation(ctx, c.ID, busyStatus, desiredStatus, op, outbox); err != nil {
		s.logger.WarnContext(ctx, "failed to queue container lifecycle operation", "container_id", c.ID, "operation", operation, "status", busyStatus, "desired_status", desiredStatus, "error", err)
		return err
	}
	return nil
}

func detachedContainerStateContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), containerStateWriteTimeout)
}

func normalizeContainerRuntimeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return apperrors.Wrap(apperrors.ErrTimeout, apperrors.ErrTimeout.Error(), err)
	}
	return err
}

func isDockerNotFound(err error) bool {
	return err != nil && cerrdefs.IsNotFound(err)
}
