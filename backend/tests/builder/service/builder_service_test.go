package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/buildqueue"
	builderservicemocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/builder/service"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const publishNotice = "[SYSTEM] Publishing image to internal registry...\n"

var errLogLimit = errors.New("log limit")

func TestBuilderServiceHandleBuildMessageSuccessUsesRuntimeConfigAndSanitizesLogs(t *testing.T) {
	h := newBuilderHarness(t, testBuilderRuntimeConfig())
	msg := testBuildMessage()
	var params model.BuildRuntimeSpec
	var savedLogs string
	var maxLogSize int64

	expectStarted(h, msg, "image-from-core")
	expectPreparedArchive(h, msg, nil)
	h.workspaces.EXPECT().Create(msg.BuildID).Return("/tmp/workspace", func() {}, nil)
	h.extractor.EXPECT().Extract("/tmp/archive.zip", "/tmp/workspace", int64(500<<20)).Return(nil)
	h.docker.EXPECT().
		RunBuildContainer(mock.Anything, mock.AnythingOfType("model.BuildRuntimeSpec")).
		RunAndReturn(func(ctx context.Context, got model.BuildRuntimeSpec) (string, io.ReadCloser, error) {
			params = got
			logs := strings.Join([]string{
				"INFO[0001] RUN npm install",
				"INFO[0002] Pushing image to registry:5000/owner-id_app:latest",
				"INFO[0003] Pushed registry:5000/owner-id_app@sha256:abc123",
				"npm WARN token=supersecret password=hunter2",
				"build complete",
			}, "\n") + "\n"
			return "container-id", io.NopCloser(strings.NewReader(logs)), nil
		})
	h.logs.EXPECT().
		SaveLogs(msg.BuildID, mock.Anything, int64(5<<20)).
		RunAndReturn(func(logID string, reader io.Reader, gotMaxLogSize int64) (string, error) {
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			savedLogs = string(data)
			maxLogSize = gotMaxLogSize
			return "/tmp/build.log", nil
		})
	h.docker.EXPECT().WaitForBuild(mock.Anything, "container-id").Return(nil)
	h.docker.EXPECT().CleanBuildContainer(mock.Anything, "container-id").Return(nil)
	expectLogUpload(h, msg, 1)
	h.core.EXPECT().
		CompleteBuildRecord(mock.Anything, msg.BuildID, "image-from-core", "success", 0).
		Return(nil)
	h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)

	err := h.service.HandleBuildMessage(context.Background(), msg)
	require.NoError(t, err)
	require.Equal(t, "kaniko:test", params.KanikoImage)
	require.Equal(t, int64(512), params.MemoryBytes)
	require.Equal(t, float64(2), params.MemorySwapMultiplier)
	require.Equal(t, int64(100000), params.CPUQuota)
	require.Equal(t, int64(100000), params.CPUPeriod)
	require.Equal(t, int64(512), params.PidsLimit)
	require.Equal(t, "build_net", params.NetworkName)
	require.Equal(t, "registry:5000/owner-id_app:latest", params.DestinationTag)
	require.Equal(t, int64(5<<20), maxLogSize)
	require.Contains(t, savedLogs, "RUN npm install")
	require.Contains(t, savedLogs, "build complete")
	require.Equal(t, 1, strings.Count(savedLogs, publishNotice))
	for _, sensitive := range []string{"registry:5000", "owner-id_app", "sha256:abc123", "supersecret", "hunter2"} {
		require.NotContains(t, savedLogs, sensitive)
	}
}

func TestBuilderServiceHandleBuildMessageSkipsTerminalAndRunningDuplicates(t *testing.T) {
	for _, tt := range []struct {
		name       string
		status     string
		wantDelete bool
	}{
		{name: "terminal success deletes archive", status: "success", wantDelete: true},
		{name: "running duplicate keeps archive", status: "running", wantDelete: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newBuilderHarness(t, testBuilderRuntimeConfig())
			msg := testBuildMessage()
			h.core.EXPECT().StartBuildRecord(mock.Anything, msg.BuildID).Return("image-id", false, tt.status, nil)
			if tt.wantDelete {
				h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)
			}

			err := h.service.HandleBuildMessage(context.Background(), msg)
			require.NoError(t, err)
		})
	}
}

