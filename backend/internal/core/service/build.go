package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/cleanupctx"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/stagedsource"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildobjects"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/gitsource"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/imageref"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/validation"
	"github.com/google/uuid"
)

type BuildRepository interface {
	AcquireCapacityLock(ctx context.Context) (func(), error)
	CountActiveByOwner(ctx context.Context, ownerID uuid.UUID) (int, error)
	CountActive(ctx context.Context) (int, error)
	Save(ctx context.Context, b model.Build) error
	CreateQueuedBuild(ctx context.Context, img model.Image, build model.Build, outbox model.BuildQueueOutbox) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Build, error)
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, opts model.ListOptions) ([]model.Build, int, error)
}

type activeSystemBuildCounter interface {
	CountActive(ctx context.Context) (int, error)
}

type BuildContainerReservationRepository interface {
	GetTotalSystemReservedMemory(ctx context.Context) (int64, error)
	GetTotalSystemReservedCPU(ctx context.Context) (int64, error)
}

type BuildImageRepository interface {
	Save(ctx context.Context, img model.Image) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Image, error)
	Delete(ctx context.Context, id uuid.UUID) error
	GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetReplacementImageSizeMB(ctx context.Context, ownerID uuid.UUID, tag string, replacementImageID uuid.UUID) (int64, error)
	UpdateBuildAndImageSizeTx(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error
	MarkBuildFailedAndDeleteImageTx(ctx context.Context, buildID, imageID uuid.UUID, status string) error
}

