package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/gitsource"
	"github.com/callmerussell04/docker-cloud-manager/pkg/imageref"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
)

type FileManager interface {
	ValidateArchive(filePath, contextDir, dockerfile string) error
	CleanUp(filePath string) error
}

type WorkspaceManager interface {
	ArchivePath(buildID, objectKey string) string
	Create(buildID string) (string, func(), error)
	CleanUp(ctx context.Context, buildID string) error
}

type ArchiveExtractor interface {
	Extract(archivePath string, destDir string, maxUnpackedSize int64) error
}

type DockerAPI interface {
	RunBuildContainer(ctx context.Context, params model.BuildRuntimeSpec) (string, io.ReadCloser, error)
	WaitForBuild(ctx context.Context, containerID string) error
	CleanBuildContainer(ctx context.Context, containerID string) error
}

type LogManager interface {
	SaveLogs(logID string, dockerStream io.Reader, maxLogSize int64) (string, error)
	WriteSystemLog(ctx context.Context, logID string, message string, maxLogSize int64) error
	IsLogSizeLimitExceeded(err error) bool
	LogPath(logID string) string
	Exists(logID string) (bool, error)
	CleanUp(logID string) error
}

type ObjectStorage interface {
	UploadFile(ctx context.Context, objectKey, filePath, contentType string) error
	DownloadFile(ctx context.Context, objectKey, filePath string) error
	DeleteObject(ctx context.Context, objectKey string) error
}

type CoreClient interface {
	StartBuildRecord(ctx context.Context, buildID string) (model.BuildClaim, bool, model.BuildStatus, error)
	CompleteBuildRecord(ctx context.Context, buildID, imageID string, status model.BuildStatus, sizeMB int) error
	GetBuildStatus(ctx context.Context, buildID string) (model.BuildStatus, error)
}

type BuildResultStore interface {
	Load(ctx context.Context, buildID string) (model.BuildResult, bool, error)
	Save(ctx context.Context, buildID string, result model.BuildResult) error
	Delete(ctx context.Context, buildID string) error
}

// BuildNetworkAdmission refuses builds while the build network lacks tenant
// isolation or its host policy is unhealthy.
type BuildNetworkAdmission interface {
	ValidateBuildNetwork(ctx context.Context, networkName string) error
}

const defaultBuildCancelPollInterval = 2 * time.Second

// buildCleanupTimeout bounds cleanup that must outlive the build's own context.
const buildCleanupTimeout = 30 * time.Second

// cleanupContext detaches cleanup from the build's cancellation while keeping
// its request values, and bounds it so an unresponsive dependency cannot hold
// the queue delivery.
func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), buildCleanupTimeout)
}

type buildJob struct {
	Claim        model.BuildClaim
	BuildID      string
	ImageID      string
	OwnerID      string
	Tag          string
	ContextDir   string
	Dockerfile   string
	BuildArgs    map[string]string
	RequestID    string
	LogObjectKey string
}

// BuilderServiceDeps requires every execution dependency, including build
// network admission, so tenant build-network isolation cannot be skipped.
// Logger is optional and defaults to slog.Default.
type BuilderServiceDeps struct {
	Files            FileManager
	Workspaces       WorkspaceManager
	Extractor        ArchiveExtractor
	Docker           DockerAPI
	NetworkAdmission BuildNetworkAdmission
	Logs             LogManager
	ObjectStore      ObjectStorage
	Core             CoreClient
	Results          BuildResultStore
	Config           *config.RuntimeManager
	Logger           *slog.Logger
}

type BuilderService struct {
	fileManager      FileManager
	workspaces       WorkspaceManager
	extractor        ArchiveExtractor
	dockerAPI        DockerAPI
	networkAdmission BuildNetworkAdmission
	logManager       LogManager
	objectStore      ObjectStorage
	coreClient       CoreClient
	results          BuildResultStore
	retainedResults  map[string]model.BuildResult
	handling         map[string]bool
	config           *config.RuntimeManager
	activeMu         sync.Mutex
	active           map[string]context.CancelFunc
	activeBuilds     int
	wg               sync.WaitGroup
	logger           *slog.Logger
}

