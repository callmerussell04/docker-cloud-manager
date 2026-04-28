package service

import (
	"context"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type SystemService struct {
	cfgManager *config.Manager
}

func NewSystemService(cfgManager *config.Manager) *SystemService {
	return &SystemService{cfgManager: cfgManager}
}

func (s *SystemService) GetConfig(ctx context.Context) config.SystemConfig {
	return s.cfgManager.Get()
}

func (s *SystemService) UpdateConfig(ctx context.Context, newConfig config.SystemConfig) error {
	if err := config.ValidateSystemConfig(newConfig); err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	if err := s.cfgManager.Update(newConfig); err != nil {
		return fmt.Errorf("failed to update system config: %w", err)
	}
	return nil
}
