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
	lockRepo, ok := s.repo.(containerLockRepository)
	if !ok {
		return nil, nil
	}
	return lockRepo.AcquireOwnerCapacityLock(ctx, ownerID)
}

func (s *ContainerService) setContainerDesiredStatus(ctx context.Context, containerID uuid.UUID, status string) error {
	stateRepo, ok := s.repo.(containerStateRepository)
	if !ok {
		return nil
	}
	if err := stateRepo.SetDesiredStatus(ctx, containerID, status); err != nil {
		s.logger.WarnContext(ctx, "failed to set desired container status", "container_id", containerID, "desired_status", status, "error", err)
		return err
	}
	return nil
}

func (s *ContainerService) markContainerError(ctx context.Context, containerID uuid.UUID, status string, cause error) {
	stateRepo, ok := s.repo.(containerStateRepository)
	if !ok {
		return
	}
	writeCtx, cancel := detachedContainerStateContext(ctx)
	defer cancel()
	if err := stateRepo.MarkStatusError(writeCtx, containerID, status, normalizeContainerRuntimeError(cause)); err != nil {
		s.logger.WarnContext(ctx, "failed to mark container error", "container_id", containerID, "status", status, "error", err)
	}
}

func (s *ContainerService) markVolumeMissing(ctx context.Context, volumeID uuid.UUID) {
	stateRepo, ok := s.volumeRepo.(volumeStateRepository)
	if !ok {
		return
	}
	if err := stateRepo.MarkStatusError(ctx, volumeID, model.VolumeStatusMissing, resourceMissingError("volume")); err != nil {
		s.logger.WarnContext(ctx, "failed to mark volume missing", "volume_id", volumeID, "error", err)
	}
}

func (s *ContainerService) markImageMissing(ctx context.Context, imageID uuid.UUID) {
	stateRepo, ok := s.imageRepo.(imageStateRepository)
	if !ok {
		return
	}
	if err := stateRepo.MarkStatusError(ctx, imageID, model.ImageStatusMissing, resourceMissingError("image")); err != nil {
		s.logger.WarnContext(ctx, "failed to mark image missing", "image_id", imageID, "error", err)
	}
}

func (s *ContainerService) createContainerOperation(ctx context.Context, containerID, ownerID uuid.UUID, operation string) error {
	stateRepo, ok := s.repo.(containerStateRepository)
	if !ok {
		return nil
	}
	op := model.ResourceOperation{
		ID:           uuid.New(),
		ResourceType: model.ResourceTypeContainer,
		ResourceID:   containerID,
		OwnerID:      ownerID,
		Operation:    operation,
		Status:       model.OperationStatusRunning,
	}
	if err := stateRepo.CreateOperation(ctx, op); err != nil {
		s.logger.WarnContext(ctx, "failed to create resource operation", "container_id", containerID, "operation", operation, "error", err)
		return err
	}
	return nil
}

func (s *ContainerService) createContainerOperationAndSetDesired(ctx context.Context, containerID, ownerID uuid.UUID, operation, desiredStatus string) error {
	stateRepo, ok := s.repo.(containerStateRepository)
	if !ok {
		return s.setContainerDesiredStatus(ctx, containerID, desiredStatus)
	}
	op := model.ResourceOperation{
		ID:           uuid.New(),
		ResourceType: model.ResourceTypeContainer,
		ResourceID:   containerID,
		OwnerID:      ownerID,
		Operation:    operation,
		Status:       model.OperationStatusRunning,
	}
	if err := stateRepo.CreateOperationAndSetDesired(ctx, containerID, desiredStatus, op); err != nil {
		s.logger.WarnContext(ctx, "failed to create resource operation and set desired container status", "container_id", containerID, "operation", operation, "desired_status", desiredStatus, "error", err)
		return err
	}
	return nil
}

func (s *ContainerService) queueContainerOperation(ctx context.Context, c model.Container, operation, busyStatus, desiredStatus string, msg containerqueue.LifecycleMessage) error {
	queueRepo, ok := s.repo.(containerLifecycleQueueRepository)
	if !ok {
		return apperrors.New(apperrors.ErrUnavailable, "container lifecycle queue repository is unavailable")
	}
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

func (s *ContainerService) completeContainerOperation(ctx context.Context, containerID uuid.UUID, status string, cause error) {
	stateRepo, ok := s.repo.(containerStateRepository)
	if !ok {
		return
	}
	writeCtx, cancel := detachedContainerStateContext(ctx)
	defer cancel()
	if err := stateRepo.CompleteLatestOperation(writeCtx, model.ResourceTypeContainer, containerID, status, normalizeContainerRuntimeError(cause)); err != nil {
		s.logger.WarnContext(ctx, "failed to complete resource operation", "container_id", containerID, "status", status, "error", err)
	}
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
