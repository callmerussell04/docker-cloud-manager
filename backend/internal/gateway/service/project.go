package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type ProjectProvider interface {
	GetUserProjects(ctx context.Context, ownerID string) ([]model.Project, error)
	DeleteProject(ctx context.Context, ownerID, projectID string) error
	StopProject(ctx context.Context, ownerID, projectID string) error
	GetAllProjects(ctx context.Context, page, limit int) (model.PaginatedProjects, error)
	AdminDeleteProject(ctx context.Context, projectID string) error
	AdminStopProject(ctx context.Context, projectID string) error
}

func (s *Core) GetUserProjects(ctx context.Context, ownerID string) ([]model.Project, error) {
	return s.provider.GetUserProjects(ctx, ownerID)
}

func (s *Core) DeleteProject(ctx context.Context, ownerID, projectID string) error {
	return s.provider.DeleteProject(ctx, ownerID, projectID)
}

func (s *Core) StopProject(ctx context.Context, ownerID, projectID string) error {
	return s.provider.StopProject(ctx, ownerID, projectID)
}

func (s *Core) GetAllProjects(ctx context.Context, page, limit int) (model.PaginatedProjects, error) {
	return s.provider.GetAllProjects(ctx, page, limit)
}

func (s *Core) AdminDeleteProject(ctx context.Context, projectID string) error {
	return s.provider.AdminDeleteProject(ctx, projectID)
}

func (s *Core) AdminStopProject(ctx context.Context, projectID string) error {
	return s.provider.AdminStopProject(ctx, projectID)
}
