package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type VolumeProvider interface {
	CreateVolume(ctx context.Context, ownerID string, createVolumeDTO model.CreateVolumeInput) (string, error)
	DeleteVolume(ctx context.Context, ownerID, volumeID string) error
	GetUserVolumes(ctx context.Context, ownerID string) ([]model.Volume, error)
	GetAllVolumes(ctx context.Context, page, limit int) (model.PaginatedVolumes, error)
	AdminDeleteVolume(ctx context.Context, volumeID string) error
}

func (s *Core) CreateVolume(ctx context.Context, ownerID string, createVolumeDTO model.CreateVolumeInput) (string, error) {
	return s.provider.CreateVolume(ctx, ownerID, createVolumeDTO)
}

func (s *Core) DeleteVolume(ctx context.Context, ownerID, volumeID string) error {
	return s.provider.DeleteVolume(ctx, ownerID, volumeID)
}

func (s *Core) GetUserVolumes(ctx context.Context, ownerID string) ([]model.Volume, error) {
	return s.provider.GetUserVolumes(ctx, ownerID)
}

func (s *Core) GetAllVolumes(ctx context.Context, page, limit int) (model.PaginatedVolumes, error) {
	return s.provider.GetAllVolumes(ctx, page, limit)
}

func (s *Core) AdminDeleteVolume(ctx context.Context, volumeID string) error {
	return s.provider.AdminDeleteVolume(ctx, volumeID)
}
