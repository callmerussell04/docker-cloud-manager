package service

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
)

func TestBuilderServiceLogLimitCleansBuildContainer(t *testing.T) {
	dockerAPI := &builderDockerFake{logs: io.NopCloser(strings.NewReader("log\n"))}
	logManager := &builderLogFake{saveErr: errBuilderLogLimit}
	coreClient := &builderCoreFake{}
	svc := NewBuilderService(
		&builderFileFake{},
		&builderExtractorFake{},
		dockerAPI,
		logManager,
		coreClient,
		config.BuilderConfig{StoragePath: t.TempDir(), MaxConcurrentBuilds: 1},
		nil,
	)

	svc.processBuild(context.Background(), func() {}, "archive.zip", "build-id", "image-id", "owner-id", "app", "latest", ".", "Dockerfile", nil, "")

	if dockerAPI.cleanCalls == 0 {
		t.Fatal("CleanBuildContainer was not called")
	}
	if coreClient.status != buildStatusFailed {
		t.Fatalf("completed status = %q, want %q", coreClient.status, buildStatusFailed)
	}
}

var errBuilderLogLimit = errors.New("log limit")

type builderFileFake struct{}

func (f *builderFileFake) SaveArchive(file *multipart.FileHeader, fileID string) (string, error) {
	return "", nil
}
func (f *builderFileFake) ValidateArchive(filePath string) error { return nil }
func (f *builderFileFake) CleanUp(filePath string) error         { return nil }

type builderExtractorFake struct{}

func (f *builderExtractorFake) Extract(archivePath string, destDir string) error { return nil }

type builderDockerFake struct {
	logs       io.ReadCloser
	cleanCalls int
}

func (f *builderDockerFake) RunBuildContainer(ctx context.Context, params model.BuildRuntimeSpec) (string, io.ReadCloser, error) {
	return "container-id", f.logs, nil
}
func (f *builderDockerFake) WaitForBuild(ctx context.Context, containerID string) error { return nil }
func (f *builderDockerFake) CleanBuildContainer(ctx context.Context, containerID string) error {
	f.cleanCalls++
	return nil
}

type builderLogFake struct {
	saveErr error
}

func (f *builderLogFake) SaveLogs(logID string, dockerStream io.Reader) (string, error) {
	return "", f.saveErr
}
func (f *builderLogFake) WriteSystemLog(logID string, message string) error { return nil }
func (f *builderLogFake) IsLogSizeLimitExceeded(err error) bool {
	return errors.Is(err, errBuilderLogLimit)
}

type builderCoreFake struct {
	status string
}

func (f *builderCoreFake) InitBuildRecord(ctx context.Context, ownerID, tag, logFilePath string) (string, string, error) {
	return "build-id", "image-id", nil
}
func (f *builderCoreFake) CompleteBuildRecord(ctx context.Context, buildID, imageID, status string, sizeMB int) error {
	f.status = status
	return nil
}
