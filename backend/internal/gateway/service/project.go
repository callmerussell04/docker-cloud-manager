package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type ProjectProvider interface {
	DeleteProject(ctx context.Context, projectID string) error
	StartProject(ctx context.Context, projectID string) error
	StopProject(ctx context.Context, projectID string) error
	CancelProject(ctx context.Context, projectID string) error
	ListProjects(ctx context.Context, page, limit int) (model.PaginatedProjects, error)
}

func (s *Core) DeleteProject(ctx context.Context, projectID string) error {
	return s.provider.DeleteProject(ctx, projectID)
}

func (s *Core) StartProject(ctx context.Context, projectID string) error {
	return s.provider.StartProject(ctx, projectID)
}

func (s *Core) StopProject(ctx context.Context, projectID string) error {
	return s.provider.StopProject(ctx, projectID)
}

func (s *Core) CancelProject(ctx context.Context, projectID string) error {
	return s.provider.CancelProject(ctx, projectID)
}

func (s *Core) ListProjects(ctx context.Context, page, limit int) (model.PaginatedProjects, error) {
	return s.provider.ListProjects(ctx, page, limit)
}
