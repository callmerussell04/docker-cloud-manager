package service

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
)

type ProjectRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Project, error)
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, opts model.ListOptions) ([]model.Project, int, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, errorMsg *string) error
	GetServiceGraph(ctx context.Context, projectID uuid.UUID) ([]model.ProjectServiceNode, error)
}

type ProjectResourceRepository interface {
	GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Container, error)
	GetVolumesByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Volume, error)
}

type ProjectDockerAPI interface {
	InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error)
	RemoveContainer(ctx context.Context, dockerID string, force bool) error
	RemoveVolume(ctx context.Context, volumeName string, force bool) error
}

type ProjectNetworkCleaner interface {
	CleanupUserNetworkIfUnused(ctx context.Context, ownerID uuid.UUID) error
}

type ProjectContainerLifecycle interface {
	Start(ctx context.Context, containerID uuid.UUID) error
	Stop(ctx context.Context, containerID uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Container, error)
}

type ProjectConfigProvider interface {
	Get() config.SystemConfig
}

type ProjectDeploymentCanceler interface {
	CancelDeployment(ctx context.Context, projectID uuid.UUID) error
}

type ProjectService struct {
	repo           ProjectRepository
	resourceRepo   ProjectResourceRepository
	dockerAPI      ProjectDockerAPI
	containers     ProjectContainerLifecycle
	networkCleaner ProjectNetworkCleaner
	cfg            ProjectConfigProvider
	deployments    ProjectDeploymentCanceler
}

func NewProjectService(repo ProjectRepository, resourceRepo ProjectResourceRepository, dockerAPI ProjectDockerAPI, containers ProjectContainerLifecycle, cfg ProjectConfigProvider) *ProjectService {
	networkCleaner, _ := containers.(ProjectNetworkCleaner)
	return &ProjectService{
		repo:           repo,
		resourceRepo:   resourceRepo,
		dockerAPI:      dockerAPI,
		containers:     containers,
		networkCleaner: networkCleaner,
		cfg:            cfg,
	}
}

func (s *ProjectService) SetDeploymentCanceler(canceler ProjectDeploymentCanceler) {
	s.deployments = canceler
}

func (s *ProjectService) Start(ctx context.Context, projectID uuid.UUID) error {
	p, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, p.OwnerID); err != nil {
		return err
	}
	return s.startProject(ctx, p)
}

func (s *ProjectService) Stop(ctx context.Context, projectID uuid.UUID) error {
	p, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, p.OwnerID); err != nil {
		return err
	}
	return s.stopProject(ctx, p)
}

func (s *ProjectService) Delete(ctx context.Context, projectID uuid.UUID) error {
	p, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, p.OwnerID); err != nil {
		return err
	}
	return s.deleteProject(ctx, p)
}

func (s *ProjectService) Cancel(ctx context.Context, projectID uuid.UUID) error {
	p, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, p.OwnerID); err != nil {
		return err
	}
	if s.deployments == nil {
		return apperrors.New(apperrors.ErrConflict, "compose deployment cancellation is unavailable")
	}
	return s.deployments.CancelDeployment(ctx, projectID)
}

func (s *ProjectService) List(ctx context.Context, limit, offset int) ([]model.Project, int, error) {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.List(ctx, model.ListOptions{
		OwnerID: scope.OwnerFilter(),
		Limit:   limit,
		Offset:  offset,
	})
}

func (s *ProjectService) RefreshProjectStatus(ctx context.Context, projectID uuid.UUID) error {
	p, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return err
	}
	if projectStatusBlocksAggregation(p.Status) {
		return nil
	}

	containers, err := s.resourceRepo.GetByProjectID(ctx, projectID)
	if err != nil {
		return err
	}
	if len(containers) == 0 {
		return nil
	}

	hasRunning := false
	allStopped := true
	for _, c := range containers {
		switch c.Status {
		case model.ContainerStatusMissing, model.ContainerStatusError:
			msg := fmt.Sprintf("container %s is %s", c.Name, c.Status)
			return s.repo.UpdateStatus(ctx, projectID, model.ProjectStatusFailed, &msg)
		case model.ContainerStatusRunning:
			hasRunning = true
			allStopped = false
		case model.ContainerStatusCreated, model.ContainerStatusExited:
		default:
			allStopped = false
		}
	}

	if hasRunning {
		return s.repo.UpdateStatus(ctx, projectID, model.ProjectStatusRunning, nil)
	}
	if allStopped {
		return s.repo.UpdateStatus(ctx, projectID, model.ProjectStatusStopped, nil)
	}
	return nil
}

