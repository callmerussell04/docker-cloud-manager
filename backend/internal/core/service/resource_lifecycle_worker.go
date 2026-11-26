package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/google/uuid"
)

type ResourceVolumeExecutor interface {
	ExecuteQueuedVolumeOperation(ctx context.Context, operationID, volumeID uuid.UUID) error
}

type ResourceImageExecutor interface {
	ExecuteQueuedImageOperation(ctx context.Context, operationID, imageID uuid.UUID) error
}

// ResourceLifecycleConfigProvider supplies the handler deadline.
type ResourceLifecycleConfigProvider interface {
	Get() config.SystemConfig
}

type ResourceLifecycleWorker struct {
	volumes ResourceVolumeExecutor
	images  ResourceImageExecutor
	cfg     ResourceLifecycleConfigProvider
	logger  *slog.Logger
}

func NewResourceLifecycleWorker(volumes ResourceVolumeExecutor, images ResourceImageExecutor, cfg ResourceLifecycleConfigProvider, logger *slog.Logger) *ResourceLifecycleWorker {
	return &ResourceLifecycleWorker{
		volumes: volumes,
		images:  images,
		cfg:     cfg,
		logger:  logging.WithComponent(logger, "resource_lifecycle_worker"),
	}
}

func (w *ResourceLifecycleWorker) HandleMessage(ctx context.Context, msg queue.ResourceLifecycleMessage) error {
	operationID, err := uuid.Parse(msg.OperationID)
	if err != nil {
		return apperrors.New(apperrors.ErrBadRequest, "invalid resource operation id")
	}
	resourceID, err := uuid.Parse(msg.ResourceID)
	if err != nil {
		return apperrors.New(apperrors.ErrBadRequest, "invalid resource id")
	}
	// The same handler deadline as container operations, which the operation
	// recovery pass relies on to know no worker still acts on an operation.
	ctx, cancel := context.WithTimeout(ctx, handlerDeadline(w.cfg.Get()))
	defer cancel()
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

// handlerDeadline bounds how long one delivery of a queued container, volume
// or image operation may run.
func handlerDeadline(cfg config.SystemConfig) time.Duration {
	return time.Duration(cfg.ContainerCreateTimeoutMinutes) * time.Minute
}