func TestBuilderServiceHandleBuildMessageFailureScenarios(t *testing.T) {
	for _, tt := range []struct {
		name       string
		configure  func(*builderHarness, buildqueue.ImageBuildMessage) buildqueue.ImageBuildMessage
		wantErr    error
		wantStatus string
	}{
		{
			name: "start build record error",
			configure: func(h *builderHarness, msg buildqueue.ImageBuildMessage) buildqueue.ImageBuildMessage {
				h.core.EXPECT().StartBuildRecord(mock.Anything, msg.BuildID).Return("", false, "", apperrors.ErrUnavailable)
				return msg
			},
			wantErr: apperrors.ErrUnavailable,
		},
		{
			name: "download error",
			configure: func(h *builderHarness, msg buildqueue.ImageBuildMessage) buildqueue.ImageBuildMessage {
				expectStarted(h, msg, msg.ImageID)
				h.workspaces.EXPECT().ArchivePath(msg.BuildID, msg.ArchiveObjectKey).Return("/tmp/archive.zip")
				h.files.EXPECT().CleanUp("/tmp/archive.zip").Return(nil)
				h.logs.EXPECT().CleanUp(msg.BuildID).Return(nil)
				h.objects.EXPECT().DownloadFile(mock.Anything, msg.ArchiveObjectKey, "/tmp/archive.zip").Return(apperrors.ErrUnavailable)
				return msg
			},
			wantErr: apperrors.ErrUnavailable,
		},
		{
			name: "invalid archive completes failed and deletes archive",
			configure: func(h *builderHarness, msg buildqueue.ImageBuildMessage) buildqueue.ImageBuildMessage {
				expectStarted(h, msg, msg.ImageID)
				expectPreparedArchive(h, msg, apperrors.ErrBadRequest)
				h.logs.EXPECT().WriteSystemLog(msg.BuildID, "Build failed because the uploaded archive is invalid.").Return(nil)
				expectLogUpload(h, msg, 1)
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, "failed", 0).Return(nil)
				h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)
				return msg
			},
			wantStatus: "failed",
		},
		{
			name: "workspace create error completes failed internal",
			configure: func(h *builderHarness, msg buildqueue.ImageBuildMessage) buildqueue.ImageBuildMessage {
				expectStarted(h, msg, msg.ImageID)
				expectPreparedArchive(h, msg, nil)
				h.files.EXPECT().CleanUp("/tmp/archive.zip").Return(nil)
				h.workspaces.EXPECT().Create(msg.BuildID).Return("", func() {}, errors.New("workspace"))
				h.logs.EXPECT().WriteSystemLog(msg.BuildID, "Build failed due to an internal platform error.").Return(nil)
				expectLogUpload(h, msg, 2)
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, "failed_internal", 0).Return(nil)
				h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)
				return msg
			},
			wantStatus: "failed_internal",
		},
		{
			name: "extract error completes failed internal",
			configure: func(h *builderHarness, msg buildqueue.ImageBuildMessage) buildqueue.ImageBuildMessage {
				expectStarted(h, msg, msg.ImageID)
				expectPreparedArchive(h, msg, nil)
				h.files.EXPECT().CleanUp("/tmp/archive.zip").Return(nil)
				h.workspaces.EXPECT().Create(msg.BuildID).Return("/tmp/workspace", func() {}, nil)
				h.extractor.EXPECT().Extract("/tmp/archive.zip", "/tmp/workspace", int64(500<<20)).Return(errors.New("extract"))
				h.logs.EXPECT().WriteSystemLog(msg.BuildID, "Build failed due to an internal platform error.").Return(nil)
				expectLogUpload(h, msg, 2)
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, "failed_internal", 0).Return(nil)
				h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)
				return msg
			},
			wantStatus: "failed_internal",
		},
		{
			name: "invalid context completes failed internal",
			configure: func(h *builderHarness, msg buildqueue.ImageBuildMessage) buildqueue.ImageBuildMessage {
				msg.ContextDir = "../outside"
				expectStarted(h, msg, msg.ImageID)
				expectPreparedArchive(h, msg, nil)
				h.files.EXPECT().CleanUp("/tmp/archive.zip").Return(nil)
				h.workspaces.EXPECT().Create(msg.BuildID).Return("/tmp/workspace", func() {}, nil)
				h.extractor.EXPECT().Extract("/tmp/archive.zip", "/tmp/workspace", int64(500<<20)).Return(nil)
				h.logs.EXPECT().WriteSystemLog(msg.BuildID, "Build failed due to an internal platform error.").Return(nil)
				expectLogUpload(h, msg, 2)
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, "failed_internal", 0).Return(nil)
				h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)
				return msg
			},
			wantStatus: "failed_internal",
		},
		{
			name: "docker start error completes failed internal",
			configure: func(h *builderHarness, msg buildqueue.ImageBuildMessage) buildqueue.ImageBuildMessage {
				expectStarted(h, msg, msg.ImageID)
				expectBuildPrepared(h, msg)
				h.docker.EXPECT().RunBuildContainer(mock.Anything, mock.Anything).Return("", nil, errors.New("docker"))
				h.logs.EXPECT().WriteSystemLog(msg.BuildID, "Build failed due to an internal platform error.").Return(nil)
				expectLogUpload(h, msg, 2)
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, "failed_internal", 0).Return(nil)
				h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)
				return msg
			},
			wantStatus: "failed_internal",
		},
		{
			name: "wait error completes failed",
			configure: func(h *builderHarness, msg buildqueue.ImageBuildMessage) buildqueue.ImageBuildMessage {
				expectStarted(h, msg, msg.ImageID)
				expectBuildPrepared(h, msg)
				expectDockerRunWithLogs(h, "log\n")
				h.logs.EXPECT().SaveLogs(msg.BuildID, mock.Anything, int64(5<<20)).Return("/tmp/build.log", nil)
				h.docker.EXPECT().WaitForBuild(mock.Anything, "container-id").Return(errors.New("wait"))
				h.docker.EXPECT().CleanBuildContainer(mock.Anything, "container-id").Return(nil)
				expectLogUpload(h, msg, 1)
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, "failed", 0).Return(nil)
				h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)
				return msg
			},
			wantStatus: "failed",
		},
		{
			name: "log limit completes failed and cleans container twice",
			configure: func(h *builderHarness, msg buildqueue.ImageBuildMessage) buildqueue.ImageBuildMessage {
				expectStarted(h, msg, msg.ImageID)
				expectBuildPrepared(h, msg)
				expectDockerRunWithLogs(h, "log\n")
				h.logs.EXPECT().SaveLogs(msg.BuildID, mock.Anything, int64(5<<20)).Return("/tmp/build.log", errLogLimit)
				h.logs.EXPECT().IsLogSizeLimitExceeded(errLogLimit).Return(true).Twice()
				h.docker.EXPECT().CleanBuildContainer(mock.Anything, "container-id").Return(nil).Twice()
				h.docker.EXPECT().WaitForBuild(mock.Anything, "container-id").Return(nil)
				expectLogUpload(h, msg, 1)
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, "failed", 0).Return(nil)
				h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)
				return msg
			},
			wantStatus: "failed",
		},
		{
			name: "complete error is returned",
			configure: func(h *builderHarness, msg buildqueue.ImageBuildMessage) buildqueue.ImageBuildMessage {
				expectStarted(h, msg, msg.ImageID)
				expectBuildPrepared(h, msg)
				expectDockerRunWithLogs(h, "log\n")
				h.logs.EXPECT().SaveLogs(msg.BuildID, mock.Anything, int64(5<<20)).Return("/tmp/build.log", nil)
				h.docker.EXPECT().WaitForBuild(mock.Anything, "container-id").Return(nil)
				h.docker.EXPECT().CleanBuildContainer(mock.Anything, "container-id").Return(nil)
				expectLogUpload(h, msg, 1)
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, "success", 0).Return(apperrors.ErrUnavailable)
				return msg
			},
			wantErr: apperrors.ErrUnavailable,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newBuilderHarness(t, testBuilderRuntimeConfig())
			msg := testBuildMessage()
			msg = tt.configure(h, msg)

			err := h.service.HandleBuildMessage(context.Background(), msg)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestBuilderServiceHandleBuildMessageCompletesCanceledWhenCoreStatusTurnsTerminal(t *testing.T) {
	cfg := testBuilderRuntimeConfig()
	cfg.BuildCancelPollInterval = time.Millisecond
	h := newBuilderHarness(t, cfg)
	msg := testBuildMessage()

	expectStarted(h, msg, msg.ImageID)
	expectBuildPrepared(h, msg)
	expectDockerRunWithLogs(h, "log\n")
	h.logs.EXPECT().SaveLogs(msg.BuildID, mock.Anything, int64(5<<20)).Return("/tmp/build.log", nil)
	h.core.EXPECT().GetBuildStatus(mock.Anything, msg.BuildID).Return("canceled", nil).Maybe()
	h.docker.EXPECT().
		WaitForBuild(mock.Anything, "container-id").
		RunAndReturn(func(ctx context.Context, containerID string) error {
			<-ctx.Done()
			return ctx.Err()
		})
	h.docker.EXPECT().CleanBuildContainer(mock.Anything, "container-id").Return(nil)
	h.logs.EXPECT().WriteSystemLog(msg.BuildID, "Build was canceled before it completed.").Return(nil)
	expectLogUpload(h, msg, 2)
	h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, "canceled", 0).Return(nil)
	h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)

	err := h.service.HandleBuildMessage(context.Background(), msg)
	require.NoError(t, err)
}

