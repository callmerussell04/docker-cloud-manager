package service

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/domain"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/infrastructure/docker"
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
	RunBuildContainer(ctx context.Context, params docker.BuildContainerParams) (string, io.ReadCloser, error)
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

type BuilderConfig struct {
	BuildMemoryBytes    int64
	BuildCPUQuota       int64
	LogsDirPath         string
	StoragePath         string // Путь для временных файлов
	RegistryURL         string // Адрес локального Registry (напр. registry:5000)
	MaxBuildTime        time.Duration
	MaxConcurrentBuilds int
}

type BuilderService struct {
	fileManager FileManager
	extractor   ArchiveExtractor
	dockerAPI   DockerAPI
	logManager  LogManager
	coreClient  CoreClient
	config      BuilderConfig
	semaphore   chan struct{}
}

func NewBuilderService(
	fileManager FileManager,
	extractor ArchiveExtractor,
	dockerAPI DockerAPI,
	logManager LogManager,
	coreClient CoreClient,
	config BuilderConfig,
) *BuilderService {
	return &BuilderService{
		fileManager: fileManager,
		extractor:   extractor,
		dockerAPI:   dockerAPI,
		logManager:  logManager,
		coreClient:  coreClient,
		config:      config,
		semaphore:   make(chan struct{}, config.MaxConcurrentBuilds),
	}
}

func (s *BuilderService) InitBuild(ctx context.Context, job domain.BuildJob) (string, error) {
	fileID := uuid.New().String()

	filePath, err := s.fileManager.SaveArchive(job.File, fileID)
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
	go s.processBuild(filePath, buildID, imageID, job.OwnerID, baseName, version, job.ContextDir, job.Dockerfile)

	return buildID, nil
}

func (s *BuilderService) processBuild(archivePath, buildID, imageID, ownerID, baseName, version, contextDir, dockerfile string) {
	s.semaphore <- struct{}{}
	defer func() { <-s.semaphore }()

	// Очищаем архив после сборки
	defer s.fileManager.CleanUp(archivePath)

	ctx, cancel := context.WithTimeout(context.Background(), s.config.MaxBuildTime)
	defer cancel()

	status := "failed"
	var sizeMB int

	// Создаем рабочую директорию (workspace) для Kaniko
	workspaceDir := filepath.Join(s.config.StoragePath, buildID+"_workspace")
	if err := os.MkdirAll(workspaceDir, 0755); err == nil {
		defer os.RemoveAll(workspaceDir) // Очищаем workspace после сборки

		// Распаковываем архив пользователя
		if err := s.extractor.Extract(archivePath, workspaceDir); err == nil {

			// 1. Формируем пути внутри контейнера Kaniko
			buildContextPath := "/workspace"
			if contextDir != "" && contextDir != "." {
				// Защита от выхода за пределы директории
				cleanContext := filepath.Clean(contextDir)
				buildContextPath = filepath.Join("/workspace", cleanContext)
			}

			dfPath := "Dockerfile"
			if dockerfile != "" {
				dfPath = filepath.Clean(dockerfile)
			}

			repoName := strings.ToLower(fmt.Sprintf("%s_%s", ownerID, baseName))
			destinationTag := fmt.Sprintf("%s/%s:%s", s.config.RegistryURL, repoName, version)

			params := docker.BuildContainerParams{
				WorkspaceDir:   workspaceDir,
				ContextDir:     buildContextPath,                        // НОВОЕ
				Dockerfile:     filepath.Join(buildContextPath, dfPath), // НОВОЕ
				DestinationTag: destinationTag,
				MemoryBytes:    s.config.BuildMemoryBytes,
				CPUQuota:       s.config.BuildCPUQuota,
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

	_ = s.coreClient.CompleteBuildRecord(context.Background(), buildID, imageID, status, sizeMB)
}

func parseImageTag(rawTag string) (baseName, version string) {
	parts := strings.SplitN(rawTag, ":", 2)
	if len(parts) == 1 || parts[1] == "" {
		return parts[0], "latest"
	}
	return parts[0], parts[1]
}
