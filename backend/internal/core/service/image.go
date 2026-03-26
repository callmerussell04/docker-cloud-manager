package service

import (
	"context"
	"errors"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

type ImageRepository interface {
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Image, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.Image, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Save(ctx context.Context, img domain.Image) error
}

type ImageDockerAPI interface {
	RemoveImage(ctx context.Context, imageID string, force bool) error
}

type ImageService struct {
	repo      ImageRepository
	dockerAPI ImageDockerAPI
}

func NewImageService(repo ImageRepository, dockerAPI ImageDockerAPI) *ImageService {
	return &ImageService{
		repo:      repo,
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

func (s *ImageService) RegisterCustomImage(ctx context.Context, ownerID uuid.UUID, tag string, sizeMB int) (uuid.UUID, error) {
	img := domain.Image{
		ID:       uuid.New(),
		OwnerID:  ownerID,
		Tag:      tag,
		SizeMB:   sizeMB,
		IsCustom: true,
	}

	if err := s.repo.Save(ctx, img); err != nil {
		return uuid.Nil, err
	}

	return img.ID, nil
}
