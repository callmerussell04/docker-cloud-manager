package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type BuildProvider interface {
	GetUserBuilds(ctx context.Context, ownerID string) ([]model.Build, error)
	DeleteBuild(ctx context.Context, ownerID, buildID string) error
	GetAllBuilds(ctx context.Context, page, limit int) (model.PaginatedBuilds, error)
	AdminDeleteBuild(ctx context.Context, buildID string) error
}

func (s *Core) GetUserBuilds(ctx context.Context, ownerID string) ([]model.Build, error) {
	return s.provider.GetUserBuilds(ctx, ownerID)
}

func (s *Core) DeleteBuild(ctx context.Context, ownerID, buildID string) error {
	return s.provider.DeleteBuild(ctx, ownerID, buildID)
}

func (s *Core) GetAllBuilds(ctx context.Context, page, limit int) (model.PaginatedBuilds, error) {
	return s.provider.GetAllBuilds(ctx, page, limit)
}

func (s *Core) AdminDeleteBuild(ctx context.Context, buildID string) error {
	return s.provider.AdminDeleteBuild(ctx, buildID)
}