func TestBuilderServiceHandleBuildMessageCompletesTimeout(t *testing.T) {
	cfg := testBuilderRuntimeConfig()
	cfg.MaxBuildTime = time.Millisecond
	cfg.BuildCancelPollInterval = time.Hour
	h := newBuilderHarness(t, cfg)
	msg := testBuildMessage()

	expectStarted(h, msg, msg.ImageID)
	expectBuildPrepared(h, msg)
	expectDockerRunWithLogs(h, "log\n")
	h.logs.EXPECT().SaveLogs(msg.BuildID, mock.Anything, int64(5<<20)).Return("/tmp/build.log", nil)
	h.docker.EXPECT().
		WaitForBuild(mock.Anything, "container-id").
		RunAndReturn(func(ctx context.Context, containerID string) error {
			<-ctx.Done()
			return ctx.Err()
		})
	h.docker.EXPECT().CleanBuildContainer(mock.Anything, "container-id").Return(nil)
	h.logs.EXPECT().WriteSystemLog(msg.BuildID, "Build failed because it exceeded the maximum build time.").Return(nil)
	expectLogUpload(h, msg, 2)
	h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, "failed_timeout", 0).Return(nil)
	h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)

	err := h.service.HandleBuildMessage(context.Background(), msg)
	require.NoError(t, err)
}

