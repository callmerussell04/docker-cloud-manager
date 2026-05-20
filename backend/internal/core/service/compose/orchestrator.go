package compose

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/dependencywait"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/pkg/composequeue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/gitsource"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type ProjectRepository interface {
	Save(ctx context.Context, p model.Project) error
	CreateWithComposeDeploymentJob(ctx context.Context, p model.Project, job model.ComposeDeploymentJob, outbox model.ComposeDeploymentOutbox) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Project, error)
	GetComposeDeploymentJob(ctx context.Context, id uuid.UUID) (model.ComposeDeploymentJob, error)
	GetActiveComposeDeploymentJobByProjectID(ctx context.Context, projectID uuid.UUID) (model.ComposeDeploymentJob, error)
	ListInterruptedComposeDeploymentJobs(ctx context.Context) ([]model.ComposeDeploymentJob, error)
	StartComposeDeploymentJob(ctx context.Context, id uuid.UUID) (model.ComposeDeploymentJob, bool, error)
	CompleteComposeDeploymentJob(ctx context.Context, id uuid.UUID, status string, errorMsg *string) error
	RequestComposeDeploymentCancel(ctx context.Context, projectID uuid.UUID) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error
	SaveServiceGraph(ctx context.Context, projectID uuid.UUID, services []model.ProjectServiceNode) error
}

type activeComposeDeploymentCounter interface {
	CountActiveComposeDeploymentsByOwner(ctx context.Context, ownerID uuid.UUID) (int, error)
}

type BuildRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Build, error)
	GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Build, error)
}

type ResourceRepository interface {
	GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Container, error)
	GetVolumesByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Volume, error)
}

type StagedObjectRepository interface {
	Reserve(ctx context.Context, reservation model.StagedObjectReservation, maxBytesPerUser int64) error
	UpdateBytes(ctx context.Context, objectKey string, bytesReserved int64) error
	Release(ctx context.Context, objectKey string) error
	ActiveBytesByOwner(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type VolumeService interface {
	Create(ctx context.Context, params model.VolumeCreateParams) (uuid.UUID, error)
	Delete(ctx context.Context, volumeID uuid.UUID) error
}

type ContainerService interface {
	Create(ctx context.Context, params model.ContainerCreateParams) (uuid.UUID, error)
	Start(ctx context.Context, containerID uuid.UUID) error
	Delete(ctx context.Context, containerID uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Container, error)
}

type CapacityChecker interface {
	CheckCapacity(ctx context.Context, ownerID uuid.UUID, requestedRam int64, projectedDiskWriteBytes int64) error
}

type ComposeDockerAPI interface {
	InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error)
}

type ConfigProvider interface {
	Get() config.SystemConfig
}

type HostDiskMetricsProvider interface {
	GetDiskUsage(path string) (model.HostDiskStats, error)
}

type UserInfoProvider interface {
	GetUser(ctx context.Context, userID uuid.UUID) (model.UserInfo, error)
}

type ImageCleaner interface {
	Delete(ctx context.Context, imageID uuid.UUID) error
}

type BuilderClient interface {
	TriggerBuild(ctx context.Context, projectID uuid.UUID, srv model.ComposeService, sourceObjectKey string) (uuid.UUID, error)
	CancelBuild(ctx context.Context, buildID uuid.UUID) error
}

type Orchestrator struct {
	parentCtx     context.Context
	parser        *Parser
	projectRepo   ProjectRepository
	buildRepo     BuildRepository
	resourceRepo  ResourceRepository
	volumeService VolumeService
	contService   ContainerService
	dockerAPI     ComposeDockerAPI
	cfg           ConfigProvider
	imageCleaner  ImageCleaner
	objectStore   ObjectStorage
	stagedObjects StagedObjectRepository
	diskMetrics   HostDiskMetricsProvider
	hostDiskPath  string
	users         UserInfoProvider
	auditor       AuditRecorder
	builderClient BuilderClient
	activeMu      sync.Mutex
	active        map[uuid.UUID]*deploymentState
	wg            sync.WaitGroup
	logger        *slog.Logger
}

const (
	imageBuildsUnavailableMessage = "Image builds are currently unavailable. Use Docker Hub images."
	maxComposeYAMLBytes           = 1 << 20
)

var (
	errComposeDeploymentCanceled    = errors.New("compose deployment canceled")
	errComposeDeploymentInterrupted = errors.New("compose deployment interrupted")
)

var (
	cloneGitRepository        = gitsource.Clone
	archiveGitRepositoryToZip = gitsource.ArchiveToZip
)

type deploymentState struct {
	jobID     uuid.UUID
	projectID uuid.UUID
	scope     accessscope.Scope
	requestID string
	cancel    context.CancelCauseFunc

	mu                sync.Mutex
	buildIDs          []uuid.UUID
	createdVolumes    []uuid.UUID
	createdContainers []uuid.UUID
	running           bool
}

type preparedDeploymentSource struct {
	ComposeYAML     []byte
	ComposeBaseDir  string
	SourceObjectKey string
	Archive         bool
}

func NewOrchestrator(
	parentCtx context.Context,
	projectRepo ProjectRepository,
	buildRepo BuildRepository,
	volumeService VolumeService,
	contService ContainerService,
	dockerAPI ComposeDockerAPI,
	cfg ConfigProvider,
	objectStore ObjectStorage,
	builds BuildJobCreator,
	imageCleaner ImageCleaner,
	logger *slog.Logger,
) *Orchestrator {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	return &Orchestrator{
		parentCtx:     parentCtx,
		parser:        NewParser(),
		projectRepo:   projectRepo,
		buildRepo:     buildRepo,
		volumeService: volumeService,
		contService:   contService,
		dockerAPI:     dockerAPI,
		cfg:           cfg,
		imageCleaner:  imageCleaner,
		objectStore:   objectStore,
		builderClient: NewLocalBuilderClient(objectStore, builds),
		active:        make(map[uuid.UUID]*deploymentState),
		logger:        logging.WithComponent(logger, "compose_orchestrator"),
	}
}

func (o *Orchestrator) SetResourceRepository(repo ResourceRepository) {
	o.resourceRepo = repo
}

func (o *Orchestrator) SetStagedObjectRepository(repo StagedObjectRepository) {
	o.stagedObjects = repo
}

func (o *Orchestrator) SetHostDiskGuard(metrics HostDiskMetricsProvider, path string) {
	o.diskMetrics = metrics
	o.hostDiskPath = path
}

func (o *Orchestrator) SetUserInfoProvider(users UserInfoProvider) {
	o.users = users
}

func (o *Orchestrator) SetAuditRecorder(auditor AuditRecorder) {
	o.auditor = auditor
}

func (s *deploymentState) addBuild(buildID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buildIDs = append(s.buildIDs, buildID)
}

func (s *deploymentState) addVolume(volumeID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdVolumes = append(s.createdVolumes, volumeID)
}

func (s *deploymentState) addContainer(containerID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdContainers = append(s.createdContainers, containerID)
}

func (s *deploymentState) markRunning() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = true
}

func (s *deploymentState) snapshot() (buildIDs []uuid.UUID, containerIDs []uuid.UUID, volumeIDs []uuid.UUID, running bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	buildIDs = append(buildIDs, s.buildIDs...)
	containerIDs = append(containerIDs, s.createdContainers...)
	volumeIDs = append(volumeIDs, s.createdVolumes...)
	return buildIDs, containerIDs, volumeIDs, s.running
}

func (o *Orchestrator) registerDeployment(state *deploymentState) {
	o.activeMu.Lock()
	defer o.activeMu.Unlock()
	o.active[state.projectID] = state
}

func (o *Orchestrator) unregisterDeployment(projectID uuid.UUID) {
	o.activeMu.Lock()
	defer o.activeMu.Unlock()
	delete(o.active, projectID)
}

func (o *Orchestrator) activeDeployment(projectID uuid.UUID) (*deploymentState, bool) {
	o.activeMu.Lock()
	defer o.activeMu.Unlock()
	state, ok := o.active[projectID]
	return state, ok
}

