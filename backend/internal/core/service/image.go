package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/imageref"
)

type ImageRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Image, error)
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, opts model.ListOptions) ([]model.Image, int, error)
}

type imageStateRepository interface {
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error
}

type ImageContainerRepository interface {
	IsImageInUse(ctx context.Context, ownerID uuid.UUID, imageTag string) (bool, error)
}

type ImageDockerAPI interface {
	RemoveImage(ctx context.Context, imageID string, force bool) error
}

type ImageRegistryAPI interface {
	GetImageSizeAndDigest(ctx context.Context, repo, tag string) (int64, string, error)
	DeleteManifest(ctx context.Context, repo, digest string) error
}

type ImageService struct {
	repo        ImageRepository
	dockerAPI   ImageDockerAPI
	registryAPI ImageRegistryAPI
	contRepo    ImageContainerRepository
	cfg         ConfigManager
	lifecycle   ImageLifecycleRepository
}

type ImageLifecycleRepository interface {
	QueueImageDelete(ctx context.Context, id uuid.UUID, op model.ResourceOperation, outbox model.ResourceLifecycleOutbox) error
	HasActiveOperation(ctx context.Context, resourceType string, resourceID uuid.UUID) (bool, error)
	ClaimPendingOperation(ctx context.Context, id uuid.UUID, maxAttempts int) (model.ResourceOperation, bool, error)
	CompleteOperation(ctx context.Context, id uuid.UUID, status string, cause error) error
	RequeueOperation(ctx context.Context, id uuid.UUID, cause error) error
}

func NewImageService(
	repo ImageRepository,
	dockerAPI ImageDockerAPI,
	registryAPI ImageRegistryAPI,
	contRepo ImageContainerRepository,
	cfg ConfigManager,
) *ImageService {
	return &ImageService{
		repo:        repo,
		dockerAPI:   dockerAPI,
		registryAPI: registryAPI,
		contRepo:    contRepo,
		cfg:         cfg,
	}
}

func (s *ImageService) SetLifecycleRepository(repo ImageLifecycleRepository) {
	s.lifecycle = repo
}

func (s *ImageService) Delete(ctx context.Context, imageID uuid.UUID) error {
	img, err := s.repo.GetByID(ctx, imageID)
	if err != nil {
		return err
	}

	if err := accessscope.RequireOwnerAccess(ctx, img.OwnerID); err != nil {
		return err
	}

	inUse, err := s.contRepo.IsImageInUse(ctx, img.OwnerID, img.Tag)
	if err != nil {
		return err
	}
	if inUse {
		return apperrors.New(apperrors.ErrResourceInUse, "image is currently used by a container")
	}
	if img.Status != model.ImageStatusAvailable && img.Status != model.ImageStatusMissing {
		return apperrors.New(apperrors.ErrConflict, "image is not available")
	}
	if s.lifecycle == nil {
		return apperrors.New(apperrors.ErrUnavailable, "image lifecycle queue is unavailable")
	}
	active, err := s.lifecycle.HasActiveOperation(ctx, model.ResourceTypeImage, imageID)
	if err != nil {
		return err
	}
	if active {
		return apperrors.New(apperrors.ErrConflict, "resource operation is already in progress")
	}
	op, outbox, err := resourceOperationOutbox(ctx, model.ResourceTypeImage, imageID, img.OwnerID, model.OperationDelete)
	if err != nil {
		return err
	}
	return s.lifecycle.QueueImageDelete(ctx, imageID, op, outbox)
}

func (s *ImageService) ExecuteQueuedImageOperation(ctx context.Context, operationID, imageID uuid.UUID) error {
	if s.lifecycle == nil {
		return apperrors.New(apperrors.ErrUnavailable, "image lifecycle queue is unavailable")
	}
	op, claimed, err := s.lifecycle.ClaimPendingOperation(ctx, operationID, s.cfg.Get().ContainerCreateMaxAttempts)
	if err != nil {
		return err
	}
	if !claimed {
		if op.Status == model.OperationStatusPending && s.cfg.Get().ContainerCreateMaxAttempts > 0 && op.Attempts >= s.cfg.Get().ContainerCreateMaxAttempts {
			cause := apperrors.New(apperrors.ErrTimeout, "image lifecycle attempts exhausted")
			s.failQueuedImageDelete(ctx, operationID, imageID, cause)
		}
		return nil
	}
	if op.ResourceType != model.ResourceTypeImage || op.ResourceID != imageID || op.Operation != model.OperationDelete {
		cause := apperrors.New(apperrors.ErrBadRequest, "image lifecycle message does not match operation")
		_ = s.lifecycle.CompleteOperation(context.WithoutCancel(ctx), operationID, model.OperationStatusFailed, cause)
		return cause
	}
	return s.executeQueuedImageDelete(ctx, operationID, imageID)
}

