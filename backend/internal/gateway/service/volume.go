package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type VolumeProvider interface {
	CreateVolume(ctx context.Context, createVolumeDTO model.CreateVolumeInput) (string, error)
	DeleteVolume(ctx context.Context, volumeID string) error
	ListVolumes(ctx context.Context, page, limit int) (model.PaginatedVolumes, error)
}

func (s *Core) CreateVolume(ctx context.Context, createVolumeDTO model.CreateVolumeInput) (string, error) {
	return s.provider.CreateVolume(ctx, createVolumeDTO)
}

func (s *Core) DeleteVolume(ctx context.Context, volumeID string) error {
	return s.provider.DeleteVolume(ctx, volumeID)
}

func (s *Core) ListVolumes(ctx context.Context, page, limit int) (model.PaginatedVolumes, error) {
	return s.provider.ListVolumes(ctx, page, limit)
}
