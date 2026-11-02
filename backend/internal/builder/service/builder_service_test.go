package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/storage"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	builderservicemocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/builder/service"
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
		CompleteBuildRecord(mock.Anything, msg.BuildID, "image-from-core", model.BuildStatus("success"), 0).
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

func TestBuilderServiceResumesFinalizationAfterRestart(t *testing.T) {
	h := newBuilderHarness(t, testBuilderRuntimeConfig())
	msg := testBuildMessage()
	expectStarted(h, msg, msg.ImageID)
	expectBuildPrepared(h, msg)
	expectDockerRunWithLogs(h, "log\n")
	h.logs.EXPECT().SaveLogs(msg.BuildID, mock.Anything, int64(5<<20)).Return("/tmp/build.log", nil).Once()
	h.docker.EXPECT().WaitForBuild(mock.Anything, "container-id").Return(nil).Once()
	h.docker.EXPECT().CleanBuildContainer(mock.Anything, "container-id").Return(nil).Once()
	expectLogUpload(h, msg, 1)
	h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("success"), 0).Return(apperrors.ErrUnavailable).Once()
	require.ErrorIs(t, h.service.HandleBuildMessage(context.Background(), msg), apperrors.ErrUnavailable)
	h.objects.AssertNotCalled(t, "DeleteObject", mock.Anything, msg.ArchiveObjectKey)
	h.logs.AssertNotCalled(t, "CleanUp", msg.BuildID)
	var err error
	h.results, err = storage.NewResultStore(h.resultDir)
	require.NoError(t, err)
	h.service = newBuilderService(t, h.deps())
	h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("success"), 0).Return(nil).Once()
	h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil).Once()
	require.NoError(t, h.service.HandleBuildMessage(context.Background(), msg))
	h.docker.AssertNumberOfCalls(t, "RunBuildContainer", 1)
}