func (o *Orchestrator) cleanupContext(state *deploymentState) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	ctx = logging.ContextWithRequestID(ctx, state.requestID)
	ctx = accessscope.WithScope(ctx, state.scope)
	return ctx, cancel
}

func (o *Orchestrator) rollbackDeployment(state *deploymentState, logger *slog.Logger, deployErr error) {
	cleanupCtx, cancel := o.cleanupContext(state)
	defer cancel()
	logger.WarnContext(cleanupCtx, "compose deployment failed; rolling back", "error", deployErr)

	_, containerIDs, volumeIDs, _ := state.snapshot()
	for i := len(containerIDs) - 1; i >= 0; i-- {
		_ = o.contService.Delete(cleanupCtx, containerIDs[i])
	}
	for i := len(volumeIDs) - 1; i >= 0; i-- {
		_ = o.volumeService.Delete(cleanupCtx, volumeIDs[i])
	}

	errMsg := apperrors.SafeMessage(deployErr)
	_ = o.projectRepo.UpdateStatus(cleanupCtx, state.projectID, model.ProjectStatusFailed, &errMsg)
	if state.jobID != uuid.Nil {
		_ = o.projectRepo.CompleteComposeDeploymentJob(cleanupCtx, state.jobID, model.ComposeDeploymentStatusFailed, &errMsg)
	}
}

func (o *Orchestrator) cancelAndCleanupDeployment(state *deploymentState, cause error) {
	cleanupCtx, cancel := o.cleanupContext(state)
	defer cancel()
	logger := o.logger.With("project_id", state.projectID, "owner_id", state.scope.UserID)
	logger.WarnContext(cleanupCtx, "compose deployment canceled; cleaning up", "error", cause)

	buildIDs, containerIDs, volumeIDs, running := state.snapshot()
	o.cancelBuilds(cleanupCtx, buildIDs)
	for i := len(containerIDs) - 1; i >= 0; i-- {
		_ = o.contService.Delete(cleanupCtx, containerIDs[i])
	}
	for i := len(volumeIDs) - 1; i >= 0; i-- {
		_ = o.volumeService.Delete(cleanupCtx, volumeIDs[i])
	}
	if !running {
		o.deleteSuccessfulBuildImages(cleanupCtx, buildIDs, logger)
	}

	errMsg := "compose deployment canceled"
	_ = o.projectRepo.UpdateStatus(cleanupCtx, state.projectID, model.ProjectStatusCanceled, &errMsg)
	if state.jobID != uuid.Nil {
		_ = o.projectRepo.CompleteComposeDeploymentJob(cleanupCtx, state.jobID, model.ComposeDeploymentStatusCanceled, &errMsg)
	}
}

func (o *Orchestrator) deleteSuccessfulBuildImages(ctx context.Context, buildIDs []uuid.UUID, logger *slog.Logger) {
	if o.imageCleaner == nil {
		return
	}
	for _, buildID := range buildIDs {
		build, err := o.buildRepo.GetByID(ctx, buildID)
		if err != nil {
			continue
		}
		if build.Status != model.BuildStatusSuccess {
			continue
		}
		if err := o.imageCleaner.Delete(ctx, build.ImageID); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			logger.WarnContext(ctx, "failed to delete compose-built image during cancellation cleanup", "build_id", build.ID, "image_id", build.ImageID, "error", err)
		}
	}
}

func (o *Orchestrator) deleteBuildImages(ctx context.Context, builds []model.Build, logger *slog.Logger) {
	if o.imageCleaner == nil {
		return
	}
	for _, build := range builds {
		if build.Status != model.BuildStatusSuccess {
			continue
		}
		if err := o.imageCleaner.Delete(ctx, build.ImageID); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			logger.WarnContext(ctx, "failed to delete compose-built image during recovery cleanup", "build_id", build.ID, "image_id", build.ImageID, "error", err)
		}
	}
}

func isDeploymentCanceled(ctx context.Context, err error) bool {
	if errors.Is(err, errComposeDeploymentCanceled) {
		return true
	}
	return errors.Is(context.Cause(ctx), errComposeDeploymentCanceled)
}

func isDeploymentInterrupted(ctx context.Context, err error) bool {
	if errors.Is(err, errComposeDeploymentInterrupted) {
		return true
	}
	return errors.Is(context.Cause(ctx), errComposeDeploymentInterrupted)
}

func (o *Orchestrator) StartDeployment(ctx context.Context, projectName, archiveName string, archive io.Reader) (uuid.UUID, error) {
	if o.objectStore == nil {
		return uuid.Nil, apperrors.New(apperrors.ErrUnavailable, "compose source storage is unavailable")
	}
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if err := o.ensureHostDiskFloorForWrite(o.cfg.Get().ComposeUploadMaxBytes); err != nil {
		return uuid.Nil, err
	}
	cfg := o.cfg.Get()
	sourceObjectKey := composeSourceObjectKey(uuid.New().String(), archiveName)
	if err := o.reserveComposeSource(ctx, ownerID, sourceObjectKey, cfg.ComposeUploadMaxBytes); err != nil {
		return uuid.Nil, err
	}
	reserved := true
	releaseReservation := func() {
		if reserved {
			cleanupCtx, cancel := detachedCleanupContext(ctx)
			o.deleteComposeSourceObject(cleanupCtx, sourceObjectKey)
			cancel()
			reserved = false
		}
	}
	limitedArchive := &composeMaxBytesReader{r: archive, remaining: cfg.ComposeUploadMaxBytes}
	if err := o.objectStore.UploadStream(ctx, sourceObjectKey, limitedArchive, -1, "application/octet-stream"); err != nil {
		releaseReservation()
		if errors.Is(err, apperrors.ErrBadRequest) {
			return uuid.Nil, apperrors.New(apperrors.ErrBadRequest, "compose archive exceeds configured size limit")
		}
		return uuid.Nil, err
	}
	if readerAt, size, err := o.objectStore.NewReaderAt(ctx, sourceObjectKey); err == nil {
		_ = readerAt
		o.updateComposeSourceBytes(ctx, sourceObjectKey, size)
	}

	prepared, err := o.prepareObjectSource(ctx, sourceObjectKey, "")
	if err != nil {
		releaseReservation()
		return uuid.Nil, err
	}
	projectID, err := o.createDeploymentJob(ctx, projectName, model.ComposeSourceTypeUpload, sourceObjectKey, "", prepared)
	if err != nil {
		releaseReservation()
		return uuid.Nil, err
	}
	reserved = false
	return projectID, nil
}

func (o *Orchestrator) StartGitDeployment(ctx context.Context, projectName string, source model.GitSource) (uuid.UUID, error) {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if err := o.ensureHostDiskFloorForWrite(o.cfg.Get().GitMaxRepositoryBytes); err != nil {
		return uuid.Nil, err
	}
	if source.RepoURL == "" {
		return uuid.Nil, apperrors.New(apperrors.ErrBadRequest, "git repository url is required")
	}
	if source.ComposeFile != "" {
		composeFile, err := gitsource.CleanRelativePath(source.ComposeFile)
		if err != nil {
			return uuid.Nil, fmt.Errorf("%w: invalid compose file path: %v", apperrors.ErrBadRequest, err)
		}
		source.ComposeFile = composeFile
	}
	cfg := o.cfg.Get()
	if !cfg.GitSourcesEnabled {
		return uuid.Nil, apperrors.New(apperrors.ErrUnavailable, "Git sources are currently unavailable. Upload an archive instead.")
	}
	if _, err := gitsource.ValidateRepoURL(source.RepoURL, cfg.GitAllowedHosts); err != nil {
		return uuid.Nil, err
	}
	if err := gitsource.ValidateRef(source.Ref); err != nil {
		return uuid.Nil, err
	}
	if o.objectStore == nil {
		return uuid.Nil, apperrors.New(apperrors.ErrUnavailable, "compose source storage is unavailable")
	}
	prepared, cleanup, err := o.stageGitSource(ctx, ownerID, source)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return uuid.Nil, err
	}
	return o.createDeploymentJob(ctx, projectName, model.ComposeSourceTypeGit, prepared.SourceObjectKey, source.ComposeFile, prepared)
}

