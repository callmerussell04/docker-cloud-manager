package compose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/dependencywait"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type composeProgressRepository interface {
	SaveComposeDeploymentPlan(ctx context.Context, id uuid.UUID, stage string, planJSON, resourceMapJSON []byte) error
	UpdateComposeDeploymentProgress(ctx context.Context, id uuid.UUID, projectID uuid.UUID, projectStatus string, stage string, planJSON, resourceMapJSON []byte) error
	ListActiveComposeDeploymentJobs(ctx context.Context, limit int) ([]model.ComposeDeploymentJob, error)
}

type composeDeploymentPlan struct {
	ProjectName string                 `json:"project_name"`
	SourceType  string                 `json:"source_type"`
	Services    []model.ComposeService `json:"services"`
	Volumes     []model.ComposeVolume  `json:"volumes"`
}

type composeDeploymentResources struct {
	BuildIDs             map[string]uuid.UUID `json:"build_ids,omitempty"`
	VolumeIDs            map[string]uuid.UUID `json:"volume_ids,omitempty"`
	ManagedVolumeAliases map[string]bool      `json:"managed_volume_aliases,omitempty"`
	ContainerIDs         map[string]uuid.UUID `json:"container_ids,omitempty"`
	StartedServices      map[string]bool      `json:"started_services,omitempty"`
	ServiceGraphSaved    bool                 `json:"service_graph_saved,omitempty"`
	CleanupStatus        string               `json:"cleanup_status,omitempty"`
	CleanupError         string               `json:"cleanup_error,omitempty"`
}

type ComposeDeploymentCoordinator struct {
	orchestrator *Orchestrator
	logger       *slog.Logger
}

func NewComposeDeploymentCoordinator(orchestrator *Orchestrator, logger *slog.Logger) *ComposeDeploymentCoordinator {
	return &ComposeDeploymentCoordinator{
		orchestrator: orchestrator,
		logger:       logging.WithComponent(logger, "compose_deployment_coordinator"),
	}
}

func (c *ComposeDeploymentCoordinator) Run(ctx context.Context) {
	c.logger.InfoContext(ctx, "compose deployment coordinator started")
	interval := time.Duration(c.orchestrator.cfg.Get().ComposeCoordinatorIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	c.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			c.logger.InfoContext(ctx, "compose deployment coordinator stopped")
			return
		case <-ticker.C:
			c.tick(ctx)
			interval = time.Duration(c.orchestrator.cfg.Get().ComposeCoordinatorIntervalSeconds) * time.Second
			if interval <= 0 {
				interval = 2 * time.Second
			}
			ticker.Reset(interval)
		}
	}
}

func (c *ComposeDeploymentCoordinator) tick(ctx context.Context) {
	repo, ok := c.orchestrator.projectRepo.(composeProgressRepository)
	if !ok {
		c.logger.WarnContext(ctx, "compose progress repository is unavailable")
		return
	}
	jobs, err := repo.ListActiveComposeDeploymentJobs(ctx, 50)
	if err != nil {
		c.logger.WarnContext(ctx, "failed to list active compose deployment jobs", "error", err)
		return
	}
	for _, job := range jobs {
		if err := c.advanceJob(ctx, repo, job); err != nil {
			c.logger.WarnContext(ctx, "failed to advance compose deployment job", "project_id", job.ProjectID, "job_id", job.ID, "stage", job.Stage, "error", err)
		}
	}
}

