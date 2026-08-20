package grpcclient

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
)

func (c *CoreClient) DeleteProject(ctx context.Context, projectID string) error {
	req := &coreapi.ProjectActionRequest{ProjectId: projectID}
	_, err := c.projectAPI.DeleteProject(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) StartProject(ctx context.Context, projectID string) error {
	req := &coreapi.ProjectActionRequest{ProjectId: projectID}
	_, err := c.projectAPI.StartProject(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) StopProject(ctx context.Context, projectID string) error {
	req := &coreapi.ProjectActionRequest{ProjectId: projectID}
	_, err := c.projectAPI.StopProject(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) CancelProject(ctx context.Context, projectID string) error {
	req := &coreapi.ProjectActionRequest{ProjectId: projectID}
	_, err := c.projectAPI.CancelProject(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) ListProjects(ctx context.Context, page, limit int) (model.PaginatedProjects, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.projectAPI.ListProjects(ctx, req)
	if err != nil {
		return model.PaginatedProjects{}, grpcerrors.FromGRPC(err)
	}
	return model.PaginatedProjects{
		Projects:   projectsFromProto(resp.GetProjects()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func projectsFromProto(items []*coreapi.ProjectData) []model.Project {
	result := make([]model.Project, 0, len(items))
	for _, item := range items {
		result = append(result, model.Project{
			ID:            item.GetId(),
			Name:          item.GetName(),
			Status:        item.GetStatus(),
			ErrorMessage:  item.GetErrorMessage(),
			LastError:     item.GetLastError(),
			CreatedAt:     item.GetCreatedAt(),
			OwnerID:       item.GetOwnerId(),
			OwnerUsername: item.GetOwnerUsername(),
		})
	}
	return result
}
