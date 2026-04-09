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
	GetAllContainers(ctx context.Context, page, limit int) (*coreapi.PaginatedContainerResponse, error)
	AdminActionContainer(ctx context.Context, containerID, action string) error
	GetAllVolumes(ctx context.Context, page, limit int) (*coreapi.PaginatedVolumeResponse, error)
	AdminDeleteVolume(ctx context.Context, volumeID string) error
	GetAllImages(ctx context.Context, page, limit int) (*coreapi.PaginatedImageResponse, error)
	AdminDeleteImage(ctx context.Context, imageID string) error
	GetAllBuilds(ctx context.Context, page, limit int) (*coreapi.PaginatedBuildResponse, error)
	AdminDeleteBuild(ctx context.Context, buildID string) error
	GetAllProjects(ctx context.Context, page, limit int) (*coreapi.PaginatedProjectResponse, error)
	AdminDeleteProject(ctx context.Context, projectID string) error
	AdminStopProject(ctx context.Context, projectID string) error
	GetSystemConfig(ctx context.Context) (*coreapi.SystemConfigData, error)
	UpdateSystemConfig(ctx context.Context, req *coreapi.SystemConfigData) error
	GetContainerStats(ctx context.Context, ownerID, containerID string) (*coreapi.ContainerStatsResponse, error)
	AdminGetContainerStats(ctx context.Context, containerID string) (*coreapi.ContainerStatsResponse, error)
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

func (s *Core) GetAllContainers(ctx context.Context, page, limit int) (*coreapi.PaginatedContainerResponse, error) {
	return s.provider.GetAllContainers(ctx, page, limit)
}

func (s *Core) AdminActionContainer(ctx context.Context, containerID, action string) error {
	return s.provider.AdminActionContainer(ctx, containerID, action)
}

func (s *Core) GetAllVolumes(ctx context.Context, page, limit int) (*coreapi.PaginatedVolumeResponse, error) {
	return s.provider.GetAllVolumes(ctx, page, limit)
}

func (s *Core) AdminDeleteVolume(ctx context.Context, volumeID string) error {
	return s.provider.AdminDeleteVolume(ctx, volumeID)
}

func (s *Core) GetAllImages(ctx context.Context, page, limit int) (*coreapi.PaginatedImageResponse, error) {
	return s.provider.GetAllImages(ctx, page, limit)
}

func (s *Core) AdminDeleteImage(ctx context.Context, imageID string) error {
	return s.provider.AdminDeleteImage(ctx, imageID)
}

func (s *Core) GetAllBuilds(ctx context.Context, page, limit int) (*coreapi.PaginatedBuildResponse, error) {
	return s.provider.GetAllBuilds(ctx, page, limit)
}

func (s *Core) AdminDeleteBuild(ctx context.Context, buildID string) error {
	return s.provider.AdminDeleteBuild(ctx, buildID)
}

func (s *Core) GetAllProjects(ctx context.Context, page, limit int) (*coreapi.PaginatedProjectResponse, error) {
	return s.provider.GetAllProjects(ctx, page, limit)
}

func (s *Core) AdminDeleteProject(ctx context.Context, projectID string) error {
	return s.provider.AdminDeleteProject(ctx, projectID)
}

func (s *Core) AdminStopProject(ctx context.Context, projectID string) error {
	return s.provider.AdminStopProject(ctx, projectID)
}

func (s *Core) GetSystemConfig(ctx context.Context) (*coreapi.SystemConfigData, error) {
	return s.provider.GetSystemConfig(ctx)
}

func (s *Core) UpdateSystemConfig(ctx context.Context, req *coreapi.SystemConfigData) error {
	return s.provider.UpdateSystemConfig(ctx, req)
}

func (s *Core) GetContainerStats(ctx context.Context, ownerID, containerID string) (*coreapi.ContainerStatsResponse, error) {
	return s.provider.GetContainerStats(ctx, ownerID, containerID)
}

func (s *Core) AdminGetContainerStats(ctx context.Context, containerID string) (*coreapi.ContainerStatsResponse, error) {
	return s.provider.AdminGetContainerStats(ctx, containerID)
}
