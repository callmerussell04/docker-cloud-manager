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
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildobjects"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/gitsource"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	"github.com/google/uuid"
)

type BuildRepository interface {
	Save(ctx context.Context, b model.Build) error
	CreateQueuedBuild(ctx context.Context, img model.Image, build model.Build, outbox model.BuildQueueOutbox) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Build, error)
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, opts model.ListOptions) ([]model.Build, int, error)
}

type BuildImageRepository interface {
	Save(ctx context.Context, img model.Image) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Image, error)
	Delete(ctx context.Context, id uuid.UUID) error
	GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error)
	UpdateBuildAndImageSizeTx(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error
	MarkBuildFailedAndDeleteImageTx(ctx context.Context, buildID, imageID uuid.UUID, status string) error
}

type BuildVolumeDiskRepository interface {
	GetUserUsedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type BuildObjectStore interface {
	UploadStream(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) error
	OpenObject(ctx context.Context, objectKey string) (io.ReadCloser, error)
	DeleteObject(ctx context.Context, objectKey string) error
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
	repo        BuildRepository
	imageRepo   BuildImageRepository
	volumeRepo  BuildVolumeDiskRepository
	registryAPI ImageRegistryAPI
	users       UserInfoProvider
	cfg         BuildConfigProvider
	objectStore BuildObjectStore
	deployments BuildDeploymentCanceler
	logger      *slog.Logger
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

func NewBuildService(repo BuildRepository, imageRepo BuildImageRepository, registryAPI ImageRegistryAPI, users UserInfoProvider, logger *slog.Logger, deps ...any) *BuildService {
	s := &BuildService{
		repo:        repo,
		imageRepo:   imageRepo,
		registryAPI: registryAPI,
		users:       users,
		logger:      logging.WithComponent(logger, "build_service"),
	}
	for _, dep := range deps {
		if v, ok := dep.(BuildVolumeDiskRepository); ok {
			s.volumeRepo = v
		}
		if v, ok := dep.(BuildConfigProvider); ok {
			s.cfg = v
		}
		if v, ok := dep.(BuildObjectStore); ok {
			s.objectStore = v
		}
		if v, ok := dep.(BuildDeploymentCanceler); ok {
			s.deployments = v
		}
	}
	return s
}

func (s *BuildService) SetDeploymentCanceler(canceler BuildDeploymentCanceler) {
	s.deployments = canceler
}

func (s *BuildService) ensureImageBuildsEnabled() error {
	if s.cfg != nil && !s.cfg.Get().ImageBuildsEnabled {
		return apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}
	return nil
}

func (s *BuildService) getUserUsedDiskMB(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	usedMB, err := s.imageRepo.GetUserUsedDiskSpace(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	if s.volumeRepo != nil {
		usedBytes, err := s.volumeRepo.GetUserUsedVolumeBytes(ctx, ownerID)
		if err != nil {
			return 0, err
		}
		usedMB += bytesToMBRoundedUp(usedBytes)
	}
	return usedMB, nil
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

func (s *BuildService) CreateBuildFromArchive(ctx context.Context, input BuildArchiveInput) (BuildInitResult, error) {
	if s.objectStore == nil {
		return BuildInitResult{}, apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
	}
	if err := s.ensureImageBuildsEnabled(); err != nil {
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

	limitedArchive := &maxBytesReader{r: input.Archive, remaining: cfg.MaxArchiveSizeBytes}
	if err := s.objectStore.UploadStream(ctx, archiveObjectKey, limitedArchive, -1, "application/octet-stream"); err != nil {
		_ = s.objectStore.DeleteObject(context.Background(), archiveObjectKey)
		if errors.Is(err, apperrors.ErrBadRequest) {
			return BuildInitResult{}, apperrors.New(apperrors.ErrBadRequest, "archive exceeds configured size limit")
		}
		return BuildInitResult{}, err
	}

	buildID, _, err := s.CreateBuildJob(ctx, input.Tag, archiveObjectKey, logObjectKey, input.ContextDir, input.Dockerfile, input.BuildArgs, logging.RequestIDFromContext(ctx))
	if err != nil {
		_ = s.objectStore.DeleteObject(context.Background(), archiveObjectKey)
		return BuildInitResult{}, err
	}

	args := []any{
		"request_id", logging.RequestIDFromContext(ctx),
		"build_id", buildID,
		"archive_object_key", archiveObjectKey,
		"log_object_key", logObjectKey,
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
	defer os.RemoveAll(tmpDir)

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

	archive, err := os.Open(archivePath)
	if err != nil {
		return BuildInitResult{}, err
	}
	defer archive.Close()

	if err := s.objectStore.UploadStream(ctx, archiveObjectKey, archive, stats.ArchiveBytes, "application/zip"); err != nil {
		_ = s.objectStore.DeleteObject(context.Background(), archiveObjectKey)
		return BuildInitResult{}, err
	}

	buildID, _, err := s.CreateBuildJob(ctx, input.Tag, archiveObjectKey, logObjectKey, contextDir, dockerfile, input.BuildArgs, logging.RequestIDFromContext(ctx))
	if err != nil {
		_ = s.objectStore.DeleteObject(context.Background(), archiveObjectKey)
		return BuildInitResult{}, err
	}

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
	if s.cfg == nil {
		return config.SystemConfig{
			ImageBuildsEnabled:     true,
			GitSourcesEnabled:      true,
			MaxArchiveSizeBytes:    50 << 20,
			GitCloneTimeoutSeconds: 60,
			GitMaxRepositoryBytes:  200 * 1024 * 1024,
		}
	}
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

	user, err := s.users.GetUser(ctx, ownerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	usedMB, err := s.getUserUsedDiskMB(ctx, ownerID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if usedMB >= user.QuotaDiskMB {
		return uuid.Nil, uuid.Nil, apperrors.New(apperrors.ErrQuotaExceeded, "user disk quota exceeded")
	}

	baseName, version := parseImageTag(input.Tag)
	normalizedTag := fmt.Sprintf("%s:%s", baseName, version)
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
		StartedAt:          now,
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

func (s *BuildService) StartBuildRecord(ctx context.Context, buildID uuid.UUID) (model.Build, bool, error) {
	b, err := s.repo.GetByID(ctx, buildID)
	if err != nil {
		return model.Build{}, false, err
	}
	if model.IsBuildTerminalStatus(b.Status) {
		return b, false, nil
	}
	if err := s.repo.UpdateStatus(ctx, buildID, model.BuildStatusRunning); err != nil {
		return model.Build{}, false, err
	}
	b.Status = model.BuildStatusRunning
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
	if b.Status == model.BuildStatusRunning {
		if img, err := s.imageRepo.GetByID(ctx, b.ImageID); err == nil {
			s.cleanupBuiltImageManifestForImage(ctx, b.ID, img)
		} else if !errors.Is(err, apperrors.ErrNotFound) {
			s.logger.WarnContext(ctx, "failed to fetch running build image for cancel cleanup", "build_id", buildID, "image_id", b.ImageID, "error", err)
		}
	}
	s.logger.WarnContext(ctx, "build record cancelled", "build_id", buildID, "image_id", b.ImageID, "owner_id", b.OwnerID)
	if err := s.imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, b.ID, b.ImageID, model.BuildStatusCanceled); err != nil {
		return err
	}
	s.cleanupBuildArchive(b)
	s.cancelDeploymentForBuild(ctx, b)
	return nil
}

func (s *BuildService) cleanupBuildArchive(build model.Build) {
	if s.objectStore == nil || build.ArchiveObjectKey == "" {
		return
	}
	if err := s.objectStore.DeleteObject(context.Background(), build.ArchiveObjectKey); err != nil {
		s.logger.WarnContext(context.Background(), "failed to delete canceled build archive object", "build_id", build.ID, "archive_object_key", build.ArchiveObjectKey, "error", err)
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
	if model.IsBuildTerminalStatus(b.Status) {
		if b.Status == model.BuildStatusCanceled && status == model.BuildStatusSuccess {
			s.cleanupBuiltImageManifest(ctx, b)
		}
		return nil
	}

	if status != model.BuildStatusSuccess {
		status = normalizeFailedBuildStatus(status)
		s.logger.WarnContext(ctx, "build record marked failed", "build_id", buildID, "image_id", imageID, "status", status)
		return s.imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, status)
	}

	img, err := s.imageRepo.GetByID(ctx, imageID)
	if err != nil {
		return err
	}

	baseName, version := parseImageTag(img.Tag)
	repoName := strings.ToLower(fmt.Sprintf("%s_%s", img.OwnerID.String(), baseName))

	sizeBytes, digest, err := s.registryAPI.GetImageSizeAndDigest(ctx, repoName, version)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to fetch built image metadata", "build_id", buildID, "image_id", imageID, "error", err)
		return s.imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, model.BuildStatusFailedInternal)
	}

	sizeMB := int(sizeBytes / (1024 * 1024))
	if sizeMB == 0 {
		sizeMB = 1
	}

	user, err := s.users.GetUser(ctx, img.OwnerID)
	if err != nil {
		return err
	}
	usedMB, err := s.getUserUsedDiskMB(ctx, img.OwnerID)
	if err != nil {
		return err
	}

	if usedMB+int64(sizeMB) > user.QuotaDiskMB {
		_ = s.registryAPI.DeleteManifest(ctx, repoName, digest)
		_ = s.imageRepo.MarkBuildFailedAndDeleteImageTx(ctx, buildID, imageID, model.BuildStatusFailedQuotaExceeded)
		s.logger.WarnContext(ctx, "built image rejected by disk quota", "build_id", buildID, "image_id", imageID, "owner_id", img.OwnerID, "size_mb", sizeMB)
		return apperrors.New(apperrors.ErrQuotaExceeded, "image size exceeds user disk quota, image removed")
	}

	if err := s.imageRepo.UpdateBuildAndImageSizeTx(ctx, buildID, imageID, status, sizeMB); err != nil {
		return err
	}
	s.logger.InfoContext(ctx, "build record completed", "build_id", buildID, "image_id", imageID, "owner_id", img.OwnerID, "size_mb", sizeMB)
	return nil
}

func (s *BuildService) cleanupBuiltImageManifest(ctx context.Context, build model.Build) {
	if s.registryAPI == nil {
		return
	}
	img, err := s.imageRepo.GetByID(ctx, build.ImageID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to fetch canceled build image for registry cleanup", "build_id", build.ID, "image_id", build.ImageID, "error", err)
		return
	}
	s.cleanupBuiltImageManifestForImage(ctx, build.ID, img)
}

func (s *BuildService) cleanupBuiltImageManifestForImage(ctx context.Context, buildID uuid.UUID, img model.Image) {
	if s.registryAPI == nil {
		return
	}

	baseName, version := parseImageTag(img.Tag)
	repoName := strings.ToLower(fmt.Sprintf("%s_%s", img.OwnerID.String(), baseName))
	_, digest, err := s.registryAPI.GetImageSizeAndDigest(ctx, repoName, version)
	if err != nil {
		s.logger.DebugContext(ctx, "canceled build manifest is not available for registry cleanup", "build_id", buildID, "image_id", img.ID, "error", err)
		return
	}
	if digest == "" {
		return
	}
	if err := s.registryAPI.DeleteManifest(ctx, repoName, digest); err != nil {
		s.logger.WarnContext(ctx, "failed to delete canceled build manifest", "build_id", buildID, "image_id", img.ID, "error", err)
		return
	}
	s.logger.InfoContext(ctx, "deleted canceled build manifest", "build_id", buildID, "image_id", img.ID)
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
	return s.repo.Delete(ctx, buildID)
}

func (s *BuildService) List(ctx context.Context, limit, offset int) ([]model.Build, int, error) {
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

func normalizeFailedBuildStatus(status string) string {
	switch status {
	case model.BuildStatusFailed,
		model.BuildStatusCanceled,
		model.BuildStatusFailedTimeout,
		model.BuildStatusFailedQuotaExceeded,
		model.BuildStatusFailedInternal:
		return status
	default:
		return model.BuildStatusFailedInternal
	}
}

type maxBytesReader struct {
	r         io.Reader
	remaining int64
}

func (r *maxBytesReader) Read(p []byte) (int, error) {
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