func NewBuilderService(deps BuilderServiceDeps) (*BuilderService, error) {
	required := []struct {
		name  string
		value any
	}{
		{"files", deps.Files}, {"workspaces", deps.Workspaces},
		{"extractor", deps.Extractor}, {"docker", deps.Docker},
		{"network admission", deps.NetworkAdmission}, {"logs", deps.Logs},
		{"object store", deps.ObjectStore}, {"core", deps.Core},
		{"results", deps.Results}, {"config", deps.Config},
	}
	for _, dependency := range required {
		if dependency.value == nil || isNilDependency(dependency.value) {
			return nil, fmt.Errorf("builder service: %s is required", dependency.name)
		}
	}
	return &BuilderService{
		fileManager:      deps.Files,
		workspaces:       deps.Workspaces,
		extractor:        deps.Extractor,
		dockerAPI:        deps.Docker,
		networkAdmission: deps.NetworkAdmission,
		logManager:       deps.Logs,
		objectStore:      deps.ObjectStore,
		coreClient:       deps.Core,
		results:          deps.Results,
		retainedResults:  make(map[string]model.BuildResult),
		handling:         make(map[string]bool),
		config:           deps.Config,
		active:           make(map[string]context.CancelFunc),
		logger:           logging.WithComponent(deps.Logger, "builder_service"),
	}, nil
}