type BuildVolumeDiskRepository interface {
	GetUserReservedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

// BuildObjectStore stages build archives and logs. StatObject sizes staged
// archives so their quota reservation reflects the stored bytes.
type BuildObjectStore interface {
	UploadStream(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) error
	OpenObject(ctx context.Context, objectKey string) (io.ReadCloser, error)
	DeleteObject(ctx context.Context, objectKey string) error
	StatObject(ctx context.Context, objectKey string) (objectstorage.ObjectInfo, error)
}

// BuildImageMetadataAPI reads built image metadata. Builds never delete
// manifests directly; revocation queues the fenced Registry cleanup.
type BuildImageMetadataAPI interface {
	GetImageSizeAndDigest(ctx context.Context, repo, tag string) (int64, string, error)
}

// BuildRegistryAuthorizer claims builds with a repository-scoped Registry
// credential and revokes that credential when the build ends. A claim is
// refused, leaving the build pending, while maxRunning builds already run.
type BuildRegistryAuthorizer interface {
	ClaimBuild(ctx context.Context, id uuid.UUID, lifetime time.Duration, maxRunning int) (model.Build, bool, error)
	RevokeBuild(ctx context.Context, id uuid.UUID, pushedTag model.RegistryTagDisposition) error
}

type BuildDeploymentCanceler interface {
	CancelDeploymentForBuild(ctx context.Context, projectID, buildID uuid.UUID) error
}

type BuildConfigProvider interface {
	Get() config.SystemConfig
}

const imageBuildsUnavailableMessage = "Image builds are currently unavailable. Use Docker Hub images."
const gitSourcesUnavailableMessage = "Git sources are currently unavailable. Upload an archive instead."

type BuildArchiveInput struct {
	Tag         string
	ContextDir  string
	Dockerfile  string
	BuildArgs   map[string]string
	ArchiveName string
	Archive     io.Reader
}

type BuildGitInput struct {
	RepoURL    string
	Ref        string
	Tag        string
	ContextDir string
	Dockerfile string
	BuildArgs  map[string]string
}

type BuildInitResult struct {
	BuildID uuid.UUID
}

type BuildService struct {
	repo         BuildRepository
	imageRepo    BuildImageRepository
	volumeRepo   BuildVolumeDiskRepository
	registryAPI  BuildImageMetadataAPI
	quotas       UserQuotaProvider
	cfg          BuildConfigProvider
	objectStore  BuildObjectStore
	deployments  BuildDeploymentCanceler
	staging      *stagedsource.Staging
	containers   BuildContainerReservationRepository
	diskMetrics  HostDiskMetricsProvider
	hostMetrics  HostMetricsProvider
	hostDiskPath string
	registryAuth BuildRegistryAuthorizer
	logger       *slog.Logger
}

// BuildServiceDeps requires admission, persistence and Registry authorization
// dependencies. ObjectStore and Deployments are optional; deployments may be
// wired after construction to resolve the BuildService/Compose orchestrator cycle.
type BuildServiceDeps struct {
	Repository    BuildRepository
	Images        BuildImageRepository
	Registry      BuildImageMetadataAPI
	Quotas        UserQuotaProvider
	Logger        *slog.Logger
	VolumeRepo    BuildVolumeDiskRepository
	Config        BuildConfigProvider
	ObjectStore   BuildObjectStore
	Deployments   BuildDeploymentCanceler
	StagedObjects StagedObjectRepository
	Containers    BuildContainerReservationRepository
	DiskMetrics   HostDiskMetricsProvider
	HostMetrics   HostMetricsProvider
	HostDiskPath  string
	RegistryAuth  BuildRegistryAuthorizer
}

type createBuildJobInput struct {
	Tag                string
	ArchiveObjectKey   string
	LogObjectKey       string
	ContextDir         string
	Dockerfile         string
	BuildArgs          map[string]string
	RequestID          string
	ProjectID          *uuid.UUID
	ProjectServiceName string
}

func NewBuildService(deps BuildServiceDeps) (*BuildService, error) {
	required := []struct {
		name  string
		value any
	}{
		{"repository", deps.Repository}, {"images", deps.Images},
		{"registry", deps.Registry}, {"quotas", deps.Quotas},
		{"config", deps.Config}, {"volumes", deps.VolumeRepo},
		{"staged objects", deps.StagedObjects}, {"containers", deps.Containers},
		{"disk metrics", deps.DiskMetrics}, {"host metrics", deps.HostMetrics},
		{"registry authorization", deps.RegistryAuth},
	}
	for _, dependency := range required {
		if dependency.value == nil || isNilServiceDependency(dependency.value) {
			return nil, fmt.Errorf("build service: %s is required", dependency.name)
		}
	}
	return &BuildService{
		repo:         deps.Repository,
		imageRepo:    deps.Images,
		registryAPI:  deps.Registry,
		quotas:       deps.Quotas,
		registryAuth: deps.RegistryAuth,
		volumeRepo:   deps.VolumeRepo,
		cfg:          deps.Config,
		objectStore:  deps.ObjectStore,
		deployments:  deps.Deployments,
		staging: stagedsource.New(stagedsource.Deps{
			Repository:   deps.StagedObjects,
			Quotas:       deps.Quotas,
			ReservedDisk: reservedDiskSource{images: deps.Images, volumes: deps.VolumeRepo},
			DiskMetrics:  deps.DiskMetrics,
			HostDiskPath: deps.HostDiskPath,
			Config:       deps.Config,
		}),
		containers:   deps.Containers,
		diskMetrics:  deps.DiskMetrics,
		hostMetrics:  deps.HostMetrics,
		hostDiskPath: deps.HostDiskPath,
		logger:       logging.WithComponent(deps.Logger, "build_service"),
	}, nil
}

func (s *BuildService) SetDeploymentCanceler(canceler BuildDeploymentCanceler) {
	s.deployments = canceler
}

func (s *BuildService) ensureImageBuildsEnabled() error {
	if !s.cfg.Get().ImageBuildsEnabled {
		return apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}
	return nil
}

func (s *BuildService) getUserReservedDiskMB(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	return reservedDiskMB(ctx, ownerID, s.imageRepo, s.volumeRepo)
}

// admitBuild checks the host memory and CPU one more build slot reserves and
// the owner's disk quota. Builds do not count against the owner's memory and
// CPU quotas, and a build queued behind MaxConcurrentBuilds others reserves
// nothing more. The caller holds the capacity lock.
func (s *BuildService) admitBuild(ctx context.Context, ownerID uuid.UUID) error {
	cfg := s.buildConfig()
	sources := admissionSources{
		quotas:       s.quotas,
		system:       s.containers,
		builds:       s.repo,
		images:       s.imageRepo,
		volumes:      s.volumeRepo,
		hostMetrics:  s.hostMetrics,
		diskMetrics:  s.diskMetrics,
		hostDiskPath: s.hostDiskPath,
		logger:       s.logger,
	}
	return sources.admit(ctx, ownerID, admissionRequest{builds: 1}, cfg, "build")
}

func (s *BuildService) ensureBuildQueueLimit(ctx context.Context, ownerID uuid.UUID) error {
	cfg := s.buildConfig()
	if cfg.MaxQueuedBuildsPerUser <= 0 {
		return nil
	}
	count, err := s.repo.CountActiveByOwner(ctx, ownerID)
	if err != nil {
		return err
	}
	if count >= cfg.MaxQueuedBuildsPerUser {
		return apperrors.ErrLimitExceeded
	}
	return nil
}

// reserveStagedBuildArchive reserves a staged build archive. A build counts the
// owner's reserved disk plus staged bytes against the disk quota.
func (s *BuildService) reserveStagedBuildArchive(ctx context.Context, ownerID uuid.UUID, objectKey string, bytesReserved int64) error {
	return s.staging.Reserve(ctx, stagedsource.Reservation{
		OwnerID:   ownerID,
		ObjectKey: objectKey,
		Kind:      model.StagedObjectKindBuildArchive,
		Bytes:     bytesReserved,
		Rule:      stagedsource.CountReservedAndStaged,
	})
}

func (s *BuildService) updateStagedBuildArchiveBytes(ctx context.Context, objectKey string) {
	if s.objectStore == nil || objectKey == "" {
		return
	}
	info, err := s.objectStore.StatObject(ctx, objectKey)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to stat staged build archive object", "error", err)
		return
	}
	if err := s.staging.Resize(ctx, objectKey, info.Size); err != nil {
		s.logger.WarnContext(ctx, "failed to update staged build archive reservation", "error", err)
	}
}