func (o *Orchestrator) createDeploymentJob(ctx context.Context, projectName, sourceType, sourceObjectKey, composeFile string, prepared preparedDeploymentSource) (uuid.UUID, error) {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		o.deleteComposeSourceObject(cleanupCtx, sourceObjectKey)
		cancel()
		return uuid.Nil, err
	}
	parsedProject, err := o.parseComposeProject(ctx, projectName, prepared.ComposeYAML, prepared.ComposeBaseDir, o.cfg.Get().ReservedDomainPrefixes)
	if err != nil {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		o.deleteComposeSourceObject(cleanupCtx, sourceObjectKey)
		cancel()
		return uuid.Nil, err
	}
	if len(parsedProject.Services) == 0 {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		o.deleteComposeSourceObject(cleanupCtx, sourceObjectKey)
		cancel()
		return uuid.Nil, apperrors.New(apperrors.ErrBadRequest, "compose project must define at least one service")
	}
	if err := o.ensureComposeQueueLimit(ctx, ownerID); err != nil {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		o.deleteComposeSourceObject(cleanupCtx, sourceObjectKey)
		cancel()
		return uuid.Nil, err
	}
	if err := o.ensureDeploymentCapacity(ctx, ownerID, parsedProject); err != nil {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		o.deleteComposeSourceObject(cleanupCtx, sourceObjectKey)
		cancel()
		return uuid.Nil, err
	}
	if composeRequiresBuild(parsedProject) && !prepared.Archive {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		o.deleteComposeSourceObject(cleanupCtx, sourceObjectKey)
		cancel()
		return uuid.Nil, apperrors.New(apperrors.ErrBadRequest, "compose build requires an archive source")
	}
	if composeRequiresBuild(parsedProject) && !o.cfg.Get().ImageBuildsEnabled {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		o.deleteComposeSourceObject(cleanupCtx, sourceObjectKey)
		cancel()
		return uuid.Nil, apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}

	projectID := uuid.New()
	jobID := uuid.New()
	requestID := logging.RequestIDFromContext(ctx)
	p := model.Project{
		ID:      projectID,
		OwnerID: ownerID,
		Name:    projectName,
		Status:  model.ProjectStatusBuilding,
	}
	now := time.Now()
	msg := composequeue.DeploymentMessage{
		JobID:     jobID.String(),
		ProjectID: projectID.String(),
		OwnerID:   ownerID.String(),
		RequestID: requestID,
		CreatedAt: now.Unix(),
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		o.deleteComposeSourceObject(cleanupCtx, sourceObjectKey)
		cancel()
		return uuid.Nil, fmt.Errorf("failed to marshal compose queue message: %w", err)
	}
	job := model.ComposeDeploymentJob{
		ID:              jobID,
		ProjectID:       projectID,
		OwnerID:         ownerID,
		SourceType:      sourceType,
		SourceObjectKey: sourceObjectKey,
		ComposeFile:     composeFile,
		Status:          model.ComposeDeploymentStatusQueued,
		RequestID:       requestID,
	}
	outbox := model.ComposeDeploymentOutbox{
		ID:         uuid.New(),
		JobID:      jobID,
		Exchange:   composequeue.ExchangeName,
		RoutingKey: composequeue.RoutingKey,
		Payload:    payload,
		Status:     model.ComposeOutboxStatusPending,
	}
	if err := o.projectRepo.CreateWithComposeDeploymentJob(ctx, p, job, outbox); err != nil {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		o.deleteComposeSourceObject(cleanupCtx, sourceObjectKey)
		cancel()
		return uuid.Nil, err
	}
	o.logger.InfoContext(ctx, "compose deployment queued", "project_id", projectID, "job_id", jobID, "owner_id", ownerID, "source_type", sourceType)

	return projectID, nil
}

func (o *Orchestrator) CancelDeployment(ctx context.Context, projectID uuid.UUID) error {
	return o.requestCancel(ctx, projectID, false)
}

func (o *Orchestrator) CancelDeploymentForBuild(ctx context.Context, projectID, _ uuid.UUID) error {
	return o.requestCancel(ctx, projectID, true)
}

func (o *Orchestrator) requestCancel(ctx context.Context, projectID uuid.UUID, fromBuild bool) error {
	project, err := o.projectRepo.GetByID(ctx, projectID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, project.OwnerID); err != nil {
		return err
	}

	switch project.Status {
	case model.ProjectStatusBuilding, model.ProjectStatusDeploying, model.ProjectStatusCanceling:
	case model.ProjectStatusCanceled:
		return nil
	default:
		if fromBuild {
			return nil
		}
		return apperrors.New(apperrors.ErrConflict, "compose deployment is not active")
	}

	if err := o.projectRepo.UpdateStatus(ctx, projectID, model.ProjectStatusCanceling, nil); err != nil {
		return err
	}
	if err := o.projectRepo.RequestComposeDeploymentCancel(ctx, projectID); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}
	state, ok := o.activeDeployment(projectID)
	if ok {
		state.cancel(errComposeDeploymentCanceled)
	}
	return nil
}

func (o *Orchestrator) Stop(ctx context.Context) error {
	o.activeMu.Lock()
	states := make([]*deploymentState, 0, len(o.active))
	for _, state := range o.active {
		states = append(states, state)
	}
	o.activeMu.Unlock()

	for _, state := range states {
		state.cancel(errComposeDeploymentInterrupted)
	}

	done := make(chan struct{})
	go func() {
		o.wg.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (o *Orchestrator) CleanupInterruptedDeployments(ctx context.Context, errorMessage string) error {
	if o.resourceRepo == nil {
		return nil
	}
	jobs, err := o.projectRepo.ListInterruptedComposeDeploymentJobs(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := o.cleanupInterruptedDeployment(ctx, job, errorMessage); err != nil {
			o.logger.WarnContext(ctx, "failed to cleanup interrupted compose deployment", "project_id", job.ProjectID, "job_id", job.ID, "error", err)
		}
	}
	return nil
}

func (o *Orchestrator) cleanupInterruptedDeployment(ctx context.Context, job model.ComposeDeploymentJob, errorMessage string) error {
	if job.Stage != "" {
		return nil
	}
	containers, err := o.resourceRepo.GetByProjectID(ctx, job.ProjectID)
	if err != nil {
		return err
	}
	volumes, err := o.resourceRepo.GetVolumesByProjectID(ctx, job.ProjectID)
	if err != nil {
		return err
	}
	builds, err := o.buildRepo.GetByProjectID(ctx, job.ProjectID)
	if err != nil {
		return err
	}
	if len(containers) == 0 && len(volumes) == 0 && len(builds) == 0 {
		return nil
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cleanupCtx = accessscope.WithScope(cleanupCtx, accessscope.Scope{Kind: accessscope.KindUser, UserID: job.OwnerID})
	cleanupCtx = logging.ContextWithRequestID(cleanupCtx, job.RequestID)

	var cleanupErrors []error
	for i := len(containers) - 1; i >= 0; i-- {
		if err := o.contService.Delete(cleanupCtx, containers[i].ID); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("container %s: %w", containers[i].ID, err))
		}
	}
	for i := len(volumes) - 1; i >= 0; i-- {
		if err := o.volumeService.Delete(cleanupCtx, volumes[i].ID); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("volume %s: %w", volumes[i].ID, err))
		}
	}

	buildIDs := make([]uuid.UUID, 0, len(builds))
	for _, build := range builds {
		buildIDs = append(buildIDs, build.ID)
	}
	o.cancelBuilds(cleanupCtx, buildIDs)
	o.deleteBuildImages(cleanupCtx, builds, o.logger.With("project_id", job.ProjectID, "job_id", job.ID))

	cleanupMsg := errorMessage
	if len(cleanupErrors) > 0 {
		cleanupMsg = fmt.Sprintf("%s; cleanup errors: %v", errorMessage, cleanupErrors)
	}
	if err := o.projectRepo.UpdateStatus(cleanupCtx, job.ProjectID, model.ProjectStatusFailed, &cleanupMsg); err != nil {
		return err
	}
	if err := o.projectRepo.CompleteComposeDeploymentJob(cleanupCtx, job.ID, model.ComposeDeploymentStatusFailed, &cleanupMsg); err != nil {
		return err
	}
	o.deleteComposeSourceObject(cleanupCtx, job.SourceObjectKey)
	o.logger.WarnContext(cleanupCtx, "interrupted compose deployment cleaned up", "project_id", job.ProjectID, "job_id", job.ID)
	return nil
}

