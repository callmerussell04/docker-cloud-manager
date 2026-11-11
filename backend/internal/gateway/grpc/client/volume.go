package client

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/grpcerrors"
)

func (c *CoreClient) CreateVolume(ctx context.Context, createVolumeDTO model.CreateVolumeInput) (string, error) {
	req := &coreapi.CreateVolumeRequest{
		Name:      createVolumeDTO.Name,
		SizeBytes: createVolumeDTO.SizeBytes,
	}
	resp, err := c.volumeAPI.CreateVolume(ctx, req)
	if err != nil {
		return "", grpcerrors.FromGRPC(err)
	}
	return resp.GetVolumeId(), nil
}

func (c *CoreClient) DeleteVolume(ctx context.Context, volumeID string) error {
	req := &coreapi.VolumeActionRequest{VolumeId: volumeID}
	_, err := c.volumeAPI.DeleteVolume(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) ListVolumes(ctx context.Context, page, limit int) (model.PaginatedVolumes, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.volumeAPI.ListVolumes(ctx, req)
	if err != nil {
		return model.PaginatedVolumes{}, grpcerrors.FromGRPC(err)
	}
	return model.PaginatedVolumes{
		Volumes:    volumesFromProto(resp.GetVolumes()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func volumesFromProto(items []*coreapi.VolumeData) []model.Volume {
	result := make([]model.Volume, 0, len(items))
	for _, item := range items {
		result = append(result, model.Volume{
			ID:              item.GetId(),
			Name:            item.GetName(),
			DockerName:      item.GetDockerName(),
			Status:          item.GetStatus(),
			LastError:       item.GetLastError(),
			UsedBytes:       item.GetUsedBytes(),
			UsageObservedAt: item.GetUsageObservedAt(),
			SizeBytes:       item.GetSizeBytes(),
			SizeEnforced:    item.GetSizeEnforced(),
			CreatedAt:       item.GetCreatedAt(),
			OwnerID:         item.GetOwnerId(),
			OwnerUsername:   item.GetOwnerUsername(),
		})
	}
	return result
}
