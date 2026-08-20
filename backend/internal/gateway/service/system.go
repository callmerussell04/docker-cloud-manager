package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type SystemProvider interface {
	GetSystemConfig(ctx context.Context) (model.SystemConfig, error)
	UpdateSystemConfig(ctx context.Context, req model.SystemConfig) error
}

func (s *Core) GetSystemConfig(ctx context.Context) (model.SystemConfig, error) {
	return s.provider.GetSystemConfig(ctx)
}

func (s *Core) UpdateSystemConfig(ctx context.Context, req model.SystemConfig) error {
	return s.provider.UpdateSystemConfig(ctx, req)
}