func (s *BuilderService) acquireBuildSlot(ctx context.Context, maxConcurrent int) bool {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	for {
		s.activeMu.Lock()
		if s.activeBuilds < maxConcurrent {
			s.activeBuilds++
			s.activeMu.Unlock()
			return true
		}
		s.activeMu.Unlock()

		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

func (s *BuilderService) releaseBuildSlot() {
	s.activeMu.Lock()
	if s.activeBuilds > 0 {
		s.activeBuilds--
	}
	s.activeMu.Unlock()
}

func (s *BuilderService) Stop(ctx context.Context) error {
	s.activeMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.active))
	for _, cancel := range s.active {
		cancels = append(cancels, cancel)
	}
	s.activeMu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (s *BuilderService) HandleBuildMessage(ctx context.Context, msg buildqueue.ImageBuildMessage) (handleErr error) {
	s.wg.Add(1)
	defer s.wg.Done()

	ctx = logging.ContextWithRequestID(ctx, msg.RequestID)
	s.activeMu.Lock()
	if s.handling[msg.BuildID] {
		s.activeMu.Unlock()
		return apperrors.New(apperrors.ErrUnavailable, "build execution is active")
	}
	s.handling[msg.BuildID] = true
	result, retained := s.retainedResults[msg.BuildID]
	s.activeMu.Unlock()
	defer func() {
		if handleErr == nil {
			s.activeMu.Lock()
			_, finalized := s.retainedResults[msg.BuildID]
			s.activeMu.Unlock()
			if finalized {
				cleanupCtx, cancel := cleanupContext(ctx)
				defer cancel()
				if err := s.cleanupFinalizedBuild(cleanupCtx, msg); err != nil {
					handleErr = apperrors.Wrap(apperrors.ErrUnavailable, "build cleanup is pending", err)
				}
			}
		}
		s.activeMu.Lock()
		delete(s.handling, msg.BuildID)
		s.activeMu.Unlock()
	}()
	if !retained {
		var err error
		result, retained, err = s.results.Load(ctx, msg.BuildID)
		if err != nil {
			return fmt.Errorf("load build finalization: %w", err)
		}
	}
	if retained {
		if err := s.completeBuild(ctx, msg.BuildID, result.ImageID, result.Status, result.SizeMB); err != nil {
			return err
		}
		return nil
	}

	cfg := s.config.Refresh(ctx)

	if !s.acquireBuildSlot(ctx, cfg.MaxConcurrentBuilds) {
		return ctx.Err()
	}
	defer s.releaseBuildSlot()

	// Admit before the claim so an unhealthy network leaves the build pending
	// and the delivery retryable instead of recording a platform failure.
	if err := s.networkAdmission.ValidateBuildNetwork(ctx, cfg.BuildNetworkName); err != nil {
		return fmt.Errorf("admit build network: %w", err)
	}
	claim, started, status, err := s.coreClient.StartBuildRecord(ctx, msg.BuildID)
	if err != nil {
		return err
	}
	if !started {
		s.logger.InfoContext(ctx, "build queue message skipped", "build_id", msg.BuildID, "status", status)
		if isTerminalBuildStatus(status) {
			cleanupCtx, cancel := cleanupContext(ctx)
			defer cancel()
			if deleteErr := s.objectStore.DeleteObject(cleanupCtx, msg.ArchiveObjectKey); deleteErr != nil {
				s.logger.WarnContext(ctx, "failed to delete skipped build archive object", "build_id", msg.BuildID, "error", deleteErr)
			}
		}
		if !isTerminalBuildStatus(status) {
			return apperrors.New(apperrors.ErrUnavailable, "build execution has no finalization result yet")
		}
		return nil
	}
	imageID := claim.ImageID
	if imageID == "" || claim.RegistryUsername == "" || claim.RegistryPassword == "" || claim.Destination == "" {
		return apperrors.New(apperrors.ErrUnavailable, "build authorization is unavailable")
	}

	buildCtx, cancel := context.WithTimeout(ctx, cfg.MaxBuildTime)
	buildCtx = logging.ContextWithRequestID(buildCtx, msg.RequestID)

	s.activeMu.Lock()
	s.active[msg.BuildID] = cancel
	s.activeMu.Unlock()
	defer func() {
		s.activeMu.Lock()
		delete(s.active, msg.BuildID)
		s.activeMu.Unlock()
	}()

	pollDone := s.watchBuildCancellation(buildCtx, cancel, msg.BuildID, cfg.BuildCancelPollInterval)
	defer func() {
		cancel()
		<-pollDone
	}()

	archivePath := s.workspaces.ArchivePath(msg.BuildID, msg.ArchiveObjectKey)
	defer func() { _ = s.fileManager.CleanUp(archivePath) }() // best effort; cleanupFinalizedBuild retries and reports removal
	if err := s.objectStore.DownloadFile(buildCtx, msg.ArchiveObjectKey, archivePath); err != nil {
		if buildCtx.Err() != nil {
			return s.completeCanceledBuild(buildCtx, msg, imageID, cfg.MaxBuildLogSizeBytes)
		}
		return err
	}
	if err := s.fileManager.ValidateArchive(archivePath, msg.ContextDir, msg.Dockerfile); err != nil {
		if buildCtx.Err() != nil {
			return s.completeCanceledBuild(buildCtx, msg, imageID, cfg.MaxBuildLogSizeBytes)
		}
		logger := s.logger.With("build_id", msg.BuildID, "image_id", imageID, "owner_id", msg.OwnerID)
		if err := s.logManager.WriteSystemLog(buildCtx, msg.BuildID, "Build failed because the uploaded archive is invalid.", cfg.MaxBuildLogSizeBytes); err != nil {
			logger.WarnContext(ctx, "failed to write invalid archive build log", "error", err)
		}
		if uploadErr := s.uploadBuildLog(buildCtx, logger, msg.BuildID, msg.LogObjectKey); uploadErr != nil {
			logger.WarnContext(ctx, "failed to upload invalid archive build log", "error", uploadErr)
		}
		if completeErr := s.completeBuild(logging.ContextWithRequestID(context.Background(), msg.RequestID), msg.BuildID, imageID, buildStatusFailed, 0); completeErr != nil {
			return completeErr
		}
		return nil
	}

	if err := s.processBuild(buildCtx, cancel, cfg, archivePath, buildJob{
		Claim:        claim,
		BuildID:      msg.BuildID,
		ImageID:      imageID,
		OwnerID:      msg.OwnerID,
		Tag:          msg.Tag,
		ContextDir:   msg.ContextDir,
		Dockerfile:   msg.Dockerfile,
		BuildArgs:    msg.BuildArgs,
		RequestID:    msg.RequestID,
		LogObjectKey: msg.LogObjectKey,
	}); err != nil {
		return err
	}
	return nil
}

func (s *BuilderService) completeBuild(ctx context.Context, buildID, imageID string, status model.BuildStatus, sizeMB int) error {
	result := model.BuildResult{ImageID: imageID, Status: status, SizeMB: sizeMB}
	s.activeMu.Lock()
	s.retainedResults[buildID] = result
	s.activeMu.Unlock()
	if err := s.results.Save(ctx, buildID, result); err != nil {
		return fmt.Errorf("persist build finalization: %w", err)
	}
	if err := s.coreClient.CompleteBuildRecord(ctx, buildID, imageID, status, sizeMB); err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			// Missing image/user metadata is not proof that the build was deleted.
			if _, statusErr := s.coreClient.GetBuildStatus(ctx, buildID); errors.Is(statusErr, apperrors.ErrNotFound) {
				return nil
			}
		}
		s.logger.WarnContext(ctx, "build finalization remains pending", "build_id", buildID, "error", err)
		// Even application errors must not dead-letter a durably retained outcome.
		return apperrors.Wrap(apperrors.ErrUnavailable, "build finalization is pending", err)
	}
	return nil
}