func (s *BuildService) releaseStagedBuildArchive(ctx context.Context, objectKey string) {
	if err := s.staging.Release(ctx, objectKey); err != nil {
		s.logger.WarnContext(ctx, "failed to release staged build archive reservation", "error", err)
	}
}

func (s *BuildService) CreateBuildJob(ctx context.Context, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (uuid.UUID, uuid.UUID, error) {
	return s.createBuildJob(ctx, createBuildJobInput{
		Tag:              tag,
		ArchiveObjectKey: archiveObjectKey,
		LogObjectKey:     logObjectKey,
		ContextDir:       contextDir,
		Dockerfile:       dockerfile,
		BuildArgs:        buildArgs,
		RequestID:        requestID,
	})
}

func (s *BuildService) CreateProjectBuildJob(ctx context.Context, projectID uuid.UUID, projectServiceName, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (uuid.UUID, uuid.UUID, error) {
	return s.createBuildJob(ctx, createBuildJobInput{
		Tag:                tag,
		ArchiveObjectKey:   archiveObjectKey,
		LogObjectKey:       logObjectKey,
		ContextDir:         contextDir,
		Dockerfile:         dockerfile,
		BuildArgs:          buildArgs,
		RequestID:          requestID,
		ProjectID:          &projectID,
		ProjectServiceName: projectServiceName,
	})
}

func (s *BuildService) ReserveBuildArchive(ctx context.Context, archiveObjectKey string) error {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return err
	}
	if archiveObjectKey == "" {
		return apperrors.New(apperrors.ErrBadRequest, "build archive object key is required")
	}
	if s.objectStore == nil {
		return apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}
	info, err := s.objectStore.StatObject(ctx, archiveObjectKey)
	if err != nil {
		return err
	}
	return s.reserveStagedBuildArchive(ctx, ownerID, archiveObjectKey, info.Size)
}

func (s *BuildService) ReleaseBuildArchive(ctx context.Context, archiveObjectKey string) {
	s.releaseStagedBuildArchive(ctx, archiveObjectKey)
}

func (s *BuildService) CreateBuildFromArchive(ctx context.Context, input BuildArchiveInput) (BuildInitResult, error) {
	if s.objectStore == nil {
		return BuildInitResult{}, apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}
	if err := s.ensureImageBuildsEnabled(); err != nil {
		return BuildInitResult{}, err
	}
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return BuildInitResult{}, err
	}
	if err := s.staging.CheckHostDiskFloor(s.buildConfig().MaxArchiveSizeBytes); err != nil {
		return BuildInitResult{}, err
	}
	cfg := s.buildConfig()
	if err := validation.ImageTag(input.Tag); err != nil {
		return BuildInitResult{}, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	if err := gitsource.ValidateRelativePath(input.ContextDir); err != nil {
		return BuildInitResult{}, fmt.Errorf("%w: invalid build context: %v", apperrors.ErrBadRequest, err)
	}
	if err := gitsource.ValidateRelativePath(input.Dockerfile); err != nil {
		return BuildInitResult{}, fmt.Errorf("%w: invalid dockerfile path: %v", apperrors.ErrBadRequest, err)
	}
	if input.Archive == nil || input.ArchiveName == "" {
		return BuildInitResult{}, apperrors.New(apperrors.ErrBadRequest, "build archive is required")
	}
	if input.BuildArgs == nil {
		input.BuildArgs = map[string]string{}
	}

	fileID := uuid.New().String()
	archiveObjectKey := buildobjects.ArchiveObjectKey(fileID, input.ArchiveName)
	logObjectKey := buildobjects.LogObjectKey(fileID)

	if err := s.reserveStagedBuildArchive(ctx, ownerID, archiveObjectKey, cfg.MaxArchiveSizeBytes); err != nil {
		return BuildInitResult{}, err
	}
	reserved := true
	releaseReservation := func() {
		if reserved {
			cleanupCtx, cancel := cleanupctx.Detached(ctx)
			s.releaseStagedBuildArchive(cleanupCtx, archiveObjectKey)
			cancel()
			reserved = false
		}
	}

	limitedArchive := stagedsource.CappedReader(input.Archive, cfg.MaxArchiveSizeBytes)
	if err := s.objectStore.UploadStream(ctx, archiveObjectKey, limitedArchive, -1, "application/octet-stream"); err != nil {
		cleanupCtx, cancel := cleanupctx.Detached(ctx)
		_ = s.objectStore.DeleteObject(cleanupCtx, archiveObjectKey) // best-effort removal of the rejected upload; the admission error is returned
		cancel()
		releaseReservation()
		if errors.Is(err, apperrors.ErrBadRequest) {
			return BuildInitResult{}, apperrors.New(apperrors.ErrBadRequest, "archive exceeds configured size limit")
		}
		return BuildInitResult{}, err
	}
	s.updateStagedBuildArchiveBytes(ctx, archiveObjectKey)

	buildID, _, err := s.CreateBuildJob(ctx, input.Tag, archiveObjectKey, logObjectKey, input.ContextDir, input.Dockerfile, input.BuildArgs, logging.RequestIDFromContext(ctx))
	if err != nil {
		cleanupCtx, cancel := cleanupctx.Detached(ctx)
		_ = s.objectStore.DeleteObject(cleanupCtx, archiveObjectKey) // best-effort removal of the rejected upload; the admission error is returned
		cancel()
		releaseReservation()
		return BuildInitResult{}, err
	}
	reserved = false

	args := []any{
		"request_id", logging.RequestIDFromContext(ctx),
		"build_id", buildID,
	}
	if scope, ok := accessscope.FromContext(ctx); ok {
		args = append(args, "user_id", scope.UserID)
	}
	s.logger.InfoContext(ctx, "build upload accepted", args...)

	return BuildInitResult{BuildID: buildID}, nil
}

func (s *BuildService) CreateBuildFromGit(ctx context.Context, input BuildGitInput) (BuildInitResult, error) {
	if s.objectStore == nil {
		return BuildInitResult{}, apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}
	if err := s.ensureImageBuildsEnabled(); err != nil {
		return BuildInitResult{}, err
	}
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return BuildInitResult{}, err
	}
	if err := s.staging.CheckHostDiskFloor(s.buildConfig().GitMaxRepositoryBytes); err != nil {
		return BuildInitResult{}, err
	}
	cfg := s.buildConfig()
	if !cfg.GitSourcesEnabled {
		return BuildInitResult{}, apperrors.New(apperrors.ErrUnavailable, gitSourcesUnavailableMessage)
	}
	if err := validation.ImageTag(input.Tag); err != nil {
		return BuildInitResult{}, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	contextDir, err := gitsource.CleanRelativePath(input.ContextDir)
	if err != nil {
		return BuildInitResult{}, fmt.Errorf("%w: invalid build context: %v", apperrors.ErrBadRequest, err)
	}
	dockerfile, err := gitsource.CleanRelativePath(input.Dockerfile)
	if err != nil {
		return BuildInitResult{}, fmt.Errorf("%w: invalid dockerfile path: %v", apperrors.ErrBadRequest, err)
	}
	if _, err := gitsource.ValidateRepoURL(input.RepoURL, cfg.GitAllowedHosts); err != nil {
		return BuildInitResult{}, err
	}
	if err := gitsource.ValidateRef(input.Ref); err != nil {
		return BuildInitResult{}, err
	}
	if input.BuildArgs == nil {
		input.BuildArgs = map[string]string{}
	}

	tmpDir, err := os.MkdirTemp("", "dcm-git-build-*")
	if err != nil {
		return BuildInitResult{}, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }() // best-effort temporary directory cleanup; failure is not actionable

	repoDir := filepath.Join(tmpDir, "repo")
	repoInfo, err := gitsource.Clone(ctx, gitsource.CloneRequest{
		RepoURL:            input.RepoURL,
		Ref:                input.Ref,
		DestDir:            repoDir,
		AllowedHosts:       cfg.GitAllowedHosts,
		Timeout:            time.Duration(cfg.GitCloneTimeoutSeconds) * time.Second,
		MaxRepositoryBytes: cfg.GitMaxRepositoryBytes,
	})
	if err != nil {
		return BuildInitResult{}, err
	}

	archivePath := filepath.Join(tmpDir, "source.zip")
	stats, err := gitsource.ArchiveToZipFile(ctx, repoDir, archivePath, gitsource.ArchiveLimits{
		MaxRepositoryBytes: cfg.GitMaxRepositoryBytes,
		MaxArchiveBytes:    cfg.MaxArchiveSizeBytes,
	})
	if err != nil {
		return BuildInitResult{}, err
	}

	fileID := uuid.New().String()
	archiveObjectKey := buildobjects.ArchiveObjectKey(fileID, "source.zip")
	logObjectKey := buildobjects.LogObjectKey(fileID)

	if err := s.reserveStagedBuildArchive(ctx, ownerID, archiveObjectKey, stats.ArchiveBytes); err != nil {
		return BuildInitResult{}, err
	}
	reserved := true
	releaseReservation := func() {
		if reserved {
			cleanupCtx, cancel := cleanupctx.Detached(ctx)
			s.releaseStagedBuildArchive(cleanupCtx, archiveObjectKey)
			cancel()
			reserved = false
		}
	}

	archive, err := os.Open(archivePath)
	if err != nil {
		releaseReservation()
		return BuildInitResult{}, err
	}
	defer func() { _ = archive.Close() }() // read-only stream; close failure is not actionable

	if err := s.objectStore.UploadStream(ctx, archiveObjectKey, archive, stats.ArchiveBytes, "application/zip"); err != nil {
		cleanupCtx, cancel := cleanupctx.Detached(ctx)
		_ = s.objectStore.DeleteObject(cleanupCtx, archiveObjectKey) // best-effort removal of the rejected upload; the admission error is returned
		cancel()
		releaseReservation()
		return BuildInitResult{}, err
	}

	buildID, _, err := s.CreateBuildJob(ctx, input.Tag, archiveObjectKey, logObjectKey, contextDir, dockerfile, input.BuildArgs, logging.RequestIDFromContext(ctx))
	if err != nil {
		cleanupCtx, cancel := cleanupctx.Detached(ctx)
		_ = s.objectStore.DeleteObject(cleanupCtx, archiveObjectKey) // best-effort removal of the rejected upload; the admission error is returned
		cancel()
		releaseReservation()
		return BuildInitResult{}, err
	}
	reserved = false

	args := []any{
		"request_id", logging.RequestIDFromContext(ctx),
		"build_id", buildID,
		"git_host", repoInfo.Host,
		"git_repo_path", repoInfo.Path,
		"git_ref", repoInfo.Ref,
		"archive_bytes", stats.ArchiveBytes,
		"repository_bytes", stats.RepositoryBytes,
	}
	if scope, ok := accessscope.FromContext(ctx); ok {
		args = append(args, "user_id", scope.UserID)
	}
	s.logger.InfoContext(ctx, "git build accepted", args...)

	return BuildInitResult{BuildID: buildID}, nil
}

func (s *BuildService) OpenBuildLogs(ctx context.Context, buildID uuid.UUID) (io.ReadCloser, error) {
	if s.objectStore == nil {
		return nil, apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}
	build, err := s.GetBuild(ctx, buildID)
	if err != nil {
		return nil, err
	}
	if build.LogFilePath == "" {
		return nil, apperrors.ErrNotFound
	}
	return s.objectStore.OpenObject(ctx, build.LogFilePath)
}

func (s *BuildService) buildConfig() config.SystemConfig {
	return s.cfg.Get()
}

func (s *BuildService) createBuildJob(ctx context.Context, input createBuildJobInput) (uuid.UUID, uuid.UUID, error) {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := s.ensureImageBuildsEnabled(); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := validation.ImageTag(input.Tag); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	if input.ArchiveObjectKey == "" || input.LogObjectKey == "" {
		return uuid.Nil, uuid.Nil, apperrors.New(apperrors.ErrBadRequest, "build archive and log object keys are required")
	}

	unlock, err := s.repo.AcquireCapacityLock(ctx)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if unlock != nil {
		defer unlock()
	}
	if err := s.ensureBuildQueueLimit(ctx, ownerID); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := s.admitBuild(ctx, ownerID); err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	normalizedTag := imageref.NormalizeTag(input.Tag)
	imageID := uuid.New()
	buildID := uuid.New()
	now := time.Now()

	message := buildqueue.ImageBuildMessage{
		BuildID:          buildID.String(),
		ImageID:          imageID.String(),
		OwnerID:          ownerID.String(),
		Tag:              normalizedTag,
		ArchiveObjectKey: input.ArchiveObjectKey,
		LogObjectKey:     input.LogObjectKey,
		ContextDir:       input.ContextDir,
		Dockerfile:       input.Dockerfile,
		BuildArgs:        input.BuildArgs,
		RequestID:        input.RequestID,
		CreatedAt:        now.Unix(),
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("failed to marshal build queue message: %w", err)
	}

	img := model.Image{
		ID:      imageID,
		OwnerID: ownerID,
		Tag:     normalizedTag,
		SizeMB:  0,
		Status:  model.ImageStatusBuilding,
	}
	build := model.Build{
		ID:                 buildID,
		ImageID:            imageID,
		OwnerID:            ownerID,
		ProjectID:          input.ProjectID,
		ProjectServiceName: input.ProjectServiceName,
		Status:             model.BuildStatusPending,
		LogFilePath:        input.LogObjectKey,
		ArchiveObjectKey:   input.ArchiveObjectKey,
		CreatedAt:          now,
	}
	outbox := model.BuildQueueOutbox{
		ID:         uuid.New(),
		BuildID:    buildID,
		Exchange:   buildqueue.ExchangeName,
		RoutingKey: buildqueue.RoutingKey,
		Payload:    payload,
		Status:     model.BuildOutboxStatusPending,
	}

	if err := s.repo.CreateQueuedBuild(ctx, img, build, outbox); err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	s.logger.InfoContext(ctx, "build job created", "build_id", buildID, "image_id", imageID, "owner_id", ownerID, "image_tag", normalizedTag)
	return buildID, imageID, nil
}

// StartBuildRecord claims a pending build for a Builder. Builds running at once
// stay within max_concurrent_builds however many Builders run: over the cap
// the build is returned unclaimed and still pending, and the Builder retries.
func (s *BuildService) StartBuildRecord(ctx context.Context, buildID uuid.UUID) (model.Build, bool, error) {
	cfg := s.buildConfig()
	b, started, err := s.registryAuth.ClaimBuild(ctx, buildID, time.Duration(cfg.MaxBuildTimeMinutes)*time.Minute, cfg.MaxConcurrentBuilds)
	if err != nil {
		return model.Build{}, false, err
	}
	if !started {
		return b, false, nil
	}
	s.logger.InfoContext(ctx, "build record started", "build_id", buildID, "image_id", b.ImageID, "owner_id", b.OwnerID)
	return b, true, nil
}

func (s *BuildService) CancelBuildRecord(ctx context.Context, buildID uuid.UUID) error {
	b, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, b.OwnerID); err != nil {
		return err
	}
	if model.IsBuildTerminalStatus(b.Status) {
		return nil
	}
	if err := s.registryAuth.RevokeBuild(ctx, b.ID, model.RegistryTagDelete); err != nil {
		return err
	}
	s.logger.WarnContext(ctx, "build record cancelled", "build_id", buildID, "image_id", b.ImageID, "owner_id", b.OwnerID)
	if err := s.imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, b.ID, b.ImageID, model.BuildStatusCanceled); err != nil {
		return err
	}
	s.cleanupBuildArchive(ctx, b)
	s.cancelDeploymentForBuild(ctx, b)
	return nil
}