func (c *ComposeDeploymentCoordinator) advanceJob(ctx context.Context, repo composeProgressRepository, job model.ComposeDeploymentJob) error {
	runCtx := c.jobContext(ctx, job)
	plan, resources, err := decodeComposeDeploymentState(job)
	if err != nil {
		return c.failJob(runCtx, job, composeDeploymentPlan{}, composeDeploymentResources{}, err)
	}
	if job.CancelRequested || job.Status == model.ComposeDeploymentStatusCanceling {
		return c.cancelJob(runCtx, job, plan, resources)
	}

	switch job.Stage {
	case model.ComposeDeploymentStageBuilding:
		return c.advanceBuilding(runCtx, repo, job, plan, resources)
	case model.ComposeDeploymentStageCreating:
		return c.advanceCreating(runCtx, repo, job, plan, resources)
	case model.ComposeDeploymentStageStarting:
		return c.advanceStarting(runCtx, repo, job, plan, resources)
	case model.ComposeDeploymentStageRollingBack:
		return c.advanceCleanup(runCtx, repo, job, plan, resources)
	default:
		return c.failJob(runCtx, job, plan, resources, fmt.Errorf("%w: unsupported compose deployment stage %q", apperrors.ErrConflict, job.Stage))
	}
}

func (c *ComposeDeploymentCoordinator) advanceBuilding(ctx context.Context, repo composeProgressRepository, job model.ComposeDeploymentJob, plan composeDeploymentPlan, resources composeDeploymentResources) error {
	if err := c.orchestrator.ensureComposeBuilds(ctx, job, plan, &resources); err != nil {
		return c.failJob(ctx, job, plan, resources, err)
	}
	if err := c.saveProgress(ctx, repo, job, model.ProjectStatusBuilding, model.ComposeDeploymentStageBuilding, plan, resources); err != nil {
		return err
	}

	allSuccess := true
	for serviceName, buildID := range resources.BuildIDs {
		build, err := c.orchestrator.buildRepo.GetByID(ctx, buildID)
		if err != nil {
			return c.failJob(ctx, job, plan, resources, fmt.Errorf("build record %s unavailable: %w", buildID, err))
		}
		if build.Status == model.BuildStatusCanceled {
			return c.cancelJob(ctx, job, plan, resources)
		}
		if model.IsBuildFailedStatus(build.Status) {
			return c.failJob(ctx, job, plan, resources, apperrors.New(apperrors.ErrConflict, fmt.Sprintf("build for service %s failed with status: %s", serviceName, build.Status)))
		}
		if build.Status != model.BuildStatusSuccess {
			allSuccess = false
		}
	}
	if !allSuccess {
		return nil
	}
	c.orchestrator.deleteComposeSourceObject(ctx, job.SourceObjectKey)
	return c.saveProgress(ctx, repo, job, model.ProjectStatusDeploying, model.ComposeDeploymentStageCreating, plan, resources)
}

