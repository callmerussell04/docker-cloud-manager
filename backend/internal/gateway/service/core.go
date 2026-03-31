package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/dto"
)

type CoreProvider interface {
	CreateContainer(ctx context.Context, ownerID string, dto dto.CreateContainerDTO) (string, error)
	GetUserContainers(ctx context.Context, ownerID string) ([]*core.ContainerData, error)
	ActionContainer(ctx context.Context, ownerID, containerID, action string) error
	ExposeContainer(ctx context.Context, ownerID, containerID, domainName string) error
	CreateVolume(ctx context.Context, ownerID string, dto dto.CreateVolumeDTO) (string, error)
	DeleteVolume(ctx context.Context, ownerID, volumeID string) error
	GetUserVolumes(ctx context.Context, ownerID string) ([]*core.VolumeData, error)
	GetUserImages(ctx context.Context, ownerID string) ([]*core.ImageData, error)
	DeleteImage(ctx context.Context, ownerID, imageID string) error
}

type Core struct {
	provider CoreProvider
}

func NewCore(provider CoreProvider) *Core {
	return &Core{
		provider: provider,
	}
}

func (s *Core) CreateContainer(ctx context.Context, ownerID string, dto dto.CreateContainerDTO) (string, error) {
	return s.provider.CreateContainer(ctx, ownerID, dto)
}

func (s *Core) GetUserContainers(ctx context.Context, ownerID string) ([]*core.ContainerData, error) {
	return s.provider.GetUserContainers(ctx, ownerID)
}

func (s *Core) ActionContainer(ctx context.Context, ownerID, containerID, action string) error {
	return s.provider.ActionContainer(ctx, ownerID, containerID, action)
}

func (s *Core) ExposeContainer(ctx context.Context, ownerID, containerID, domainName string) error {
	return s.provider.ExposeContainer(ctx, ownerID, containerID, domainName)
}

func (s *Core) CreateVolume(ctx context.Context, ownerID string, dto dto.CreateVolumeDTO) (string, error) {
	return s.provider.CreateVolume(ctx, ownerID, dto)
}

func (s *Core) DeleteVolume(ctx context.Context, ownerID, volumeID string) error {
	return s.provider.DeleteVolume(ctx, ownerID, volumeID)
}

func (s *Core) GetUserVolumes(ctx context.Context, ownerID string) ([]*core.VolumeData, error) {
	return s.provider.GetUserVolumes(ctx, ownerID)
}

func (s *Core) GetUserImages(ctx context.Context, ownerID string) ([]*core.ImageData, error) {
	return s.provider.GetUserImages(ctx, ownerID)
}

func (s *Core) DeleteImage(ctx context.Context, ownerID, imageID string) error {
	return s.provider.DeleteImage(ctx, ownerID, imageID)
}
