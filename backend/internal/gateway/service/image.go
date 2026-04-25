package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type ImageProvider interface {
	GetUserImages(ctx context.Context, ownerID string) ([]model.Image, error)
	DeleteImage(ctx context.Context, ownerID, imageID string) error
	GetAllImages(ctx context.Context, page, limit int) (model.PaginatedImages, error)
	AdminDeleteImage(ctx context.Context, imageID string) error
}

func (s *Core) GetUserImages(ctx context.Context, ownerID string) ([]model.Image, error) {
	return s.provider.GetUserImages(ctx, ownerID)
}

func (s *Core) DeleteImage(ctx context.Context, ownerID, imageID string) error {
	return s.provider.DeleteImage(ctx, ownerID, imageID)
}

func (s *Core) GetAllImages(ctx context.Context, page, limit int) (model.PaginatedImages, error) {
	return s.provider.GetAllImages(ctx, page, limit)
}

func (s *Core) AdminDeleteImage(ctx context.Context, imageID string) error {
	return s.provider.AdminDeleteImage(ctx, imageID)
}