func (o *Orchestrator) parseComposeProject(ctx context.Context, projectName string, composeYAML []byte, composeBaseDir string, reservedDomainPrefixes []string) (*model.ComposeProject, error) {
	return o.parser.ParseAndValidateWithBase(ctx, projectName, composeYAML, reservedDomainPrefixes, composeBaseDir)
}

func (o *Orchestrator) ensureHostDiskFloor() error {
	return o.ensureHostDiskFloorForWrite(0)
}

func (o *Orchestrator) ensureHostDiskFloorForWrite(projectedWriteBytes int64) error {
	cfg := o.cfg.Get()
	if o.diskMetrics == nil || cfg.HostMinFreeDiskBytes <= 0 {
		return nil
	}
	path := o.hostDiskPath
	if path == "" {
		path = "/"
	}
	stats, err := o.diskMetrics.GetDiskUsage(path)
	if err != nil {
		return err
	}
	if projectedWriteBytes < 0 {
		projectedWriteBytes = 0
	}
	if stats.FreeBytes-projectedWriteBytes < cfg.HostMinFreeDiskBytes {
		return apperrors.ErrHostExhausted
	}
	return nil
}

func (o *Orchestrator) ensureComposeQueueLimit(ctx context.Context, ownerID uuid.UUID) error {
	limit := o.cfg.Get().MaxQueuedComposeDeploysPerUser
	if limit <= 0 {
		return nil
	}
	counter, ok := o.projectRepo.(activeComposeDeploymentCounter)
	if !ok {
		return nil
	}
	count, err := counter.CountActiveComposeDeploymentsByOwner(ctx, ownerID)
	if err != nil {
		return err
	}
	if count >= limit {
		return apperrors.ErrLimitExceeded
	}
	return nil
}

func (o *Orchestrator) ensureDeploymentCapacity(ctx context.Context, ownerID uuid.UUID, project *model.ComposeProject) error {
	if err := o.ensureHostDiskFloor(); err != nil {
		return err
	}
	checker, ok := o.contService.(CapacityChecker)
	if !ok || project == nil {
		return nil
	}
	requestedRam := int64(len(project.Services)) * o.cfg.Get().DefaultMemoryReservation
	return checker.CheckCapacity(ctx, ownerID, requestedRam, 0)
}

func (o *Orchestrator) reserveComposeSource(ctx context.Context, ownerID uuid.UUID, objectKey string, bytesReserved int64) error {
	if o.stagedObjects == nil || objectKey == "" {
		return nil
	}
	if bytesReserved < 0 {
		bytesReserved = 0
	}
	if err := o.ensureUserStagedDiskQuota(ctx, ownerID, bytesReserved); err != nil {
		return err
	}
	return o.stagedObjects.Reserve(ctx, model.StagedObjectReservation{
		ID:            uuid.New(),
		OwnerID:       ownerID,
		ObjectKey:     objectKey,
		Kind:          model.StagedObjectKindComposeSource,
		BytesReserved: bytesReserved,
	}, o.cfg.Get().MaxStagedSourceBytesPerUser)
}

func (o *Orchestrator) ensureUserStagedDiskQuota(ctx context.Context, ownerID uuid.UUID, incomingBytes int64) error {
	if o.stagedObjects == nil {
		return nil
	}
	// Compose already creates containers/volumes/builds through scoped services. At staging time,
	// only existing staged objects can bypass those regular disk quota checks.
	stagedBytes, err := o.stagedObjects.ActiveBytesByOwner(ctx, ownerID)
	if err != nil {
		return err
	}
	user, err := o.lookupUser(ctx, ownerID)
	if err != nil {
		return err
	}
	if bytesToMBRoundedUp(stagedBytes+incomingBytes) > user.QuotaDiskMB {
		return apperrors.New(apperrors.ErrQuotaExceeded, "user disk quota exceeded")
	}
	return nil
}

func (o *Orchestrator) lookupUser(ctx context.Context, ownerID uuid.UUID) (model.UserInfo, error) {
	if o.users == nil {
		return model.UserInfo{ID: ownerID, QuotaDiskMB: 1<<62 - 1}, nil
	}
	return o.users.GetUser(ctx, ownerID)
}

func (o *Orchestrator) updateComposeSourceBytes(ctx context.Context, objectKey string, bytesReserved int64) {
	if o.stagedObjects == nil || objectKey == "" {
		return
	}
	if err := o.stagedObjects.UpdateBytes(ctx, objectKey, bytesReserved); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		o.logger.WarnContext(ctx, "failed to update staged compose source reservation", "source_object_key", objectKey, "error", err)
	}
}

func (o *Orchestrator) deleteComposeSourceObject(ctx context.Context, objectKey string) {
	if objectKey == "" {
		return
	}
	if o.objectStore != nil {
		_ = o.objectStore.DeleteObject(ctx, objectKey)
	}
	if o.stagedObjects != nil {
		if err := o.stagedObjects.Release(ctx, objectKey); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			o.logger.WarnContext(ctx, "failed to release staged compose source reservation", "source_object_key", objectKey, "error", err)
		}
	}
}

func composeRequiresBuild(project *model.ComposeProject) bool {
	if project == nil {
		return false
	}
	for _, srv := range project.Services {
		if srv.BuildContext != "" {
			return true
		}
	}
	return false
}

func bytesToMBRoundedUp(bytes int64) int64 {
	if bytes <= 0 {
		return 0
	}
	const mb = 1024 * 1024
	return (bytes + mb - 1) / mb
}

type composeMaxBytesReader struct {
	r         io.Reader
	remaining int64
}

