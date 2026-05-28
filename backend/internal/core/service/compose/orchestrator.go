package compose

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
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
	ResolveByName(ctx context.Context, name string) (uuid.UUID, error)
	Delete(ctx context.Context, volumeID uuid.UUID) error
}

type ContainerService interface {
	Create(ctx context.Context, params model.ContainerCreateParams) (uuid.UUID, error)
	Start(ctx context.Context, containerID uuid.UUID) error
	Delete(ctx context.Context, containerID uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Container, error)
}

type CapacityChecker interface {
	CheckCapacity(ctx context.Context, ownerID uuid.UUID, requestedRam int64, requestedCPU int64, projectedDiskWriteBytes int64) error
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
	logger        *slog.Logger
}

const (
	imageBuildsUnavailableMessage = "Image builds are currently unavailable. Use Docker Hub images."
	maxComposeYAMLBytes           = 1 << 20
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

	buildIDs          []uuid.UUID
	createdVolumes    []uuid.UUID
	createdContainers []uuid.UUID
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

func (o *Orchestrator) StartDeployment(ctx context.Context, projectName, archiveName, composeFile string, archive io.Reader) (uuid.UUID, error) {
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
	if composeFile != "" {
		clean, err := gitsource.CleanRelativePath(composeFile)
		if err != nil {
			return uuid.Nil, fmt.Errorf("%w: invalid compose file path: %v", apperrors.ErrBadRequest, err)
		}
		composeFile = clean
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

	prepared, err := o.prepareObjectSource(ctx, sourceObjectKey, composeFile)
	if err != nil {
		releaseReservation()
		return uuid.Nil, err
	}
	projectID, err := o.createDeploymentJob(ctx, projectName, model.ComposeSourceTypeUpload, sourceObjectKey, composeFile, prepared)
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
	return nil
}

func (o *Orchestrator) Stop(context.Context) error {
	return nil
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
	cfg := o.cfg.Get()
	requestedRam := int64(len(project.Services)) * cfg.DefaultMemoryReservation
	requestedCPU := int64(len(project.Services)) * cfg.DefaultCPUReservation
	return checker.CheckCapacity(ctx, ownerID, requestedRam, requestedCPU, 0)
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
		o.logger.WarnContext(ctx, "failed to update staged compose source reservation", "error", err)
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
			o.logger.WarnContext(ctx, "failed to release staged compose source reservation", "error", err)
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

func (o *Orchestrator) cancelBuilds(ctx context.Context, buildIDs []uuid.UUID) {
	for _, buildID := range buildIDs {
		if err := o.builderClient.CancelBuild(ctx, buildID); err != nil {
			o.logger.WarnContext(ctx, "failed to cancel build", "build_id", buildID, "error", err)
		}
	}
}

func (o *Orchestrator) prepareObjectSource(ctx context.Context, objectKey, composeFile string) (preparedDeploymentSource, error) {
	switch composeSourceExt(objectKey) {
	case ".zip":
		readerAt, size, err := o.objectStore.NewReaderAt(ctx, objectKey)
		if err != nil {
			return preparedDeploymentSource{}, err
		}
		zr, err := zip.NewReader(readerAt, size)
		if err != nil {
			return preparedDeploymentSource{}, apperrors.New(apperrors.ErrBadRequest, "invalid zip archive")
		}
		return extractComposeFileFromZip(zr, objectKey, composeFile)
	case ".tar":
		return o.prepareTarObjectSource(ctx, objectKey, composeFile, false)
	case ".tar.gz", ".tgz":
		return o.prepareTarObjectSource(ctx, objectKey, composeFile, true)
	case ".yml", ".yaml":
		if composeFile != "" {
			return preparedDeploymentSource{}, apperrors.New(apperrors.ErrBadRequest, "compose_file is only supported for archive uploads")
		}
		return o.prepareRawComposeObjectSource(ctx, objectKey)
	default:
		return preparedDeploymentSource{}, apperrors.ErrInvalidFileFormat
	}
}

func (o *Orchestrator) prepareRawComposeObjectSource(ctx context.Context, objectKey string) (preparedDeploymentSource, error) {
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
		return preparedDeploymentSource{}, apperrors.New(apperrors.ErrBadRequest, "invalid file format: expected archive or raw docker-compose.yml")
	}
	return preparedDeploymentSource{
		ComposeYAML:     composeYAML,
		SourceObjectKey: objectKey,
		Archive:         false,
	}, nil
}

func (o *Orchestrator) prepareTarObjectSource(ctx context.Context, objectKey, composeFile string, gzipped bool) (preparedDeploymentSource, error) {
	rc, err := o.objectStore.OpenObject(ctx, objectKey)
	if err != nil {
		return preparedDeploymentSource{}, err
	}
	defer rc.Close()
	var tarReader io.Reader = rc
	if gzipped {
		gzr, err := gzip.NewReader(rc)
		if err != nil {
			return preparedDeploymentSource{}, apperrors.New(apperrors.ErrBadRequest, "invalid gzip archive")
		}
		defer gzr.Close()
		tarReader = gzr
	}
	return extractComposeFileFromTar(tar.NewReader(tarReader), objectKey, composeFile)
}

func extractComposeFileFromZip(zr *zip.Reader, objectKey, requested string) (preparedDeploymentSource, error) {
	candidates := composeFileCandidates(requested)
	for _, clean := range candidates {
		for _, f := range zr.File {
			if cleanArchiveEntryName(f.Name) != clean {
				continue
			}
			if f.FileInfo().IsDir() {
				return preparedDeploymentSource{}, apperrors.New(apperrors.ErrBadRequest, "compose file path points to a directory")
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

func extractComposeFileFromTar(tr *tar.Reader, objectKey, requested string) (preparedDeploymentSource, error) {
	candidates := composeFileCandidates(requested)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return preparedDeploymentSource{}, apperrors.New(apperrors.ErrBadRequest, "invalid tar archive")
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		name := cleanArchiveEntryName(header.Name)
		if !isComposeFileCandidate(name, candidates) {
			continue
		}
		if header.Size > maxComposeYAMLBytes {
			return preparedDeploymentSource{}, apperrors.New(apperrors.ErrBadRequest, "compose file exceeds configured size limit")
		}
		composeYAML, err := readLimitedComposeYAML(tr)
		if err != nil {
			return preparedDeploymentSource{}, err
		}
		return preparedDeploymentSource{
			ComposeYAML:     composeYAML,
			ComposeBaseDir:  filepath.ToSlash(filepath.Dir(name)),
			SourceObjectKey: objectKey,
			Archive:         true,
		}, nil
	}
	return preparedDeploymentSource{}, apperrors.New(apperrors.ErrBadRequest, "docker-compose.yml not found in tar archive")
}

func composeFileCandidates(requested string) []string {
	if requested != "" {
		return []string{filepath.ToSlash(filepath.Clean(requested))}
	}
	return []string{"docker-compose.yml", "docker-compose.yaml"}
}

func isComposeFileCandidate(name string, candidates []string) bool {
	for _, candidate := range candidates {
		if candidate != "" && name == candidate {
			return true
		}
	}
	return false
}

func cleanArchiveEntryName(name string) string {
	name = strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "./")
	return filepath.ToSlash(filepath.Clean(name))
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
	ext := composeSourceExt(fileName)
	return "compose-sources/" + fileID + ext
}

func composeSourceExt(fileName string) string {
	lower := strings.ToLower(fileName)
	if strings.HasSuffix(lower, ".tar.gz") {
		return ".tar.gz"
	}
	switch ext := strings.ToLower(filepath.Ext(fileName)); ext {
	case ".zip", ".tar", ".tgz", ".yml", ".yaml":
		return ext
	default:
		return ".archive"
	}
}
