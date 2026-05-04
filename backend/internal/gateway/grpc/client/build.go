package grpcclient

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
)

func (c *CoreClient) CreateBuildJob(ctx context.Context, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (string, string, error) {
	req := &coreapi.CreateBuildJobRequest{
		Tag:              tag,
		ArchiveObjectKey: archiveObjectKey,
		LogObjectKey:     logObjectKey,
		ContextDir:       contextDir,
		Dockerfile:       dockerfile,
		BuildArgs:        buildArgs,
		RequestId:        requestID,
	}
	resp, err := c.imageAPI.CreateBuildJob(ctx, req)
	if err != nil {
		return "", "", grpcerrors.FromGRPC(err)
	}
	return resp.GetBuildId(), resp.GetImageId(), nil
}

func (c *CoreClient) GetBuild(ctx context.Context, buildID string) (model.Build, error) {
	req := &coreapi.BuildActionRequest{BuildId: buildID}
	resp, err := c.imageAPI.GetBuild(ctx, req)
	if err != nil {
		return model.Build{}, grpcerrors.FromGRPC(err)
	}
	return buildFromProto(resp), nil
}

func (c *CoreClient) CancelBuildRecord(ctx context.Context, buildID string) error {
	req := &coreapi.BuildActionRequest{BuildId: buildID}
	_, err := c.imageAPI.CancelBuildRecord(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) DeleteBuild(ctx context.Context, buildID string) error {
	req := &coreapi.BuildActionRequest{BuildId: buildID}
	_, err := c.imageAPI.DeleteBuild(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) GetAllBuilds(ctx context.Context, page, limit int) (model.PaginatedBuilds, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.imageAPI.ListBuilds(ctx, req)
	if err != nil {
		return model.PaginatedBuilds{}, grpcerrors.FromGRPC(err)
	}
	return model.PaginatedBuilds{
		Builds:     buildsFromProto(resp.GetBuilds()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func buildsFromProto(items []*coreapi.BuildData) []model.Build {
	result := make([]model.Build, 0, len(items))
	for _, item := range items {
		result = append(result, buildFromProto(item))
	}
	return result
}

func buildFromProto(item *coreapi.BuildData) model.Build {
	if item == nil {
		return model.Build{}
	}
	return model.Build{
		ID:            item.GetId(),
		ImageID:       item.GetImageId(),
		Status:        item.GetStatus(),
		StartedAt:     item.GetStartedAt(),
		FinishedAt:    item.GetFinishedAt(),
		LogFilePath:   item.GetLogFilePath(),
		OwnerID:       item.GetOwnerId(),
		OwnerUsername: item.GetOwnerUsername(),
	}
}
