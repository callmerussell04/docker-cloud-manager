package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

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
		&builderWorkspaceFake{},
		&builderExtractorFake{},
		dockerAPI,
		logManager,
		&builderObjectStoreFake{},
		coreClient,
		testBuilderRuntimeConfig(t),
		nil,
	)

	err := svc.processBuild(context.Background(), func() {}, svc.config.Get(), "archive.zip", testBuildJob(""))
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

func TestBuilderServiceProcessBuildUsesRuntimeConfig(t *testing.T) {
	dockerAPI := &builderDockerFake{logs: io.NopCloser(strings.NewReader(""))}
	logManager := &builderLogFake{}
	cfg := testBuilderRuntimeConfig(t)
	svc := NewBuilderService(
		&builderFileFake{},
		&builderWorkspaceFake{},
		&builderExtractorFake{},
		dockerAPI,
		logManager,
		&builderObjectStoreFake{},
		&builderCoreFake{},
		cfg,
		nil,
	)

	err := svc.processBuild(context.Background(), func() {}, cfg.Get(), "archive.zip", testBuildJob("build-logs/build.log"))
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

func TestBuilderServiceProcessBuildSanitizesKanikoLogsBeforeSaving(t *testing.T) {
	dockerAPI := &builderDockerFake{logs: io.NopCloser(strings.NewReader(strings.Join([]string{
		"INFO[0001] RUN npm install",
		"INFO[0002] Pushing image to registry:5000/owner-id_app:latest",
		"INFO[0003] Pushed registry:5000/owner-id_app@sha256:abc123",
		"npm WARN token=supersecret password=hunter2",
		"build complete",
	}, "\n") + "\n"))}
	logManager := &builderLogFake{}
	cfg := testBuilderRuntimeConfig(t)
	svc := NewBuilderService(
		&builderFileFake{},
		&builderWorkspaceFake{},
		&builderExtractorFake{},
		dockerAPI,
		logManager,
		&builderObjectStoreFake{},
		&builderCoreFake{},
		cfg,
		nil,
	)

	err := svc.processBuild(context.Background(), func() {}, cfg.Get(), "archive.zip", testBuildJob("build-logs/build.log"))
	if err != nil {
		t.Fatalf("processBuild returned error: %v", err)
	}

	if !strings.Contains(logManager.savedLogs, "RUN npm install") || !strings.Contains(logManager.savedLogs, "build complete") {
		t.Fatalf("expected ordinary build output to be preserved, got:\n%s", logManager.savedLogs)
	}
	if !strings.Contains(logManager.savedLogs, publishNoticeLine) {
		t.Fatalf("expected publish notice in sanitized logs, got:\n%s", logManager.savedLogs)
	}
	for _, sensitive := range []string{"registry:5000", "owner-id_app", "sha256:abc123", "supersecret", "hunter2"} {
		if strings.Contains(logManager.savedLogs, sensitive) {
			t.Fatalf("sanitized logs contain sensitive value %q:\n%s", sensitive, logManager.savedLogs)
		}
	}
	if strings.Count(logManager.savedLogs, publishNoticeLine) != 1 {
		t.Fatalf("publish notice count = %d, want 1; logs:\n%s", strings.Count(logManager.savedLogs, publishNoticeLine), logManager.savedLogs)
	}
}

func TestBuilderServiceProcessBuildCompletesCanceledAndCleansContainer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dockerAPI := &builderDockerFake{
		logs:        io.NopCloser(strings.NewReader("log\n")),
		waitUsesCtx: true,
	}
	logManager := &builderLogFake{onSave: cancel}
	coreClient := &builderCoreFake{}
	cfg := testBuilderRuntimeConfig(t)
	svc := NewBuilderService(
		&builderFileFake{},
		&builderWorkspaceFake{},
		&builderExtractorFake{},
		dockerAPI,
		logManager,
		&builderObjectStoreFake{},
		coreClient,
		cfg,
		nil,
	)

	err := svc.processBuild(ctx, cancel, cfg.Get(), "archive.zip", testBuildJob("build-logs/build.log"))
	if err != nil {
		t.Fatalf("processBuild returned error: %v", err)
	}
	if coreClient.status != buildStatusCanceled {
		t.Fatalf("completed status = %q, want %q", coreClient.status, buildStatusCanceled)
	}
	if dockerAPI.cleanCalls == 0 {
		t.Fatal("CleanBuildContainer was not called for canceled build")
	}
}