func (r *composeMaxBytesReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		var one [1]byte
		n, err := r.r.Read(one[:])
		if n > 0 {
			return 0, apperrors.ErrBadRequest
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func (o *Orchestrator) stageGitSource(ctx context.Context, ownerID uuid.UUID, source model.GitSource) (preparedDeploymentSource, func(), error) {
	cfg := o.cfg.Get()
	tmpDir, err := os.MkdirTemp("", "dcm-compose-git-*")
	if err != nil {
		return preparedDeploymentSource{}, nil, err
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	repoDir := filepath.Join(tmpDir, "repo")
	repoInfo, err := cloneGitRepository(ctx, gitsource.CloneRequest{
		RepoURL:            source.RepoURL,
		Ref:                source.Ref,
		DestDir:            repoDir,
		AllowedHosts:       cfg.GitAllowedHosts,
		Timeout:            time.Duration(cfg.GitCloneTimeoutSeconds) * time.Second,
		MaxRepositoryBytes: cfg.GitMaxRepositoryBytes,
	})
	if err != nil {
		cleanup()
		return preparedDeploymentSource{}, nil, err
	}

	composeFile, err := selectComposeFile(repoDir, source.ComposeFile)
	if err != nil {
		cleanup()
		return preparedDeploymentSource{}, nil, err
	}
	composeYAML, err := os.ReadFile(filepath.Join(repoDir, composeFile))
	if err != nil {
		cleanup()
		if errors.Is(err, os.ErrNotExist) {
			return preparedDeploymentSource{}, nil, apperrors.New(apperrors.ErrBadRequest, "compose file not found in git repository")
		}
		return preparedDeploymentSource{}, nil, err
	}
	sourceObjectKey := composeSourceObjectKey(uuid.New().String(), "source.zip")
	if err := o.reserveComposeSource(ctx, ownerID, sourceObjectKey, cfg.ComposeUploadMaxBytes); err != nil {
		cleanup()
		return preparedDeploymentSource{}, nil, err
	}
	releaseReservation := func() {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		o.deleteComposeSourceObject(cleanupCtx, sourceObjectKey)
		cancel()
	}
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		stats, archiveErr := archiveGitRepositoryToZip(ctx, repoDir, pw, gitsource.ArchiveLimits{
			MaxRepositoryBytes: cfg.GitMaxRepositoryBytes,
			MaxArchiveBytes:    cfg.ComposeUploadMaxBytes,
		})
		if archiveErr != nil {
			_ = pw.CloseWithError(archiveErr)
			errCh <- archiveErr
			return
		}
		o.logger.InfoContext(ctx, "git compose source archived",
			"archive_bytes", stats.ArchiveBytes,
			"repository_bytes", stats.RepositoryBytes,
		)
		errCh <- pw.Close()
	}()
	uploadErr := o.objectStore.UploadStream(ctx, sourceObjectKey, pr, -1, "application/zip")
	if uploadErr != nil {
		_ = pr.CloseWithError(uploadErr)
	}
	archiveErr := <-errCh
	if uploadErr != nil {
		releaseReservation()
		cleanup()
		return preparedDeploymentSource{}, nil, uploadErr
	}
	if archiveErr != nil {
		releaseReservation()
		cleanup()
		return preparedDeploymentSource{}, nil, archiveErr
	}
	if readerAt, size, err := o.objectStore.NewReaderAt(ctx, sourceObjectKey); err == nil {
		_ = readerAt
		o.updateComposeSourceBytes(ctx, sourceObjectKey, size)
	}

	o.logger.InfoContext(ctx, "git compose source cloned",
		"git_host", repoInfo.Host,
		"git_repo_path", repoInfo.Path,
		"git_ref", repoInfo.Ref,
		"compose_file", composeFile,
	)

	return preparedDeploymentSource{
		ComposeYAML:     composeYAML,
		ComposeBaseDir:  filepath.ToSlash(filepath.Dir(composeFile)),
		SourceObjectKey: sourceObjectKey,
		Archive:         true,
	}, cleanup, nil
}

func selectComposeFile(repoDir, requested string) (string, error) {
	if requested != "" {
		clean, err := gitsource.CleanRelativePath(requested)
		if err != nil {
			return "", fmt.Errorf("%w: invalid compose file path: %v", apperrors.ErrBadRequest, err)
		}
		info, err := os.Stat(filepath.Join(repoDir, clean))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", apperrors.New(apperrors.ErrBadRequest, "compose file not found in git repository")
			}
			return "", err
		}
		if info.IsDir() {
			return "", apperrors.New(apperrors.ErrBadRequest, "compose file path points to a directory")
		}
		return clean, nil
	}
	for _, candidate := range []string{"docker-compose.yml", "docker-compose.yaml"} {
		info, err := os.Stat(filepath.Join(repoDir, candidate))
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", apperrors.New(apperrors.ErrBadRequest, "docker-compose.yml not found in git repository")
}

func (o *Orchestrator) HandleDeploymentMessage(ctx context.Context, msg composequeue.DeploymentMessage) error {
	jobID, err := uuid.Parse(msg.JobID)
	if err != nil {
		return apperrors.New(apperrors.ErrBadRequest, "invalid compose deployment job id")
	}
	job, err := o.projectRepo.GetComposeDeploymentJob(ctx, jobID)
	if err != nil {
		return err
	}
	if job.CancelRequested || job.Status == model.ComposeDeploymentStatusCanceling {
		cancelMsg := "compose deployment canceled"
		_ = o.projectRepo.CompleteComposeDeploymentJob(ctx, job.ID, model.ComposeDeploymentStatusCanceled, &cancelMsg)
		_ = o.projectRepo.UpdateStatus(ctx, job.ProjectID, model.ProjectStatusCanceled, &cancelMsg)
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		o.deleteComposeSourceObject(cleanupCtx, job.SourceObjectKey)
		cancel()
		return nil
	}
	if job.Status != model.ComposeDeploymentStatusQueued {
		return nil
	}
	job, started, err := o.projectRepo.StartComposeDeploymentJob(ctx, jobID)
	if err != nil {
		return err
	}
	if !started {
		if job.CancelRequested || job.Status == model.ComposeDeploymentStatusCanceling {
			cancelMsg := "compose deployment canceled"
			_ = o.projectRepo.CompleteComposeDeploymentJob(ctx, job.ID, model.ComposeDeploymentStatusCanceled, &cancelMsg)
			_ = o.projectRepo.UpdateStatus(ctx, job.ProjectID, model.ProjectStatusCanceled, &cancelMsg)
			cleanupCtx, cancel := detachedCleanupContext(ctx)
			o.deleteComposeSourceObject(cleanupCtx, job.SourceObjectKey)
			cancel()
		}
		return nil
	}

	scope := accessscope.Scope{Kind: accessscope.KindUser, UserID: job.OwnerID}
	requestID := job.RequestID
	if requestID == "" {
		requestID = msg.RequestID
	}
	jobCtx := logging.ContextWithRequestID(ctx, requestID)
	jobCtx = accessscope.WithScope(jobCtx, scope)

	progressRepo, ok := o.projectRepo.(composeProgressRepository)
	if !ok {
		return apperrors.New(apperrors.ErrUnavailable, "compose progress repository is unavailable")
	}
	project, err := o.projectRepo.GetByID(jobCtx, job.ProjectID)
	if err != nil {
		return err
	}
	prepared, err := o.prepareObjectSource(jobCtx, job.SourceObjectKey, job.ComposeFile)
	if err != nil {
		o.failDeploymentJob(jobCtx, job, err)
		return nil
	}
	parsedProject, err := o.parseComposeProject(jobCtx, project.Name, prepared.ComposeYAML, prepared.ComposeBaseDir, o.cfg.Get().ReservedDomainPrefixes)
	if err != nil {
		o.failDeploymentJob(jobCtx, job, err)
		return nil
	}
	if composeRequiresBuild(parsedProject) && !prepared.Archive {
		o.failDeploymentJob(jobCtx, job, apperrors.New(apperrors.ErrBadRequest, "compose build requires an archive source"))
		return nil
	}
	if composeRequiresBuild(parsedProject) && !o.cfg.Get().ImageBuildsEnabled {
		o.failDeploymentJob(jobCtx, job, apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage))
		return nil
	}

	plan := composeDeploymentPlan{
		ProjectName: project.Name,
		SourceType:  job.SourceType,
		Services:    parsedProject.Services,
		Volumes:     parsedProject.Volumes,
	}
	resources := composeDeploymentResources{}
	ensureComposeResourceMaps(&resources)
	stage := model.ComposeDeploymentStageCreating
	projectStatus := model.ProjectStatusDeploying
	if composeRequiresBuild(parsedProject) {
		stage = model.ComposeDeploymentStageBuilding
		projectStatus = model.ProjectStatusBuilding
	}
	planJSON, resourceJSON, err := encodeComposeDeploymentState(plan, resources)
	if err != nil {
		o.failDeploymentJob(jobCtx, job, err)
		return nil
	}
	if err := progressRepo.SaveComposeDeploymentPlan(jobCtx, job.ID, stage, planJSON, resourceJSON); err != nil {
		return err
	}
	if err := o.projectRepo.UpdateStatus(jobCtx, job.ProjectID, projectStatus, nil); err != nil {
		return err
	}
	if stage == model.ComposeDeploymentStageBuilding {
		if err := o.ensureComposeBuilds(jobCtx, job, plan, &resources); err != nil {
			o.failDeploymentJob(jobCtx, job, err)
			return nil
		}
		planJSON, resourceJSON, err = encodeComposeDeploymentState(plan, resources)
		if err != nil {
			o.failDeploymentJob(jobCtx, job, err)
			return nil
		}
		if err := progressRepo.UpdateComposeDeploymentProgress(jobCtx, job.ID, job.ProjectID, projectStatus, stage, planJSON, resourceJSON); err != nil {
			return err
		}
		o.deleteComposeSourceObject(jobCtx, job.SourceObjectKey)
	} else {
		o.deleteComposeSourceObject(jobCtx, job.SourceObjectKey)
	}
	o.logger.InfoContext(jobCtx, "compose deployment plan persisted", "project_id", job.ProjectID, "job_id", job.ID, "stage", stage)
	return nil
}

