package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
)

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
	if err := stateRepo.MarkStatusError(ctx, containerID, status, cause); err != nil {
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

func (s *ContainerService) completeContainerOperation(ctx context.Context, containerID uuid.UUID, status string, cause error) {
	stateRepo, ok := s.repo.(containerStateRepository)
	if !ok {
		return
	}
	if err := stateRepo.CompleteLatestOperation(ctx, model.ResourceTypeContainer, containerID, status, cause); err != nil {
		s.logger.WarnContext(ctx, "failed to complete resource operation", "container_id", containerID, "status", status, "error", err)
	}
}

func isDockerNotFound(err error) bool {
	return err != nil && cerrdefs.IsNotFound(err)
}