// StopBuildForResourceExhaustion is the memory pressure worker's stop of a
// running build. Core never reaches the build container (ADR 0003): it
// records the build failed_resource_exhausted, and Builder's status poll
// stops Kaniko and reports the same status, which completion then leaves
// unchanged. The status is written first, then the Registry credential is
// revoked with tag deletion and the staged archive is cleaned up. Unlike
// cancellation, the stop does not cascade to a Compose deployment, which
// fails as for any failed build. A build that is not running, or that
// reaches another terminal status first, is left alone. It reports whether
// the build was stopped. A failed revocation is logged and still counts as
// a stop, because Builder's completion revokes the credential again.
func (s *BuildService) StopBuildForResourceExhaustion(ctx context.Context, buildID uuid.UUID) (bool, error) {
	b, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return false, fmt.Errorf("read build to stop: %w", err)
	}
	if b.Status != model.BuildStatusRunning {
		return false, nil
	}
	if err := s.imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, b.ID, b.ImageID, model.BuildStatusFailedResourceExhausted); err != nil {
		return false, fmt.Errorf("record build stopped for resource exhaustion: %w", err)
	}
	// The write is guarded by status, so a completion that landed first
	// keeps its outcome; read back which one was recorded.
	recorded, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return false, fmt.Errorf("read stopped build status: %w", err)
	}
	if recorded.Status != model.BuildStatusFailedResourceExhausted {
		return false, nil
	}
	s.logger.WarnContext(ctx, "build record stopped for resource exhaustion", "build_id", buildID, "image_id", b.ImageID, "owner_id", b.OwnerID)
	if err := s.registryAuth.RevokeBuild(ctx, b.ID, model.RegistryTagDelete); err != nil {
		s.logger.WarnContext(ctx, "failed to revoke stopped build registry credential", "build_id", buildID, "error", err)
	}
	s.cleanupBuildArchive(ctx, b)
	return true, nil
}

