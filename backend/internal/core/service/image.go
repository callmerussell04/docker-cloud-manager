package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type ImageRepository interface {
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]model.Image, error)
	GetByID(ctx context.Context, id uuid.UUID) (model.Image, error)
	Delete(ctx context.Context, id uuid.UUID) error
	GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Image, int, error)
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

func (s *ImageService) GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Image, error) {
	return s.repo.GetByOwnerID(ctx, ownerID)
}

func (s *ImageService) Delete(ctx context.Context, ownerID, imageID uuid.UUID) error {
	img, err := s.repo.GetByID(ctx, imageID)
	if err != nil {
		return err
	}

	if img.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	if !img.IsCustom {
		return errors.New("cannot delete system image")
	}

	inUse, err := s.contRepo.IsImageInUse(ctx, ownerID, img.Tag)
	if err != nil {
		return err
	}
	if inUse {
		return fmt.Errorf("conflict: unable to remove image, it is currently in use by a container")
	}

	baseName, version := parseImageTag(img.Tag)
	repoName := strings.ToLower(fmt.Sprintf("%s_%s", img.OwnerID.String(), baseName))

	// Удаление из Registry (Soft Delete)
	_, digest, err := s.registryAPI.GetImageSizeAndDigest(ctx, repoName, version)
	if err == nil && digest != "" {
		_ = s.registryAPI.DeleteManifest(ctx, repoName, digest)
	}

	// Удаление из локального кэша Docker Engine
	fullTag := fmt.Sprintf("%s/%s:%s", s.cfg.Get().RegistryPublicURL, repoName, version)
	_ = s.dockerAPI.RemoveImage(ctx, fullTag, false)

	// Удаление записи из бд
	return s.repo.Delete(ctx, imageID)
}

func parseImageTag(rawTag string) (baseName, version string) {
	parts := strings.SplitN(rawTag, ":", 2)
	if len(parts) == 1 || parts[1] == "" {
		return parts[0], "latest"
	}
	return parts[0], parts[1]
}

func (s *ImageService) GetAllPaginatedImages(ctx context.Context, limit, offset int) ([]model.Image, int, error) {
	return s.repo.GetAllPaginated(ctx, limit, offset)
}

func (s *ImageService) AdminDeleteImage(ctx context.Context, imageID uuid.UUID) error {
	img, err := s.repo.GetByID(ctx, imageID)
	if err != nil {
		return err
	}

	if !img.IsCustom {
		return errors.New("cannot delete system image")
	}

	inUse, err := s.contRepo.IsImageInUse(ctx, img.OwnerID, img.Tag)
	if err != nil {
		return err
	}
	if inUse {
		return fmt.Errorf("conflict: unable to remove image, it is currently in use")
	}

	baseName, version := parseImageTag(img.Tag)
	repoName := strings.ToLower(fmt.Sprintf("%s_%s", img.OwnerID.String(), baseName))

	_, digest, err := s.registryAPI.GetImageSizeAndDigest(ctx, repoName, version)
	if err == nil && digest != "" {
		_ = s.registryAPI.DeleteManifest(ctx, repoName, digest)
	}

	fullTag := fmt.Sprintf("%s/%s:%s", s.cfg.Get().RegistryPublicURL, repoName, version)
	_ = s.dockerAPI.RemoveImage(ctx, fullTag, false)

	return s.repo.Delete(ctx, imageID)
}