func TestBuilderServiceHandleBuildMessageKeepsRunningDuplicatesRetryable(t *testing.T) {
	for _, tt := range []struct {
		name       string
		status     model.BuildStatus
		wantDelete bool
	}{
		{name: "terminal success deletes archive", status: "success", wantDelete: true},
		{name: "running duplicate keeps archive", status: "running", wantDelete: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newBuilderHarness(t, testBuilderRuntimeConfig())
			msg := testBuildMessage()
			h.core.EXPECT().StartBuildRecord(mock.Anything, msg.BuildID).Return(model.BuildClaim{ImageID: "image-id"}, false, tt.status, nil)
			if tt.wantDelete {
				h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)
			}

			err := h.service.HandleBuildMessage(context.Background(), msg)
			if tt.status == "running" {
				require.ErrorIs(t, err, apperrors.ErrUnavailable)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBuilderServiceRefusesBuildWhenNetworkAdmissionFails(t *testing.T) {
	for _, tt := range []struct {
		name      string
		admission error
	}{
		{
			name:      "invalid network",
			admission: apperrors.Wrap(apperrors.ErrUnavailable, "build network inspection failed", errors.New("network build_net not found")),
		},
		{
			name:      "unisolated network",
			admission: apperrors.New(apperrors.ErrUnavailable, "build network is not isolated"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newBuilderHarness(t, testBuilderRuntimeConfig())
			network := builderservicemocks.NewBuildNetworkAdmission(t)
			network.EXPECT().ValidateBuildNetwork(mock.Anything, "build_net").Return(tt.admission).Once()
			deps := h.deps()
			deps.NetworkAdmission = network
			h.service = newBuilderService(t, deps)
			msg := testBuildMessage()

			err := h.service.HandleBuildMessage(context.Background(), msg)

			// A retryable refusal leaves the build pending for redelivery
			// rather than claiming it or recording a platform failure.
			require.ErrorIs(t, err, apperrors.ErrUnavailable)
			require.ErrorIs(t, err, tt.admission)
			h.core.AssertNotCalled(t, "StartBuildRecord", mock.Anything, mock.Anything)
			h.docker.AssertNotCalled(t, "RunBuildContainer", mock.Anything, mock.Anything)
			h.core.AssertNotCalled(t, "CompleteBuildRecord", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			_, journaled, loadErr := h.results.Load(context.Background(), msg.BuildID)
			require.NoError(t, loadErr)
			require.False(t, journaled)
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
				h.core.EXPECT().StartBuildRecord(mock.Anything, msg.BuildID).Return(model.BuildClaim{}, false, "", apperrors.ErrUnavailable)
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
				h.logs.EXPECT().WriteSystemLog(mock.Anything, msg.BuildID, "Build failed because the uploaded archive is invalid.", int64(5<<20)).Return(nil)
				expectLogUpload(h, msg, 1)
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("failed"), 0).Return(nil)
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
				h.logs.EXPECT().WriteSystemLog(mock.Anything, msg.BuildID, "Build failed due to an internal platform error.", int64(5<<20)).Return(nil)
				expectLogUpload(h, msg, 2)
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("failed_internal"), 0).Return(nil)
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
				h.logs.EXPECT().WriteSystemLog(mock.Anything, msg.BuildID, "Build failed due to an internal platform error.", int64(5<<20)).Return(nil)
				expectLogUpload(h, msg, 2)
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("failed_internal"), 0).Return(nil)
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
				h.logs.EXPECT().WriteSystemLog(mock.Anything, msg.BuildID, "Build failed due to an internal platform error.", int64(5<<20)).Return(nil)
				expectLogUpload(h, msg, 2)
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("failed_internal"), 0).Return(nil)
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
				h.logs.EXPECT().WriteSystemLog(mock.Anything, msg.BuildID, "Build failed due to an internal platform error.", int64(5<<20)).Return(nil)
				expectLogUpload(h, msg, 2)
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("failed_internal"), 0).Return(nil)
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
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("failed"), 0).Return(nil)
				h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)
				return msg
			},
			wantStatus: "failed",
		},
		{
			name: "log limit completes failed and stops container",
			configure: func(h *builderHarness, msg buildqueue.ImageBuildMessage) buildqueue.ImageBuildMessage {
				expectStarted(h, msg, msg.ImageID)
				expectBuildPrepared(h, msg)
				expectDockerRunWithLogs(h, "log\n")
				h.logs.EXPECT().SaveLogs(msg.BuildID, mock.Anything, int64(5<<20)).Return("/tmp/build.log", errLogLimit)
				h.logs.EXPECT().IsLogSizeLimitExceeded(errLogLimit).Return(true).Once()
				h.docker.EXPECT().CleanBuildContainer(mock.Anything, "container-id").Return(nil).Once()
				expectLogUpload(h, msg, 1)
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("failed"), 0).Return(nil)
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
				h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("success"), 0).Return(apperrors.ErrUnavailable)
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