func (o *Orchestrator) failDeploymentJob(ctx context.Context, job model.ComposeDeploymentJob, cause error) {
	errMsg := apperrors.SafeMessage(cause)
	o.logger.WarnContext(ctx, "compose deployment job failed", "project_id", job.ProjectID, "job_id", job.ID, "error", cause)
	_ = o.projectRepo.UpdateStatus(ctx, job.ProjectID, model.ProjectStatusFailed, &errMsg)
	_ = o.projectRepo.CompleteComposeDeploymentJob(ctx, job.ID, model.ComposeDeploymentStatusFailed, &errMsg)
	cleanupCtx, cancel := detachedCleanupContext(ctx)
	o.deleteComposeSourceObject(cleanupCtx, job.SourceObjectKey)
	cancel()
}

func (o *Orchestrator) ensureComposeBuilds(ctx context.Context, job model.ComposeDeploymentJob, plan composeDeploymentPlan, resources *composeDeploymentResources) error {
	ensureComposeResourceMaps(resources)
	if o.buildRepo != nil {
		builds, err := o.buildRepo.GetByProjectID(ctx, job.ProjectID)
		if err != nil {
			return err
		}
		for _, build := range builds {
			if build.ProjectServiceName != "" {
				resources.BuildIDs[build.ProjectServiceName] = build.ID
			}
		}
	}
	for _, srv := range plan.Services {
		if srv.BuildContext == "" {
			continue
		}
		if _, ok := resources.BuildIDs[srv.Name]; ok {
			continue
		}
		buildID, err := o.builderClient.TriggerBuild(ctx, job.ProjectID, srv, job.SourceObjectKey)
		if err != nil {
			return fmt.Errorf("failed to trigger build for service %s: %w", srv.Name, err)
		}
		resources.BuildIDs[srv.Name] = buildID
	}
	return nil
}

func (o *Orchestrator) runPipeline(ctx context.Context, state *deploymentState, projectName string, job model.ComposeDeploymentJob) {
	projectID := state.projectID
	ownerID := state.scope.UserID
	logger := o.logger.With("project_id", projectID, "owner_id", ownerID)
	logger.InfoContext(ctx, "compose deployment pipeline started", "project_name", projectName)
	cleanupSource := false
	defer func() {
		if cleanupSource {
			cleanupCtx, cancel := detachedCleanupContext(ctx)
			o.deleteComposeSourceObject(cleanupCtx, job.SourceObjectKey)
			cancel()
		}
	}()

	failProject := func(err error) {
		if isDeploymentInterrupted(ctx, err) {
			buildIDs, containerIDs, volumeIDs, _ := state.snapshot()
			if len(buildIDs) > 0 || len(containerIDs) > 0 || len(volumeIDs) > 0 {
				cleanupSource = true
				errMsg := "compose deployment interrupted after creating resources; redeploy the project"
				updateCtx, cancel := o.cleanupContext(state)
				defer cancel()
				logger.WarnContext(updateCtx, "compose deployment interrupted after partial work; marking failed", "error", err)
				_ = o.projectRepo.UpdateStatus(updateCtx, projectID, model.ProjectStatusFailed, &errMsg)
				_ = o.projectRepo.CompleteComposeDeploymentJob(updateCtx, job.ID, model.ComposeDeploymentStatusFailed, &errMsg)
				return
			}
			logCtx := context.WithoutCancel(ctx)
			logger.WarnContext(logCtx, "compose deployment interrupted; leaving job recoverable", "error", err)
			return
		}
		cleanupSource = true
		if isDeploymentCanceled(ctx, err) {
			o.cancelAndCleanupDeployment(state, err)
			return
		}
		updateCtx := ctx
		var cancel context.CancelFunc
		if ctx.Err() != nil {
			updateCtx, cancel = o.cleanupContext(state)
			defer cancel()
		}
		errMsg := apperrors.SafeMessage(err)
		logger.ErrorContext(updateCtx, "compose deployment failed", "error", err)
		_ = o.projectRepo.UpdateStatus(updateCtx, projectID, model.ProjectStatusFailed, &errMsg)
		_ = o.projectRepo.CompleteComposeDeploymentJob(updateCtx, job.ID, model.ComposeDeploymentStatusFailed, &errMsg)
	}

	// 1. Извлечение, парсинг и валидация YAML
	prepared, err := o.prepareObjectSource(ctx, job.SourceObjectKey, job.ComposeFile)
	if err != nil {
		failProject(err)
		return
	}

	parsedProject, err := o.parseComposeProject(ctx, projectName, prepared.ComposeYAML, prepared.ComposeBaseDir, o.cfg.Get().ReservedDomainPrefixes)
	if err != nil {
		failProject(err)
		return
	}
	if composeRequiresBuild(parsedProject) && !prepared.Archive {
		failProject(apperrors.New(apperrors.ErrBadRequest, "compose build requires an archive source"))
		return
	}
	// 2. Оркестрация сборок (Kaniko)
	var buildIDs []uuid.UUID
	for _, srv := range parsedProject.Services {
		if srv.BuildContext != "" { // Нужна сборка
			if !o.cfg.Get().ImageBuildsEnabled {
				o.cancelBuilds(ctx, buildIDs)
				failProject(apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage))
				return
			}
			if err := o.checkCanceled(ctx, state); err != nil {
				o.cancelBuilds(ctx, buildIDs)
				failProject(err)
				return
			}
			if status, ok, err := o.abortBuildStatus(ctx, buildIDs); err != nil {
				o.cancelBuilds(ctx, buildIDs)
				failProject(err)
				return
			} else if ok {
				o.cancelBuilds(ctx, buildIDs)
				if status == model.BuildStatusCanceled {
					failProject(errComposeDeploymentCanceled)
				} else {
					failProject(apperrors.New(apperrors.ErrConflict, fmt.Sprintf("build failed with status: %s", status)))
				}
				return
			}

			buildID, err := o.builderClient.TriggerBuild(ctx, projectID, srv, job.SourceObjectKey)
			if err != nil {
				o.cancelBuilds(ctx, buildIDs)
				failProject(fmt.Errorf("failed to trigger build for service %s: %w", srv.Name, err))
				return
			}
			buildIDs = append(buildIDs, buildID)
			state.addBuild(buildID)
		}
	}

	// Ожидание завершения всех сборок
	if len(buildIDs) > 0 {
		err = o.waitForBuilds(ctx, state, buildIDs)
		if err != nil {
			failProject(fmt.Errorf("build phase failed: %w", err))
			return
		}
	}

	// Развертывание контейнеров
	_ = o.projectRepo.UpdateStatus(ctx, projectID, model.ProjectStatusDeploying, nil)
	logger.InfoContext(ctx, "compose builds completed; starting deployment")

	rollback := func(deployErr error) {
		if isDeploymentInterrupted(ctx, deployErr) {
			cleanupSource = true
			errMsg := "compose deployment interrupted during deploy; redeploy the project"
			updateCtx, cancel := o.cleanupContext(state)
			defer cancel()
			logger.WarnContext(updateCtx, "compose deployment interrupted during deploy; marking failed", "error", deployErr)
			_ = o.projectRepo.UpdateStatus(updateCtx, projectID, model.ProjectStatusFailed, &errMsg)
			_ = o.projectRepo.CompleteComposeDeploymentJob(updateCtx, job.ID, model.ComposeDeploymentStatusFailed, &errMsg)
			return
		}
		cleanupSource = true
		if isDeploymentCanceled(ctx, deployErr) {
			o.cancelAndCleanupDeployment(state, deployErr)
			return
		}
		o.rollbackDeployment(state, logger, deployErr)
	}

	// Создаем Именованные Тома (Volumes)
	// Маппинг: имя тома из YAML -> реальный UUID в базе
	volumeNameMap := make(map[string]uuid.UUID)

	for _, volParams := range parsedProject.Volumes {
		if err := o.checkCanceled(ctx, state); err != nil {
			rollback(err)
			return
		}
		volParams.ProjectID = &projectID

		volID, err := o.volumeService.Create(ctx, volParams)
		if err != nil {
			rollback(fmt.Errorf("failed to create volume %s: %w", volParams.Name, err))
			return
		}
		state.addVolume(volID)
		volumeNameMap[volParams.Name] = volID
	}

	serviceToContainerID := make(map[string]uuid.UUID)

	// Создаем Контейнеры (Services)
	for _, srv := range parsedProject.Services {
		if err := o.checkCanceled(ctx, state); err != nil {
			rollback(err)
			return
		}
		// Подготавливаем Mounts (меняем строковое имя из YAML на сгенерированный UUID тома)
		var resolvedMounts []model.VolumeMountParams
		for _, m := range srv.VolumeMounts {
			if vid, ok := volumeNameMap[m.VolumeName]; ok {
				resolvedMounts = append(resolvedMounts, model.VolumeMountParams{
					VolumeID:   vid,
					MountPath:  m.MountPath,
					IsReadOnly: m.IsReadOnly,
				})
			}
		}

		// Формируем параметры
		createParams := model.ContainerCreateParams{
			ProjectID:    &projectID,
			Name:         fmt.Sprintf("%s_%s", projectName, srv.Name), // Визуальное имя для юзера
			NetworkAlias: srv.Name,                                    // DNS алиас из compose
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

		contID, err := o.contService.Create(ctx, createParams)
		if err != nil {
			// Если нет квоты или Docker упал -> откатываем весь проект
			rollback(fmt.Errorf("failed to create service %s: %w", srv.Name, err))
			return
		}
		state.addContainer(contID)
		serviceToContainerID[srv.Name] = contID
		o.recordComposeExposeAudit(ctx, ownerID, projectID, projectName, job.SourceType, srv, contID, createParams.Name)
	}

	if err := o.waitForContainerCreates(ctx, state, serviceToContainerID); err != nil {
		rollback(err)
		return
	}

	serviceGraph, err := projectServiceGraph(projectID, parsedProject.Services, serviceToContainerID)
	if err != nil {
		rollback(err)
		return
	}
	if err := o.projectRepo.SaveServiceGraph(ctx, projectID, serviceGraph); err != nil {
		rollback(fmt.Errorf("failed to save compose service graph: %w", err))
		return
	}

	// 3.3 Запуск контейнеров (Авто-старт после создания)
	for _, srv := range parsedProject.Services {
		if err := o.checkCanceled(ctx, state); err != nil {
			rollback(err)
			return
		}
		contID := serviceToContainerID[srv.Name]

		// 1. Ждем выполнения условий зависимостей
		for _, dep := range srv.DependsOn {
			depContID, ok := serviceToContainerID[dep.ServiceName]
			if !ok {
				err := fmt.Errorf("dependency %s not found for service %s", dep.ServiceName, srv.Name)
				if dep.Optional {
					logger.WarnContext(ctx, "optional compose dependency unavailable; continuing deployment",
						"project_id", projectID,
						"service_name", srv.Name,
						"dependency_service_name", dep.ServiceName,
						"condition", dep.Condition,
						"error", err,
					)
					continue
				}
				rollback(err)
				return
			}

			// Получаем DockerID зависимости из БД
			depContInfo, err := o.contService.GetByID(ctx, depContID)
			if err != nil {
				if dep.Optional {
					logger.WarnContext(ctx, "optional compose dependency unavailable; continuing deployment",
						"project_id", projectID,
						"service_name", srv.Name,
						"dependency_service_name", dep.ServiceName,
						"condition", dep.Condition,
						"error", err,
					)
					continue
				}
				rollback(err)
				return
			}

			// Блокирующий поллинг состояния
			err = o.waitForCondition(ctx, state, depContInfo.DockerID, dep.Condition)
			if err != nil {
				if dep.Optional {
					logger.WarnContext(ctx, "optional compose dependency failed; continuing deployment",
						"project_id", projectID,
						"service_name", srv.Name,
						"dependency_service_name", dep.ServiceName,
						"condition", dep.Condition,
						"error", err,
					)
					continue
				}
				rollback(fmt.Errorf("dependency %s failed condition %s: %w", dep.ServiceName, dep.Condition, err))
				return
			}
		}

		// 2. Все зависимости готовы, запускаем сам сервис
		err := o.contService.Start(ctx, contID)
		if err != nil {
			rollback(fmt.Errorf("failed to start service %s: %w", srv.Name, err))
			return
		}
	}

	// 4. Финал
	if err := ctx.Err(); err != nil {
		rollback(err)
		return
	}
	if err := o.projectRepo.UpdateStatus(ctx, projectID, model.ProjectStatusRunning, nil); err != nil {
		rollback(fmt.Errorf("failed to mark compose project running: %w", err))
		return
	}
	if err := o.projectRepo.CompleteComposeDeploymentJob(ctx, job.ID, model.ComposeDeploymentStatusSucceeded, nil); err != nil {
		rollback(fmt.Errorf("failed to mark compose deployment succeeded: %w", err))
		return
	}
	cleanupSource = true
	state.markRunning()
	logger.InfoContext(ctx, "compose deployment completed")
}

