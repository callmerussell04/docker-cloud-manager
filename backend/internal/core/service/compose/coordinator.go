package compose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/dependencywait"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/nilcheck"
	"github.com/google/uuid"
)

// DeploymentStore is the durable Compose deployment state that the coordinator
// resumes, advances, cancels and rolls back.
type DeploymentStore interface {
	ListActiveComposeDeploymentJobs(ctx context.Context, limit int) ([]model.ComposeDeploymentJob, error)
	UpdateComposeDeploymentProgress(ctx context.Context, id uuid.UUID, projectID uuid.UUID, projectStatus string, stage string, planJSON, resourceMapJSON []byte) error
	CompleteComposeDeploymentJob(ctx context.Context, id uuid.UUID, status string, errorMsg *string) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error
	SaveServiceGraph(ctx context.Context, projectID uuid.UUID, services []model.ProjectServiceNode) error
}

// SourceReleaser deletes a staged Compose source object and releases its
// staged reservation. Source ingestion owns both.
type SourceReleaser interface {
	ReleaseComposeSource(ctx context.Context, objectKey string)
}

// DeploymentCoordinatorDeps contains the capabilities needed to execute
// and recover persisted Compose deployments. Auditor and Users are optional:
// expose audit is best effort. Now is the clock the deployment deadlines
// read; nil means time.Now.
type DeploymentCoordinatorDeps struct {
	Deployments       DeploymentStore
	Builds            BuildRepository
	Builder           BuilderClient
	Containers        ContainerService
	ProjectContainers ProjectContainerRepository
	Volumes           VolumeService
	Docker            DockerAPI
	Images            ImageCleaner
	Sources           SourceReleaser
	Config            ConfigProvider
	Auditor           AuditRecorder
	Users             UserInfoProvider
	Logger            *slog.Logger
	Now               func() time.Time
}

type deploymentPlan struct {
	ProjectName string                 `json:"project_name"`
	SourceType  string                 `json:"source_type"`
	Services    []model.ComposeService `json:"services"`
	Volumes     []model.ComposeVolume  `json:"volumes"`
}

type deploymentResources struct {
	BuildIDs             map[string]uuid.UUID `json:"build_ids,omitempty"`
	VolumeIDs            map[string]uuid.UUID `json:"volume_ids,omitempty"`
	ManagedVolumeAliases map[string]bool      `json:"managed_volume_aliases,omitempty"`
	ContainerIDs         map[string]uuid.UUID `json:"container_ids,omitempty"`
	StartedServices      map[string]bool      `json:"started_services,omitempty"`
	ServiceGraphSaved    bool                 `json:"service_graph_saved,omitempty"`
	// DependencyWaitStartedAt records, per service, when the service first
	// waited on its dependencies.
	DependencyWaitStartedAt map[string]time.Time `json:"dependency_wait_started_at,omitempty"`
	CleanupStatus           string               `json:"cleanup_status,omitempty"`
	CleanupError            string               `json:"cleanup_error,omitempty"`
}

type DeploymentCoordinator struct {
	store             DeploymentStore
	builds            BuildRepository
	builder           BuilderClient
	containers        ContainerService
	projectContainers ProjectContainerRepository
	volumes           VolumeService
	docker            DockerAPI
	images            ImageCleaner
	sources           SourceReleaser
	cfg               ConfigProvider
	auditor           AuditRecorder
	users             UserInfoProvider
	logger            *slog.Logger
	now               func() time.Time
}

