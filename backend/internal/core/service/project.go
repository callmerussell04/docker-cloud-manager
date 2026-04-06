package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

type ProjectRepository interface {
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Project, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.Project, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type ProjectResourceRepository interface {
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Container, error)
	GetVolumesByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Volume, error)
}

type ProjectDockerAPI interface {
	RemoveContainer(ctx context.Context, dockerID string, force bool) error
	RemoveVolume(ctx context.Context, volumeName string, force bool) error
}

type ProjectService struct {
	repo         ProjectRepository
	resourceRepo ProjectResourceRepository
	dockerAPI    ProjectDockerAPI
}

func NewProjectService(repo ProjectRepository, resourceRepo ProjectResourceRepository, dockerAPI ProjectDockerAPI) *ProjectService {
	return &ProjectService{
		repo:         repo,
		resourceRepo: resourceRepo,
		dockerAPI:    dockerAPI,
	}
}

func (s *ProjectService) GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Project, error) {
	return s.repo.GetByOwnerID(ctx, ownerID)
}

func (s *ProjectService) Delete(ctx context.Context, ownerID, projectID uuid.UUID) error {
	p, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return err
	}
	if p.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	containers, err := s.resourceRepo.GetByOwnerID(ctx, ownerID)
	if err == nil {
		for _, c := range containers {
			if c.ProjectID != nil && *c.ProjectID == projectID {
				_ = s.dockerAPI.RemoveContainer(ctx, c.DockerID, true)
			}
		}
	}

	volumes, err := s.resourceRepo.GetVolumesByOwnerID(ctx, ownerID)
	if err == nil {
		for _, v := range volumes {
			if v.ProjectID != nil && *v.ProjectID == projectID {
				_ = s.dockerAPI.RemoveVolume(ctx, v.DockerName, true)
			}
		}
	}

	return s.repo.Delete(ctx, projectID)
}