func TestBuilderServiceStopCancelsActiveBuildsAndRespectsContext(t *testing.T) {
	cfg := testBuilderRuntimeConfig()
	cfg.BuildCancelPollInterval = time.Hour
	h := newBuilderHarness(t, cfg)
	msg := testBuildMessage()
	started := make(chan struct{})

	expectStarted(h, msg, msg.ImageID)
	expectBuildPrepared(h, msg)
	expectDockerRunWithLogs(h, "log\n")
	h.logs.EXPECT().SaveLogs(msg.BuildID, mock.Anything, int64(5<<20)).Return("/tmp/build.log", nil)
	h.docker.EXPECT().
		WaitForBuild(mock.Anything, "container-id").
		RunAndReturn(func(ctx context.Context, containerID string) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	h.docker.EXPECT().CleanBuildContainer(mock.Anything, "container-id").Return(nil)
	h.logs.EXPECT().WriteSystemLog(msg.BuildID, "Build was canceled before it completed.").Return(nil)
	expectLogUpload(h, msg, 2)
	h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, "canceled", 0).Return(nil)
	h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)

	done := make(chan error, 1)
	go func() {
		done <- h.service.HandleBuildMessage(context.Background(), msg)
	}()
	<-started
	require.ErrorIs(t, h.service.Stop(canceledContext()), context.Canceled)
	require.NoError(t, h.service.Stop(context.Background()))
	require.NoError(t, <-done)
}

func TestBuilderServiceMaxConcurrentBuildsBlocksUntilContextCanceled(t *testing.T) {
	cfg := testBuilderRuntimeConfig()
	cfg.MaxConcurrentBuilds = 1
	cfg.BuildCancelPollInterval = time.Hour
	h := newBuilderHarness(t, cfg)
	msg := testBuildMessage()
	firstRunning := make(chan struct{})
	releaseFirst := make(chan struct{})

	expectStarted(h, msg, msg.ImageID)
	expectBuildPrepared(h, msg)
	expectDockerRunWithLogs(h, "log\n")
	h.logs.EXPECT().SaveLogs(msg.BuildID, mock.Anything, int64(5<<20)).Return("/tmp/build.log", nil)
	h.docker.EXPECT().
		WaitForBuild(mock.Anything, "container-id").
		RunAndReturn(func(ctx context.Context, containerID string) error {
			close(firstRunning)
			<-releaseFirst
			return nil
		})
	h.docker.EXPECT().CleanBuildContainer(mock.Anything, "container-id").Return(nil)
	expectLogUpload(h, msg, 1)
	h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, "success", 0).Return(nil)
	h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- h.service.HandleBuildMessage(context.Background(), msg)
	}()
	<-firstRunning
	secondCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := h.service.HandleBuildMessage(secondCtx, testBuildMessageWithID("second-build"))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	close(releaseFirst)
	require.NoError(t, <-firstDone)
}

type builderHarness struct {
	files      *builderservicemocks.FileManager
	workspaces *builderservicemocks.WorkspaceManager
	extractor  *builderservicemocks.ArchiveExtractor
	docker     *builderservicemocks.DockerAPI
	logs       *builderservicemocks.LogManager
	objects    *builderservicemocks.ObjectStorage
	core       *builderservicemocks.CoreClient
	service    *service.BuilderService
}

