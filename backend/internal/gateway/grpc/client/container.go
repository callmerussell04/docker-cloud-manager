package grpcclient

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
)

func (c *CoreClient) CreateContainer(ctx context.Context, ownerID string, createContainerDTO model.CreateContainerInput) (string, error) {
	var mounts []*coreapi.VolumeMount
	for _, m := range createContainerDTO.VolumeMounts {
		mounts = append(mounts, &coreapi.VolumeMount{
			VolumeId:   m.VolumeID,
			MountPath:  m.MountPath,
			IsReadonly: m.IsReadOnly,
		})
	}

	req := &coreapi.CreateContainerRequest{
		OwnerId:      ownerID,
		Name:         createContainerDTO.Name,
		ImageTag:     createContainerDTO.ImageTag,
		InternalPort: int32(createContainerDTO.InternalPort),
		EnvVars:      createContainerDTO.EnvVars,
		VolumeMounts: mounts,
		DomainPrefix: createContainerDTO.DomainPrefix,
	}

	resp, err := c.containerAPI.CreateContainer(ctx, req)
	if err != nil {
		return "", grpcerrors.FromGRPC(err)
	}

	return resp.GetContainerId(), nil
}

func (c *CoreClient) GetUserContainers(ctx context.Context, ownerID string) ([]model.Container, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.containerAPI.GetUserContainers(ctx, req)
	if err != nil {
		return nil, grpcerrors.FromGRPC(err)
	}
	return containersFromProto(resp.GetContainers()), nil
}

func (c *CoreClient) ActionContainer(ctx context.Context, ownerID, containerID, action string) error {
	req := &coreapi.ContainerActionRequest{
		OwnerId:     ownerID,
		ContainerId: containerID,
	}

	var err error
	switch action {
	case "start":
		_, err = c.containerAPI.StartContainer(ctx, req)
	case "stop":
		_, err = c.containerAPI.StopContainer(ctx, req)
	case "delete":
		_, err = c.containerAPI.DeleteContainer(ctx, req)
	default:
		return apperrors.ErrBadRequest
	}

	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) ExposeContainer(ctx context.Context, ownerID, containerID, domainPrefix string, internalPort int) error {
	req := &coreapi.ExposeRequest{
		OwnerId:      ownerID,
		ContainerId:  containerID,
		DomainPrefix: domainPrefix,
		InternalPort: int32(internalPort),
	}
	_, err := c.containerAPI.ExposeContainer(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) GetAllContainers(ctx context.Context, page, limit int) (model.PaginatedContainers, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.containerAPI.GetAllContainers(ctx, req)
	if err != nil {
		return model.PaginatedContainers{}, grpcerrors.FromGRPC(err)
	}
	return model.PaginatedContainers{
		Containers: containersFromProto(resp.GetContainers()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func (c *CoreClient) AdminActionContainer(ctx context.Context, containerID, action string) error {
	req := &coreapi.ContainerActionRequest{
		ContainerId: containerID,
		Action:      action,
	}

	_, err := c.containerAPI.AdminActionContainer(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) GetContainerStats(ctx context.Context, ownerID, containerID string) (model.ContainerStats, error) {
	req := &coreapi.ContainerActionRequest{
		OwnerId:     ownerID,
		ContainerId: containerID,
	}
	resp, err := c.containerAPI.GetContainerStats(ctx, req)
	if err != nil {
		return model.ContainerStats{}, grpcerrors.FromGRPC(err)
	}
	return containerStatsFromProto(resp), nil
}

func (c *CoreClient) AdminGetContainerStats(ctx context.Context, containerID string) (model.ContainerStats, error) {
	req := &coreapi.ContainerActionRequest{
		ContainerId: containerID,
	}
	resp, err := c.containerAPI.AdminGetContainerStats(ctx, req)
	if err != nil {
		return model.ContainerStats{}, grpcerrors.FromGRPC(err)
	}
	return containerStatsFromProto(resp), nil
}

func containersFromProto(items []*coreapi.ContainerData) []model.Container {
	result := make([]model.Container, 0, len(items))
	for _, item := range items {
		result = append(result, model.Container{
			ID:            item.GetId(),
			DockerID:      item.GetDockerId(),
			Name:          item.GetName(),
			ImageTag:      item.GetImageTag(),
			InternalPort:  item.GetInternalPort(),
			DomainPrefix:  item.GetDomainPrefix(),
			Status:        item.GetStatus(),
			CreatedAt:     item.GetCreatedAt(),
			OwnerID:       item.GetOwnerId(),
			OwnerUsername: item.GetOwnerUsername(),
		})
	}
	return result
}

func containerStatsFromProto(data *coreapi.ContainerStatsResponse) model.ContainerStats {
	return model.ContainerStats{
		CPUPercentage:    data.GetCpuPercentage(),
		MemoryUsageBytes: data.GetMemoryUsageBytes(),
		MemoryLimitBytes: data.GetMemoryLimitBytes(),
		NetworkRxBytes:   data.GetNetworkRxBytes(),
		NetworkTxBytes:   data.GetNetworkTxBytes(),
	}
}