func (c *ComposeDeploymentCoordinator) advanceCreating(ctx context.Context, repo composeProgressRepository, job model.ComposeDeploymentJob, plan composeDeploymentPlan, resources composeDeploymentResources) error {
	if resources.VolumeIDs == nil {
		resources.VolumeIDs = make(map[string]uuid.UUID)
	}
	if resources.ManagedVolumeAliases == nil {
		resources.ManagedVolumeAliases = make(map[string]bool)
	}
	for _, volParams := range plan.Volumes {
		if _, ok := resources.VolumeIDs[volParams.Alias]; ok {
			continue
		}
		var volumeID uuid.UUID
		var err error
		if volParams.External {
			volumeID, err = c.orchestrator.volumeService.ResolveByName(ctx, volParams.Name)
			if err != nil {
				return c.failJob(ctx, job, plan, resources, fmt.Errorf("failed to resolve external volume %s: %w", volParams.Name, err))
			}
			resources.ManagedVolumeAliases[volParams.Alias] = false
		} else {
			volumeID, err = c.orchestrator.volumeService.Create(ctx, model.VolumeCreateParams{
				ProjectID: &job.ProjectID,
				Name:      volParams.Name,
			})
			if err != nil {
				return c.failJob(ctx, job, plan, resources, fmt.Errorf("failed to create volume %s: %w", volParams.Name, err))
			}
			resources.ManagedVolumeAliases[volParams.Alias] = true
		}
		resources.VolumeIDs[volParams.Alias] = volumeID
		if err := c.saveProgress(ctx, repo, job, model.ProjectStatusDeploying, model.ComposeDeploymentStageCreating, plan, resources); err != nil {
			return err
		}
	}
	if reader, ok := c.orchestrator.volumeService.(interface {
		GetByID(context.Context, uuid.UUID) (model.Volume, error)
	}); ok {
		for alias, volumeID := range resources.VolumeIDs {
			if !resources.ManagedVolumeAliases[alias] {
				continue
			}
			vol, err := reader.GetByID(ctx, volumeID)
			if err != nil {
				return c.failJob(ctx, job, plan, resources, fmt.Errorf("failed to inspect volume %s: %w", alias, err))
			}
			switch vol.Status {
			case model.VolumeStatusAvailable:
				continue
			case model.VolumeStatusCreating, model.VolumeStatusDeleting:
				return nil
			case model.VolumeStatusMissing, model.VolumeStatusError:
				return c.failJob(ctx, job, plan, resources, fmt.Errorf("volume %s is %s", alias, vol.Status))
			default:
				return nil
			}
		}
	}

	if resources.ContainerIDs == nil {
		resources.ContainerIDs = make(map[string]uuid.UUID)
	}
	if err := c.populateExistingContainers(ctx, job.ProjectID, plan, &resources); err != nil {
		return err
	}
	for _, srv := range plan.Services {
		if _, ok := resources.ContainerIDs[srv.Name]; ok {
			continue
		}
		createParams := c.containerCreateParams(job.ProjectID, plan.ProjectName, srv, resources.VolumeIDs)
		containerID, err := c.orchestrator.contService.Create(ctx, createParams)
		if err != nil {
			return c.failJob(ctx, job, plan, resources, fmt.Errorf("failed to create service %s: %w", srv.Name, err))
		}
		resources.ContainerIDs[srv.Name] = containerID
		c.orchestrator.recordComposeExposeAudit(ctx, job.OwnerID, job.ProjectID, plan.ProjectName, plan.SourceType, srv, containerID, createParams.Name)
		if err := c.saveProgress(ctx, repo, job, model.ProjectStatusDeploying, model.ComposeDeploymentStageCreating, plan, resources); err != nil {
			return err
		}
	}

	ready, err := c.containersCreated(ctx, resources.ContainerIDs)
	if err != nil {
		return c.failJob(ctx, job, plan, resources, err)
	}
	if !ready {
		return nil
	}
	if err := c.saveServiceGraph(ctx, repo, job, plan, &resources); err != nil {
		return c.failJob(ctx, job, plan, resources, err)
	}
	if resources.StartedServices == nil {
		resources.StartedServices = make(map[string]bool)
	}
	return c.saveProgress(ctx, repo, job, model.ProjectStatusDeploying, model.ComposeDeploymentStageStarting, plan, resources)
}

