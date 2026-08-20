package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type StatsProvider interface {
	GetUserStats(ctx context.Context) (model.UserStats, error)
	GetSystemMonitoring(ctx context.Context) (model.SystemMonitoring, error)
}

func (s *Core) GetUserStats(ctx context.Context) (model.UserStats, error) {
	return s.provider.GetUserStats(ctx)
}

func (s *Core) GetSystemMonitoring(ctx context.Context) (model.SystemMonitoring, error) {
	return s.provider.GetSystemMonitoring(ctx)
}