func (s *BuilderService) cleanupFinalizedBuild(ctx context.Context, msg buildqueue.ImageBuildMessage) error {
	if err := s.objectStore.DeleteObject(ctx, msg.ArchiveObjectKey); err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return fmt.Errorf("clean finalized build archive object: %w", err)
	}
	if err := s.fileManager.CleanUp(s.workspaces.ArchivePath(msg.BuildID, msg.ArchiveObjectKey)); err != nil {
		return fmt.Errorf("clean finalized build archive file: %w", err)
	}
	if err := s.workspaces.CleanUp(ctx, msg.BuildID); err != nil {
		return fmt.Errorf("clean finalized build workspace: %w", err)
	}
	if err := s.logManager.CleanUp(msg.BuildID); err != nil {
		return fmt.Errorf("clean finalized build log: %w", err)
	}
	return s.forgetBuildResult(ctx, msg.BuildID)
}

func (s *BuilderService) forgetBuildResult(ctx context.Context, buildID string) error {
	if err := s.results.Delete(ctx, buildID); err != nil {
		return fmt.Errorf("clean build finalization: %w", err)
	}
	s.activeMu.Lock()
	delete(s.retainedResults, buildID)
	s.activeMu.Unlock()
	return nil
}

// completeCanceledBuild runs after the build context is done, so its log
// write and upload use a detached cleanup context.
func (s *BuilderService) completeCanceledBuild(ctx context.Context, msg buildqueue.ImageBuildMessage, imageID string, maxLogSize int64) error {
	logger := s.logger.With("build_id", msg.BuildID, "image_id", imageID, "owner_id", msg.OwnerID)
	logCtx, cancel := cleanupContext(ctx)
	defer cancel()
	s.writeSystemBuildLog(logCtx, logger, msg.BuildID, msg.LogObjectKey, maxLogSize, buildLogCanceled)
	if err := s.completeBuild(logging.ContextWithRequestID(context.Background(), msg.RequestID), msg.BuildID, imageID, buildStatusCanceled, 0); err != nil {
		return err
	}
	return nil
}

