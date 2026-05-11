package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
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
	WriteSystemLog(logID string, message string) error
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
	StartBuildRecord(ctx context.Context, buildID string) (string, bool, string, error)
	CompleteBuildRecord(ctx context.Context, buildID, imageID, status string, sizeMB int) error
	GetBuildStatus(ctx context.Context, buildID string) (string, error)
}

const defaultBuildCancelPollInterval = 2 * time.Second

type buildJob struct {
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

type BuilderService struct {
	fileManager  FileManager
	workspaces   WorkspaceManager
	extractor    ArchiveExtractor
	dockerAPI    DockerAPI
	logManager   LogManager
	objectStore  ObjectStorage
	coreClient   CoreClient
	config       *config.RuntimeManager
	activeMu     sync.Mutex
	active       map[string]context.CancelFunc
	activeBuilds int
	wg           sync.WaitGroup
	logger       *slog.Logger
}

func NewBuilderService(
	fileManager FileManager,
	workspaces WorkspaceManager,
	extractor ArchiveExtractor,
	dockerAPI DockerAPI,
	logManager LogManager,
	objectStore ObjectStorage,
	coreClient CoreClient,
	config *config.RuntimeManager,
	logger *slog.Logger,
) *BuilderService {
	return &BuilderService{
		fileManager: fileManager,
		workspaces:  workspaces,
		extractor:   extractor,
		dockerAPI:   dockerAPI,
		logManager:  logManager,
		objectStore: objectStore,
		coreClient:  coreClient,
		config:      config,
		active:      make(map[string]context.CancelFunc),
		logger:      logging.WithComponent(logger, "builder_service"),
	}
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

func (s *BuilderService) HandleBuildMessage(ctx context.Context, msg buildqueue.ImageBuildMessage) error {
	s.wg.Add(1)
	defer s.wg.Done()

	ctx = logging.ContextWithRequestID(ctx, msg.RequestID)
	cfg := s.config.Refresh(ctx)

	if !s.acquireBuildSlot(ctx, cfg.MaxConcurrentBuilds) {
		return ctx.Err()
	}
	defer s.releaseBuildSlot()

	imageID, started, status, err := s.coreClient.StartBuildRecord(ctx, msg.BuildID)
	if err != nil {
		return err
	}
	if !started {
		s.logger.InfoContext(ctx, "build queue message skipped", "build_id", msg.BuildID, "status", status)
		if isTerminalBuildStatus(status) {
			if deleteErr := s.objectStore.DeleteObject(context.Background(), msg.ArchiveObjectKey); deleteErr != nil {
				s.logger.WarnContext(ctx, "failed to delete skipped build archive object", "build_id", msg.BuildID, "error", deleteErr)
			}
		}
		return nil
	}
	if imageID == "" {
		imageID = msg.ImageID
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
	defer s.fileManager.CleanUp(archivePath)
	defer s.logManager.CleanUp(msg.BuildID)
	if err := s.objectStore.DownloadFile(buildCtx, msg.ArchiveObjectKey, archivePath); err != nil {
		if buildCtx.Err() != nil {
			return s.completeCanceledBuild(msg, imageID, archivePath)
		}
		return err
	}
	if err := s.fileManager.ValidateArchive(archivePath, msg.ContextDir, msg.Dockerfile); err != nil {
		if buildCtx.Err() != nil {
			return s.completeCanceledBuild(msg, imageID, archivePath)
		}
		_ = s.logManager.WriteSystemLog(msg.BuildID, "Build failed because the uploaded archive is invalid.")
		if uploadErr := s.uploadBuildLog(buildCtx, s.logger.With("build_id", msg.BuildID, "image_id", imageID, "owner_id", msg.OwnerID), msg.BuildID, msg.LogObjectKey); uploadErr != nil {
			s.logger.WarnContext(ctx, "failed to upload invalid archive build log", "build_id", msg.BuildID, "error", uploadErr)
		}
		if completeErr := s.coreClient.CompleteBuildRecord(logging.ContextWithRequestID(context.Background(), msg.RequestID), msg.BuildID, imageID, buildStatusFailed, 0); completeErr != nil {
			return completeErr
		}
		if deleteErr := s.objectStore.DeleteObject(context.Background(), msg.ArchiveObjectKey); deleteErr != nil {
			s.logger.WarnContext(ctx, "failed to delete invalid build archive object", "build_id", msg.BuildID, "error", deleteErr)
		}
		return nil
	}

	if err := s.processBuild(buildCtx, cancel, cfg, archivePath, buildJob{
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
	if err := s.objectStore.DeleteObject(context.Background(), msg.ArchiveObjectKey); err != nil {
		s.logger.WarnContext(ctx, "failed to delete build archive object", "build_id", msg.BuildID, "error", err)
	}
	return nil
}

func (s *BuilderService) completeCanceledBuild(msg buildqueue.ImageBuildMessage, imageID, archivePath string) error {
	logger := s.logger.With("build_id", msg.BuildID, "image_id", imageID, "owner_id", msg.OwnerID)
	if archivePath != "" {
		_ = s.fileManager.CleanUp(archivePath)
	}
	s.writeCancelledBuildLog(context.Background(), logger, msg.BuildID, msg.LogObjectKey)
	if err := s.coreClient.CompleteBuildRecord(logging.ContextWithRequestID(context.Background(), msg.RequestID), msg.BuildID, imageID, buildStatusCanceled, 0); err != nil {
		return err
	}
	if err := s.objectStore.DeleteObject(context.Background(), msg.ArchiveObjectKey); err != nil {
		logger.WarnContext(context.Background(), "failed to delete canceled build archive object", "error", err)
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
	// Очищаем архив после сборки
	defer s.fileManager.CleanUp(archivePath)

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
		s.writeInternalBuildLog(ctx, logger, job.BuildID, job.LogObjectKey)
	} else {
		defer cleanupWorkspace()

		// Распаковываем архив пользователя
		if err := s.extractor.Extract(archivePath, workspaceDir, cfg.MaxUnpackedSizeBytes); err != nil {
			logger.ErrorContext(ctx, "failed to extract build archive", "error", err)
			s.writeInternalBuildLog(ctx, logger, job.BuildID, job.LogObjectKey)
		} else {
			cleanContextDir, err := gitsource.CleanRelativePath(job.ContextDir)
			if err != nil {
				logger.ErrorContext(ctx, "invalid build context path in queue message", "error", err)
				s.writeInternalBuildLog(ctx, logger, job.BuildID, job.LogObjectKey)
			} else {
				dfPath, err := cleanDockerfilePath(job.Dockerfile)
				if err != nil {
					logger.ErrorContext(ctx, "invalid dockerfile path in queue message", "error", err)
					s.writeInternalBuildLog(ctx, logger, job.BuildID, job.LogObjectKey)
				} else {
					repoName := imageref.CustomRepositoryNameString(job.OwnerID, baseName)
					destinationTag := fmt.Sprintf("%s/%s:%s", cfg.RegistryURL, repoName, version)

					params := model.BuildRuntimeSpec{
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

	if ctx.Err() == context.DeadlineExceeded {
		status = buildStatusFailedTimeout
		if err := s.logManager.WriteSystemLog(job.BuildID, "Build failed because it exceeded the maximum build time."); err != nil {
			logger.WarnContext(ctx, "failed to write timeout build log", "error", err)
		}
		if uploadErr := s.uploadBuildLog(ctx, logger, job.BuildID, job.LogObjectKey); uploadErr != nil {
			logger.WarnContext(ctx, "failed to upload timeout build log", "error", uploadErr)
		}
	} else if ctx.Err() == context.Canceled {
		status = buildStatusCanceled
		s.writeCancelledBuildLog(ctx, logger, job.BuildID, job.LogObjectKey)
	}

	if uploadErr := s.uploadBuildLog(ctx, logger, job.BuildID, job.LogObjectKey); uploadErr != nil {
		logger.WarnContext(ctx, "failed to upload final build log", "error", uploadErr)
	}
	if err := s.coreClient.CompleteBuildRecord(logging.ContextWithRequestID(context.Background(), job.RequestID), job.BuildID, job.ImageID, status, sizeMB); err != nil {
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

func (s *BuilderService) runBuildContainer(ctx context.Context, logger *slog.Logger, cfg config.BuilderConfig, params model.BuildRuntimeSpec, destinationTag string, job buildJob) string {
	containerID, logStream, buildErr := s.dockerAPI.RunBuildContainer(ctx, params)
	if buildErr != nil {
		if ctx.Err() == nil {
			logger.ErrorContext(ctx, "failed to start build container", "error", buildErr)
			s.writeInternalBuildLog(ctx, logger, job.BuildID, job.LogObjectKey)
		}
		return buildStatusFailedInternal
	}
	defer logStream.Close()
	defer s.dockerAPI.CleanBuildContainer(context.Background(), containerID)

	sanitizedLogs := newBuildLogSanitizer(logStream, buildLogSanitizerOptions{
		RegistryURL:    cfg.RegistryURL,
		DestinationTag: destinationTag,
	})

	_, logErr := s.logManager.SaveLogs(job.BuildID, sanitizedLogs, cfg.MaxBuildLogSizeBytes)
	if logErr != nil && s.logManager.IsLogSizeLimitExceeded(logErr) {
		_ = logStream.Close()
		_ = s.dockerAPI.CleanBuildContainer(context.Background(), containerID)
	}

	waitErr := s.dockerAPI.WaitForBuild(ctx, containerID)

	switch {
	case logErr == nil && waitErr == nil && ctx.Err() == nil:
		return buildStatusSuccess
	case waitErr != nil:
		if logErr != nil {
			logger.WarnContext(ctx, "failed to save build logs", "error", logErr)
		}
		return buildStatusFailed
	case logErr != nil && s.logManager.IsLogSizeLimitExceeded(logErr):
		logger.WarnContext(ctx, "build log size limit exceeded", "error", logErr)
		return buildStatusFailed
	case logErr != nil:
		logger.WarnContext(ctx, "failed to save build logs", "error", logErr)
		s.writeInternalBuildLog(ctx, logger, job.BuildID, job.LogObjectKey)
		return buildStatusFailedInternal
	default:
		return buildStatusFailedInternal
	}
}

func (s *BuilderService) writeInternalBuildLog(ctx context.Context, logger *slog.Logger, buildID, logObjectKey string) {
	if err := s.logManager.WriteSystemLog(buildID, "Build failed due to an internal platform error."); err != nil {
		logger.WarnContext(ctx, "failed to write internal build log", "error", err)
	}
	if err := s.uploadBuildLog(ctx, logger, buildID, logObjectKey); err != nil {
		logger.WarnContext(ctx, "failed to upload internal build log", "error", err)
	}
}

func (s *BuilderService) writeCancelledBuildLog(ctx context.Context, logger *slog.Logger, buildID, logObjectKey string) {
	if err := s.logManager.WriteSystemLog(buildID, "Build was canceled before it completed."); err != nil {
		logger.WarnContext(ctx, "failed to write cancelled build log", "error", err)
	}
	if err := s.uploadBuildLog(ctx, logger, buildID, logObjectKey); err != nil {
		logger.WarnContext(ctx, "failed to upload cancelled build log", "error", err)
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
	buildStatusRunning        = "running"
	buildStatusSuccess        = "success"
	buildStatusCanceled       = "canceled"
	buildStatusFailed         = "failed"
	buildStatusFailedTimeout  = "failed_timeout"
	buildStatusFailedInternal = "failed_internal"
)

func isTerminalBuildStatus(status string) bool {
	switch status {
	case buildStatusSuccess,
		buildStatusCanceled,
		buildStatusFailed,
		buildStatusFailedTimeout,
		buildStatusFailedInternal,
		"failed_quota_exceeded":
		return true
	default:
		return false
	}
}