func (s *ImageService) executeQueuedImageDelete(ctx context.Context, operationID, imageID uuid.UUID) error {
	img, err := s.repo.GetByID(ctx, imageID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			_ = s.lifecycle.CompleteOperation(context.WithoutCancel(ctx), operationID, model.OperationStatusDone, nil)
			return nil
		}
		_ = s.lifecycle.CompleteOperation(context.WithoutCancel(ctx), operationID, model.OperationStatusFailed, err)
		return err
	}

	baseName, version := parseImageTag(img.Tag)
	repoName := customImageRepositoryName(img.OwnerID, baseName)

	// Удаление из Registry (Soft Delete)
	_, digest, err := s.registryAPI.GetImageSizeAndDigest(ctx, repoName, version)
	if err == nil && digest != "" {
		_ = s.registryAPI.DeleteManifest(ctx, repoName, digest)
	}

	// Удаление из локального кэша Docker Engine
	fullTag := customImageFullTag(s.cfg.Get().RegistryPublicURL, img.OwnerID, baseName, version)
	_ = s.dockerAPI.RemoveImage(ctx, fullTag, false)

	// Удаление записи из бд
	if err := s.repo.Delete(ctx, imageID); err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			_ = s.lifecycle.CompleteOperation(context.WithoutCancel(ctx), operationID, model.OperationStatusDone, nil)
			return nil
		}
		if retryableLifecycleError(err) {
			if requeueErr := s.lifecycle.RequeueOperation(context.WithoutCancel(ctx), operationID, err); requeueErr != nil {
				s.failQueuedImageDelete(ctx, operationID, imageID, requeueErr)
			}
			return err
		}
		s.failQueuedImageDelete(ctx, operationID, imageID, err)
		return err
	}
	_ = s.lifecycle.CompleteOperation(context.WithoutCancel(ctx), operationID, model.OperationStatusDone, nil)
	return nil
}

func (s *ImageService) failQueuedImageDelete(ctx context.Context, operationID, imageID uuid.UUID, cause error) {
	writeCtx, cancel := detachedContainerStateContext(ctx)
	defer cancel()
	s.markImageError(writeCtx, imageID, model.ImageStatusError, normalizeContainerRuntimeError(cause))
	_ = s.lifecycle.CompleteOperation(writeCtx, operationID, model.OperationStatusFailed, normalizeContainerRuntimeError(cause))
}

func parseImageTag(rawTag string) (baseName, version string) {
	return imageref.ParseTag(rawTag)
}

func (s *ImageService) List(ctx context.Context, limit, offset int) ([]model.Image, int, error) {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return nil, 0, err
	}
	if scope.Kind != accessscope.KindUser && scope.Kind != accessscope.KindAdmin {
		return nil, 0, apperrors.ErrForbidden
	}
	return s.repo.List(ctx, model.ListOptions{
		OwnerID: scope.OwnerFilter(),
		Limit:   limit,
		Offset:  offset,
	})
}

func (s *ImageService) setImageStatus(ctx context.Context, imageID uuid.UUID, status string) {
	stateRepo, ok := s.repo.(imageStateRepository)
	if !ok {
		return
	}
	_ = stateRepo.UpdateStatus(ctx, imageID, status)
}

func (s *ImageService) markImageError(ctx context.Context, imageID uuid.UUID, status string, cause error) {
	stateRepo, ok := s.repo.(imageStateRepository)
	if !ok {
		return
	}
	_ = stateRepo.MarkStatusError(ctx, imageID, status, cause)
}
