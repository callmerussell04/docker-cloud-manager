package grpcclient

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
)

func (c *CoreClient) GetUserBuilds(ctx context.Context, ownerID string) ([]dto.BuildDTO, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.imageAPI.GetUserBuilds(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return buildsFromProto(resp.GetBuilds()), nil
}

func (c *CoreClient) DeleteBuild(ctx context.Context, ownerID, buildID string) error {
	req := &coreapi.BuildActionRequest{
		OwnerId: ownerID,
		BuildId: buildID,
	}
	_, err := c.imageAPI.DeleteBuild(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetAllBuilds(ctx context.Context, page, limit int) (dto.PaginatedBuilds, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.imageAPI.GetAllBuilds(ctx, req)
	if err != nil {
		return dto.PaginatedBuilds{}, mapCoreError(err)
	}
	return dto.PaginatedBuilds{
		Builds:     buildsFromProto(resp.GetBuilds()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func (c *CoreClient) AdminDeleteBuild(ctx context.Context, buildID string) error {
	req := &coreapi.BuildActionRequest{BuildId: buildID}
	_, err := c.imageAPI.AdminDeleteBuild(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func buildsFromProto(items []*coreapi.BuildData) []dto.BuildDTO {
	result := make([]dto.BuildDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.BuildDTO{
			ID:            item.GetId(),
			ImageID:       item.GetImageId(),
			Status:        item.GetStatus(),
			StartedAt:     item.GetStartedAt(),
			FinishedAt:    item.GetFinishedAt(),
			LogFilePath:   item.GetLogFilePath(),
			OwnerID:       item.GetOwnerId(),
			OwnerUsername: item.GetOwnerUsername(),
		})
	}
	return result
}
