package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	"github.com/google/uuid"
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
		&builderObjectStoreFake{},
		coreClient,
		testBuilderRuntimeConfig(t),
		nil,
	)

	err := svc.processBuild(context.Background(), func() {}, svc.config.Get(), "archive.zip", "build-id", "image-id", "owner-id", "app", "latest", ".", "Dockerfile", nil, "", "")
	if err != nil {
		t.Fatalf("processBuild returned error: %v", err)
	}

	if dockerAPI.cleanCalls == 0 {
		t.Fatal("CleanBuildContainer was not called")
	}
	if coreClient.status != buildStatusFailed {
		t.Fatalf("completed status = %q, want %q", coreClient.status, buildStatusFailed)
	}
}

func TestBuilderServiceInitBuildUploadsArchiveAndCreatesJob(t *testing.T) {
	fileManager := &builderFileFake{savePath: "/tmp/upload.zip"}
	objectStore := &builderObjectStoreFake{}
	coreClient := &builderCoreFake{}
	svc := NewBuilderService(
		fileManager,
		&builderExtractorFake{},
		&builderDockerFake{},
		&builderLogFake{},
		objectStore,
		coreClient,
		testBuilderRuntimeConfig(t),
		nil,
	)

	buildID, err := svc.InitBuild(context.Background(), model.BuildJob{
		OwnerID: uuid.NewString(),
		Tag:     "demo-app",
	}, "upload.zip", strings.NewReader("archive"))
	if err != nil {
		t.Fatalf("InitBuild returned error: %v", err)
	}
	if buildID != "build-id" {
		t.Fatalf("buildID = %q, want build-id", buildID)
	}
	if len(objectStore.uploads) != 1 {
		t.Fatalf("uploads = %d, want 1", len(objectStore.uploads))
	}
	if !strings.HasPrefix(objectStore.uploads[0], "build-archives/") || !strings.HasSuffix(objectStore.uploads[0], ".zip") {
		t.Fatalf("archive object key = %q", objectStore.uploads[0])
	}
	if coreClient.archiveObjectKey != objectStore.uploads[0] {
		t.Fatalf("core archive key = %q, want %q", coreClient.archiveObjectKey, objectStore.uploads[0])
	}
	if !strings.HasPrefix(coreClient.logObjectKey, "build-logs/") || !strings.HasSuffix(coreClient.logObjectKey, ".log") {
		t.Fatalf("log object key = %q", coreClient.logObjectKey)
	}
}

func TestBuilderServiceProcessBuildUsesRuntimeConfig(t *testing.T) {
	dockerAPI := &builderDockerFake{logs: io.NopCloser(strings.NewReader(""))}
	logManager := &builderLogFake{}
	cfg := testBuilderRuntimeConfig(t)
	svc := NewBuilderService(
		&builderFileFake{},
		&builderExtractorFake{},
		dockerAPI,
		logManager,
		&builderObjectStoreFake{},
		&builderCoreFake{},
		cfg,
		nil,
	)

	err := svc.processBuild(context.Background(), func() {}, cfg.Get(), "archive.zip", "build-id", "image-id", "owner-id", "app", "latest", ".", "Dockerfile", nil, "", "build-logs/build.log")
	if err != nil {
		t.Fatalf("processBuild returned error: %v", err)
	}

	params := dockerAPI.params
	if params.KanikoImage != "kaniko:test" || params.PidsLimit != 512 || params.CPUPeriod != 100000 {
		t.Fatalf("build runtime params not applied: %+v", params)
	}
	if params.MemorySwapMultiplier != 2 || params.NetworkName != "build_net" {
		t.Fatalf("build runtime params not applied: %+v", params)
	}
	if logManager.maxLogSize != (5 << 20) {
		t.Fatalf("max log size = %d", logManager.maxLogSize)
	}
}

func TestBuilderServiceInitBuildDeletesArchiveObjectOnCoreError(t *testing.T) {
	objectStore := &builderObjectStoreFake{}
	coreClient := &builderCoreFake{createErr: errors.New("core unavailable")}
	svc := NewBuilderService(
		&builderFileFake{savePath: "/tmp/upload.tar.gz"},
		&builderExtractorFake{},
		&builderDockerFake{},
		&builderLogFake{},
		objectStore,
		coreClient,
		testBuilderRuntimeConfig(t),
		nil,
	)

	_, err := svc.InitBuild(context.Background(), model.BuildJob{
		OwnerID: uuid.NewString(),
		Tag:     "demo-app:latest",
	}, "upload.tar.gz", strings.NewReader("archive"))
	if err == nil {
		t.Fatal("InitBuild returned nil error")
	}
	if len(objectStore.deletes) != 1 {
		t.Fatalf("deleted objects = %v, want one archive cleanup", objectStore.deletes)
	}
}

