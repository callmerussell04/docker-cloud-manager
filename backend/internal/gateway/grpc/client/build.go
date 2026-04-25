package grpcclient

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
)

func (c *CoreClient) GetUserBuilds(ctx context.Context, ownerID string) ([]model.Build, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.imageAPI.GetUserBuilds(ctx, req)
	if err != nil {
		return nil, grpcerrors.FromGRPC(err)
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
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) GetAllBuilds(ctx context.Context, page, limit int) (model.PaginatedBuilds, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.imageAPI.GetAllBuilds(ctx, req)
	if err != nil {
		return model.PaginatedBuilds{}, grpcerrors.FromGRPC(err)
	}
	return model.PaginatedBuilds{
		Builds:     buildsFromProto(resp.GetBuilds()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func (c *CoreClient) AdminDeleteBuild(ctx context.Context, buildID string) error {
	req := &coreapi.BuildActionRequest{BuildId: buildID}
	_, err := c.imageAPI.AdminDeleteBuild(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func buildsFromProto(items []*coreapi.BuildData) []model.Build {
	result := make([]model.Build, 0, len(items))
	for _, item := range items {
		result = append(result, model.Build{
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
