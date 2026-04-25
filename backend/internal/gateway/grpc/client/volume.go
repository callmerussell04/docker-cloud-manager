package grpcclient

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

func (c *CoreClient) CreateVolume(ctx context.Context, ownerID string, createVolumeDTO dto.CreateVolumeDTO) (string, error) {
	req := &coreapi.CreateVolumeRequest{
		OwnerId: ownerID,
		Name:    createVolumeDTO.Name,
	}
	resp, err := c.volumeAPI.CreateVolume(ctx, req)
	if err != nil {
		return "", apperrors.FromGRPC(err)
	}
	return resp.GetVolumeId(), nil
}

func (c *CoreClient) DeleteVolume(ctx context.Context, ownerID, volumeID string) error {
	req := &coreapi.VolumeActionRequest{
		OwnerId:  ownerID,
		VolumeId: volumeID,
	}
	_, err := c.volumeAPI.DeleteVolume(ctx, req)
	if err != nil {
		return apperrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) GetUserVolumes(ctx context.Context, ownerID string) ([]dto.VolumeDTO, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.volumeAPI.GetUserVolumes(ctx, req)
	if err != nil {
		return nil, apperrors.FromGRPC(err)
	}
	return volumesFromProto(resp.GetVolumes()), nil
}

func (c *CoreClient) GetAllVolumes(ctx context.Context, page, limit int) (dto.PaginatedVolumes, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.volumeAPI.GetAllVolumes(ctx, req)
	if err != nil {
		return dto.PaginatedVolumes{}, apperrors.FromGRPC(err)
	}
	return dto.PaginatedVolumes{
		Volumes:    volumesFromProto(resp.GetVolumes()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func (c *CoreClient) AdminDeleteVolume(ctx context.Context, volumeID string) error {
	req := &coreapi.VolumeActionRequest{VolumeId: volumeID}
	_, err := c.volumeAPI.AdminDeleteVolume(ctx, req)
	if err != nil {
		return apperrors.FromGRPC(err)
	}
	return nil
}

func volumesFromProto(items []*coreapi.VolumeData) []dto.VolumeDTO {
	result := make([]dto.VolumeDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.VolumeDTO{
			ID:            item.GetId(),
			DockerName:    item.GetDockerName(),
			Driver:        item.GetDriver(),
			CreatedAt:     item.GetCreatedAt(),
			OwnerID:       item.GetOwnerId(),
			OwnerUsername: item.GetOwnerUsername(),
		})
	}
	return result
}
