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

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	"github.com/google/uuid"
)

type FileManager interface {
	SaveArchive(file *multipart.FileHeader, fileID string) (string, error)
	ValidateArchive(filePath string) error
	CleanUp(filePath string) error
}

type ArchiveExtractor interface {
	Extract(archivePath string, destDir string) error
}

type DockerAPI interface {
	RunBuildContainer(ctx context.Context, params model.BuildRuntimeSpec) (string, io.ReadCloser, error)
	WaitForBuild(ctx context.Context, containerID string) error
	CleanBuildContainer(ctx context.Context, containerID string) error
}

type LogManager interface {
	SaveLogs(logID string, dockerStream io.Reader) (string, error)
	WriteSystemLog(logID string, message string) error
	IsLogSizeLimitExceeded(err error) bool
}

type CoreClient interface {
	InitBuildRecord(ctx context.Context, ownerID, tag, logFilePath string) (string, string, error)
	CompleteBuildRecord(ctx context.Context, buildID, imageID, status string, sizeMB int) error
}

type BuilderService struct {
	fileManager FileManager
	extractor   ArchiveExtractor
	dockerAPI   DockerAPI
	logManager  LogManager
	coreClient  CoreClient
	config      config.BuilderConfig
	semaphore   chan struct{}
	activeMu    sync.Mutex
	active      map[string]context.CancelFunc
	wg          sync.WaitGroup
	logger      *slog.Logger
}

func NewBuilderService(
	fileManager FileManager,
	extractor ArchiveExtractor,
	dockerAPI DockerAPI,
	logManager LogManager,
	coreClient CoreClient,
	config config.BuilderConfig,
	logger *slog.Logger,
) *BuilderService {
	return &BuilderService{
		fileManager: fileManager,
		extractor:   extractor,
		dockerAPI:   dockerAPI,
		logManager:  logManager,
		coreClient:  coreClient,
		config:      config,
		semaphore:   make(chan struct{}, config.MaxConcurrentBuilds),
		active:      make(map[string]context.CancelFunc),
		logger:      logging.WithComponent(logger, "builder_service"),
	}
}

func (s *BuilderService) InitBuild(ctx context.Context, job model.BuildJob, archive *multipart.FileHeader) (string, error) {
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

	filePath, err := s.fileManager.SaveArchive(archive, fileID)
	if err != nil {
		return "", err
	}

	if err := s.fileManager.ValidateArchive(filePath); err != nil {
		_ = s.fileManager.CleanUp(filePath)
		return "", err
	}

	// Парсим тег, переданный пользователем
	baseName, version := parseImageTag(job.Tag)
	normalizedTag := fmt.Sprintf("%s:%s", baseName, version)

	// Отправляем в Core нормализованный тег (с версией)
	buildID, imageID, err := s.coreClient.InitBuildRecord(ctx, job.OwnerID, normalizedTag, "")
	if err != nil {
		_ = s.fileManager.CleanUp(filePath)
		return "", err
	}

	// Передаем распарсенные baseName и version
	requestID := logging.RequestIDFromContext(ctx)
	buildCtx, cancel := context.WithTimeout(context.Background(), s.config.MaxBuildTime)
	s.activeMu.Lock()
	s.active[buildID] = cancel
	s.activeMu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.processBuild(buildCtx, cancel, filePath, buildID, imageID, job.OwnerID, baseName, version, job.ContextDir, job.Dockerfile, job.BuildArgs, requestID)
	}()

	return buildID, nil
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
	if !ok {
		return apperrors.ErrNotFound
	}

	cancel()
	s.logger.InfoContext(ctx, "build cancellation requested", "build_id", buildID)
	return nil
}