func TestBuilderServiceReportsInvalidArchiveSystemLogWriteFailure(t *testing.T) {
	h := newBuilderHarness(t, testBuilderRuntimeConfig())
	msg := testBuildMessage()
	writeErr := errors.New("disk full")
	expectStarted(h, msg, msg.ImageID)
	expectPreparedArchive(h, msg, apperrors.ErrBadRequest)
	h.logs.EXPECT().WriteSystemLog(mock.Anything, msg.BuildID, "Build failed because the uploaded archive is invalid.", int64(5<<20)).Return(writeErr)
	expectLogUpload(h, msg, 1)
	h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("failed"), 0).Return(nil)
	h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)

	require.NoError(t, h.service.HandleBuildMessage(context.Background(), msg), "the build still completes as failed")

	entries := h.logEntries(t, "failed to write invalid archive build log")
	require.Len(t, entries, 1, "the write failure is reported once")
	require.Equal(t, "WARN", entries[0]["level"])
	require.Equal(t, msg.BuildID, entries[0]["build_id"])
	require.Equal(t, writeErr.Error(), entries[0]["error"])
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
	h.logs.EXPECT().WriteSystemLog(mock.Anything, msg.BuildID, "Build was canceled before it completed.", int64(5<<20)).Return(nil)
	expectLogUpload(h, msg, 2)
	h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("canceled"), 0).Return(nil)
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
	h.logs.EXPECT().WriteSystemLog(mock.Anything, msg.BuildID, "Build failed because it exceeded the maximum build time.", int64(5<<20)).Return(nil)
	expectLogUpload(h, msg, 2)
	h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("failed_timeout"), 0).Return(nil)
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
	h.logs.EXPECT().WriteSystemLog(mock.Anything, msg.BuildID, "Build was canceled before it completed.", int64(5<<20)).Return(nil)
	expectLogUpload(h, msg, 2)
	h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("canceled"), 0).Return(nil)
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
	h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("success"), 0).Return(nil)
	h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- h.service.HandleBuildMessage(context.Background(), msg)
	}()
	<-firstRunning
	require.ErrorIs(t, h.service.HandleBuildMessage(context.Background(), msg), apperrors.ErrUnavailable)
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
	network    *builderservicemocks.BuildNetworkAdmission
	logs       *builderservicemocks.LogManager
	objects    *builderservicemocks.ObjectStorage
	core       *builderservicemocks.CoreClient
	results    *storage.ResultStore
	resultDir  string
	config     *config.RuntimeManager
	service    *service.BuilderService
	// output captures the service's structured log output.
	output bytes.Buffer
}

func newBuilderHarness(t *testing.T, cfg config.BuilderConfig) *builderHarness {
	t.Helper()
	h := &builderHarness{
		files:      builderservicemocks.NewFileManager(t),
		workspaces: builderservicemocks.NewWorkspaceManager(t),
		extractor:  builderservicemocks.NewArchiveExtractor(t),
		docker:     builderservicemocks.NewDockerAPI(t),
		network:    builderservicemocks.NewBuildNetworkAdmission(t),
		logs:       builderservicemocks.NewLogManager(t),
		objects:    builderservicemocks.NewObjectStorage(t),
		core:       builderservicemocks.NewCoreClient(t),
		config:     config.NewRuntimeManager(cfg, nil, nil),
	}
	h.workspaces.EXPECT().CleanUp(mock.Anything, mock.Anything).Return(nil).Maybe()
	h.network.EXPECT().ValidateBuildNetwork(mock.Anything, cfg.BuildNetworkName).Return(nil).Maybe()
	var err error
	h.resultDir = t.TempDir()
	h.results, err = storage.NewResultStore(h.resultDir)
	require.NoError(t, err)
	h.service = newBuilderService(t, h.deps())
	return h
}

func (h *builderHarness) deps() service.BuilderServiceDeps {
	return service.BuilderServiceDeps{
		Files:            h.files,
		Workspaces:       h.workspaces,
		Extractor:        h.extractor,
		Docker:           h.docker,
		NetworkAdmission: h.network,
		Logs:             h.logs,
		ObjectStore:      h.objects,
		Core:             h.core,
		Results:          h.results,
		Config:           h.config,
		Logger:           slog.New(slog.NewJSONHandler(&h.output, nil)),
	}
}

func newBuilderService(t *testing.T, deps service.BuilderServiceDeps) *service.BuilderService {
	t.Helper()
	svc, err := service.NewBuilderService(deps)
	require.NoError(t, err)
	return svc
}

func (h *builderHarness) logEntries(t *testing.T, msg string) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(h.output.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		if entry["msg"] == msg {
			entries = append(entries, entry)
		}
	}
	return entries
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
	h.core.EXPECT().StartBuildRecord(mock.Anything, msg.BuildID).Return(model.BuildClaim{ImageID: imageID, RegistryUsername: msg.BuildID, RegistryPassword: "scoped-secret", Destination: "registry:5000/owner-id_app:latest"}, true, "running", nil).Once()
}