func (s *ProjectService) startProject(ctx context.Context, p model.Project) error {
	graph, err := s.repo.GetServiceGraph(ctx, p.ID)
	if err != nil {
		return apperrors.Wrap(apperrors.ErrConflict, "project service graph is missing; redeploy the compose project", err)
	}

	_ = s.repo.UpdateStatus(ctx, p.ID, model.ProjectStatusStarting, nil)
	for _, node := range graph {
		for _, dep := range node.Dependencies {
			depContainer, err := s.containers.GetByID(ctx, dep.DependsOnContainerID)
			if err != nil {
				if dep.Optional {
					slog.WarnContext(ctx, "optional compose dependency unavailable; continuing project start",
						"project_id", p.ID,
						"service_name", node.ServiceName,
						"dependency_service_name", dep.DependsOnServiceName,
						"condition", dep.Condition,
						"error", err,
					)
					continue
				}
				s.failProject(ctx, p.ID, err)
				return err
			}
			if err := s.waitForCondition(ctx, depContainer.DockerID, dep.Condition); err != nil {
				err = fmt.Errorf("dependency %s failed condition %s: %w", dep.DependsOnServiceName, dep.Condition, err)
				if dep.Optional {
					slog.WarnContext(ctx, "optional compose dependency failed; continuing project start",
						"project_id", p.ID,
						"service_name", node.ServiceName,
						"dependency_service_name", dep.DependsOnServiceName,
						"condition", dep.Condition,
						"error", err,
					)
					continue
				}
				s.failProject(ctx, p.ID, err)
				return err
			}
		}
		current, err := s.containers.GetByID(ctx, node.ContainerID)
		if err != nil {
			s.failProject(ctx, p.ID, err)
			return err
		}
		if current.Status == model.ContainerStatusRunning {
			continue
		}
		if err := s.containers.Start(ctx, node.ContainerID); err != nil {
			err = fmt.Errorf("failed to start service %s: %w", node.ServiceName, err)
			s.failProject(ctx, p.ID, err)
			return err
		}
	}
	return s.repo.UpdateStatus(ctx, p.ID, model.ProjectStatusRunning, nil)
}

func (s *ProjectService) stopProject(ctx context.Context, p model.Project) error {
	graph, err := s.repo.GetServiceGraph(ctx, p.ID)
	if err != nil {
		return apperrors.Wrap(apperrors.ErrConflict, "project service graph is missing; redeploy the compose project", err)
	}
	slices.Reverse(graph)

	_ = s.repo.UpdateStatus(ctx, p.ID, model.ProjectStatusStopping, nil)
	for _, node := range graph {
		current, err := s.containers.GetByID(ctx, node.ContainerID)
		if err != nil {
			s.failProject(ctx, p.ID, err)
			return err
		}
		if current.Status == model.ContainerStatusCreated || current.Status == model.ContainerStatusExited {
			continue
		}
		if err := s.containers.Stop(ctx, node.ContainerID); err != nil {
			err = fmt.Errorf("failed to stop service %s: %w", node.ServiceName, err)
			s.failProject(ctx, p.ID, err)
			return err
		}
	}
	return s.repo.UpdateStatus(ctx, p.ID, model.ProjectStatusStopped, nil)
}