func (s *BuilderService) processBuild(ctx context.Context, cancel context.CancelFunc, archivePath, buildID, imageID, ownerID, baseName, version, contextDir, dockerfile string, buildArgs map[string]string, requestID string) {
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

	select {
	case s.semaphore <- struct{}{}:
		defer func() { <-s.semaphore }()
	case <-ctx.Done():
		status = buildStatusFailed
		s.writeCancelledBuildLog(ctx, logger, buildID)
		if err := s.coreClient.CompleteBuildRecord(logging.ContextWithRequestID(context.Background(), requestID), buildID, imageID, status, sizeMB); err != nil {
			logger.ErrorContext(ctx, "failed to complete build record", "status", status, "error", err)
		}
		return
	}

	// Создаем рабочую директорию (workspace) для Kaniko
	workspaceDir := filepath.Join(s.config.StoragePath, buildID+"_workspace")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		logger.ErrorContext(ctx, "failed to create build workspace", "error", err)
		s.writeInternalBuildLog(ctx, logger, buildID)
	} else {
		defer os.RemoveAll(workspaceDir)

		// Распаковываем архив пользователя
		if err := s.extractor.Extract(archivePath, workspaceDir); err != nil {
			logger.ErrorContext(ctx, "failed to extract build archive", "error", err)
			s.writeInternalBuildLog(ctx, logger, buildID)
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
			destinationTag := fmt.Sprintf("%s/%s:%s", s.config.RegistryURL, repoName, version)

			params := model.BuildRuntimeSpec{
				BuildID:        buildID,
				OwnerID:        ownerID,
				WorkspaceDir:   workspaceDir,    // Путь к папке на хосте/в контейнере билдера
				ContextSubDir:  cleanContextDir, // Относительный путь (если юзер указал подпапку)
				Dockerfile:     dfPath,          // Относительный путь к Dockerfile
				DestinationTag: destinationTag,
				MemoryBytes:    s.config.BuildMemoryBytes,
				CPUQuota:       s.config.BuildCPUQuota,
				BuildArgs:      buildArgs,
				NetworkName:    s.config.BuildNetworkName,
			}

			// Запускаем контейнер Kaniko
			containerID, logStream, buildErr := s.dockerAPI.RunBuildContainer(ctx, params)
			if buildErr != nil {
				if ctx.Err() == nil {
					logger.ErrorContext(ctx, "failed to start build container", "error", buildErr)
					s.writeInternalBuildLog(ctx, logger, buildID)
				}
			} else {
				defer logStream.Close()
				defer s.dockerAPI.CleanBuildContainer(context.Background(), containerID)

				// Сохраняем логи. При превышении лимита закрываем stream и убираем контейнер,
				// чтобы не оставить stdcopy goroutine заблокированной на pipe write.
				_, logErr := s.logManager.SaveLogs(buildID, logStream)
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
						s.writeInternalBuildLog(ctx, logger, buildID)
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
	} else if ctx.Err() == context.Canceled {
		status = buildStatusFailed
		s.writeCancelledBuildLog(ctx, logger, buildID)
	}

	if err := s.coreClient.CompleteBuildRecord(logging.ContextWithRequestID(context.Background(), requestID), buildID, imageID, status, sizeMB); err != nil {
		logger.ErrorContext(ctx, "failed to complete build record", "status", status, "error", err)
		return
	}
	if status == buildStatusSuccess {
		logger.InfoContext(ctx, "build completed", "status", status)
	} else {
		logger.WarnContext(ctx, "build failed", "status", status)
	}
}

func (s *BuilderService) writeInternalBuildLog(ctx context.Context, logger *slog.Logger, buildID string) {
	if err := s.logManager.WriteSystemLog(buildID, "Build failed due to an internal platform error."); err != nil {
		logger.WarnContext(ctx, "failed to write internal build log", "error", err)
	}
}

func (s *BuilderService) writeCancelledBuildLog(ctx context.Context, logger *slog.Logger, buildID string) {
	if err := s.logManager.WriteSystemLog(buildID, "Build cancelled because the compose build phase was aborted."); err != nil {
		logger.WarnContext(ctx, "failed to write cancelled build log", "error", err)
	}
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
