package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type ImageProvider interface {
	DeleteImage(ctx context.Context, imageID string) error
	ListImages(ctx context.Context, page, limit int) (model.PaginatedImages, error)
}

func (s *Core) DeleteImage(ctx context.Context, imageID string) error {
	return s.provider.DeleteImage(ctx, imageID)
}

func (s *Core) ListImages(ctx context.Context, page, limit int) (model.PaginatedImages, error) {
	return s.provider.ListImages(ctx, page, limit)
}
