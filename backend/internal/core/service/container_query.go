package service

import (
	"context"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

func resourceUnavailableError(resource string) error {
	if resource == "" {
		resource = "resource"
	}
	return apperrors.New(apperrors.ErrConflict, resource+" is missing in Docker and can only be deleted")
}

func (s *ContainerService) Action(ctx context.Context, containerID uuid.UUID, action string) error {
	switch action {
	case "start":
		return s.Start(ctx, containerID)
	case "stop":
		return s.Stop(ctx, containerID)
	case "delete":
		return s.Delete(ctx, containerID)
	default:
		return fmt.Errorf("%w: invalid container action", apperrors.ErrBadRequest)
	}
}

func (s *ContainerService) refreshProjectStatus(ctx context.Context, projectID *uuid.UUID) {
	if s.projects == nil || projectID == nil {
		return
	}
	if err := s.projects.RefreshProjectStatus(ctx, *projectID); err != nil {
		s.logger.WarnContext(ctx, "failed to refresh project status", "project_id", *projectID, "error", err)
	}
}

func (s *ContainerService) GetStats(ctx context.Context, containerID uuid.UUID) (model.ContainerStats, error) {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return model.ContainerStats{}, err
	}
	if err := accessscope.RequireOwnerAccess(ctx, c.OwnerID); err != nil {
		return model.ContainerStats{}, err
	}
	if c.Status == model.ContainerStatusMissing {
		return model.ContainerStats{}, resourceUnavailableError("container")
	}
	if c.Status != model.ContainerStatusRunning {
		return model.ContainerStats{}, nil
	}
	stats, err := s.dockerAPI.GetContainerStats(ctx, c.DockerID)
	if err != nil && isDockerNotFound(err) {
		s.markContainerError(ctx, containerID, model.ContainerStatusMissing, resourceMissingError("container"))
		return model.ContainerStats{}, resourceUnavailableError("container")
	}
	return stats, err
}

func (s *ContainerService) GetRuntimeTarget(ctx context.Context, containerID uuid.UUID) (model.ContainerRuntimeTarget, error) {
	c, err := s.repo.GetByID(ctx, containerID)
	if err != nil {
		return model.ContainerRuntimeTarget{}, err
	}
	if err := accessscope.RequireOwnerAccess(ctx, c.OwnerID); err != nil {
		return model.ContainerRuntimeTarget{}, err
	}
	if c.Status == model.ContainerStatusMissing {
		return model.ContainerRuntimeTarget{}, resourceUnavailableError("container")
	}
	if c.DockerID == "" {
		return model.ContainerRuntimeTarget{}, apperrors.ErrNotFound
	}
	return model.ContainerRuntimeTarget{
		ContainerID:      c.ID,
		DockerID:         c.DockerID,
		Status:           c.Status,
		OwnerID:          c.OwnerID,
		DockerGeneration: c.DockerGeneration,
	}, nil
}
