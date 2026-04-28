package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	"github.com/google/uuid"
)

type FileManager interface {
	SaveArchive(file *multipart.FileHeader, fileID string, maxArchiveSize int64) (string, error)
	ValidateArchive(filePath string) error
	CleanUp(filePath string) error
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
}

type ObjectStorage interface {
	UploadFile(ctx context.Context, objectKey, filePath, contentType string) error
	DownloadFile(ctx context.Context, objectKey, filePath string) error
	DeleteObject(ctx context.Context, objectKey string) error
	OpenObject(ctx context.Context, objectKey string) (io.ReadCloser, error)
}

type CoreClient interface {
	CreateBuildJob(ctx context.Context, ownerID, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (string, string, error)
	StartBuildRecord(ctx context.Context, buildID string) (string, bool, string, error)
	CompleteBuildRecord(ctx context.Context, buildID, imageID, status string, sizeMB int) error
	CancelBuildRecord(ctx context.Context, buildID string) error
	GetBuildLogObjectKey(ctx context.Context, buildID string) (string, error)
}

type BuilderService struct {
	fileManager  FileManager
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

func (s *BuilderService) InitBuild(ctx context.Context, job model.BuildJob, archive *multipart.FileHeader) (string, error) {
	cfg := s.config.Refresh(ctx)
	if _, err := uuid.Parse(job.OwnerID); err != nil {
		return "", apperrors.ErrUnauthorized
	}
	if err := validation.ImageTag(job.Tag); err != nil {
		return "", fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	if err := validateRelativeBuildPath(job.ContextDir); err != nil {
		return "", fmt.Errorf("%w: invalid build context: %v", apperrors.ErrBadRequest, err)
	}
	if err := validateRelativeBuildPath(job.Dockerfile); err != nil {
		return "", fmt.Errorf("%w: invalid dockerfile path: %v", apperrors.ErrBadRequest, err)
	}

	fileID := uuid.New().String()

	filePath, err := s.fileManager.SaveArchive(archive, fileID, cfg.MaxArchiveSizeBytes)
	if err != nil {
		return "", err
	}
	defer s.fileManager.CleanUp(filePath)

	if err := s.fileManager.ValidateArchive(filePath); err != nil {
		return "", err
	}

	baseName, version := parseImageTag(job.Tag)
	normalizedTag := fmt.Sprintf("%s:%s", baseName, version)
	archiveObjectKey := buildArchiveObjectKey(fileID, filePath)
	logObjectKey := buildLogObjectKey(fileID)

	if err := s.objectStore.UploadFile(ctx, archiveObjectKey, filePath, "application/octet-stream"); err != nil {
		return "", err
	}

	buildID, _, err := s.coreClient.CreateBuildJob(
		ctx,
		job.OwnerID,
		normalizedTag,
		archiveObjectKey,
		logObjectKey,
		job.ContextDir,
		job.Dockerfile,
		job.BuildArgs,
		logging.RequestIDFromContext(ctx),
	)
	if err != nil {
		_ = s.objectStore.DeleteObject(context.Background(), archiveObjectKey)
		return "", err
	}

	return buildID, nil
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

func (s *BuilderService) CancelBuild(ctx context.Context, buildID string) error {
	s.activeMu.Lock()
	cancel, ok := s.active[buildID]
	s.activeMu.Unlock()

	if ok {
		cancel()
	}

	if err := s.coreClient.CancelBuildRecord(ctx, buildID); err != nil {
		return err
	}
	s.logger.InfoContext(ctx, "build cancellation requested", "build_id", buildID)
	return nil
}

func (s *BuilderService) GetLogs(ctx context.Context, buildID string) (io.ReadCloser, error) {
	logObjectKey, err := s.coreClient.GetBuildLogObjectKey(ctx, buildID)
	if err != nil {
		return nil, err
	}
	if logObjectKey == "" {
		return nil, apperrors.ErrNotFound
	}
	return s.objectStore.OpenObject(ctx, logObjectKey)
}

func (s *BuilderService) HandleBuildMessage(ctx context.Context, msg buildqueue.ImageBuildMessage) error {
	ctx = logging.ContextWithRequestID(ctx, msg.RequestID)
	cfg := s.config.Refresh(ctx)
	imageID, started, status, err := s.coreClient.StartBuildRecord(ctx, msg.BuildID)
	if err != nil {
		return err
	}
	if !started {
		s.logger.InfoContext(ctx, "build queue message skipped", "build_id", msg.BuildID, "status", status)
		return nil
	}
	if imageID == "" {
		imageID = msg.ImageID
	}

	archivePath := filepath.Join(cfg.StoragePath, msg.BuildID+archiveObjectExt(msg.ArchiveObjectKey))
	if err := s.objectStore.DownloadFile(ctx, msg.ArchiveObjectKey, archivePath); err != nil {
		return err
	}

	baseName, version := parseImageTag(msg.Tag)
	buildCtx, cancel := context.WithTimeout(context.Background(), cfg.MaxBuildTime)
	buildCtx = logging.ContextWithRequestID(buildCtx, msg.RequestID)

	s.activeMu.Lock()
	s.active[msg.BuildID] = cancel
	s.activeMu.Unlock()

	s.wg.Add(1)
	defer s.wg.Done()

	if err := s.processBuild(buildCtx, cancel, cfg, archivePath, msg.BuildID, imageID, msg.OwnerID, baseName, version, msg.ContextDir, msg.Dockerfile, msg.BuildArgs, msg.RequestID, msg.LogObjectKey); err != nil {
		return err
	}
	if err := s.objectStore.DeleteObject(context.Background(), msg.ArchiveObjectKey); err != nil {
		s.logger.WarnContext(ctx, "failed to delete build archive object", "build_id", msg.BuildID, "error", err)
	}
	return nil
}

func (s *BuilderService) processBuild(ctx context.Context, cancel context.CancelFunc, cfg config.BuilderConfig, archivePath, buildID, imageID, ownerID, baseName, version, contextDir, dockerfile string, buildArgs map[string]string, requestID string, logObjectKey string) error {
	// Очищаем архив после сборки
	defer s.fileManager.CleanUp(archivePath)

	defer cancel()
	ctx = logging.ContextWithRequestID(ctx, requestID)

	defer func() {
		s.activeMu.Lock()
		delete(s.active, buildID)
		s.activeMu.Unlock()
	}()

	logger := s.logger.With("build_id", buildID, "image_id", imageID, "owner_id", ownerID)
	logger.InfoContext(ctx, "build started", "image_tag", fmt.Sprintf("%s:%s", baseName, version))

	status := buildStatusFailedInternal
	var sizeMB int

	if !s.acquireBuildSlot(ctx, cfg.MaxConcurrentBuilds) {
		status = buildStatusFailed
		s.writeCancelledBuildLog(ctx, logger, buildID, logObjectKey)
		if err := s.coreClient.CompleteBuildRecord(logging.ContextWithRequestID(context.Background(), requestID), buildID, imageID, status, sizeMB); err != nil {
			logger.ErrorContext(ctx, "failed to complete build record", "status", status, "error", err)
			return err
		}
		return nil
	}
	defer s.releaseBuildSlot()

	// Создаем рабочую директорию (workspace) для Kaniko
	workspaceDir := filepath.Join(cfg.StoragePath, buildID+"_workspace")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		logger.ErrorContext(ctx, "failed to create build workspace", "error", err)
		s.writeInternalBuildLog(ctx, logger, buildID, logObjectKey)
	} else {
		defer os.RemoveAll(workspaceDir)

		// Распаковываем архив пользователя
		if err := s.extractor.Extract(archivePath, workspaceDir, cfg.MaxUnpackedSizeBytes); err != nil {
			logger.ErrorContext(ctx, "failed to extract build archive", "error", err)
			s.writeInternalBuildLog(ctx, logger, buildID, logObjectKey)
		} else {
			cleanContextDir := ""
			if contextDir != "" && contextDir != "." {
				cleanContextDir = filepath.Clean(contextDir)
			}

			dfPath := "Dockerfile"
			if dockerfile != "" {
				dfPath = filepath.Clean(dockerfile)
			}

			repoName := strings.ToLower(fmt.Sprintf("%s_%s", ownerID, baseName))
			destinationTag := fmt.Sprintf("%s/%s:%s", cfg.RegistryURL, repoName, version)

			params := model.BuildRuntimeSpec{
				BuildID:              buildID,
				OwnerID:              ownerID,
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
				BuildArgs:            buildArgs,
				NetworkName:          cfg.BuildNetworkName,
			}

			// Запускаем контейнер Kaniko
			containerID, logStream, buildErr := s.dockerAPI.RunBuildContainer(ctx, params)
			if buildErr != nil {
				if ctx.Err() == nil {
					logger.ErrorContext(ctx, "failed to start build container", "error", buildErr)
					s.writeInternalBuildLog(ctx, logger, buildID, logObjectKey)
				}
			} else {
				defer logStream.Close()
				defer s.dockerAPI.CleanBuildContainer(context.Background(), containerID)

				// Сохраняем логи. При превышении лимита закрываем stream и убираем контейнер,
				// чтобы не оставить stdcopy goroutine заблокированной на pipe write.
				_, logErr := s.logManager.SaveLogs(buildID, logStream, cfg.MaxBuildLogSizeBytes)
				if uploadErr := s.uploadBuildLog(ctx, logger, buildID, logObjectKey); uploadErr != nil {
					logger.WarnContext(ctx, "failed to upload build log", "error", uploadErr)
				}
				if logErr != nil && s.logManager.IsLogSizeLimitExceeded(logErr) {
					_ = logStream.Close()
					_ = s.dockerAPI.CleanBuildContainer(context.Background(), containerID)
				}

				// Ждем завершения контейнера (успех или ошибка)
				waitErr := s.dockerAPI.WaitForBuild(ctx, containerID)

				if logErr == nil && waitErr == nil && ctx.Err() == nil {
					status = buildStatusSuccess
					sizeMB = 0
				} else if waitErr != nil {
					status = buildStatusFailed
					if logErr != nil {
						logger.WarnContext(ctx, "failed to save build logs", "error", logErr)
					}
				} else if logErr != nil {
					if s.logManager.IsLogSizeLimitExceeded(logErr) {
						status = buildStatusFailed
					} else {
						status = buildStatusFailedInternal
						s.writeInternalBuildLog(ctx, logger, buildID, logObjectKey)
					}
					logger.WarnContext(ctx, "failed to save build logs", "error", logErr)
				}
			}
		}
	}

	if ctx.Err() == context.DeadlineExceeded {
		status = buildStatusFailedTimeout
		if err := s.logManager.WriteSystemLog(buildID, "Build failed because it exceeded the maximum build time."); err != nil {
			logger.WarnContext(ctx, "failed to write timeout build log", "error", err)
		}
		if uploadErr := s.uploadBuildLog(ctx, logger, buildID, logObjectKey); uploadErr != nil {
			logger.WarnContext(ctx, "failed to upload timeout build log", "error", uploadErr)
		}
	} else if ctx.Err() == context.Canceled {
		status = buildStatusFailed
		s.writeCancelledBuildLog(ctx, logger, buildID, logObjectKey)
	}

	if uploadErr := s.uploadBuildLog(ctx, logger, buildID, logObjectKey); uploadErr != nil {
		logger.WarnContext(ctx, "failed to upload final build log", "error", uploadErr)
	}
	if err := s.coreClient.CompleteBuildRecord(logging.ContextWithRequestID(context.Background(), requestID), buildID, imageID, status, sizeMB); err != nil {
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

func (s *BuilderService) writeInternalBuildLog(ctx context.Context, logger *slog.Logger, buildID, logObjectKey string) {
	if err := s.logManager.WriteSystemLog(buildID, "Build failed due to an internal platform error."); err != nil {
		logger.WarnContext(ctx, "failed to write internal build log", "error", err)
	}
	if err := s.uploadBuildLog(ctx, logger, buildID, logObjectKey); err != nil {
		logger.WarnContext(ctx, "failed to upload internal build log", "error", err)
	}
}

func (s *BuilderService) writeCancelledBuildLog(ctx context.Context, logger *slog.Logger, buildID, logObjectKey string) {
	if err := s.logManager.WriteSystemLog(buildID, "Build cancelled because the compose build phase was aborted."); err != nil {
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
	logPath := s.logManager.LogPath(buildID)
	if _, err := os.Stat(logPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := s.objectStore.UploadFile(ctx, logObjectKey, logPath, "text/plain"); err != nil {
		return err
	}
	logger.DebugContext(ctx, "build log uploaded", "build_id", buildID)
	return nil
}

const (
	buildStatusSuccess        = "success"
	buildStatusFailed         = "failed"
	buildStatusFailedTimeout  = "failed_timeout"
	buildStatusFailedInternal = "failed_internal"
)

func parseImageTag(rawTag string) (baseName, version string) {
	parts := strings.SplitN(rawTag, ":", 2)
	if len(parts) == 1 || parts[1] == "" {
		return parts[0], "latest"
	}
	return parts[0], parts[1]
}

func buildArchiveObjectKey(fileID, filePath string) string {
	return "build-archives/" + fileID + archiveObjectExt(filePath)
}

func buildLogObjectKey(fileID string) string {
	return "build-logs/" + fileID + ".log"
}

func archiveObjectExt(path string) string {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".tar.gz") {
		return ".tar.gz"
	}
	ext := filepath.Ext(path)
	if ext == "" {
		return ".archive"
	}
	return ext
}

func validateRelativeBuildPath(path string) error {
	if path == "" || path == "." {
		return nil
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("absolute paths are not allowed")
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "\x00") {
		return fmt.Errorf("parent directory traversal is not allowed")
	}
	return nil
}