func (o *Orchestrator) recordComposeExposeAudit(ctx context.Context, ownerID, projectID uuid.UUID, projectName, sourceType string, srv model.ComposeService, containerID uuid.UUID, containerName string) {
	if o.auditor == nil || srv.DomainPrefix == "" || srv.InternalPort <= 0 {
		return
	}
	actorID, actorUsername, actorScope := auditActorFromContext(ctx)
	ownerUsername := actorUsername
	if ownerUsername == "" && o.users != nil {
		if user, err := o.users.GetUser(ctx, ownerID); err == nil {
			ownerUsername = user.Username
			if actorUsername == "" && actorID != nil && *actorID == ownerID {
				actorUsername = user.Username
			}
		}
	}
	baseDomain := ""
	if o.cfg != nil {
		baseDomain = o.cfg.Get().BaseDomain
	}
	_ = o.auditor.RecordAuditEvent(ctx, model.AuditEvent{
		ActorUserID:   actorID,
		ActorUsername: actorUsername,
		ActorScope:    actorScope,
		Action:        auditlog.ActionContainerExpose,
		Outcome:       auditlog.OutcomeSuccess,
		ResourceType:  auditlog.ResourceContainer,
		ResourceID:    containerID.String(),
		ResourceName:  containerName,
		OwnerID:       ownerPtr(ownerID),
		OwnerUsername: ownerUsername,
		RequestID:     requestIDFromContext(ctx),
		DetailsJSON: auditlog.SafeDetailsJSON(map[string]string{
			auditlog.DetailSourceType:     sourceType,
			auditlog.DetailProjectID:      projectID.String(),
			auditlog.DetailProjectName:    projectName,
			auditlog.DetailComposeService: srv.Name,
			auditlog.DetailContainerID:    containerID.String(),
			auditlog.DetailContainerName:  containerName,
			auditlog.DetailDomainPrefix:   srv.DomainPrefix,
			auditlog.DetailFullDomain:     composeFullDomain(srv.DomainPrefix, baseDomain),
			auditlog.DetailInternalPort:   auditlog.IntDetail(srv.InternalPort),
		}),
	})
}

func projectServiceGraph(projectID uuid.UUID, services []model.ComposeService, serviceToContainerID map[string]uuid.UUID) ([]model.ProjectServiceNode, error) {
	graph := make([]model.ProjectServiceNode, 0, len(services))
	serviceOrder := make(map[string]int, len(services))
	for i, srv := range services {
		serviceOrder[srv.Name] = i
	}
	for i, srv := range services {
		containerID, ok := serviceToContainerID[srv.Name]
		if !ok {
			return nil, fmt.Errorf("container mapping not found for service %s", srv.Name)
		}
		node := model.ProjectServiceNode{
			ProjectID:   projectID,
			ContainerID: containerID,
			ServiceName: srv.Name,
			StartOrder:  i,
		}
		for _, dep := range srv.DependsOn {
			depOrder, ok := serviceOrder[dep.ServiceName]
			if !ok {
				if dep.Optional {
					continue
				}
				return nil, fmt.Errorf("dependency %s not found for service %s", dep.ServiceName, srv.Name)
			}
			if depOrder >= i {
				return nil, fmt.Errorf("dependency %s must be ordered before service %s", dep.ServiceName, srv.Name)
			}
			depContainerID, ok := serviceToContainerID[dep.ServiceName]
			if !ok {
				if dep.Optional {
					continue
				}
				return nil, fmt.Errorf("container mapping not found for dependency %s", dep.ServiceName)
			}
			node.Dependencies = append(node.Dependencies, model.ProjectServiceDependency{
				ProjectID:            projectID,
				ContainerID:          containerID,
				DependsOnContainerID: depContainerID,
				DependsOnServiceName: dep.ServiceName,
				Condition:            dep.Condition,
				Optional:             dep.Optional,
			})
		}
		graph = append(graph, node)
	}
	return graph, nil
}