func (s *BuildService) cleanupBuildArchive(ctx context.Context, build model.Build) {
	if build.ArchiveObjectKey == "" {
		return
	}
	s.releaseStagedBuildArchive(ctx, build.ArchiveObjectKey)
	if s.objectStore == nil {
		return
	}
	cleanupCtx, cancel := cleanupctx.Detached(ctx)
	defer cancel()
	if err := s.objectStore.DeleteObject(cleanupCtx, build.ArchiveObjectKey); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		s.logger.WarnContext(cleanupCtx, "failed to delete canceled build archive object", "build_id", build.ID, "error", err)
	}
}

func (s *BuildService) cancelDeploymentForBuild(ctx context.Context, build model.Build) {
	if s.deployments == nil || build.ProjectID == nil {
		return
	}
	if err := s.deployments.CancelDeploymentForBuild(ctx, *build.ProjectID, build.ID); err != nil {
		s.logger.WarnContext(ctx, "failed to cascade compose deployment cancellation from build", "build_id", build.ID, "project_id", *build.ProjectID, "error", err)
	}
}

func (s *BuildService) CompleteBuildRecord(ctx context.Context, buildID, imageID uuid.UUID, status string, _ int) error {
	b, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return err
	}
	if b.ImageID != imageID {
		return apperrors.ErrForbidden
	}
	// A redelivered success after cancellation or failure still queues cleanup
	// of the manifest it may have pushed.
	failedEarlier := model.IsBuildTerminalStatus(b.Status) && b.Status != model.BuildStatusSuccess
	pushedTag := model.PushedTagDisposition(status)
	if failedEarlier {
		pushedTag = model.RegistryTagDelete
	}
	if err := s.registryAuth.RevokeBuild(ctx, b.ID, pushedTag); err != nil {
		return err
	}
	if model.IsBuildTerminalStatus(b.Status) {
		s.cleanupBuildArchive(ctx, b)
		return nil
	}

	if status != model.BuildStatusSuccess {
		status = normalizeFailedBuildStatus(status)
		s.logger.WarnContext(ctx, "build record marked failed", "build_id", buildID, "image_id", imageID, "status", status)
		if err := s.imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, status); err != nil {
			return err
		}
		s.cleanupBuildArchive(ctx, b)
		s.cancelDeploymentForBuild(ctx, b)
		return nil
	}

	img, err := s.imageRepo.GetByID(ctx, imageID)
	if err != nil {
		return err
	}

	baseName, version := parseImageTag(img.Tag)
	repoName := customImageRepositoryName(img.OwnerID, baseName)

	sizeBytes, _, err := s.registryAPI.GetImageSizeAndDigest(ctx, repoName, version)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to fetch built image metadata", "build_id", buildID, "image_id", imageID, "error", err)
		if revokeErr := s.registryAuth.RevokeBuild(ctx, b.ID, model.RegistryTagDelete); revokeErr != nil {
			return revokeErr
		}
		return s.imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, model.BuildStatusFailedInternal)
	}

	sizeMB := int(sizeBytes / (1024 * 1024))
	if sizeMB == 0 {
		sizeMB = 1
	}

	quota, err := s.quotas.EffectiveUserQuota(ctx, img.OwnerID)
	if err != nil {
		return err
	}
	usedMB, err := s.getUserReservedDiskMB(ctx, img.OwnerID)
	if err != nil {
		return err
	}
	replacedMB, err := s.imageRepo.GetReplacementImageSizeMB(ctx, img.OwnerID, img.Tag, img.ID)
	if err != nil {
		return err
	}
	usedMB -= replacedMB
	if usedMB < 0 {
		usedMB = 0
	}

	if usedMB+int64(sizeMB) > quota.DiskMB {
		if err := s.imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, model.BuildStatusFailedQuotaExceeded); err != nil {
			return fmt.Errorf("failed to mark quota-exceeded build failed: %w", err)
		}
		if err := s.registryAuth.RevokeBuild(ctx, b.ID, model.RegistryTagDelete); err != nil {
			return err
		}
		s.cleanupBuildArchive(ctx, b)
		s.cancelDeploymentForBuild(ctx, b)
		s.logger.WarnContext(ctx, "built image rejected by disk quota", "build_id", buildID, "image_id", imageID, "owner_id", img.OwnerID, "size_mb", sizeMB)
		return nil
	}

	if err := s.imageRepo.UpdateBuildAndImageSizeTx(ctx, buildID, imageID, status, sizeMB); err != nil {
		return err
	}
	s.cleanupBuildArchive(ctx, b)
	s.logger.InfoContext(ctx, "build record completed", "build_id", buildID, "image_id", imageID, "owner_id", img.OwnerID, "size_mb", sizeMB)
	return nil
}