func expectPreparedArchive(h *builderHarness, msg buildqueue.ImageBuildMessage, validateErr error) {
	h.workspaces.EXPECT().ArchivePath(msg.BuildID, msg.ArchiveObjectKey).Return("/tmp/archive.zip")
	h.files.EXPECT().CleanUp("/tmp/archive.zip").Return(nil)
	h.logs.EXPECT().CleanUp(msg.BuildID).Return(nil).Maybe()
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

func TestBuilderServiceBoundsRawNewlineFreeLogs(t *testing.T) {
	cfg := testBuilderRuntimeConfig()
	cfg.MaxBuildLogSizeBytes = 1024
	h := newBuilderHarness(t, cfg)
	msg := testBuildMessage()
	manager, err := storage.NewLogManager(t.TempDir())
	require.NoError(t, err)
	source := &generatedOutput{remaining: 1 << 20}
	expectStarted(h, msg, msg.ImageID)
	expectBuildPrepared(h, msg)
	h.docker.EXPECT().RunBuildContainer(mock.Anything, mock.Anything).Return("container-id", io.NopCloser(source), nil)
	var path string
	h.logs.EXPECT().SaveLogs(msg.BuildID, mock.Anything, int64(1024)).RunAndReturn(func(id string, input io.Reader, limit int64) (string, error) {
		var err error
		path, err = manager.SaveLogs(id, input, limit)
		return path, err
	})
	h.logs.EXPECT().IsLogSizeLimitExceeded(mock.Anything).Return(true).Maybe()
	h.docker.EXPECT().CleanBuildContainer(mock.Anything, "container-id").Return(nil).Maybe()
	h.docker.EXPECT().WaitForBuild(mock.Anything, "container-id").Return(nil).Maybe()
	expectLogUpload(h, msg, 1)
	h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("failed"), 0).Return(nil)
	h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)
	require.NoError(t, h.service.HandleBuildMessage(context.Background(), msg))
	require.LessOrEqual(t, source.read, int64(1025))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.LessOrEqual(t, info.Size(), int64(1024))
}

type generatedOutput struct{ remaining, read int64 }

func (r *generatedOutput) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), r.remaining)
	for i := range p[:n] {
		p[i] = 'x'
	}
	r.remaining -= n
	r.read += n
	return int(n), nil
}