func TestBuilderServiceHandleBuildMessageSkipsTerminalBuild(t *testing.T) {
	objectStore := &builderObjectStoreFake{}
	coreClient := &builderCoreFake{startStarted: false, startStatus: buildStatusSuccess}
	svc := NewBuilderService(
		&builderFileFake{},
		&builderWorkspaceFake{},
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
	if len(objectStore.deletes) != 1 || objectStore.deletes[0] != "build-archives/archive.zip" {
		t.Fatalf("deleted objects = %v, want skipped archive deleted", objectStore.deletes)
	}
}

func TestBuilderServiceHandleBuildMessageSkipsRunningDuplicateWithoutDeletingArchive(t *testing.T) {
	objectStore := &builderObjectStoreFake{}
	coreClient := &builderCoreFake{startStarted: false, startStatus: buildStatusRunning}
	dockerAPI := &builderDockerFake{}
	svc := NewBuilderService(
		&builderFileFake{},
		&builderWorkspaceFake{},
		&builderExtractorFake{},
		dockerAPI,
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
	if len(objectStore.deletes) != 0 {
		t.Fatalf("deleted objects = %v, want none for duplicate running delivery", objectStore.deletes)
	}
	if dockerAPI.runCalls != 0 {
		t.Fatalf("run calls = %d, want 0", dockerAPI.runCalls)
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
		MaxUnpackedSizeBytes:      500 << 20,
		MaxBuildLogSizeBytes:      5 << 20,
		BuildCancelPollInterval:   2 * time.Second,
	}, nil, nil)
}

func testBuildJob(logObjectKey string) buildJob {
	return buildJob{
		BuildID:      "build-id",
		ImageID:      "image-id",
		OwnerID:      "owner-id",
		Tag:          "app:latest",
		ContextDir:   ".",
		Dockerfile:   "Dockerfile",
		RequestID:    "request-id",
		LogObjectKey: logObjectKey,
	}
}

type builderFileFake struct {
	savePath string
	cleaned  []string
}

func (f *builderFileFake) ValidateArchive(filePath, contextDir, dockerfile string) error { return nil }
func (f *builderFileFake) CleanUp(filePath string) error {
	f.cleaned = append(f.cleaned, filePath)
	return nil
}

type builderWorkspaceFake struct{}

func (f *builderWorkspaceFake) ArchivePath(buildID, objectKey string) string {
	return buildID + ".zip"
}

func (f *builderWorkspaceFake) Create(buildID string) (string, func(), error) {
	return "workspace", func() {}, nil
}

type builderExtractorFake struct{}

func (f *builderExtractorFake) Extract(archivePath string, destDir string, maxUnpackedSize int64) error {
	return nil
}

type builderDockerFake struct {
	logs        io.ReadCloser
	cleanCalls  int
	runCalls    int
	params      model.BuildRuntimeSpec
	waitUsesCtx bool
}

func (f *builderDockerFake) RunBuildContainer(ctx context.Context, params model.BuildRuntimeSpec) (string, io.ReadCloser, error) {
	f.runCalls++
	f.params = params
	return "container-id", f.logs, nil
}
func (f *builderDockerFake) WaitForBuild(ctx context.Context, containerID string) error {
	if f.waitUsesCtx {
		return ctx.Err()
	}
	return nil
}
func (f *builderDockerFake) CleanBuildContainer(ctx context.Context, containerID string) error {
	f.cleanCalls++
	return nil
}

type builderLogFake struct {
	saveErr    error
	maxLogSize int64
	onSave     func()
	savedLogs  string
}

func (f *builderLogFake) SaveLogs(logID string, dockerStream io.Reader, maxLogSize int64) (string, error) {
	f.maxLogSize = maxLogSize
	data, _ := io.ReadAll(dockerStream)
	f.savedLogs = string(data)
	if f.onSave != nil {
		f.onSave()
	}
	return "", f.saveErr
}
func (f *builderLogFake) WriteSystemLog(logID string, message string) error { return nil }
func (f *builderLogFake) IsLogSizeLimitExceeded(err error) bool {
	return errors.Is(err, errBuilderLogLimit)
}
func (f *builderLogFake) LogPath(logID string) string { return "" }
func (f *builderLogFake) Exists(logID string) (bool, error) {
	return true, nil
}

type builderObjectStoreFake struct {
	uploads   []string
	downloads []string
	deletes   []string
}

func (f *builderObjectStoreFake) UploadFile(ctx context.Context, objectKey, filePath, contentType string) error {
	f.uploads = append(f.uploads, objectKey)
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

type builderCoreFake struct {
	status       string
	startStarted bool
	startStatus  string
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
func (f *builderCoreFake) GetBuildStatus(ctx context.Context, buildID string) (string, error) {
	return "running", nil
}