func (s *BuilderService) watchBuildCancellation(ctx context.Context, cancel context.CancelFunc, buildID string, pollInterval time.Duration) <-chan struct{} {
	if pollInterval <= 0 {
		pollInterval = defaultBuildCancelPollInterval
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		startedAt := time.Now()
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				status, err := s.coreClient.GetBuildStatus(ctx, buildID)
				if err != nil {
					s.logger.WarnContext(ctx, "failed to poll build status", "build_id", buildID, "error", err)
					continue
				}
				if isTerminalBuildStatus(status) {
					s.logger.InfoContext(ctx, "build cancellation detected", "build_id", buildID, "status", status, "duration_ms", time.Since(startedAt).Milliseconds())
					cancel()
					return
				}
			}
		}
	}()
	return done
}

func (s *BuilderService) processBuild(ctx context.Context, cancel context.CancelFunc, cfg config.BuilderConfig, archivePath string, job buildJob) error {
	// Очищаем архив после сборки; cleanupFinalizedBuild retries and reports removal.
	defer func() { _ = s.fileManager.CleanUp(archivePath) }()

	defer cancel()
	ctx = logging.ContextWithRequestID(ctx, job.RequestID)

	baseName, version := imageref.ParseTag(job.Tag)
	logger := s.logger.With("build_id", job.BuildID, "image_id", job.ImageID, "owner_id", job.OwnerID)
	logger.InfoContext(ctx, "build started", "image_tag", fmt.Sprintf("%s:%s", baseName, version))

	status := buildStatusFailedInternal
	var sizeMB int

	workspaceDir, cleanupWorkspace, err := s.workspaces.Create(job.BuildID)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create build workspace", "error", err)
		s.writeSystemBuildLog(ctx, logger, job.BuildID, job.LogObjectKey, cfg.MaxBuildLogSizeBytes, buildLogInternal)
	} else {
		defer cleanupWorkspace()

		// Распаковываем архив пользователя
		if err := s.extractor.Extract(archivePath, workspaceDir, cfg.MaxUnpackedSizeBytes); err != nil {
			logger.ErrorContext(ctx, "failed to extract build archive", "error", err)
			if isResourceExhaustedError(err) {
				status = buildStatusFailedResourceExhausted
				s.writeSystemBuildLog(ctx, logger, job.BuildID, job.LogObjectKey, cfg.MaxBuildLogSizeBytes, buildLogResourceExhausted)
			} else {
				s.writeSystemBuildLog(ctx, logger, job.BuildID, job.LogObjectKey, cfg.MaxBuildLogSizeBytes, buildLogInternal)
			}
		} else {
			cleanContextDir, err := gitsource.CleanRelativePath(job.ContextDir)
			if err != nil {
				logger.ErrorContext(ctx, "invalid build context path in queue message", "error", err)
				s.writeSystemBuildLog(ctx, logger, job.BuildID, job.LogObjectKey, cfg.MaxBuildLogSizeBytes, buildLogInternal)
			} else {
				dfPath, err := cleanDockerfilePath(job.Dockerfile)
				if err != nil {
					logger.ErrorContext(ctx, "invalid dockerfile path in queue message", "error", err)
					s.writeSystemBuildLog(ctx, logger, job.BuildID, job.LogObjectKey, cfg.MaxBuildLogSizeBytes, buildLogInternal)
				} else {
					destinationTag := job.Claim.Destination

					params := model.BuildRuntimeSpec{
						RegistryUsername:     job.Claim.RegistryUsername,
						RegistryPassword:     job.Claim.RegistryPassword,
						BuildID:              job.BuildID,
						OwnerID:              job.OwnerID,
						WorkspaceDir:         workspaceDir,    // Путь к папке на хосте/в контейнере билдера
						ContextSubDir:        cleanContextDir, // Относительный путь (если юзер указал подпапку)
						Dockerfile:           dfPath,          // Относительный путь к Dockerfile
						DestinationTag:       destinationTag,
						KanikoImage:          cfg.KanikoImage,
						MemoryBytes:          cfg.BuildMemoryBytes,
						MemorySwapMultiplier: cfg.BuildMemorySwapMultiplier,
						CPUQuota:             cfg.BuildCPUQuota,
						CPUPeriod:            cfg.BuildCPUPeriod,
						PidsLimit:            cfg.BuildPidsLimit,
						BuildArgs:            job.BuildArgs,
						NetworkName:          cfg.BuildNetworkName,
					}

					status = s.runBuildContainer(ctx, logger, cfg, params, destinationTag, job)
				}
			}
		}
	}

	// Once the build context is done, terminal log writes and the final upload
	// use a detached cleanup context so the outcome still reaches the log.
	logCtx := ctx
	if ctx.Err() != nil {
		var cancelLog context.CancelFunc
		logCtx, cancelLog = cleanupContext(ctx)
		defer cancelLog()
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		status = buildStatusFailedTimeout
		s.writeSystemBuildLog(logCtx, logger, job.BuildID, job.LogObjectKey, cfg.MaxBuildLogSizeBytes, buildLogTimeout)
	} else if errors.Is(ctx.Err(), context.Canceled) {
		status = buildStatusCanceled
		s.writeSystemBuildLog(logCtx, logger, job.BuildID, job.LogObjectKey, cfg.MaxBuildLogSizeBytes, buildLogCanceled)
	}

	if uploadErr := s.uploadBuildLog(logCtx, logger, job.BuildID, job.LogObjectKey); uploadErr != nil {
		logger.WarnContext(ctx, "failed to upload final build log", "error", uploadErr)
		if status == buildStatusSuccess {
			status = buildStatusFailedInternal
		}
	}
	if err := s.completeBuild(logging.ContextWithRequestID(context.Background(), job.RequestID), job.BuildID, job.ImageID, status, sizeMB); err != nil {
		logger.ErrorContext(ctx, "failed to complete build record", "status", status, "error", err)
		return err
	}
	if status == buildStatusSuccess {
		logger.InfoContext(ctx, "build completed", "status", status)
	} else {
		logger.WarnContext(ctx, "build failed", "status", status)
	}
	return nil
}