func newBuilderHarness(t *testing.T, cfg config.BuilderConfig) *builderHarness {
	t.Helper()
	h := &builderHarness{
		files:      builderservicemocks.NewFileManager(t),
		workspaces: builderservicemocks.NewWorkspaceManager(t),
		extractor:  builderservicemocks.NewArchiveExtractor(t),
		docker:     builderservicemocks.NewDockerAPI(t),
		logs:       builderservicemocks.NewLogManager(t),
		objects:    builderservicemocks.NewObjectStorage(t),
		core:       builderservicemocks.NewCoreClient(t),
	}
	h.service = service.NewBuilderService(
		h.files,
		h.workspaces,
		h.extractor,
		h.docker,
		h.logs,
		h.objects,
		h.core,
		config.NewRuntimeManager(cfg, nil, nil),
		slog.Default(),
	)
	return h
}

func testBuilderRuntimeConfig() config.BuilderConfig {
	return config.BuilderConfig{
		StoragePath:               "/tmp/builder-storage",
		RegistryURL:               "registry:5000",
		BuildNetworkName:          "build_net",
		KanikoImage:               "kaniko:test",
		BuildMemoryBytes:          512,
		BuildMemorySwapMultiplier: 2,
		BuildCPUQuota:             100000,
		BuildCPUPeriod:            100000,
		BuildPidsLimit:            512,
		MaxBuildTime:              time.Minute,
		MaxConcurrentBuilds:       1,
		MaxUnpackedSizeBytes:      500 << 20,
		MaxBuildLogSizeBytes:      5 << 20,
		BuildCancelPollInterval:   time.Hour,
	}
}

func testBuildMessage() buildqueue.ImageBuildMessage {
	return testBuildMessageWithID("build-id")
}

func testBuildMessageWithID(buildID string) buildqueue.ImageBuildMessage {
	return buildqueue.ImageBuildMessage{
		BuildID:          buildID,
		ImageID:          "image-id",
		OwnerID:          "owner-id",
		Tag:              "app:latest",
		ContextDir:       ".",
		Dockerfile:       "Dockerfile",
		RequestID:        "request-id",
		ArchiveObjectKey: "build-archives/archive.zip",
		LogObjectKey:     "build-logs/build.log",
		BuildArgs:        map[string]string{"APP_ENV": "test"},
	}
}

func expectStarted(h *builderHarness, msg buildqueue.ImageBuildMessage, imageID string) {
	h.core.EXPECT().StartBuildRecord(mock.Anything, msg.BuildID).Return(imageID, true, "running", nil)
}

func expectPreparedArchive(h *builderHarness, msg buildqueue.ImageBuildMessage, validateErr error) {
	h.workspaces.EXPECT().ArchivePath(msg.BuildID, msg.ArchiveObjectKey).Return("/tmp/archive.zip")
	h.files.EXPECT().CleanUp("/tmp/archive.zip").Return(nil)
	h.logs.EXPECT().CleanUp(msg.BuildID).Return(nil)
	h.objects.EXPECT().DownloadFile(mock.Anything, msg.ArchiveObjectKey, "/tmp/archive.zip").Return(nil)
	h.files.EXPECT().ValidateArchive("/tmp/archive.zip", msg.ContextDir, msg.Dockerfile).Return(validateErr)
}

func expectBuildPrepared(h *builderHarness, msg buildqueue.ImageBuildMessage) {
	expectPreparedArchive(h, msg, nil)
	h.files.EXPECT().CleanUp("/tmp/archive.zip").Return(nil)
	h.workspaces.EXPECT().Create(msg.BuildID).Return("/tmp/workspace", func() {}, nil)
	h.extractor.EXPECT().Extract("/tmp/archive.zip", "/tmp/workspace", int64(500<<20)).Return(nil)
}

func expectDockerRunWithLogs(h *builderHarness, logs string) {
	h.docker.EXPECT().RunBuildContainer(mock.Anything, mock.AnythingOfType("model.BuildRuntimeSpec")).
		Return("container-id", io.NopCloser(strings.NewReader(logs)), nil)
}

func expectLogUpload(h *builderHarness, msg buildqueue.ImageBuildMessage, times int) {
	h.logs.EXPECT().Exists(msg.BuildID).Return(true, nil).Times(times)
	h.logs.EXPECT().LogPath(msg.BuildID).Return("/tmp/build.log").Times(times)
	h.objects.EXPECT().UploadFile(mock.Anything, msg.LogObjectKey, "/tmp/build.log", "text/plain").Return(nil).Times(times)
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