func (c *ComposeDeploymentCoordinator) advanceStarting(ctx context.Context, repo composeProgressRepository, job model.ComposeDeploymentJob, plan composeDeploymentPlan, resources composeDeploymentResources) error {
	if err := c.saveServiceGraph(ctx, repo, job, plan, &resources); err != nil {
		return c.failJob(ctx, job, plan, resources, err)
	}
	if resources.StartedServices == nil {
		resources.StartedServices = make(map[string]bool)
	}
	startQueued := false
	for _, srv := range plan.Services {
		if resources.StartedServices[srv.Name] {
			continue
		}
		containerID, ok := resources.ContainerIDs[srv.Name]
		if !ok {
			return c.failJob(ctx, job, plan, resources, fmt.Errorf("container mapping not found for service %s", srv.Name))
		}
		container, err := c.orchestrator.contService.GetByID(ctx, containerID)
		if err != nil {
			return c.failJob(ctx, job, plan, resources, err)
		}
		completed, err := composeServiceStartCompleted(srv.Name, container)
		if err != nil {
			return c.failJob(ctx, job, plan, resources, err)
		}
		if completed {
			resources.StartedServices[srv.Name] = true
			if err := c.saveProgress(ctx, repo, job, model.ProjectStatusDeploying, model.ComposeDeploymentStageStarting, plan, resources); err != nil {
				return err
			}
			continue
		}
		if containerStatusBlocksComposeStart(container.Status) {
			return nil
		}
		ready, err := c.dependenciesReady(ctx, srv, resources.ContainerIDs)
		if err != nil {
			return c.failJob(ctx, job, plan, resources, err)
		}
		if !ready {
			return nil
		}
		if err := c.orchestrator.contService.Start(ctx, containerID); err != nil {
			return c.failJob(ctx, job, plan, resources, fmt.Errorf("failed to start service %s: %w", srv.Name, err))
		}
		startQueued = true
	}
	if startQueued {
		return nil
	}

	if err := c.orchestrator.projectRepo.UpdateStatus(ctx, job.ProjectID, model.ProjectStatusRunning, nil); err != nil {
		return c.failJob(ctx, job, plan, resources, fmt.Errorf("failed to mark compose project running: %w", err))
	}
	if err := c.orchestrator.projectRepo.CompleteComposeDeploymentJob(ctx, job.ID, model.ComposeDeploymentStatusSucceeded, nil); err != nil {
		return c.failJob(ctx, job, plan, resources, fmt.Errorf("failed to mark compose deployment succeeded: %w", err))
	}
	c.orchestrator.deleteComposeSourceObject(ctx, job.SourceObjectKey)
	c.logger.InfoContext(ctx, "compose deployment completed", "project_id", job.ProjectID, "job_id", job.ID)
	return nil
}

func (c *ComposeDeploymentCoordinator) dependenciesReady(ctx context.Context, srv model.ComposeService, serviceToContainerID map[string]uuid.UUID) (bool, error) {
	for _, dep := range srv.DependsOn {
		depContainerID, ok := serviceToContainerID[dep.ServiceName]
		if !ok {
			if dep.Optional {
				continue
			}
			return false, fmt.Errorf("dependency %s not found for service %s", dep.ServiceName, srv.Name)
		}
		depContainer, err := c.orchestrator.contService.GetByID(ctx, depContainerID)
		if err != nil {
			if dep.Optional {
				continue
			}
			return false, err
		}
		if depContainer.DockerID == "" {
			return false, nil
		}
		inspect, err := c.orchestrator.dockerAPI.InspectContainer(ctx, depContainer.DockerID)
		if err != nil {
			if dep.Optional {
				continue
			}
			return false, err
		}
		done, err := dependencywait.EvaluateInspection(inspect, dep.Condition)
		if err != nil {
			if dep.Optional {
				continue
			}
			return false, fmt.Errorf("dependency %s failed condition %s: %w", dep.ServiceName, dep.Condition, err)
		}
		if !done {
			return false, nil
		}
	}
	return true, nil
}

func composeServiceStartCompleted(serviceName string, container model.Container) (bool, error) {
	switch container.Status {
	case model.ContainerStatusRunning:
		return true, nil
	case model.ContainerStatusExited:
		if container.LastExitCode == nil {
			return false, nil
		}
		if *container.LastExitCode == 0 {
			return true, nil
		}
		return false, apperrors.New(apperrors.ErrConflict, fmt.Sprintf("service %s exited with non-zero code", serviceName))
	case model.ContainerStatusError, model.ContainerStatusMissing:
		if container.LastError != nil && *container.LastError != "" {
			return false, apperrors.New(apperrors.ErrConflict, fmt.Sprintf("service %s failed: %s", serviceName, *container.LastError))
		}
		return false, apperrors.New(apperrors.ErrConflict, fmt.Sprintf("service %s is %s", serviceName, container.Status))
	default:
		return false, nil
	}
}

