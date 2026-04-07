package service

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/domain/dto"
)

type CoreProvider interface {
	CreateContainer(ctx context.Context, ownerID string, createContainerDTO dto.CreateContainerDTO) (string, error)
	GetUserContainers(ctx context.Context, ownerID string) ([]*coreapi.ContainerData, error)
	ActionContainer(ctx context.Context, ownerID, containerID, action string) error
	ExposeContainer(ctx context.Context, ownerID, containerID, domainPrefix string, internalPort int) error
	CreateVolume(ctx context.Context, ownerID string, createVolumeDTO dto.CreateVolumeDTO) (string, error)
	DeleteVolume(ctx context.Context, ownerID, volumeID string) error
	GetUserVolumes(ctx context.Context, ownerID string) ([]*coreapi.VolumeData, error)
	GetUserImages(ctx context.Context, ownerID string) ([]*coreapi.ImageData, error)
	DeleteImage(ctx context.Context, ownerID, imageID string) error
	GetUserBuilds(ctx context.Context, ownerID string) ([]*coreapi.BuildData, error)
	DeleteBuild(ctx context.Context, ownerID, buildID string) error
	GetUserProjects(ctx context.Context, ownerID string) ([]*coreapi.ProjectData, error)
	DeleteProject(ctx context.Context, ownerID, projectID string) error
	StopProject(ctx context.Context, ownerID, projectID string) error
}

type Core struct {
	provider CoreProvider
}

func NewCore(provider CoreProvider) *Core {
	return &Core{
		provider: provider,
	}
}

func (s *Core) CreateContainer(ctx context.Context, ownerID string, createContainerDTO dto.CreateContainerDTO) (string, error) {
	return s.provider.CreateContainer(ctx, ownerID, createContainerDTO)
}

func (s *Core) GetUserContainers(ctx context.Context, ownerID string) ([]*coreapi.ContainerData, error) {
	return s.provider.GetUserContainers(ctx, ownerID)
}

func (s *Core) ActionContainer(ctx context.Context, ownerID, containerID, action string) error {
	return s.provider.ActionContainer(ctx, ownerID, containerID, action)
}

func (s *Core) ExposeContainer(ctx context.Context, ownerID, containerID, domainPrefix string, internalPort int) error {
	return s.provider.ExposeContainer(ctx, ownerID, containerID, domainPrefix, internalPort)
}

func (s *Core) CreateVolume(ctx context.Context, ownerID string, createVolumeDTO dto.CreateVolumeDTO) (string, error) {
	return s.provider.CreateVolume(ctx, ownerID, createVolumeDTO)
}

func (s *Core) DeleteVolume(ctx context.Context, ownerID, volumeID string) error {
	return s.provider.DeleteVolume(ctx, ownerID, volumeID)
}

func (s *Core) GetUserVolumes(ctx context.Context, ownerID string) ([]*coreapi.VolumeData, error) {
	return s.provider.GetUserVolumes(ctx, ownerID)
}

func (s *Core) GetUserImages(ctx context.Context, ownerID string) ([]*coreapi.ImageData, error) {
	return s.provider.GetUserImages(ctx, ownerID)
}

func (s *Core) DeleteImage(ctx context.Context, ownerID, imageID string) error {
	return s.provider.DeleteImage(ctx, ownerID, imageID)
}

func (s *Core) GetUserBuilds(ctx context.Context, ownerID string) ([]*coreapi.BuildData, error) {
	return s.provider.GetUserBuilds(ctx, ownerID)
}

func (s *Core) DeleteBuild(ctx context.Context, ownerID, buildID string) error {
	return s.provider.DeleteBuild(ctx, ownerID, buildID)
}

func (s *Core) GetUserProjects(ctx context.Context, ownerID string) ([]*coreapi.ProjectData, error) {
	return s.provider.GetUserProjects(ctx, ownerID)
}

func (s *Core) DeleteProject(ctx context.Context, ownerID, projectID string) error {
	return s.provider.DeleteProject(ctx, ownerID, projectID)
}

func (s *Core) StopProject(ctx context.Context, ownerID, projectID string) error {
	return s.provider.StopProject(ctx, ownerID, projectID)
}
