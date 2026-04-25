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
	go s.processBuild(filePath, buildID, imageID, job.OwnerID, baseName, version, job.ContextDir, job.Dockerfile, job.BuildArgs, requestID)

	return buildID, nil
}

func (s *BuilderService) processBuild(archivePath, buildID, imageID, ownerID, baseName, version, contextDir, dockerfile string, buildArgs map[string]string, requestID string) {
	s.semaphore <- struct{}{}
	defer func() { <-s.semaphore }()

	// Очищаем архив после сборки
	defer s.fileManager.CleanUp(archivePath)

	ctx, cancel := context.WithTimeout(context.Background(), s.config.MaxBuildTime)
	defer cancel()
	ctx = logging.ContextWithRequestID(ctx, requestID)

	logger := s.logger.With("build_id", buildID, "image_id", imageID, "owner_id", ownerID)
	logger.InfoContext(ctx, "build started", "image_tag", fmt.Sprintf("%s:%s", baseName, version))

	status := "failed"
	var sizeMB int

	// Создаем рабочую директорию (workspace) для Kaniko
	workspaceDir := filepath.Join(s.config.StoragePath, buildID+"_workspace")
	if err := os.MkdirAll(workspaceDir, 0755); err == nil {
		defer os.RemoveAll(workspaceDir)

		// Распаковываем архив пользователя
		if err := s.extractor.Extract(archivePath, workspaceDir); err == nil {

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
			if buildErr == nil {
				defer logStream.Close()
				defer s.dockerAPI.CleanBuildContainer(context.Background(), containerID)

				// Сохраняем логи
				_, logErr := s.logManager.SaveLogs(buildID, logStream)

				// Ждем завершения контейнера (успех или ошибка)
				waitErr := s.dockerAPI.WaitForBuild(ctx, containerID)

				if logErr == nil && waitErr == nil && ctx.Err() == nil {
					status = "success"
					sizeMB = 0
				}
			}
		}
	}

	if ctx.Err() == context.DeadlineExceeded {
		status = "failed_timeout"
	}

	if err := s.coreClient.CompleteBuildRecord(logging.ContextWithRequestID(context.Background(), requestID), buildID, imageID, status, sizeMB); err != nil {
		logger.ErrorContext(ctx, "failed to complete build record", "status", status, "error", err)
		return
	}
	if status == "success" {
		logger.InfoContext(ctx, "build completed", "status", status)
	} else {
		logger.WarnContext(ctx, "build failed", "status", status)
	}
}

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
