package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type ImageRepository interface {
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Image, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.Image, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Save(ctx context.Context, img domain.Image) error
	UpdateSize(ctx context.Context, id uuid.UUID, sizeMB int) error
}

type BuildRepository interface {
	Save(ctx context.Context, b domain.Build) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
}

type ImageDockerAPI interface {
	RemoveImage(ctx context.Context, imageID string, force bool) error
}

type ImageService struct {
	repo      ImageRepository
	buildRepo BuildRepository
	dockerAPI ImageDockerAPI
}

func NewImageService(repo ImageRepository, buildRepo BuildRepository, dockerAPI ImageDockerAPI) *ImageService {
	return &ImageService{
		repo:      repo,
		buildRepo: buildRepo,
		dockerAPI: dockerAPI,
	}
}

func (s *ImageService) GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Image, error) {
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

	err = s.dockerAPI.RemoveImage(ctx, img.Tag, false)
	if err != nil {
		return err
	}

	return s.repo.Delete(ctx, imageID)
}

func (s *ImageService) InitBuild(ctx context.Context, ownerID uuid.UUID, tag, logFilePath string) (uuid.UUID, uuid.UUID, error) {
	imageID := uuid.New()
	buildID := uuid.New()

	img := domain.Image{
		ID:        imageID,
		OwnerID:   ownerID,
		Tag:       tag,
		SizeMB:    0,
		IsCustom:  true,
		CreatedAt: time.Now(),
	}

	if err := s.repo.Save(ctx, img); err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	build := domain.Build{
		ID:          buildID,
		ImageID:     imageID,
		Status:      domain.BuildStatusRunning,
		LogFilePath: logFilePath,
		StartedAt:   time.Now(),
	}

	if err := s.buildRepo.Save(ctx, build); err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	return imageID, buildID, nil
}

func (s *ImageService) CompleteBuild(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error {
	if err := s.buildRepo.UpdateStatus(ctx, buildID, status); err != nil {
		return err
	}

	if status == domain.BuildStatusSuccess {
		if err := s.repo.UpdateSize(ctx, imageID, sizeMB); err != nil {
			return err
		}
	}

	return nil
}
