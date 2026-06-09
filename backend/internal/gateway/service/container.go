package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
)

type ContainerProvider interface {
	CreateContainer(ctx context.Context, createContainerDTO model.CreateContainerInput) (string, error)
	ActionContainer(ctx context.Context, containerID, action string) error
	ExposeContainer(ctx context.Context, containerID, domainPrefix string, internalPort int) error
	ListContainers(ctx context.Context, page, limit int, projectID string) (model.PaginatedContainers, error)
	GetContainerStats(ctx context.Context, containerID string) (model.ContainerStats, error)
}

func (s *Core) CreateContainer(ctx context.Context, createContainerDTO model.CreateContainerInput) (string, error) {
	return s.provider.CreateContainer(ctx, createContainerDTO)
}

func (s *Core) ActionContainer(ctx context.Context, containerID, action string) error {
	return s.provider.ActionContainer(ctx, containerID, action)
}

func (s *Core) ExposeContainer(ctx context.Context, containerID, domainPrefix string, internalPort int) error {
	return s.provider.ExposeContainer(ctx, containerID, domainPrefix, internalPort)
}

func (s *Core) ListContainers(ctx context.Context, page, limit int, projectID string) (model.PaginatedContainers, error) {
	return s.provider.ListContainers(ctx, page, limit, projectID)
}

func (s *Core) GetContainerStats(ctx context.Context, containerID string) (model.ContainerStats, error) {
	return s.provider.GetContainerStats(ctx, containerID)
}
