package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
)

type VolumeProvider interface {
	CreateVolume(ctx context.Context, ownerID string, createVolumeDTO dto.CreateVolumeDTO) (string, error)
	DeleteVolume(ctx context.Context, ownerID, volumeID string) error
	GetUserVolumes(ctx context.Context, ownerID string) ([]dto.VolumeDTO, error)
	GetAllVolumes(ctx context.Context, page, limit int) (dto.PaginatedVolumes, error)
	AdminDeleteVolume(ctx context.Context, volumeID string) error
}

func (s *Core) CreateVolume(ctx context.Context, ownerID string, createVolumeDTO dto.CreateVolumeDTO) (string, error) {
	return s.provider.CreateVolume(ctx, ownerID, createVolumeDTO)
}

func (s *Core) DeleteVolume(ctx context.Context, ownerID, volumeID string) error {
	return s.provider.DeleteVolume(ctx, ownerID, volumeID)
}

func (s *Core) GetUserVolumes(ctx context.Context, ownerID string) ([]dto.VolumeDTO, error) {
	return s.provider.GetUserVolumes(ctx, ownerID)
}

func (s *Core) GetAllVolumes(ctx context.Context, page, limit int) (dto.PaginatedVolumes, error) {
	return s.provider.GetAllVolumes(ctx, page, limit)
}

func (s *Core) AdminDeleteVolume(ctx context.Context, volumeID string) error {
	return s.provider.AdminDeleteVolume(ctx, volumeID)
}
