package service

import (
	"context"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

type ProjectRepository interface {
	GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]model.Project, error)
	GetByID(ctx context.Context, id uuid.UUID) (model.Project, error)
	Delete(ctx context.Context, id uuid.UUID) error
	GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Project, int, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, errorMsg *string) error
}

type ProjectResourceRepository interface {
	GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Container, error)
	GetVolumesByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Volume, error)
}

type ProjectDockerAPI interface {
	StopContainer(ctx context.Context, dockerID string, timeout int) error // НОВОЕ
	RemoveContainer(ctx context.Context, dockerID string, force bool) error
	RemoveVolume(ctx context.Context, volumeName string, force bool) error
}

type ProjectNetworkCleaner interface {
	CleanupUserNetworkIfUnused(ctx context.Context, ownerID uuid.UUID) error
}

type ProjectService struct {
	repo           ProjectRepository
	resourceRepo   ProjectResourceRepository
	dockerAPI      ProjectDockerAPI
	networkCleaner ProjectNetworkCleaner
	parser         *compose.Parser
}

func NewProjectService(repo ProjectRepository, resourceRepo ProjectResourceRepository, dockerAPI ProjectDockerAPI, networkCleaner ProjectNetworkCleaner) *ProjectService {
	return &ProjectService{
		repo:           repo,
		resourceRepo:   resourceRepo,
		dockerAPI:      dockerAPI,
		networkCleaner: networkCleaner,
		parser:         compose.NewParser(),
	}
}

func (s *ProjectService) GetByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Project, error) {
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
		if c.Status == model.ContainerStatusRunning {
			err := s.dockerAPI.StopContainer(ctx, c.DockerID, 10)
			if err != nil {
				stopErrors = append(stopErrors, fmt.Errorf("failed to stop %s: %v", c.Name, err))
			}
		}
	}

	if len(stopErrors) > 0 {
		return fmt.Errorf("errors occurred while stopping project: %v", stopErrors)
	}

	return s.repo.UpdateStatus(ctx, projectID, model.ProjectStatusStopped, nil)
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
	if err := s.repo.Delete(ctx, projectID); err != nil {
		return err
	}

	s.cleanupUserNetwork(ctx, ownerID)
	return nil
}

func (s *ProjectService) GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Project, int, error) {
	return s.repo.GetAllPaginated(ctx, limit, offset)
}

func (s *ProjectService) AdminStop(ctx context.Context, projectID uuid.UUID) error {
	_, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return err
	}

	containers, err := s.resourceRepo.GetByProjectID(ctx, projectID)
	if err != nil {
		return err
	}

	var stopErrors []error
	for _, c := range containers {
		if c.Status == model.ContainerStatusRunning {
			err := s.dockerAPI.StopContainer(ctx, c.DockerID, 10)
			if err != nil {
				stopErrors = append(stopErrors, fmt.Errorf("failed to stop %s: %v", c.Name, err))
			}
		}
	}

	if len(stopErrors) > 0 {
		return fmt.Errorf("errors occurred while stopping project: %v", stopErrors)
	}

	return s.repo.UpdateStatus(ctx, projectID, model.ProjectStatusStopped, nil)
}

func (s *ProjectService) AdminDelete(ctx context.Context, projectID uuid.UUID) error {
	p, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return err
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

	if err := s.repo.Delete(ctx, projectID); err != nil {
		return err
	}

	s.cleanupUserNetwork(ctx, p.OwnerID)
	return nil
}

func (s *ProjectService) cleanupUserNetwork(ctx context.Context, ownerID uuid.UUID) {
	if s.networkCleaner == nil {
		return
	}
	_ = s.networkCleaner.CleanupUserNetworkIfUnused(ctx, ownerID)
}