func TestBuilderServiceLogPipelineOutcomes(t *testing.T) {
	for _, tt := range []struct {
		name, input   string
		status        model.BuildStatus
		limit         int64
		failure       error
		uploadFailure bool
		closeFailure  bool
	}{
		{name: "normal", input: "hello\nworld", status: "success", limit: 8192},
		{name: "secrets and references across buffers", input: strings.Repeat("x", 4093) + " token=supersecret password=hunter2 registry:5000/owner-id_app:latest\nAuthorization: Bearer private-token", status: "success", limit: 8192},
		{name: "large line within budget", input: strings.Repeat("x", 70<<10), status: "success", limit: 80 << 10},
		{name: "oversized line with newline", input: strings.Repeat("x", 2048) + "\n", status: "failed", limit: 1024},
		{name: "suppressed output still uses raw budget", input: strings.Repeat("INFO[0001] Pushing registry:5000/owner-id_app:latest\n", 100), status: "failed", limit: 1024},
		{name: "secret crosses budget boundary", input: strings.Repeat("x", 1015) + " token=supersecret", status: "failed", limit: 1024},
		{name: "write failure", input: "hello\n", status: "failed_internal", limit: 8192, failure: io.ErrClosedPipe},
		{name: "short write", input: "hello\n", status: "failed_internal", limit: 8192, failure: io.ErrShortWrite},
		{name: "close failure", input: "hello\n", status: "failed_internal", limit: 8192, failure: errors.New("close build log failed")},
		{name: "upstream read failure", input: "hello\n", status: "failed_internal", limit: 8192, failure: io.ErrUnexpectedEOF},
		{name: "raw overflow and close failure", input: strings.Repeat("x", 2048), status: "failed_internal", limit: 1024, closeFailure: true},
		{name: "saved overflow and close failure", input: "token=x", status: "failed_internal", limit: 8, closeFailure: true},
		{name: "upload failure", input: "hello\n", status: "failed_internal", limit: 8192, uploadFailure: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testBuilderRuntimeConfig()
			cfg.MaxBuildLogSizeBytes = tt.limit
			h := newBuilderHarness(t, cfg)
			msg := testBuildMessage()
			manager, err := storage.NewLogManager(t.TempDir())
			require.NoError(t, err)
			expectStarted(h, msg, msg.ImageID)
			expectBuildPrepared(h, msg)
			h.docker.EXPECT().RunBuildContainer(mock.Anything, mock.Anything).Return("container-id", io.NopCloser(iotest.OneByteReader(strings.NewReader(tt.input))), nil)
			var savedPath string
			h.logs.EXPECT().SaveLogs(msg.BuildID, mock.Anything, tt.limit).RunAndReturn(func(id string, reader io.Reader, limit int64) (string, error) {
				var err error
				if tt.closeFailure {
					savedPath = manager.LogPath(id)
					file, err := os.Create(savedPath)
					if err != nil {
						return "", err
					}
					return savedPath, storage.WriteLogs(&closeFailingLogFile{File: file}, reader, limit)
				}
				savedPath, err = manager.SaveLogs(id, reader, limit)
				if tt.failure != nil {
					return savedPath, tt.failure
				}
				return savedPath, err
			})
			h.logs.EXPECT().IsLogSizeLimitExceeded(mock.Anything).RunAndReturn(manager.IsLogSizeLimitExceeded).Maybe()
			h.docker.EXPECT().CleanBuildContainer(mock.Anything, "container-id").Return(nil).Once()
			if tt.status == "success" || tt.uploadFailure {
				h.docker.EXPECT().WaitForBuild(mock.Anything, "container-id").Return(nil)
			}
			h.logs.EXPECT().Exists(msg.BuildID).Return(true, nil)
			h.logs.EXPECT().LogPath(msg.BuildID).RunAndReturn(func(string) string { return savedPath })
			var uploadErr error
			if tt.uploadFailure {
				uploadErr = apperrors.ErrUnavailable
			}
			h.objects.EXPECT().UploadFile(mock.Anything, msg.LogObjectKey, mock.Anything, "text/plain").Return(uploadErr)
			h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, tt.status, 0).Return(nil)
			h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)
			require.NoError(t, h.service.HandleBuildMessage(context.Background(), msg))
			data, err := os.ReadFile(savedPath)
			require.NoError(t, err)
			require.LessOrEqual(t, int64(len(data)), tt.limit)
			for _, secret := range []string{"supersecret", "hunter2", "registry:5000", "owner-id_app", "private-token"} {
				require.NotContains(t, string(data), secret)
			}
			if tt.name == "normal" {
				require.Equal(t, "hello\nworld", string(data))
			}
			if tt.name == "secrets and references across buffers" {
				require.Contains(t, string(data), "token=[REDACTED]")
				require.Contains(t, string(data), "[internal-image]")
				require.Contains(t, string(data), "Authorization=[REDACTED]")
			}
		})
	}
}

type closeFailingLogFile struct{ *os.File }

func (f *closeFailingLogFile) Close() error {
	return errors.Join(f.File.Close(), errors.New("injected close failure"))
}