func containerStatusBlocksComposeStart(status string) bool {
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

func (c *ComposeDeploymentCoordinator) containersCreated(ctx context.Context, serviceToContainerID map[string]uuid.UUID) (bool, error) {
	allCreated := true
	for serviceName, containerID := range serviceToContainerID {
		container, err := c.orchestrator.contService.GetByID(ctx, containerID)
		if err != nil {
			return false, fmt.Errorf("container for service %s unavailable: %w", serviceName, err)
		}
		switch container.Status {
		case model.ContainerStatusCreated, model.ContainerStatusRunning, model.ContainerStatusExited:
		case model.ContainerStatusError, model.ContainerStatusMissing:
			if container.LastError != nil && *container.LastError != "" {
				return false, apperrors.New(apperrors.ErrConflict, fmt.Sprintf("container create for service %s failed: %s", serviceName, *container.LastError))
			}
			return false, apperrors.New(apperrors.ErrConflict, fmt.Sprintf("container create for service %s failed with status %s", serviceName, container.Status))
		default:
			allCreated = false
		}
	}
	return allCreated, nil
}

func (c *ComposeDeploymentCoordinator) saveServiceGraph(ctx context.Context, repo composeProgressRepository, job model.ComposeDeploymentJob, plan composeDeploymentPlan, resources *composeDeploymentResources) error {
	if resources.ServiceGraphSaved {
		return nil
	}
	graph, err := projectServiceGraph(job.ProjectID, plan.Services, resources.ContainerIDs)
	if err != nil {
		return err
	}
	if err := c.orchestrator.projectRepo.SaveServiceGraph(ctx, job.ProjectID, graph); err != nil {
		return fmt.Errorf("failed to save compose service graph: %w", err)
	}
	resources.ServiceGraphSaved = true
	return c.saveProgress(ctx, repo, job, model.ProjectStatusDeploying, model.ComposeDeploymentStageStarting, plan, *resources)
}

func (c *ComposeDeploymentCoordinator) populateExistingContainers(ctx context.Context, projectID uuid.UUID, plan composeDeploymentPlan, resources *composeDeploymentResources) error {
	if c.orchestrator.resourceRepo == nil {
		return nil
	}
	containers, err := c.orchestrator.resourceRepo.GetByProjectID(ctx, projectID)
	if err != nil {
		return err
	}
	serviceByContainerName := make(map[string]string, len(plan.Services))
	for _, srv := range plan.Services {
		serviceByContainerName[fmt.Sprintf("%s_%s", plan.ProjectName, srv.Name)] = srv.Name
	}
	for _, container := range containers {
		serviceName, ok := serviceByContainerName[container.Name]
		if ok {
			resources.ContainerIDs[serviceName] = container.ID
		}
	}
	return nil
}

func (c *ComposeDeploymentCoordinator) containerCreateParams(projectID uuid.UUID, projectName string, srv model.ComposeService, volumeNameMap map[string]uuid.UUID) model.ContainerCreateParams {
	var resolvedMounts []model.VolumeMountParams
	for _, mount := range srv.VolumeMounts {
		if volumeID, ok := volumeNameMap[mount.VolumeName]; ok {
			resolvedMounts = append(resolvedMounts, model.VolumeMountParams{
				VolumeID:   volumeID,
				MountPath:  mount.MountPath,
				IsReadOnly: mount.IsReadOnly,
			})
		}
	}
	return model.ContainerCreateParams{
		ProjectID:    &projectID,
		Name:         fmt.Sprintf("%s_%s", projectName, srv.Name),
		NetworkAlias: srv.Name,
		ImageTag:     srv.ImageTag,
		InternalPort: srv.InternalPort,
		DomainPrefix: srv.DomainPrefix,
		EnvVars:      srv.EnvVars,
		VolumeMounts: resolvedMounts,
		Command:      srv.Command,
		Entrypoint:   srv.Entrypoint,
		Restart:      srv.Restart,
		Healthcheck:  srv.Healthcheck,
	}
}

func (c *ComposeDeploymentCoordinator) failJob(ctx context.Context, job model.ComposeDeploymentJob, plan composeDeploymentPlan, resources composeDeploymentResources, cause error) error {
	repo, ok := c.orchestrator.projectRepo.(composeProgressRepository)
	if !ok {
		return apperrors.New(apperrors.ErrUnavailable, "compose progress repository is unavailable")
	}
	state := c.stateFromResources(job, resources)
	c.orchestrator.cancelBuilds(ctx, state.buildIDs)
	errMsg := apperrors.SafeMessage(cause)
	resources.CleanupStatus = model.ComposeDeploymentStatusFailed
	resources.CleanupError = errMsg
	if err := c.orchestrator.projectRepo.UpdateStatus(ctx, job.ProjectID, model.ProjectStatusFailed, &errMsg); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}
	c.logger.WarnContext(ctx, "compose deployment failed", "project_id", job.ProjectID, "job_id", job.ID, "project_name", plan.ProjectName, "error", cause)
	if err := c.saveProgress(ctx, repo, job, model.ProjectStatusFailed, model.ComposeDeploymentStageRollingBack, plan, resources); err != nil {
		return err
	}
	return c.cleanupJob(ctx, repo, job, plan, resources)
}

