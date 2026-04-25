package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
)

type SystemProvider interface {
	GetSystemConfig(ctx context.Context) (dto.SystemConfigDTO, error)
	UpdateSystemConfig(ctx context.Context, req dto.SystemConfigDTO) error
}

func (s *Core) GetSystemConfig(ctx context.Context) (dto.SystemConfigDTO, error) {
	return s.provider.GetSystemConfig(ctx)
}

func (s *Core) UpdateSystemConfig(ctx context.Context, req dto.SystemConfigDTO) error {
	return s.provider.UpdateSystemConfig(ctx, req)
}
