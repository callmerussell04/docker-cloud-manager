package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type BuildProvider interface {
	CreateBuildJob(ctx context.Context, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (string, string, error)
	GetBuild(ctx context.Context, buildID string) (model.Build, error)
	CancelBuildRecord(ctx context.Context, buildID string) error
	DeleteBuild(ctx context.Context, buildID string) error
	GetAllBuilds(ctx context.Context, page, limit int) (model.PaginatedBuilds, error)
}

func (s *Core) CreateBuildJob(ctx context.Context, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (string, string, error) {
	return s.provider.CreateBuildJob(ctx, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile, buildArgs, requestID)
}

func (s *Core) GetBuild(ctx context.Context, buildID string) (model.Build, error) {
	return s.provider.GetBuild(ctx, buildID)
}

func (s *Core) CancelBuildRecord(ctx context.Context, buildID string) error {
	return s.provider.CancelBuildRecord(ctx, buildID)
}

func (s *Core) DeleteBuild(ctx context.Context, buildID string) error {
	return s.provider.DeleteBuild(ctx, buildID)
}

func (s *Core) GetAllBuilds(ctx context.Context, page, limit int) (model.PaginatedBuilds, error) {
	return s.provider.GetAllBuilds(ctx, page, limit)
}