func NewDeploymentCoordinator(deps DeploymentCoordinatorDeps) (*DeploymentCoordinator, error) {
	required := []struct {
		name  string
		value any
	}{
		{"deployments", deps.Deployments}, {"builds", deps.Builds},
		{"builder", deps.Builder}, {"containers", deps.Containers},
		{"project containers", deps.ProjectContainers}, {"volumes", deps.Volumes},
		{"docker", deps.Docker}, {"images", deps.Images},
		{"sources", deps.Sources}, {"config", deps.Config},
	}
	for _, dependency := range required {
		if nilcheck.IsNil(dependency.value) {
			return nil, fmt.Errorf("compose deployment coordinator: %s is required", dependency.name)
		}
	}
	coordinator := &DeploymentCoordinator{
		store:             deps.Deployments,
		builds:            deps.Builds,
		builder:           deps.Builder,
		containers:        deps.Containers,
		projectContainers: deps.ProjectContainers,
		volumes:           deps.Volumes,
		docker:            deps.Docker,
		images:            deps.Images,
		sources:           deps.Sources,
		cfg:               deps.Config,
		logger:            logging.WithComponent(deps.Logger, "compose_deployment_coordinator"),
		now:               deps.Now,
	}
	if coordinator.now == nil {
		coordinator.now = time.Now
	}
	if !nilcheck.IsNil(deps.Auditor) {
		coordinator.auditor = deps.Auditor
	}
	if !nilcheck.IsNil(deps.Users) {
		coordinator.users = deps.Users
	}
	return coordinator, nil
}

func (c *DeploymentCoordinator) Run(ctx context.Context) {
	c.logger.InfoContext(ctx, "compose deployment coordinator started")
	ticker := time.NewTicker(c.interval())
	defer ticker.Stop()

	c.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			c.logger.InfoContext(ctx, "compose deployment coordinator stopped")
			return
		case <-ticker.C:
			c.tick(ctx)
			ticker.Reset(c.interval())
		}
	}
}

func (c *DeploymentCoordinator) interval() time.Duration {
	if configured := time.Duration(c.cfg.Get().ComposeCoordinatorIntervalSeconds) * time.Second; configured > 0 {
		return configured
	}
	return 2 * time.Second
}

func (c *DeploymentCoordinator) tick(ctx context.Context) {
	jobs, err := c.store.ListActiveComposeDeploymentJobs(ctx, 50)
	if err != nil {
		c.logger.WarnContext(ctx, "failed to list active compose deployment jobs", "error", err)
		return
	}
	for _, job := range jobs {
		if err := c.advanceJob(ctx, job); err != nil {
			c.logger.WarnContext(ctx, "failed to advance compose deployment job", "project_id", job.ProjectID, "job_id", job.ID, "stage", job.Stage, "error", err)
		}
	}
}

func (c *DeploymentCoordinator) advanceJob(ctx context.Context, job model.ComposeDeploymentJob) error {
	runCtx := c.jobContext(ctx, job)
	plan, resources, err := decodeComposeDeploymentState(job)
	if err != nil {
		return c.failJob(runCtx, job, deploymentPlan{}, deploymentResources{}, err)
	}
	if job.CancelRequested || job.Status == model.ComposeDeploymentStatusCanceling {
		return c.cancelJob(runCtx, job, plan, resources)
	}
	if job.Stage != model.ComposeDeploymentStageRollingBack {
		if err := c.checkPipelineDeadline(job); err != nil {
			return c.failJob(runCtx, job, plan, resources, err)
		}
	}

	switch job.Stage {
	case model.ComposeDeploymentStageBuilding:
		return c.advanceBuilding(runCtx, job, plan, resources)
	case model.ComposeDeploymentStageCreating:
		return c.advanceCreating(runCtx, job, plan, resources)
	case model.ComposeDeploymentStageStarting:
		return c.advanceStarting(runCtx, job, plan, resources)
	case model.ComposeDeploymentStageRollingBack:
		return c.advanceCleanup(runCtx, job, plan, resources)
	default:
		return c.failJob(runCtx, job, plan, resources, fmt.Errorf("%w: unsupported compose deployment stage %q", apperrors.ErrConflict, job.Stage))
	}
}

// checkPipelineDeadline fails a deployment still unfinished
// compose_pipeline_timeout_minutes after its job started.
func (c *DeploymentCoordinator) checkPipelineDeadline(job model.ComposeDeploymentJob) error {
	minutes := c.cfg.Get().ComposePipelineTimeoutMinutes
	if minutes <= 0 || job.StartedAt == nil {
		return nil
	}
	if c.now().Sub(*job.StartedAt) <= time.Duration(minutes)*time.Minute {
		return nil
	}
	return apperrors.New(apperrors.ErrTimeout, fmt.Sprintf("compose deployment timed out after %d minutes", minutes))
}