func TestBuilderServiceRetainsFinalizationUntilCoreAccepts(t *testing.T) {
	for _, tt := range []struct {
		name                     string
		completionErr, statusErr error
		wantRetry                bool
	}{
		{name: "temporary failure", completionErr: apperrors.ErrUnavailable, wantRetry: true},
		{name: "application conflict", completionErr: apperrors.ErrConflict, wantRetry: true},
		{name: "missing metadata", completionErr: apperrors.ErrNotFound, wantRetry: true},
		{name: "deleted build", completionErr: apperrors.ErrNotFound, statusErr: apperrors.ErrNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newBuilderHarness(t, testBuilderRuntimeConfig())
			msg := testBuildMessage()
			result := model.BuildResult{ImageID: msg.ImageID, Status: "success"}
			require.NoError(t, h.results.Save(context.Background(), msg.BuildID, result))
			h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("success"), 0).Return(tt.completionErr).Once()
			if errors.Is(tt.completionErr, apperrors.ErrNotFound) {
				h.core.EXPECT().GetBuildStatus(mock.Anything, msg.BuildID).Return("running", tt.statusErr).Once()
			}
			if !tt.wantRetry {
				h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil).Once()
				h.workspaces.EXPECT().ArchivePath(msg.BuildID, msg.ArchiveObjectKey).Return("/tmp/archive.zip")
				h.files.EXPECT().CleanUp("/tmp/archive.zip").Return(nil)
				h.logs.EXPECT().CleanUp(msg.BuildID).Return(nil)
			}
			err := h.service.HandleBuildMessage(context.Background(), msg)
			if tt.wantRetry {
				require.ErrorIs(t, err, apperrors.ErrUnavailable)
				require.ErrorIs(t, err, tt.completionErr)
			} else {
				require.NoError(t, err)
			}
			_, found, err := h.results.Load(context.Background(), msg.BuildID)
			require.NoError(t, err)
			require.Equal(t, tt.wantRetry, found)
			h.docker.AssertNotCalled(t, "RunBuildContainer", mock.Anything, mock.Anything)
		})
	}
}

func TestBuilderServiceRetainsOutcomeWhenJournalWriteFails(t *testing.T) {
	h := newBuilderHarness(t, testBuilderRuntimeConfig())
	msg := testBuildMessage()
	results := builderservicemocks.NewBuildResultStore(t)
	deps := h.deps()
	deps.Results = results
	h.service = newBuilderService(t, deps)
	result := model.BuildResult{ImageID: msg.ImageID, Status: "success"}
	results.EXPECT().Load(mock.Anything, msg.BuildID).Return(model.BuildResult{}, false, nil).Once()
	results.EXPECT().Save(mock.Anything, msg.BuildID, result).Return(errors.New("disk full")).Once()
	expectStarted(h, msg, msg.ImageID)
	expectBuildPrepared(h, msg)
	expectDockerRunWithLogs(h, "built\n")
	h.logs.EXPECT().SaveLogs(msg.BuildID, mock.Anything, int64(5<<20)).Return("/tmp/build.log", nil).Once()
	h.docker.EXPECT().WaitForBuild(mock.Anything, "container-id").Return(nil).Once()
	h.docker.EXPECT().CleanBuildContainer(mock.Anything, "container-id").Return(nil).Once()
	expectLogUpload(h, msg, 1)
	require.ErrorContains(t, h.service.HandleBuildMessage(context.Background(), msg), "persist build finalization")
	h.core.AssertNotCalled(t, "CompleteBuildRecord", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	h.logs.AssertNotCalled(t, "CleanUp", msg.BuildID)
	results.EXPECT().Save(mock.Anything, msg.BuildID, result).Return(nil).Once()
	results.EXPECT().Delete(mock.Anything, msg.BuildID).Return(nil).Once()
	h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("success"), 0).Return(nil).Once()
	h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil).Once()
	require.NoError(t, h.service.HandleBuildMessage(context.Background(), msg))
	h.docker.AssertNumberOfCalls(t, "RunBuildContainer", 1)
}