func (s *ProjectService) deleteProject(ctx context.Context, p model.Project) error {
	_ = s.repo.UpdateStatus(ctx, p.ID, model.ProjectStatusDeleting, nil)

	var cleanupErrors []error
	containers, err := s.resourceRepo.GetByProjectID(ctx, p.ID)
	if err == nil {
		for _, c := range containers {
			if err := s.dockerAPI.RemoveContainer(ctx, c.DockerID, true); err != nil && !cerrdefs.IsNotFound(err) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("failed to remove container %s: %w", c.Name, err))
			}
		}
	}

	volumes, err := s.resourceRepo.GetVolumesByProjectID(ctx, p.ID)
	if err == nil {
		for _, v := range volumes {
			if err := s.dockerAPI.RemoveVolume(ctx, v.DockerName, true); err != nil && !cerrdefs.IsNotFound(err) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("failed to remove volume %s: %w", v.DockerName, err))
			}
		}
	}
	if len(cleanupErrors) > 0 {
		msg := fmt.Sprintf("cleanup failed: %v", cleanupErrors)
		_ = s.repo.UpdateStatus(ctx, p.ID, model.ProjectStatusFailed, &msg)
		return fmt.Errorf("errors occurred while deleting project: %v", cleanupErrors)
	}

	if err := s.repo.Delete(ctx, p.ID); err != nil {
		return err
	}

	s.cleanupUserNetwork(ctx, p.OwnerID)
	return nil
}

func (s *ProjectService) waitForCondition(ctx context.Context, dockerID string, condition string) error {
	if dockerID == "" {
		return resourceUnavailableError("container")
	}
	timeout := time.After(time.Duration(s.cfg.Get().ComposeDependencyWaitTimeoutMinutes) * time.Minute)
	ticker := time.NewTicker(time.Duration(s.cfg.Get().ComposeDependencyPollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return apperrors.New(apperrors.ErrConflict, "timeout waiting for dependency state")
		case <-ticker.C:
			ticker.Reset(time.Duration(s.cfg.Get().ComposeDependencyPollIntervalSeconds) * time.Second)
			inspect, err := s.dockerAPI.InspectContainer(ctx, dockerID)
			if err != nil {
				if cerrdefs.IsNotFound(err) {
					return resourceUnavailableError("container")
				}
				continue
			}

			state := inspect.State
			switch condition {
			case model.ComposeDependencyConditionHealthy:
				if state.HealthStatus == nil {
					return apperrors.New(apperrors.ErrBadRequest, "service_healthy requested, but no healthcheck defined for container")
				}
				if *state.HealthStatus == "healthy" {
					return nil
				}
				if *state.HealthStatus == "unhealthy" {
					return apperrors.New(apperrors.ErrConflict, "dependency became unhealthy")
				}
				if !state.Running && state.ExitCode != 0 {
					return apperrors.New(apperrors.ErrConflict, "dependency exited before becoming healthy")
				}
			case model.ComposeDependencyConditionCompletedSuccessfully:
				if !state.Running {
					if state.ExitCode == 0 {
						return nil
					}
					return apperrors.New(apperrors.ErrConflict, "dependency exited with non-zero code")
				}
			case model.ComposeDependencyConditionStarted:
				if state.Running {
					return nil
				}
				if !state.Running && state.ExitCode != 0 {
					return apperrors.New(apperrors.ErrConflict, "dependency failed to start")
				}
			default:
				return apperrors.New(apperrors.ErrBadRequest, "unsupported dependency condition")
			}
		}
	}
}

func (s *ProjectService) failProject(ctx context.Context, projectID uuid.UUID, cause error) {
	msg := apperrors.SafeMessage(cause)
	_ = s.repo.UpdateStatus(ctx, projectID, model.ProjectStatusFailed, &msg)
}

func projectStatusBlocksAggregation(status string) bool {
	switch status {
	case model.ProjectStatusBuilding, model.ProjectStatusDeploying, model.ProjectStatusCanceling, model.ProjectStatusStarting, model.ProjectStatusStopping, model.ProjectStatusDeleting:
		return true
	default:
		return false
	}
}

func (s *ProjectService) cleanupUserNetwork(ctx context.Context, ownerID uuid.UUID) {
	if s.networkCleaner == nil {
		return
	}
	_ = s.networkCleaner.CleanupUserNetworkIfUnused(ctx, ownerID)
}
