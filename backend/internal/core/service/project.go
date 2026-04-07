package service

import (
	"context"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

type ProjectRepository interface {
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Project, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.Project, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type ProjectResourceRepository interface {
	GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]domain.Container, error)
	GetVolumesByProjectID(ctx context.Context, projectID uuid.UUID) ([]domain.Volume, error)
}

type ProjectDockerAPI interface {
	StopContainer(ctx context.Context, dockerID string, timeout int) error // НОВОЕ
	RemoveContainer(ctx context.Context, dockerID string, force bool) error
	RemoveVolume(ctx context.Context, volumeName string, force bool) error
}

type ProjectService struct {
	repo         ProjectRepository
	resourceRepo ProjectResourceRepository
	dockerAPI    ProjectDockerAPI
	parser       *compose.Parser
}

func NewProjectService(repo ProjectRepository, resourceRepo ProjectResourceRepository, dockerAPI ProjectDockerAPI) *ProjectService {
	return &ProjectService{
		repo:         repo,
		resourceRepo: resourceRepo,
		dockerAPI:    dockerAPI,
		parser:       compose.NewParser(),
	}
}

func (s *ProjectService) GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]domain.Project, error) {
	return s.repo.GetByOwnerID(ctx, ownerID)
}

func (s *ProjectService) Stop(ctx context.Context, ownerID, projectID uuid.UUID) error {
	p, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return err
	}
	if p.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	containers, err := s.resourceRepo.GetByProjectID(ctx, projectID)
	if err != nil {
		return err
	}

	var stopErrors []error
	for _, c := range containers {
		if c.Status == domain.ContainerStatusRunning {
			err := s.dockerAPI.StopContainer(ctx, c.DockerID, 10)
			if err != nil {
				stopErrors = append(stopErrors, fmt.Errorf("failed to stop %s: %v", c.Name, err))
			}
		}
	}

	if len(stopErrors) > 0 {
		return fmt.Errorf("errors occurred while stopping project: %v", stopErrors)
	}

	return nil
}

func (s *ProjectService) Delete(ctx context.Context, ownerID, projectID uuid.UUID) error {
	p, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return err
	}
	if p.OwnerID != ownerID {
		return apperrors.ErrNotFound
	}

	containers, err := s.resourceRepo.GetByProjectID(ctx, projectID)
	if err == nil {
		for _, c := range containers {
			_ = s.dockerAPI.RemoveContainer(ctx, c.DockerID, true)
		}
	}

	volumes, err := s.resourceRepo.GetVolumesByProjectID(ctx, projectID)
	if err == nil {
		for _, v := range volumes {
			_ = s.dockerAPI.RemoveVolume(ctx, v.DockerName, true)
		}
	}

	// 3. Удаляем запись проекта из БД (Сработает ON DELETE CASCADE для контейнеров и томов в БД)
	return s.repo.Delete(ctx, projectID)
}
