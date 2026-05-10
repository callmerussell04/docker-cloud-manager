package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
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

	baseName, version := parseImageTag(img.Tag)
	repoName := customImageRepositoryName(img.OwnerID, baseName)
	s.setImageStatus(ctx, imageID, model.ImageStatusDeleting)

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
		s.markImageError(ctx, imageID, model.ImageStatusError, err)
		return err
	}
	return nil
}

func parseImageTag(rawTag string) (baseName, version string) {
	parts := strings.SplitN(rawTag, ":", 2)
	if len(parts) == 1 || parts[1] == "" {
		return parts[0], "latest"
	}
	return parts[0], parts[1]
}

func (s *ImageService) List(ctx context.Context, limit, offset int) ([]model.Image, int, error) {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return nil, 0, err
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
