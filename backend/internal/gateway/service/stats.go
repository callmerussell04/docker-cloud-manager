package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
)

type StatsProvider interface {
	GetUserStats(ctx context.Context, ownerID string) (dto.UserStatsDTO, error)
}

func (s *Core) GetUserStats(ctx context.Context, ownerID string) (dto.UserStatsDTO, error) {
	return s.provider.GetUserStats(ctx, ownerID)
}
