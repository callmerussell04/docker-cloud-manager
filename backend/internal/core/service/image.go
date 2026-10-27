package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/imageref"
)

type ImageRepository interface {
	imageStateRepository
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
	repo         ImageRepository
	dockerAPI    ImageDockerAPI
	registryAPI  ImageRegistryAPI
	contRepo     ImageContainerRepository
	cfg          ConfigManager
	lifecycle    ImageLifecycleRepository
	registryAuth *RegistryAuthorization
}

type ImageLifecycleRepository interface {
	QueueImageDelete(ctx context.Context, id uuid.UUID, op model.ResourceOperation, outbox model.ResourceLifecycleOutbox) error
	HasActiveOperation(ctx context.Context, resourceType string, resourceID uuid.UUID) (bool, error)
	ClaimPendingOperation(ctx context.Context, id uuid.UUID, maxAttempts int) (model.ResourceOperation, bool, error)
	CompleteOperation(ctx context.Context, id uuid.UUID, status string, cause error) error
	RequeueOperation(ctx context.Context, id uuid.UUID, cause error) error
}

// ImageServiceDeps requires the durable deletion workflow dependencies.
type ImageServiceDeps struct {
	Repository ImageRepository
	// Docker is required for best-effort local cache cleanup after Registry deletion.
	Docker     ImageDockerAPI
	Registry   ImageRegistryAPI
	Containers ImageContainerRepository
	Config     ConfigManager
	Lifecycle  ImageLifecycleRepository
	// RegistryAuth optionally fences repositories against concurrent build pushes.
	// Production supplies it; isolated workflows without registry grants may omit it.
	RegistryAuth *RegistryAuthorization
}

func NewImageService(deps ImageServiceDeps) (*ImageService, error) {
	required := []struct {
		name  string
		value any
	}{
		{"repository", deps.Repository}, {"docker", deps.Docker},
		{"registry", deps.Registry}, {"containers", deps.Containers},
		{"config", deps.Config}, {"lifecycle", deps.Lifecycle},
	}
	for _, dependency := range required {
		if dependency.value == nil || isNilServiceDependency(dependency.value) {
			return nil, fmt.Errorf("image service: %s is required", dependency.name)
		}
	}
	return &ImageService{
		repo:         deps.Repository,
		dockerAPI:    deps.Docker,
		registryAPI:  deps.Registry,
		contRepo:     deps.Containers,
		cfg:          deps.Config,
		lifecycle:    deps.Lifecycle,
		registryAuth: deps.RegistryAuth,
	}, nil
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

	if err := s.deleteRegistryManifest(ctx, repoName, version); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		writeCtx, cancel := detachedContainerStateContext(ctx)
		defer cancel()
		if requeueErr := s.lifecycle.RequeueOperation(writeCtx, operationID, err); requeueErr != nil {
			return apperrors.Wrap(apperrors.ErrUnavailable, "image deletion retry could not be saved", errors.Join(err, requeueErr))
		}
		return err
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

// deleteRegistryManifest removes the tag from the Registry (soft delete) while
// new builds of the repository are excluded. The repository lock is released
// before Docker and database cleanup so it covers only the bounded Registry
// attempt. ErrNotFound means the manifest is already absent.
func (s *ImageService) deleteRegistryManifest(ctx context.Context, repoName, version string) error {
	if s.registryAuth != nil {
		unlock, err := s.registryAuth.FenceAndLockRepository(ctx, repoName)
		if err != nil {
			return err
		}
		defer unlock()
	}
	_, digest, err := s.registryAPI.GetImageSizeAndDigest(ctx, repoName, version)
	if err != nil {
		return err
	}
	if digest == "" {
		return apperrors.New(apperrors.ErrUnavailable, "registry manifest digest is missing")
	}
	return s.registryAPI.DeleteManifest(ctx, repoName, digest)
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

func (s *ImageService) markImageError(ctx context.Context, imageID uuid.UUID, status string, cause error) {
	_ = s.repo.MarkStatusError(ctx, imageID, status, cause)
}
