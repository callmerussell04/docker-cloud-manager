package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
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
	return s.cfgManager.Update(newConfig)
}