func (c *DeploymentCoordinator) advanceBuilding(ctx context.Context, job model.ComposeDeploymentJob, plan deploymentPlan, resources deploymentResources) error {
	if err := ensureComposeBuilds(ctx, c.builds, c.builder, job, plan, &resources); err != nil {
		return c.failJob(ctx, job, plan, resources, err)
	}
	if err := c.saveProgress(ctx, job, model.ProjectStatusBuilding, model.ComposeDeploymentStageBuilding, plan, resources); err != nil {
		return err
	}

	allSuccess := true
	for serviceName, buildID := range resources.BuildIDs {
		build, err := c.builds.GetByID(ctx, buildID)
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
	c.sources.ReleaseComposeSource(ctx, job.SourceObjectKey)
	return c.saveProgress(ctx, job, model.ProjectStatusDeploying, model.ComposeDeploymentStageCreating, plan, resources)
}

func (c *DeploymentCoordinator) advanceCreating(ctx context.Context, job model.ComposeDeploymentJob, plan deploymentPlan, resources deploymentResources) error {
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
			volumeID, err = c.volumes.ResolveByName(ctx, volParams.Name)
			if err != nil {
				return c.failJob(ctx, job, plan, resources, fmt.Errorf("failed to resolve external volume %s: %w", volParams.Name, err))
			}
			resources.ManagedVolumeAliases[volParams.Alias] = false
		} else {
			var reused bool
			volumeID, reused, err = c.volumes.ResolveProjectManagedByName(ctx, job.ProjectID, volParams.Name)
			if err != nil {
				return c.failJob(ctx, job, plan, resources, fmt.Errorf("failed to prepare volume %s: %w", volParams.Name, err))
			}
			resources.ManagedVolumeAliases[volParams.Alias] = !reused
		}
		resources.VolumeIDs[volParams.Alias] = volumeID
		if err := c.saveProgress(ctx, job, model.ProjectStatusDeploying, model.ComposeDeploymentStageCreating, plan, resources); err != nil {
			return err
		}
	}
	for alias, volumeID := range resources.VolumeIDs {
		if !resources.ManagedVolumeAliases[alias] {
			continue
		}
		vol, err := c.volumes.GetByID(ctx, volumeID)
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
		containerID, err := c.containers.Create(ctx, createParams)
		if apperrors.LimitOf(err) == apperrors.LimitQueuedContainerCreates {
			// The owner's create queue is full. Each created service is
			// already saved, so a later pass creates the rest once earlier
			// creates finish; the pipeline deadline bounds the wait.
			return nil
		}
		if err != nil {
			return c.failJob(ctx, job, plan, resources, fmt.Errorf("failed to create service %s: %w", srv.Name, err))
		}
		resources.ContainerIDs[srv.Name] = containerID
		c.recordComposeExposeAudit(ctx, job.OwnerID, job.ProjectID, plan.ProjectName, plan.SourceType, srv, containerID, createParams.Name)
		if err := c.saveProgress(ctx, job, model.ProjectStatusDeploying, model.ComposeDeploymentStageCreating, plan, resources); err != nil {
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
	if err := c.saveServiceGraph(ctx, job, plan, &resources); err != nil {
		return c.failJob(ctx, job, plan, resources, err)
	}
	if resources.StartedServices == nil {
		resources.StartedServices = make(map[string]bool)
	}
	return c.saveProgress(ctx, job, model.ProjectStatusDeploying, model.ComposeDeploymentStageStarting, plan, resources)
}

func (c *DeploymentCoordinator) advanceStarting(ctx context.Context, job model.ComposeDeploymentJob, plan deploymentPlan, resources deploymentResources) error {
	if err := c.saveServiceGraph(ctx, job, plan, &resources); err != nil {
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
		container, err := c.containers.GetByID(ctx, containerID)
		if err != nil {
			return c.failJob(ctx, job, plan, resources, err)
		}
		completed, err := composeServiceStartCompleted(srv.Name, container)
		if err != nil {
			return c.failJob(ctx, job, plan, resources, err)
		}
		if completed {
			resources.StartedServices[srv.Name] = true
			if err := c.saveProgress(ctx, job, model.ProjectStatusDeploying, model.ComposeDeploymentStageStarting, plan, resources); err != nil {
				return err
			}
			continue
		}
		if containerStatusBlocksComposeStart(container.Status) {
			return nil
		}
		unmet, err := c.unmetDependency(ctx, srv, resources.ContainerIDs)
		if err != nil {
			return c.failJob(ctx, job, plan, resources, err)
		}
		if unmet != nil {
			return c.waitForDependency(ctx, job, plan, resources, srv.Name, *unmet)
		}
		if err := c.containers.Start(ctx, containerID); err != nil {
			return c.failJob(ctx, job, plan, resources, fmt.Errorf("failed to start service %s: %w", srv.Name, err))
		}
		startQueued = true
	}
	if startQueued {
		return nil
	}

	if err := c.store.UpdateStatus(ctx, job.ProjectID, model.ProjectStatusRunning, nil); err != nil {
		return c.failJob(ctx, job, plan, resources, fmt.Errorf("failed to mark compose project running: %w", err))
	}
	if err := c.store.CompleteComposeDeploymentJob(ctx, job.ID, model.ComposeDeploymentStatusSucceeded, nil); err != nil {
		return c.failJob(ctx, job, plan, resources, fmt.Errorf("failed to mark compose deployment succeeded: %w", err))
	}
	c.sources.ReleaseComposeSource(ctx, job.SourceObjectKey)
	c.logger.InfoContext(ctx, "compose deployment completed", "project_id", job.ProjectID, "job_id", job.ID)
	return nil
}

// waitForDependency keeps a service waiting on unmet and fails the deployment
// once the service has waited longer than
// compose_dependency_wait_timeout_minutes. The clock starts when the service
// first waits and is saved with the job, so a Core restart does not reset it.
func (c *DeploymentCoordinator) waitForDependency(ctx context.Context, job model.ComposeDeploymentJob, plan deploymentPlan, resources deploymentResources, serviceName string, unmet model.ComposeDependency) error {
	now := c.now()
	since, ok := resources.DependencyWaitStartedAt[serviceName]
	if !ok {
		resources.DependencyWaitStartedAt[serviceName] = now
		return c.saveProgress(ctx, job, model.ProjectStatusDeploying, model.ComposeDeploymentStageStarting, plan, resources)
	}
	minutes := c.cfg.Get().ComposeDependencyWaitTimeoutMinutes
	if minutes <= 0 || now.Sub(since) <= time.Duration(minutes)*time.Minute {
		return nil
	}
	return c.failJob(ctx, job, plan, resources, apperrors.New(apperrors.ErrTimeout,
		fmt.Sprintf("service %s timed out waiting for dependency %s (%s)", serviceName, unmet.ServiceName, unmet.Condition)))
}

// unmetDependency returns the first dependency of srv that has not yet met
// its condition, or nil when all have. It fails once a required dependency
// can no longer meet its condition.
func (c *DeploymentCoordinator) unmetDependency(ctx context.Context, srv model.ComposeService, serviceToContainerID map[string]uuid.UUID) (*model.ComposeDependency, error) {
	for _, dep := range srv.DependsOn {
		depContainerID, ok := serviceToContainerID[dep.ServiceName]
		if !ok {
			if dep.Optional {
				continue
			}
			return nil, fmt.Errorf("dependency %s not found for service %s", dep.ServiceName, srv.Name)
		}
		depContainer, err := c.containers.GetByID(ctx, depContainerID)
		if err != nil {
			if dep.Optional {
				continue
			}
			return nil, err
		}
		if depContainer.DockerID == "" {
			return &dep, nil
		}
		inspect, err := c.docker.InspectContainer(ctx, depContainer.DockerID)
		if err != nil {
			if dep.Optional {
				continue
			}
			return nil, err
		}
		done, err := dependencywait.EvaluateInspection(inspect, dep.Condition)
		if err != nil {
			if dep.Optional {
				continue
			}
			return nil, fmt.Errorf("dependency %s failed condition %s: %w", dep.ServiceName, dep.Condition, err)
		}
		if !done {
			return &dep, nil
		}
	}
	return nil, nil
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

func (c *DeploymentCoordinator) containersCreated(ctx context.Context, serviceToContainerID map[string]uuid.UUID) (bool, error) {
	allCreated := true
	for serviceName, containerID := range serviceToContainerID {
		container, err := c.containers.GetByID(ctx, containerID)
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

func (c *DeploymentCoordinator) saveServiceGraph(ctx context.Context, job model.ComposeDeploymentJob, plan deploymentPlan, resources *deploymentResources) error {
	if resources.ServiceGraphSaved {
		return nil
	}
	graph, err := projectServiceGraph(job.ProjectID, plan.Services, resources.ContainerIDs)
	if err != nil {
		return err
	}
	if err := c.store.SaveServiceGraph(ctx, job.ProjectID, graph); err != nil {
		return fmt.Errorf("failed to save compose service graph: %w", err)
	}
	resources.ServiceGraphSaved = true
	return c.saveProgress(ctx, job, model.ProjectStatusDeploying, model.ComposeDeploymentStageStarting, plan, *resources)
}

func (c *DeploymentCoordinator) populateExistingContainers(ctx context.Context, projectID uuid.UUID, plan deploymentPlan, resources *deploymentResources) error {
	containers, err := c.projectContainers.GetByProjectID(ctx, projectID)
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

func (c *DeploymentCoordinator) containerCreateParams(projectID uuid.UUID, projectName string, srv model.ComposeService, volumeNameMap map[string]uuid.UUID) model.ContainerCreateParams {
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

		MemoryReservationMB:      srv.MemoryReservationMB,
		CPUReservationMillicores: srv.CPUReservationMillicores,
	}
}

func (c *DeploymentCoordinator) failJob(ctx context.Context, job model.ComposeDeploymentJob, plan deploymentPlan, resources deploymentResources, cause error) error {
	errMsg := apperrors.SafeMessage(cause)
	resources.CleanupStatus = model.ComposeDeploymentStatusFailed
	resources.CleanupError = errMsg
	if err := c.store.UpdateStatus(ctx, job.ProjectID, model.ProjectStatusFailed, &errMsg); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}
	// Cancel builds only once the project is failed: each canceled build
	// cascades to the deployment, which would otherwise record a cancel.
	cancelComposeBuilds(ctx, c.builder, c.logger, resources.buildIDList())
	c.logger.WarnContext(ctx, "compose deployment failed", "project_id", job.ProjectID, "job_id", job.ID, "project_name", plan.ProjectName, "error", cause)
	if err := c.saveProgress(ctx, job, model.ProjectStatusFailed, model.ComposeDeploymentStageRollingBack, plan, resources); err != nil {
		return err
	}
	return c.cleanupJob(ctx, job, plan, resources)
}

func (c *DeploymentCoordinator) cancelJob(ctx context.Context, job model.ComposeDeploymentJob, plan deploymentPlan, resources deploymentResources) error {
	cancelComposeBuilds(ctx, c.builder, c.logger, resources.buildIDList())
	resources.CleanupStatus = model.ComposeDeploymentStatusCanceled
	resources.CleanupError = "compose deployment canceled"
	c.sources.ReleaseComposeSource(ctx, job.SourceObjectKey)
	if err := c.saveProgress(ctx, job, model.ProjectStatusCanceled, model.ComposeDeploymentStageRollingBack, plan, resources); err != nil {
		return err
	}
	return c.cleanupJob(ctx, job, plan, resources)
}

func (c *DeploymentCoordinator) advanceCleanup(ctx context.Context, job model.ComposeDeploymentJob, plan deploymentPlan, resources deploymentResources) error {
	return c.cleanupJob(ctx, job, plan, resources)
}

func (c *DeploymentCoordinator) cleanupJob(ctx context.Context, job model.ComposeDeploymentJob, plan deploymentPlan, resources deploymentResources) error {
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
	if err := c.store.UpdateStatus(ctx, job.ProjectID, projectStatus, &errMsg); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}

	cleanupDone := true
	for _, containerID := range resources.ContainerIDs {
		if err := c.containers.Delete(ctx, containerID); err != nil {
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
		if !c.planVolumeManaged(plan, alias) {
			continue
		}
		if resources.volumeAliasCreated(alias) {
			if err := c.volumes.Delete(ctx, volumeID); err != nil {
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
			continue
		}
		if err := c.volumes.UnlinkProjectVolume(ctx, job.ProjectID, volumeID); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
	}
	if !cleanupDone {
		return c.saveProgress(ctx, job, projectStatus, model.ComposeDeploymentStageRollingBack, plan, resources)
	}

	c.deleteSuccessfulBuildImages(ctx, resources.buildIDList(), c.logger.With("project_id", job.ProjectID, "job_id", job.ID))
	if err := c.store.CompleteComposeDeploymentJob(ctx, job.ID, resources.CleanupStatus, &errMsg); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}
	c.sources.ReleaseComposeSource(ctx, job.SourceObjectKey)
	return nil
}

func (c *DeploymentCoordinator) deleteSuccessfulBuildImages(ctx context.Context, buildIDs []uuid.UUID, logger *slog.Logger) {
	for _, buildID := range buildIDs {
		build, err := c.builds.GetByID(ctx, buildID)
		if err != nil {
			continue
		}
		if build.Status != model.BuildStatusSuccess {
			continue
		}
		if err := c.images.Delete(ctx, build.ImageID); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			logger.WarnContext(ctx, "failed to delete compose-built image during cancellation cleanup", "build_id", build.ID, "image_id", build.ImageID, "error", err)
		}
	}
}

func (r deploymentResources) buildIDList() []uuid.UUID {
	buildIDs := make([]uuid.UUID, 0, len(r.BuildIDs))
	for _, buildID := range r.BuildIDs {
		buildIDs = append(buildIDs, buildID)
	}
	return buildIDs
}

func (r deploymentResources) volumeAliasCreated(alias string) bool {
	if len(r.ManagedVolumeAliases) == 0 {
		return true
	}
	return r.ManagedVolumeAliases[alias]
}

func (c *DeploymentCoordinator) planVolumeManaged(plan deploymentPlan, alias string) bool {
	for _, vol := range plan.Volumes {
		if vol.Alias == alias {
			return !vol.External
		}
	}
	return false
}

func (c *DeploymentCoordinator) saveProgress(ctx context.Context, job model.ComposeDeploymentJob, projectStatus string, stage string, plan deploymentPlan, resources deploymentResources) error {
	planJSON, resourceJSON, err := encodeComposeDeploymentState(plan, resources)
	if err != nil {
		return err
	}
	return c.store.UpdateComposeDeploymentProgress(ctx, job.ID, job.ProjectID, projectStatus, stage, planJSON, resourceJSON)
}

func (c *DeploymentCoordinator) jobContext(ctx context.Context, job model.ComposeDeploymentJob) context.Context {
	ctx = logging.ContextWithRequestID(ctx, job.RequestID)
	return accessscope.WithScope(ctx, accessscope.Scope{Kind: accessscope.KindUser, UserID: job.OwnerID})
}

func decodeComposeDeploymentState(job model.ComposeDeploymentJob) (deploymentPlan, deploymentResources, error) {
	var plan deploymentPlan
	var resources deploymentResources
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

func encodeComposeDeploymentState(plan deploymentPlan, resources deploymentResources) ([]byte, []byte, error) {
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

func ensureComposeResourceMaps(resources *deploymentResources) {
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
	if resources.DependencyWaitStartedAt == nil {
		resources.DependencyWaitStartedAt = make(map[string]time.Time)
	}
}
