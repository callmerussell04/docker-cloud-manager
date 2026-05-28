package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/dependencywait"
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

type ProjectLifecycleRepository interface {
	ListByStatuses(ctx context.Context, statuses []string, limit int) ([]model.Project, error)
}

type ProjectResourceRepository interface {
	GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Container, error)
	GetVolumesByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Volume, error)
}

type ProjectDockerAPI interface {
	InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error)
}

type ProjectNetworkCleaner interface {
	CleanupUserNetworkIfUnused(ctx context.Context, ownerID uuid.UUID) error
}

type ProjectContainerLifecycle interface {
	Start(ctx context.Context, containerID uuid.UUID) error
	Stop(ctx context.Context, containerID uuid.UUID) error
	Delete(ctx context.Context, containerID uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Container, error)
}

type ProjectVolumeLifecycle interface {
	Delete(ctx context.Context, volumeID uuid.UUID) error
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
	volumes        ProjectVolumeLifecycle
	networkCleaner ProjectNetworkCleaner
	cfg            ProjectConfigProvider
	deployments    ProjectDeploymentCanceler
}

func NewProjectService(repo ProjectRepository, resourceRepo ProjectResourceRepository, dockerAPI ProjectDockerAPI, containers ProjectContainerLifecycle, volumes ProjectVolumeLifecycle, cfg ProjectConfigProvider) *ProjectService {
	networkCleaner, _ := containers.(ProjectNetworkCleaner)
	return &ProjectService{
		repo:           repo,
		resourceRepo:   resourceRepo,
		dockerAPI:      dockerAPI,
		containers:     containers,
		volumes:        volumes,
		networkCleaner: networkCleaner,
		cfg:            cfg,
	}
}

func (s *ProjectService) SetDeploymentCanceler(canceler ProjectDeploymentCanceler) {
	s.deployments = canceler
}

func (s *ProjectService) RunLifecycleCoordinator(ctx context.Context) {
	interval := 2 * time.Second
	if s.cfg != nil {
		if configured := time.Duration(s.cfg.Get().ComposeCoordinatorIntervalSeconds) * time.Second; configured > 0 {
			interval = configured
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	s.advanceLifecycleProjects(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.advanceLifecycleProjects(ctx)
			interval = 2 * time.Second
			if s.cfg != nil {
				if configured := time.Duration(s.cfg.Get().ComposeCoordinatorIntervalSeconds) * time.Second; configured > 0 {
					interval = configured
				}
			}
			ticker.Reset(interval)
		}
	}
}

func (s *ProjectService) advanceLifecycleProjects(ctx context.Context) {
	lifecycleRepo, ok := s.repo.(ProjectLifecycleRepository)
	if !ok {
		return
	}
	projects, err := lifecycleRepo.ListByStatuses(ctx, []string{model.ProjectStatusStarting, model.ProjectStatusStopping, model.ProjectStatusDeleting}, 50)
	if err != nil {
		return
	}
	for _, p := range projects {
		var err error
		projectCtx := accessscope.WithUserScope(ctx, p.OwnerID, "", "")
		switch p.Status {
		case model.ProjectStatusStarting:
			err = s.advanceProjectStart(projectCtx, p)
		case model.ProjectStatusStopping:
			err = s.advanceProjectStop(projectCtx, p)
		case model.ProjectStatusDeleting:
			err = s.advanceProjectDelete(projectCtx, p)
		}
		if err != nil {
			s.failProject(ctx, p.ID, err)
		}
	}
}

func (s *ProjectService) Start(ctx context.Context, projectID uuid.UUID) error {
	p, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, p.OwnerID); err != nil {
		return err
	}
	if projectStatusBlocksManualOperation(p.Status) {
		return apperrors.New(apperrors.ErrConflict, "project operation is already in progress")
	}
	if err := s.ensureStartCapacity(ctx, p); err != nil {
		return err
	}
	return s.repo.UpdateStatus(ctx, p.ID, model.ProjectStatusStarting, nil)
}

func (s *ProjectService) Stop(ctx context.Context, projectID uuid.UUID) error {
	p, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, p.OwnerID); err != nil {
		return err
	}
	if projectStatusBlocksManualOperation(p.Status) {
		return apperrors.New(apperrors.ErrConflict, "project operation is already in progress")
	}
	return s.repo.UpdateStatus(ctx, p.ID, model.ProjectStatusStopping, nil)
}

func (s *ProjectService) Delete(ctx context.Context, projectID uuid.UUID) error {
	p, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, p.OwnerID); err != nil {
		return err
	}
	if projectStatusBlocksManualOperation(p.Status) {
		return apperrors.New(apperrors.ErrConflict, "project operation is already in progress")
	}
	return s.repo.UpdateStatus(ctx, p.ID, model.ProjectStatusDeleting, nil)
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