func TestBuilderServiceHandleBuildMessageSkipsTerminalBuild(t *testing.T) {
	objectStore := &builderObjectStoreFake{}
	coreClient := &builderCoreFake{startStarted: false, startStatus: buildStatusSuccess}
	svc := NewBuilderService(
		&builderFileFake{},
		&builderExtractorFake{},
		&builderDockerFake{},
		&builderLogFake{},
		objectStore,
		coreClient,
		testBuilderRuntimeConfig(t),
		nil,
	)

	err := svc.HandleBuildMessage(context.Background(), buildqueue.ImageBuildMessage{
		BuildID:          "build-id",
		ImageID:          "image-id",
		OwnerID:          uuid.NewString(),
		Tag:              "demo-app:latest",
		ArchiveObjectKey: "build-archives/archive.zip",
		LogObjectKey:     "build-logs/build.log",
	})
	if err != nil {
		t.Fatalf("HandleBuildMessage returned error: %v", err)
	}
	if len(objectStore.downloads) != 0 {
		t.Fatalf("downloads = %v, want none", objectStore.downloads)
	}
}

var errBuilderLogLimit = errors.New("log limit")

func testBuilderRuntimeConfig(t *testing.T) *config.RuntimeManager {
	t.Helper()
	return config.NewRuntimeManager(config.BuilderConfig{
		StoragePath:               t.TempDir(),
		RegistryURL:               "registry:5000",
		BuildNetworkName:          "build_net",
		KanikoImage:               "kaniko:test",
		BuildMemoryBytes:          512,
		BuildMemorySwapMultiplier: 2,
		BuildCPUQuota:             100000,
		BuildCPUPeriod:            100000,
		BuildPidsLimit:            512,
		MaxConcurrentBuilds:       1,
		MaxArchiveSizeBytes:       50 << 20,
		MaxUnpackedSizeBytes:      500 << 20,
		MaxBuildLogSizeBytes:      5 << 20,
	}, nil, nil)
}

type builderFileFake struct {
	savePath string
	cleaned  []string
}

func (f *builderFileFake) ValidateArchive(filePath string) error { return nil }
func (f *builderFileFake) CleanUp(filePath string) error {
	f.cleaned = append(f.cleaned, filePath)
	return nil
}

type builderExtractorFake struct{}

func (f *builderExtractorFake) Extract(archivePath string, destDir string, maxUnpackedSize int64) error {
	return nil
}

type builderDockerFake struct {
	logs       io.ReadCloser
	cleanCalls int
	params     model.BuildRuntimeSpec
}

func (f *builderDockerFake) RunBuildContainer(ctx context.Context, params model.BuildRuntimeSpec) (string, io.ReadCloser, error) {
	f.params = params
	return "container-id", f.logs, nil
}
func (f *builderDockerFake) WaitForBuild(ctx context.Context, containerID string) error { return nil }
func (f *builderDockerFake) CleanBuildContainer(ctx context.Context, containerID string) error {
	f.cleanCalls++
	return nil
}

type builderLogFake struct {
	saveErr    error
	maxLogSize int64
}

func (f *builderLogFake) SaveLogs(logID string, dockerStream io.Reader, maxLogSize int64) (string, error) {
	f.maxLogSize = maxLogSize
	return "", f.saveErr
}
func (f *builderLogFake) WriteSystemLog(logID string, message string) error { return nil }
func (f *builderLogFake) IsLogSizeLimitExceeded(err error) bool {
	return errors.Is(err, errBuilderLogLimit)
}
func (f *builderLogFake) LogPath(logID string) string { return "" }

type builderObjectStoreFake struct {
	uploads   []string
	downloads []string
	deletes   []string
}

func (f *builderObjectStoreFake) UploadFile(ctx context.Context, objectKey, filePath, contentType string) error {
	f.uploads = append(f.uploads, objectKey)
	return nil
}
func (f *builderObjectStoreFake) UploadStream(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) error {
	f.uploads = append(f.uploads, objectKey)
	_, _ = io.Copy(io.Discard, reader)
	return nil
}
func (f *builderObjectStoreFake) DownloadFile(ctx context.Context, objectKey, filePath string) error {
	f.downloads = append(f.downloads, objectKey)
	return nil
}
func (f *builderObjectStoreFake) DeleteObject(ctx context.Context, objectKey string) error {
	f.deletes = append(f.deletes, objectKey)
	return nil
}
func (f *builderObjectStoreFake) OpenObject(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

type builderCoreFake struct {
	status           string
	createErr        error
	archiveObjectKey string
	logObjectKey     string
	startStarted     bool
	startStatus      string
}

func (f *builderCoreFake) CreateBuildJob(ctx context.Context, ownerID, tag, archiveObjectKey, logObjectKey, contextDir, dockerfile string, buildArgs map[string]string, requestID string) (string, string, error) {
	f.archiveObjectKey = archiveObjectKey
	f.logObjectKey = logObjectKey
	if f.createErr != nil {
		return "", "", f.createErr
	}
	return "build-id", "image-id", nil
}
func (f *builderCoreFake) StartBuildRecord(ctx context.Context, buildID string) (string, bool, string, error) {
	status := f.startStatus
	if status == "" {
		status = "running"
	}
	started := f.startStarted
	if !f.startStarted && f.startStatus == "" {
		started = true
	}
	return "image-id", started, status, nil
}
func (f *builderCoreFake) CompleteBuildRecord(ctx context.Context, buildID, imageID, status string, sizeMB int) error {
	f.status = status
	return nil
}
func (f *builderCoreFake) CancelBuildRecord(ctx context.Context, buildID string) error { return nil }
func (f *builderCoreFake) GetBuildLogObjectKey(ctx context.Context, buildID string) (string, error) {
	return "", nil
}