func (s *BuilderService) runBuildContainer(ctx context.Context, logger *slog.Logger, cfg config.BuilderConfig, params model.BuildRuntimeSpec, destinationTag string, job buildJob) model.BuildStatus {
	containerID, logStream, buildErr := s.dockerAPI.RunBuildContainer(ctx, params)
	if buildErr != nil {
		if ctx.Err() == nil {
			logger.ErrorContext(ctx, "failed to start build container", "error", buildErr)
			if isResourceExhaustedError(buildErr) {
				s.writeSystemBuildLog(ctx, logger, job.BuildID, job.LogObjectKey, cfg.MaxBuildLogSizeBytes, buildLogResourceExhausted)
				return buildStatusFailedResourceExhausted
			}
			s.writeSystemBuildLog(ctx, logger, job.BuildID, job.LogObjectKey, cfg.MaxBuildLogSizeBytes, buildLogInternal)
		}
		return buildStatusFailedInternal
	}
	defer func() { _ = logStream.Close() }() // read-only stream; log failures are reported by SaveLogs
	defer func() {
		// Orphan build containers are otherwise only removed at Builder startup.
		cleanupCtx, cancel := cleanupContext(ctx)
		defer cancel()
		if err := s.dockerAPI.CleanBuildContainer(cleanupCtx, containerID); err != nil {
			logger.WarnContext(ctx, "failed to remove build container", "container_id", containerID, "error", err)
		}
	}()

	sanitizedLogs := newBuildLogSanitizer(logStream, buildLogSanitizerOptions{
		MaxBytes:       cfg.MaxBuildLogSizeBytes,
		RegistryURL:    cfg.RegistryURL,
		DestinationTag: destinationTag,
	})

	_, logErr := s.logManager.SaveLogs(job.BuildID, sanitizedLogs, cfg.MaxBuildLogSizeBytes)
	if logErr != nil {
		// Stop Kaniko immediately on any log failure; keep the build context's
		// cancellation reason independent from a local logging failure.
		_ = logStream.Close() // stops Kaniko's output; the log error decides the outcome
		if s.logManager.IsLogSizeLimitExceeded(logErr) {
			logger.WarnContext(ctx, "build log size limit exceeded")
			return buildStatusFailed
		}
		logger.WarnContext(ctx, "failed to save build logs", "error", logErr)
		return buildStatusFailedInternal
	}

	waitErr := s.dockerAPI.WaitForBuild(ctx, containerID)

	switch {
	case waitErr == nil && ctx.Err() == nil:
		return buildStatusSuccess
	case isResourceExhaustedError(waitErr):
		return buildStatusFailedResourceExhausted
	case waitErr != nil:
		return buildStatusFailed
	default:
		return buildStatusFailedInternal
	}
}