func (o *Orchestrator) waitForBuilds(ctx context.Context, state *deploymentState, buildIDs []uuid.UUID) error {
	interval := time.Duration(o.cfg.Get().ComposeBuildPollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := o.checkCanceled(ctx, state); err != nil {
			o.cancelBuilds(ctx, buildIDs)
			return err
		}
		allSuccess, err := o.checkBuilds(ctx, buildIDs)
		if err != nil {
			return err
		}
		if allSuccess {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			interval = time.Duration(o.cfg.Get().ComposeBuildPollIntervalSeconds) * time.Second
			if interval <= 0 {
				interval = time.Second
			}
			ticker.Reset(interval)
		}
	}
}

func (o *Orchestrator) checkBuilds(ctx context.Context, buildIDs []uuid.UUID) (bool, error) {
	allSuccess := true
	for _, bid := range buildIDs {
		b, err := o.buildRepo.GetByID(ctx, bid)
		if err != nil {
			o.cancelBuilds(ctx, buildIDs)
			return false, fmt.Errorf("build record %s unavailable: %w", bid, err)
		}
		if b.Status == model.BuildStatusCanceled {
			o.cancelBuilds(ctx, buildIDs)
			return false, errComposeDeploymentCanceled
		}
		if model.IsBuildFailedStatus(b.Status) {
			o.cancelBuilds(ctx, buildIDs)
			return false, apperrors.New(apperrors.ErrConflict, fmt.Sprintf("build failed with status: %s", b.Status))
		}
		if b.Status != model.BuildStatusSuccess {
			allSuccess = false
		}
	}
	return allSuccess, nil
}

func (o *Orchestrator) waitForContainerCreates(ctx context.Context, state *deploymentState, serviceToContainerID map[string]uuid.UUID) error {
	interval := time.Duration(o.cfg.Get().ComposeBuildPollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := o.checkCanceled(ctx, state); err != nil {
			return err
		}
		allCreated := true
		for serviceName, containerID := range serviceToContainerID {
			c, err := o.contService.GetByID(ctx, containerID)
			if err != nil {
				return fmt.Errorf("container for service %s unavailable: %w", serviceName, err)
			}
			switch c.Status {
			case model.ContainerStatusCreated, model.ContainerStatusRunning, model.ContainerStatusExited:
			case model.ContainerStatusError, model.ContainerStatusMissing:
				if c.LastError != nil && *c.LastError != "" {
					return apperrors.New(apperrors.ErrConflict, fmt.Sprintf("container create for service %s failed: %s", serviceName, *c.LastError))
				}
				return apperrors.New(apperrors.ErrConflict, fmt.Sprintf("container create for service %s failed with status %s", serviceName, c.Status))
			default:
				allCreated = false
			}
		}
		if allCreated {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			interval = time.Duration(o.cfg.Get().ComposeBuildPollIntervalSeconds) * time.Second
			if interval <= 0 {
				interval = time.Second
			}
			ticker.Reset(interval)
		}
	}
}

func (o *Orchestrator) abortBuildStatus(ctx context.Context, buildIDs []uuid.UUID) (string, bool, error) {
	for _, buildID := range buildIDs {
		b, err := o.buildRepo.GetByID(ctx, buildID)
		if err != nil {
			return "", false, fmt.Errorf("build record %s unavailable: %w", buildID, err)
		}
		if b.Status == model.BuildStatusCanceled {
			return b.Status, true, nil
		}
		if model.IsBuildFailedStatus(b.Status) {
			return b.Status, true, nil
		}
	}
	return "", false, nil
}

func (o *Orchestrator) cancelBuilds(ctx context.Context, buildIDs []uuid.UUID) {
	for _, buildID := range buildIDs {
		if err := o.builderClient.CancelBuild(ctx, buildID); err != nil {
			o.logger.WarnContext(ctx, "failed to cancel build", "build_id", buildID, "error", err)
		}
	}
}

func (o *Orchestrator) checkCanceled(ctx context.Context, state *deploymentState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if state == nil || state.jobID == uuid.Nil {
		return nil
	}
	job, err := o.projectRepo.GetComposeDeploymentJob(ctx, state.jobID)
	if err != nil {
		return err
	}
	if job.CancelRequested || job.Status == model.ComposeDeploymentStatusCanceling {
		state.cancel(errComposeDeploymentCanceled)
		return errComposeDeploymentCanceled
	}
	return nil
}

func (o *Orchestrator) prepareObjectSource(ctx context.Context, objectKey, composeFile string) (preparedDeploymentSource, error) {
	readerAt, size, err := o.objectStore.NewReaderAt(ctx, objectKey)
	if err != nil {
		return preparedDeploymentSource{}, err
	}
	if zr, err := zip.NewReader(readerAt, size); err == nil {
		return extractComposeFileFromZip(zr, objectKey, composeFile)
	}
	if composeFile != "" {
		return preparedDeploymentSource{}, apperrors.New(apperrors.ErrBadRequest, "compose source object is not a zip archive")
	}
	rc, err := o.objectStore.OpenObject(ctx, objectKey)
	if err != nil {
		return preparedDeploymentSource{}, err
	}
	defer rc.Close()
	composeYAML, err := readLimitedComposeYAML(rc)
	if err != nil {
		return preparedDeploymentSource{}, err
	}
	if !strings.Contains(string(composeYAML), "services:") {
		return preparedDeploymentSource{}, apperrors.New(apperrors.ErrBadRequest, "invalid file format: expected zip archive or raw docker-compose.yml")
	}
	return preparedDeploymentSource{
		ComposeYAML:     composeYAML,
		SourceObjectKey: objectKey,
		Archive:         false,
	}, nil
}

func extractComposeFileFromZip(zr *zip.Reader, objectKey, requested string) (preparedDeploymentSource, error) {
	candidates := []string{requested}
	if requested == "" {
		candidates = []string{"docker-compose.yml", "docker-compose.yaml"}
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		clean := filepath.ToSlash(filepath.Clean(candidate))
		for _, f := range zr.File {
			if filepath.ToSlash(f.Name) != clean {
				continue
			}
			if f.UncompressedSize64 > maxComposeYAMLBytes {
				return preparedDeploymentSource{}, apperrors.New(apperrors.ErrBadRequest, "compose file exceeds configured size limit")
			}
			rc, err := f.Open()
			if err != nil {
				return preparedDeploymentSource{}, err
			}
			composeYAML, err := readLimitedComposeYAML(rc)
			_ = rc.Close()
			if err != nil {
				return preparedDeploymentSource{}, err
			}
			return preparedDeploymentSource{
				ComposeYAML:     composeYAML,
				ComposeBaseDir:  filepath.ToSlash(filepath.Dir(clean)),
				SourceObjectKey: objectKey,
				Archive:         true,
			}, nil
		}
	}
	return preparedDeploymentSource{}, apperrors.New(apperrors.ErrBadRequest, "docker-compose.yml not found in zip archive")
}

func readLimitedComposeYAML(r io.Reader) ([]byte, error) {
	limited := io.LimitReader(r, maxComposeYAMLBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > maxComposeYAMLBytes {
		return nil, apperrors.New(apperrors.ErrBadRequest, "compose file exceeds configured size limit")
	}
	return data, nil
}

func composeSourceObjectKey(fileID, fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext != ".zip" {
		ext = ".zip"
	}
	return "compose-sources/" + fileID + ext
}

func (o *Orchestrator) waitForCondition(ctx context.Context, state *deploymentState, dockerID string, condition string) error {
	return dependencywait.Wait(ctx, o.cfg, o.dockerAPI, dockerID, condition, dependencywait.Options{
		BeforePoll: func(ctx context.Context) error {
			return o.checkCanceled(ctx, state)
		},
	})
}
