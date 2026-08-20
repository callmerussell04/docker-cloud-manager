package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/resourcequeue"
	"github.com/google/uuid"
)

type ResourceVolumeExecutor interface {
	ExecuteQueuedVolumeOperation(ctx context.Context, operationID, volumeID uuid.UUID) error
}

type ResourceImageExecutor interface {
	ExecuteQueuedImageOperation(ctx context.Context, operationID, imageID uuid.UUID) error
}

type ResourceLifecycleWorker struct {
	volumes ResourceVolumeExecutor
	images  ResourceImageExecutor
	logger  *slog.Logger
}

func NewResourceLifecycleWorker(volumes ResourceVolumeExecutor, images ResourceImageExecutor, logger *slog.Logger) *ResourceLifecycleWorker {
	return &ResourceLifecycleWorker{
		volumes: volumes,
		images:  images,
		logger:  logging.WithComponent(logger, "resource_lifecycle_worker"),
	}
}

func (w *ResourceLifecycleWorker) HandleMessage(ctx context.Context, msg resourcequeue.LifecycleMessage) error {
	operationID, err := uuid.Parse(msg.OperationID)
	if err != nil {
		return apperrors.New(apperrors.ErrBadRequest, "invalid resource operation id")
	}
	resourceID, err := uuid.Parse(msg.ResourceID)
	if err != nil {
		return apperrors.New(apperrors.ErrBadRequest, "invalid resource id")
	}
	switch msg.ResourceType {
	case model.ResourceTypeVolume:
		if w.volumes == nil {
			return apperrors.New(apperrors.ErrUnavailable, "volume lifecycle executor is unavailable")
		}
		return w.volumes.ExecuteQueuedVolumeOperation(ctx, operationID, resourceID)
	case model.ResourceTypeImage:
		if w.images == nil {
			return apperrors.New(apperrors.ErrUnavailable, "image lifecycle executor is unavailable")
		}
		return w.images.ExecuteQueuedImageOperation(ctx, operationID, resourceID)
	default:
		w.logger.WarnContext(ctx, "unsupported resource lifecycle message", "resource_type", msg.ResourceType, "resource_id", msg.ResourceID, "operation_id", msg.OperationID)
		return apperrors.New(apperrors.ErrBadRequest, "unsupported resource type")
	}
}

func retryableLifecycleError(err error) bool {
	return errors.Is(err, apperrors.ErrTimeout) || errors.Is(err, apperrors.ErrUnavailable)
}