func (s *ProjectService) ensureStartCapacity(ctx context.Context, p model.Project) error {
	checker, ok := s.containers.(CapacityChecker)
	if !ok || s.resourceRepo == nil {
		return nil
	}
	containers, err := s.resourceRepo.GetByProjectID(ctx, p.ID)
	if err != nil {
		return err
	}
	requestedRam := int64(0)
	requestedCPU := int64(0)
	cfg := config.SystemConfig{}
	if s.cfg != nil {
		cfg = s.cfg.Get()
	}
	for _, container := range containers {
		switch container.Status {
		case model.ContainerStatusRunning, model.ContainerStatusStarting, model.ContainerStatusMissing, model.ContainerStatusError:
			continue
		}
		reservation := container.BaseMemoryReservation
		if reservation <= 0 && s.cfg != nil {
			reservation = cfg.DefaultMemoryReservation
		}
		requestedRam += reservation
		cpuReservation := container.BaseCPUReservation
		if cpuReservation <= 0 && s.cfg != nil {
			cpuReservation = cfg.DefaultCPUReservation
		}
		requestedCPU += cpuReservation
	}
	if requestedRam <= 0 && requestedCPU <= 0 {
		return nil
	}
	return checker.CheckCapacity(ctx, p.OwnerID, requestedRam, requestedCPU, 0)
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

func (s *ProjectService) advanceProjectStart(ctx context.Context, p model.Project) error {
	graph, err := s.repo.GetServiceGraph(ctx, p.ID)
	if err != nil {
		return apperrors.Wrap(apperrors.ErrConflict, "project service graph is missing; redeploy the compose project", err)
	}
	for _, node := range graph {
		current, err := s.containers.GetByID(ctx, node.ContainerID)
		if err != nil {
			return err
		}
		if current.Status == model.ContainerStatusRunning {
			continue
		}
		if current.Status == model.ContainerStatusMissing || current.Status == model.ContainerStatusError {
			return fmt.Errorf("service %s container is %s", node.ServiceName, current.Status)
		}
		if containerStatusBlocksProjectTick(current.Status) {
			return nil
		}
		for _, dep := range node.Dependencies {
			ready, err := s.projectDependencyReady(ctx, dep)
			if err != nil {
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
				return fmt.Errorf("dependency %s failed condition %s: %w", dep.DependsOnServiceName, dep.Condition, err)
			}
			if !ready {
				return nil
			}
		}
		if err := s.containers.Start(ctx, node.ContainerID); err != nil {
			return fmt.Errorf("failed to start service %s: %w", node.ServiceName, err)
		}
		return nil
	}
	return s.repo.UpdateStatus(ctx, p.ID, model.ProjectStatusRunning, nil)
}

func (s *ProjectService) advanceProjectStop(ctx context.Context, p model.Project) error {
	graph, err := s.repo.GetServiceGraph(ctx, p.ID)
	if err != nil {
		return apperrors.Wrap(apperrors.ErrConflict, "project service graph is missing; redeploy the compose project", err)
	}
	slices.Reverse(graph)
	for _, node := range graph {
		current, err := s.containers.GetByID(ctx, node.ContainerID)
		if err != nil {
			return err
		}
		if current.Status == model.ContainerStatusCreated || current.Status == model.ContainerStatusExited || current.Status == model.ContainerStatusMissing {
			continue
		}
		if containerStatusBlocksProjectTick(current.Status) {
			return nil
		}
		if err := s.containers.Stop(ctx, node.ContainerID); err != nil {
			return fmt.Errorf("failed to stop service %s: %w", node.ServiceName, err)
		}
		return nil
	}
	return s.repo.UpdateStatus(ctx, p.ID, model.ProjectStatusStopped, nil)
}

func (s *ProjectService) advanceProjectDelete(ctx context.Context, p model.Project) error {
	if s.deployments != nil {
		if err := s.deployments.CancelDeployment(ctx, p.ID); err != nil && !errors.Is(err, apperrors.ErrConflict) && !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
	}
	containers, err := s.resourceRepo.GetByProjectID(ctx, p.ID)
	if err != nil {
		return err
	}
	for _, c := range containers {
		if c.Status == model.ContainerStatusDeleting {
			return nil
		}
		if err := s.containers.Delete(ctx, c.ID); err != nil && !errors.Is(err, apperrors.ErrNotFound) && !cerrdefs.IsNotFound(err) {
			return fmt.Errorf("failed to remove container %s: %w", c.Name, err)
		}
		return nil
	}
	if err := s.repo.Delete(ctx, p.ID); err != nil {
		return err
	}
	s.cleanupUserNetwork(ctx, p.OwnerID)
	return nil
}

func (s *ProjectService) projectDependencyReady(ctx context.Context, dep model.ProjectServiceDependency) (bool, error) {
	depContainer, err := s.containers.GetByID(ctx, dep.DependsOnContainerID)
	if err != nil {
		return false, err
	}
	if depContainer.DockerID == "" {
		return false, nil
	}
	inspect, err := s.dockerAPI.InspectContainer(ctx, depContainer.DockerID)
	if err != nil {
		return false, err
	}
	return dependencywait.EvaluateInspection(inspect, dep.Condition)
}

func containerStatusBlocksProjectTick(status string) bool {
	switch status {
	case model.ContainerStatusPending,
		model.ContainerStatusCreating,
		model.ContainerStatusStarting,
		model.ContainerStatusStopping,
		model.ContainerStatusExposing,
		model.ContainerStatusDeleting,
		model.ContainerStatusReconciling:
		return true
	default:
		return false
	}
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
			if err := s.containers.Delete(ctx, c.ID); err != nil && !cerrdefs.IsNotFound(err) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("failed to remove container %s: %w", c.Name, err))
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
	return dependencywait.Wait(ctx, s.cfg, s.dockerAPI, dockerID, condition, dependencywait.Options{MissingIsUnavailable: true})
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

func projectStatusBlocksManualOperation(status string) bool {
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