// System log messages appended when a build fails or is canceled.
const (
	buildLogInternal          = "Build failed due to an internal platform error."
	buildLogResourceExhausted = "Build failed because server resources were exhausted."
	buildLogCanceled          = "Build was canceled before it completed."
	buildLogTimeout           = "Build failed because it exceeded the maximum build time."
)

// writeSystemBuildLog appends message to the build log and uploads it; failures
// are logged and never change the build outcome.
func (s *BuilderService) writeSystemBuildLog(ctx context.Context, logger *slog.Logger, buildID, logObjectKey string, maxLogSize int64, message string) {
	if err := s.logManager.WriteSystemLog(ctx, buildID, message, maxLogSize); err != nil {
		logger.WarnContext(ctx, "failed to write system build log", "message", message, "error", err)
	}
	if err := s.uploadBuildLog(ctx, logger, buildID, logObjectKey); err != nil {
		logger.WarnContext(ctx, "failed to upload system build log", "message", message, "error", err)
	}
}

func (s *BuilderService) uploadBuildLog(ctx context.Context, logger *slog.Logger, buildID, logObjectKey string) error {
	if logObjectKey == "" {
		return nil
	}
	exists, err := s.logManager.Exists(buildID)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	logPath := s.logManager.LogPath(buildID)
	if err := s.objectStore.UploadFile(ctx, logObjectKey, logPath, "text/plain"); err != nil {
		return err
	}
	logger.DebugContext(ctx, "build log uploaded", "build_id", buildID)
	return nil
}

func cleanDockerfilePath(raw string) (string, error) {
	path, err := gitsource.CleanRelativePath(raw)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "Dockerfile", nil
	}
	return path, nil
}

const (
	buildStatusSuccess                 model.BuildStatus = "success"
	buildStatusCanceled                model.BuildStatus = "canceled"
	buildStatusFailed                  model.BuildStatus = "failed"
	buildStatusFailedTimeout           model.BuildStatus = "failed_timeout"
	buildStatusFailedResourceExhausted model.BuildStatus = "failed_resource_exhausted"
	buildStatusFailedInternal          model.BuildStatus = "failed_internal"
	buildStatusFailedQuotaExceeded     model.BuildStatus = "failed_quota_exceeded"
)

func isTerminalBuildStatus(status model.BuildStatus) bool {
	switch status {
	case buildStatusSuccess,
		buildStatusCanceled,
		buildStatusFailed,
		buildStatusFailedTimeout,
		buildStatusFailedResourceExhausted,
		buildStatusFailedInternal,
		buildStatusFailedQuotaExceeded:
		return true
	default:
		return false
	}
}

func isResourceExhaustedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, apperrors.ErrResourceExhausted) || errors.Is(err, apperrors.ErrHostExhausted) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no space left") ||
		strings.Contains(msg, "cannot allocate memory") ||
		strings.Contains(msg, "out of memory") ||
		strings.Contains(msg, "resource temporarily unavailable")
}
