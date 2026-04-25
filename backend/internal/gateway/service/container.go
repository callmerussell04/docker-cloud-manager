package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type ContainerProvider interface {
	CreateContainer(ctx context.Context, ownerID string, createContainerDTO model.CreateContainerInput) (string, error)
	GetUserContainers(ctx context.Context, ownerID string) ([]model.Container, error)
	ActionContainer(ctx context.Context, ownerID, containerID, action string) error
	ExposeContainer(ctx context.Context, ownerID, containerID, domainPrefix string, internalPort int) error
	GetAllContainers(ctx context.Context, page, limit int) (model.PaginatedContainers, error)
	AdminActionContainer(ctx context.Context, containerID, action string) error
	GetContainerStats(ctx context.Context, ownerID, containerID string) (model.ContainerStats, error)
	AdminGetContainerStats(ctx context.Context, containerID string) (model.ContainerStats, error)
}

func (s *Core) CreateContainer(ctx context.Context, ownerID string, createContainerDTO model.CreateContainerInput) (string, error) {
	return s.provider.CreateContainer(ctx, ownerID, createContainerDTO)
}

func (s *Core) GetUserContainers(ctx context.Context, ownerID string) ([]model.Container, error) {
	return s.provider.GetUserContainers(ctx, ownerID)
}

func (s *Core) ActionContainer(ctx context.Context, ownerID, containerID, action string) error {
	return s.provider.ActionContainer(ctx, ownerID, containerID, action)
}

func (s *Core) ExposeContainer(ctx context.Context, ownerID, containerID, domainPrefix string, internalPort int) error {
	return s.provider.ExposeContainer(ctx, ownerID, containerID, domainPrefix, internalPort)
}

func (s *Core) GetAllContainers(ctx context.Context, page, limit int) (model.PaginatedContainers, error) {
	return s.provider.GetAllContainers(ctx, page, limit)
}

func (s *Core) AdminActionContainer(ctx context.Context, containerID, action string) error {
	return s.provider.AdminActionContainer(ctx, containerID, action)
}

func (s *Core) GetContainerStats(ctx context.Context, ownerID, containerID string) (model.ContainerStats, error) {
	return s.provider.GetContainerStats(ctx, ownerID, containerID)
}

func (s *Core) AdminGetContainerStats(ctx context.Context, containerID string) (model.ContainerStats, error) {
	return s.provider.AdminGetContainerStats(ctx, containerID)
}