func (s *BuildService) GetBuild(ctx context.Context, buildID uuid.UUID) (model.Build, error) {
	b, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return model.Build{}, err
	}
	if err := accessscope.RequireOwnerAccess(ctx, b.OwnerID); err != nil {
		return model.Build{}, err
	}
	return b, nil
}

func (s *BuildService) DeleteBuild(ctx context.Context, buildID uuid.UUID) error {
	b, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return err
	}
	if err := accessscope.RequireOwnerAccess(ctx, b.OwnerID); err != nil {
		return err
	}
	if err := s.registryAuth.RevokeBuild(ctx, b.ID, model.PushedTagDisposition(b.Status)); err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, buildID); err != nil {
		return err
	}
	s.cleanupBuildArchive(ctx, b)
	s.cleanupBuildLog(ctx, b)
	return nil
}

func (s *BuildService) List(ctx context.Context, limit, offset int) ([]model.Build, int, error) {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return nil, 0, err
	}
	if scope.Kind != accessscope.KindUser && scope.Kind != accessscope.KindAdmin {
		return nil, 0, apperrors.ErrForbidden
	}
	return s.repo.List(ctx, model.ListOptions{
		OwnerID: scope.OwnerFilter(),
		Limit:   limit,
		Offset:  offset,
	})
}

func normalizeFailedBuildStatus(status string) string {
	switch status {
	case model.BuildStatusFailed,
		model.BuildStatusCanceled,
		model.BuildStatusFailedTimeout,
		model.BuildStatusFailedQuotaExceeded,
		model.BuildStatusFailedResourceExhausted,
		model.BuildStatusFailedInternal:
		return status
	default:
		return model.BuildStatusFailedInternal
	}
}

func (s *BuildService) cleanupBuildLog(ctx context.Context, build model.Build) {
	if s.objectStore == nil || build.LogFilePath == "" {
		return
	}
	cleanupCtx, cancel := cleanupctx.Detached(ctx)
	defer cancel()
	if err := s.objectStore.DeleteObject(cleanupCtx, build.LogFilePath); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		s.logger.WarnContext(cleanupCtx, "failed to delete build log object", "build_id", build.ID, "error", err)
	}
}
