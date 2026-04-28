package grpcclient

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/grpcerrors"
)

func (c *CoreClient) GetUserProjects(ctx context.Context, ownerID string) ([]model.Project, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.projectAPI.GetUserProjects(ctx, req)
	if err != nil {
		return nil, grpcerrors.FromGRPC(err)
	}
	return projectsFromProto(resp.GetProjects()), nil
}

func (c *CoreClient) DeleteProject(ctx context.Context, ownerID, projectID string) error {
	req := &coreapi.ProjectActionRequest{OwnerId: ownerID, ProjectId: projectID}
	_, err := c.projectAPI.DeleteProject(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) StartProject(ctx context.Context, ownerID, projectID string) error {
	req := &coreapi.ProjectActionRequest{OwnerId: ownerID, ProjectId: projectID}
	_, err := c.projectAPI.StartProject(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) StopProject(ctx context.Context, ownerID, projectID string) error {
	req := &coreapi.ProjectActionRequest{OwnerId: ownerID, ProjectId: projectID}
	_, err := c.projectAPI.StopProject(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) GetAllProjects(ctx context.Context, page, limit int) (model.PaginatedProjects, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.projectAPI.GetAllProjects(ctx, req)
	if err != nil {
		return model.PaginatedProjects{}, grpcerrors.FromGRPC(err)
	}
	return model.PaginatedProjects{
		Projects:   projectsFromProto(resp.GetProjects()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func (c *CoreClient) AdminDeleteProject(ctx context.Context, projectID string) error {
	req := &coreapi.ProjectActionRequest{ProjectId: projectID}
	_, err := c.projectAPI.AdminDeleteProject(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) AdminStartProject(ctx context.Context, projectID string) error {
	req := &coreapi.ProjectActionRequest{ProjectId: projectID}
	_, err := c.projectAPI.AdminStartProject(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
}

func (c *CoreClient) AdminStopProject(ctx context.Context, projectID string) error {
	req := &coreapi.ProjectActionRequest{ProjectId: projectID}
	_, err := c.projectAPI.AdminStopProject(ctx, req)
	if err != nil {
		return grpcerrors.FromGRPC(err)
	}
	return nil
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