func (c *ComposeDeploymentCoordinator) cancelJob(ctx context.Context, job model.ComposeDeploymentJob, plan composeDeploymentPlan, resources composeDeploymentResources) error {
	repo, ok := c.orchestrator.projectRepo.(composeProgressRepository)
	if !ok {
		return apperrors.New(apperrors.ErrUnavailable, "compose progress repository is unavailable")
	}
	resources.CleanupStatus = model.ComposeDeploymentStatusCanceled
	resources.CleanupError = "compose deployment canceled"
	c.orchestrator.deleteComposeSourceObject(ctx, job.SourceObjectKey)
	if err := c.saveProgress(ctx, repo, job, model.ProjectStatusCanceled, model.ComposeDeploymentStageRollingBack, plan, resources); err != nil {
		return err
	}
	return c.cleanupJob(ctx, repo, job, plan, resources)
}

func (c *ComposeDeploymentCoordinator) advanceCleanup(ctx context.Context, repo composeProgressRepository, job model.ComposeDeploymentJob, plan composeDeploymentPlan, resources composeDeploymentResources) error {
	return c.cleanupJob(ctx, repo, job, plan, resources)
}

func (c *ComposeDeploymentCoordinator) cleanupJob(ctx context.Context, repo composeProgressRepository, job model.ComposeDeploymentJob, plan composeDeploymentPlan, resources composeDeploymentResources) error {
	if resources.CleanupStatus == "" {
		resources.CleanupStatus = model.ComposeDeploymentStatusFailed
	}
	projectStatus := model.ProjectStatusFailed
	if resources.CleanupStatus == model.ComposeDeploymentStatusCanceled {
		projectStatus = model.ProjectStatusCanceled
	}
	errMsg := resources.CleanupError
	if errMsg == "" {
		errMsg = "compose deployment failed"
		if resources.CleanupStatus == model.ComposeDeploymentStatusCanceled {
			errMsg = "compose deployment canceled"
		}
	}
	if err := c.orchestrator.projectRepo.UpdateStatus(ctx, job.ProjectID, projectStatus, &errMsg); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}

	cleanupDone := true
	for _, containerID := range resources.ContainerIDs {
		if err := c.orchestrator.contService.Delete(ctx, containerID); err != nil {
			if errors.Is(err, apperrors.ErrNotFound) {
				continue
			}
			if errors.Is(err, apperrors.ErrConflict) {
				cleanupDone = false
				continue
			}
			return err
		}
		cleanupDone = false
	}
	for alias, volumeID := range resources.VolumeIDs {
		if !resources.volumeAliasManaged(alias) {
			continue
		}
		if err := c.orchestrator.volumeService.Delete(ctx, volumeID); err != nil {
			if errors.Is(err, apperrors.ErrNotFound) {
				continue
			}
			if errors.Is(err, apperrors.ErrConflict) {
				cleanupDone = false
				continue
			}
			return err
		}
		cleanupDone = false
	}
	if !cleanupDone {
		return c.saveProgress(ctx, repo, job, projectStatus, model.ComposeDeploymentStageRollingBack, plan, resources)
	}

	state := c.stateFromResources(job, resources)
	c.orchestrator.deleteSuccessfulBuildImages(ctx, state.buildIDs, c.logger.With("project_id", job.ProjectID, "job_id", job.ID))
	if err := c.orchestrator.projectRepo.CompleteComposeDeploymentJob(ctx, job.ID, resources.CleanupStatus, &errMsg); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}
	c.orchestrator.deleteComposeSourceObject(ctx, job.SourceObjectKey)
	return nil
}

