package service

import (
	"context"
	"io"
	"mime/multipart"
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

type ArchiveConverter interface {
	ToTarStream(filePath string) (io.ReadCloser, error)
}

type DockerAPI interface {
	BuildImage(ctx context.Context, tarStream io.Reader, params docker.BuildParams) (io.ReadCloser, error)
	InspectImage(ctx context.Context, imageTag string) (int64, error)
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
	MaxBuildTime        time.Duration
	MaxConcurrentBuilds int
}

type BuilderService struct {
	fileManager FileManager
	converter   ArchiveConverter
	dockerAPI   DockerAPI
	logManager  LogManager
	coreClient  CoreClient
	config      BuilderConfig
	semaphore   chan struct{}
}

func NewBuilderService(
	fileManager FileManager,
	converter ArchiveConverter,
	dockerAPI DockerAPI,
	logManager LogManager,
	coreClient CoreClient,
	config BuilderConfig,
) *BuilderService {
	return &BuilderService{
		fileManager: fileManager,
		converter:   converter,
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

	buildID, imageID, err := s.coreClient.InitBuildRecord(ctx, job.OwnerID, job.Tag, "")
	if err != nil {
		_ = s.fileManager.CleanUp(filePath)
		return "", err
	}

	go s.processBuild(filePath, buildID, imageID, job.Tag)

	return buildID, nil
}

func (s *BuilderService) processBuild(archivePath, buildID, imageID, tag string) {
	s.semaphore <- struct{}{}
	defer func() { <-s.semaphore }()
	defer s.fileManager.CleanUp(archivePath)

	ctx, cancel := context.WithTimeout(context.Background(), s.config.MaxBuildTime)
	defer cancel()

	status := "failed"
	var sizeMB int

	tarStream, err := s.converter.ToTarStream(archivePath)
	if err == nil {
		defer tarStream.Close()

		params := docker.BuildParams{
			Tag:         tag,
			MemoryBytes: s.config.BuildMemoryBytes,
			CPUQuota:    s.config.BuildCPUQuota,
		}

		dockerStream, buildErr := s.dockerAPI.BuildImage(ctx, tarStream, params)
		if buildErr == nil {
			defer dockerStream.Close()

			_, logErr := s.logManager.SaveLogs(buildID, dockerStream)
			if logErr == nil && ctx.Err() == nil {
				status = "success"
				sizeBytes, insErr := s.dockerAPI.InspectImage(ctx, tag)
				if insErr == nil {
					sizeMB = int(sizeBytes / (1024 * 1024))
				}
			}
		}
	}

	if ctx.Err() == context.DeadlineExceeded {
		status = "failed_timeout"
	}

	_ = s.coreClient.CompleteBuildRecord(context.Background(), buildID, imageID, status, sizeMB)
}