func TestBuilderServiceRetriesCleanupBeforeDiscardingResult(t *testing.T) {
	for _, stage := range []string{"archive object", "archive file", "workspace", "log"} {
		t.Run(stage, func(t *testing.T) {
			h := newBuilderHarness(t, testBuilderRuntimeConfig())
			msg := testBuildMessage()
			result := model.BuildResult{ImageID: msg.ImageID, Status: "success"}
			require.NoError(t, h.results.Save(context.Background(), msg.BuildID, result))
			workspaces := builderservicemocks.NewWorkspaceManager(t)
			deps := h.deps()
			deps.Workspaces = workspaces
			h.service = newBuilderService(t, deps)
			h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("success"), 0).Return(nil).Twice()
			errCleanup := apperrors.ErrForbidden
			switch stage {
			case "archive object":
				h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(errCleanup).Once()
			case "archive file":
				h.files.EXPECT().CleanUp("/tmp/archive.zip").Return(errCleanup).Once()
			case "workspace":
				workspaces.EXPECT().CleanUp(mock.Anything, msg.BuildID).Return(errCleanup).Once()
			case "log":
				h.logs.EXPECT().CleanUp(msg.BuildID).Return(errCleanup).Once()
			}
			h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil)
			workspaces.EXPECT().ArchivePath(msg.BuildID, msg.ArchiveObjectKey).Return("/tmp/archive.zip").Maybe()
			h.files.EXPECT().CleanUp("/tmp/archive.zip").Return(nil).Maybe()
			workspaces.EXPECT().CleanUp(mock.Anything, msg.BuildID).Return(nil).Maybe()
			h.logs.EXPECT().CleanUp(msg.BuildID).Return(nil).Maybe()
			require.ErrorIs(t, h.service.HandleBuildMessage(context.Background(), msg), apperrors.ErrUnavailable)
			_, found, err := h.results.Load(context.Background(), msg.BuildID)
			require.NoError(t, err)
			require.True(t, found)
			require.NoError(t, h.service.HandleBuildMessage(context.Background(), msg))
			_, found, err = h.results.Load(context.Background(), msg.BuildID)
			require.NoError(t, err)
			require.False(t, found)
		})
	}
}

func TestBuilderServiceQueuedBuildReceivesFullExecutionBudget(t *testing.T) {
	cfg := testBuilderRuntimeConfig()
	h := newBuilderHarness(t, cfg)
	msg := testBuildMessage()
	msg.CreatedAt = time.Now().Add(-24 * time.Hour).Unix()
	expectStarted(h, msg, msg.ImageID)
	expectBuildPrepared(h, msg)
	h.docker.EXPECT().RunBuildContainer(mock.Anything, mock.AnythingOfType("model.BuildRuntimeSpec")).
		RunAndReturn(func(ctx context.Context, spec model.BuildRuntimeSpec) (string, io.ReadCloser, error) {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.Greater(t, time.Until(deadline), cfg.MaxBuildTime-5*time.Second)
			require.LessOrEqual(t, time.Until(deadline), cfg.MaxBuildTime)
			return "container-id", io.NopCloser(strings.NewReader("built\n")), nil
		}).Once()
	h.logs.EXPECT().SaveLogs(msg.BuildID, mock.Anything, cfg.MaxBuildLogSizeBytes).Return("/tmp/build.log", nil).Once()
	h.docker.EXPECT().WaitForBuild(mock.Anything, "container-id").Return(nil).Once()
	h.docker.EXPECT().CleanBuildContainer(mock.Anything, "container-id").Return(nil).Once()
	expectLogUpload(h, msg, 1)
	h.core.EXPECT().CompleteBuildRecord(mock.Anything, msg.BuildID, msg.ImageID, model.BuildStatus("success"), 0).Return(nil).Once()
	h.objects.EXPECT().DeleteObject(mock.Anything, msg.ArchiveObjectKey).Return(nil).Once()
	require.NoError(t, h.service.HandleBuildMessage(context.Background(), msg))
}