func (c *ComposeDeploymentCoordinator) stateFromResources(job model.ComposeDeploymentJob, resources composeDeploymentResources) *deploymentState {
	state := &deploymentState{
		jobID:     job.ID,
		projectID: job.ProjectID,
		scope:     accessscope.Scope{Kind: accessscope.KindUser, UserID: job.OwnerID},
		requestID: job.RequestID,
	}
	for _, buildID := range resources.BuildIDs {
		state.buildIDs = append(state.buildIDs, buildID)
	}
	for _, containerID := range resources.ContainerIDs {
		state.createdContainers = append(state.createdContainers, containerID)
	}
	for _, volumeID := range resources.VolumeIDs {
		state.createdVolumes = append(state.createdVolumes, volumeID)
	}
	return state
}

func (r composeDeploymentResources) volumeAliasManaged(alias string) bool {
	if len(r.ManagedVolumeAliases) == 0 {
		return true
	}
	return r.ManagedVolumeAliases[alias]
}

func (c *ComposeDeploymentCoordinator) saveProgress(ctx context.Context, repo composeProgressRepository, job model.ComposeDeploymentJob, projectStatus string, stage string, plan composeDeploymentPlan, resources composeDeploymentResources) error {
	planJSON, resourceJSON, err := encodeComposeDeploymentState(plan, resources)
	if err != nil {
		return err
	}
	return repo.UpdateComposeDeploymentProgress(ctx, job.ID, job.ProjectID, projectStatus, stage, planJSON, resourceJSON)
}

func (c *ComposeDeploymentCoordinator) jobContext(ctx context.Context, job model.ComposeDeploymentJob) context.Context {
	ctx = logging.ContextWithRequestID(ctx, job.RequestID)
	return accessscope.WithScope(ctx, accessscope.Scope{Kind: accessscope.KindUser, UserID: job.OwnerID})
}

func decodeComposeDeploymentState(job model.ComposeDeploymentJob) (composeDeploymentPlan, composeDeploymentResources, error) {
	var plan composeDeploymentPlan
	var resources composeDeploymentResources
	if len(job.PlanJSON) == 0 {
		return plan, resources, apperrors.New(apperrors.ErrConflict, "compose deployment plan is missing")
	}
	if err := json.Unmarshal(job.PlanJSON, &plan); err != nil {
		return plan, resources, err
	}
	if len(job.ResourceMapJSON) > 0 {
		if err := json.Unmarshal(job.ResourceMapJSON, &resources); err != nil {
			return plan, resources, err
		}
	}
	ensureComposeResourceMaps(&resources)
	return plan, resources, nil
}

func encodeComposeDeploymentState(plan composeDeploymentPlan, resources composeDeploymentResources) ([]byte, []byte, error) {
	ensureComposeResourceMaps(&resources)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return nil, nil, err
	}
	resourceJSON, err := json.Marshal(resources)
	if err != nil {
		return nil, nil, err
	}
	return planJSON, resourceJSON, nil
}

func ensureComposeResourceMaps(resources *composeDeploymentResources) {
	if resources.BuildIDs == nil {
		resources.BuildIDs = make(map[string]uuid.UUID)
	}
	if resources.VolumeIDs == nil {
		resources.VolumeIDs = make(map[string]uuid.UUID)
	}
	if resources.ManagedVolumeAliases == nil {
		resources.ManagedVolumeAliases = make(map[string]bool)
	}
	if resources.ContainerIDs == nil {
		resources.ContainerIDs = make(map[string]uuid.UUID)
	}
	if resources.StartedServices == nil {
		resources.StartedServices = make(map[string]bool)
	}
}
