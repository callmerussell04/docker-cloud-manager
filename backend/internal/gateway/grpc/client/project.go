package grpcclient

import (
	"context"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
)

func (c *CoreClient) GetUserProjects(ctx context.Context, ownerID string) ([]dto.ProjectDTO, error) {
	req := &coreapi.GetUserRequest{OwnerId: ownerID}
	resp, err := c.projectAPI.GetUserProjects(ctx, req)
	if err != nil {
		return nil, mapCoreError(err)
	}
	return projectsFromProto(resp.GetProjects()), nil
}

func (c *CoreClient) DeleteProject(ctx context.Context, ownerID, projectID string) error {
	req := &coreapi.ProjectActionRequest{OwnerId: ownerID, ProjectId: projectID}
	_, err := c.projectAPI.DeleteProject(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) StopProject(ctx context.Context, ownerID, projectID string) error {
	req := &coreapi.ProjectActionRequest{OwnerId: ownerID, ProjectId: projectID}
	_, err := c.projectAPI.StopProject(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) GetAllProjects(ctx context.Context, page, limit int) (dto.PaginatedProjects, error) {
	req := &coreapi.PaginationRequest{Page: int32(page), Limit: int32(limit)}
	resp, err := c.projectAPI.GetAllProjects(ctx, req)
	if err != nil {
		return dto.PaginatedProjects{}, mapCoreError(err)
	}
	return dto.PaginatedProjects{
		Projects:   projectsFromProto(resp.GetProjects()),
		TotalCount: resp.GetTotalCount(),
	}, nil
}

func (c *CoreClient) AdminDeleteProject(ctx context.Context, projectID string) error {
	req := &coreapi.ProjectActionRequest{ProjectId: projectID}
	_, err := c.projectAPI.AdminDeleteProject(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func (c *CoreClient) AdminStopProject(ctx context.Context, projectID string) error {
	req := &coreapi.ProjectActionRequest{ProjectId: projectID}
	_, err := c.projectAPI.AdminStopProject(ctx, req)
	if err != nil {
		return mapCoreError(err)
	}
	return nil
}

func projectsFromProto(items []*coreapi.ProjectData) []dto.ProjectDTO {
	result := make([]dto.ProjectDTO, 0, len(items))
	for _, item := range items {
		result = append(result, dto.ProjectDTO{
			ID:            item.GetId(),
			Name:          item.GetName(),
			Status:        item.GetStatus(),
			ErrorMessage:  item.GetErrorMessage(),
			CreatedAt:     item.GetCreatedAt(),
			OwnerID:       item.GetOwnerId(),
			OwnerUsername: item.GetOwnerUsername(),
		})
	}
	return result
}
