package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/telemetry/model"
	telemetryservice "github.com/callmerussell04/docker-cloud-manager/internal/telemetry/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	telemetrymodelmocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/telemetry/model"
	telemetryservicemocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/telemetry/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const containerID = "00000000-0000-0000-0000-000000000001"

func TestStreamLogsAllowsExpectedStatusesAndNormalizesOptions(t *testing.T) {
	statuses := []string{"created", "running", "exited", "error"}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			core := telemetryservicemocks.NewCoreClient(t)
			docker := telemetryservicemocks.NewDockerClient(t)
			svc := telemetryservice.NewTelemetry(core, docker, 2, 2, discardLogger())
			ctx := userCtx()
			cfg := model.RuntimeConfig{MaxLogTailLines: 100, MaxLogStreamsPerUser: 1}
			target := targetWithStatus(status)
			stream := &trackingReadCloser{Reader: strings.NewReader("line\n")}

			cfgCall := core.EXPECT().GetRuntimeConfig(mock.Anything).Return(cfg, nil).Once()
			targetCall := core.EXPECT().GetContainerTarget(mock.Anything, containerID).Return(target, nil).Once()
			ensureCall := docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(nil).Once()
			streamCall := docker.EXPECT().
				StreamLogs(mock.Anything, target, mock.MatchedBy(func(opts model.LogOptions) bool {
					return opts.Tail == 100 && opts.Follow && opts.Timestamps
				})).
				Return(stream, nil).
				Once()
			mock.InOrder(cfgCall, targetCall, ensureCall, streamCall)

			got, err := svc.StreamLogs(ctx, containerID, model.LogOptions{Tail: 500, Follow: true, Timestamps: true})
			require.NoError(t, err)
			require.NotNil(t, got)

			require.NoError(t, got.Close())
			require.True(t, stream.closed)
		})
	}
}

func TestStreamLogsRejectsUnavailableStatuses(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{name: "missing", status: "missing"},
		{name: "unknown", status: "starting"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core := telemetryservicemocks.NewCoreClient(t)
			docker := telemetryservicemocks.NewDockerClient(t)
			svc := telemetryservice.NewTelemetry(core, docker, 2, 2, discardLogger())

			core.EXPECT().GetRuntimeConfig(mock.Anything).Return(model.RuntimeConfig{MaxLogStreamsPerUser: 1}, nil).Once()
			core.EXPECT().GetContainerTarget(mock.Anything, containerID).Return(targetWithStatus(tt.status), nil).Once()

			stream, err := svc.StreamLogs(userCtx(), containerID, model.LogOptions{})
			require.Nil(t, stream)
			require.ErrorIs(t, err, apperrors.ErrConflict)
			docker.AssertNotCalled(t, "EnsureContainerTarget", mock.Anything, mock.Anything)
			docker.AssertNotCalled(t, "StreamLogs", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func TestStreamLogsErrorsAndLimiterRelease(t *testing.T) {
	t.Run("missing scope returns unauthorized after target lookup", func(t *testing.T) {
		core := telemetryservicemocks.NewCoreClient(t)
		docker := telemetryservicemocks.NewDockerClient(t)
		svc := telemetryservice.NewTelemetry(core, docker, 2, 2, discardLogger())

		core.EXPECT().GetRuntimeConfig(mock.Anything).Return(model.RuntimeConfig{MaxLogStreamsPerUser: 1}, nil).Once()
		core.EXPECT().GetContainerTarget(mock.Anything, containerID).Return(targetWithStatus("running"), nil).Once()

		stream, err := svc.StreamLogs(context.Background(), containerID, model.LogOptions{})
		require.Nil(t, stream)
		require.ErrorIs(t, err, apperrors.ErrUnauthorized)
		docker.AssertNotCalled(t, "EnsureContainerTarget", mock.Anything, mock.Anything)
	})

	t.Run("core and docker errors are propagated", func(t *testing.T) {
		tests := []struct {
			name  string
			setup func(core *telemetryservicemocks.CoreClient, docker *telemetryservicemocks.DockerClient)
		}{
			{
				name: "runtime config",
				setup: func(core *telemetryservicemocks.CoreClient, docker *telemetryservicemocks.DockerClient) {
					core.EXPECT().GetRuntimeConfig(mock.Anything).Return(model.RuntimeConfig{}, apperrors.ErrUnavailable).Once()
				},
			},
			{
				name: "target",
				setup: func(core *telemetryservicemocks.CoreClient, docker *telemetryservicemocks.DockerClient) {
					core.EXPECT().GetRuntimeConfig(mock.Anything).Return(model.RuntimeConfig{}, nil).Once()
					core.EXPECT().GetContainerTarget(mock.Anything, containerID).Return(model.ContainerTarget{}, apperrors.ErrNotFound).Once()
				},
			},
			{
				name: "ensure target",
				setup: func(core *telemetryservicemocks.CoreClient, docker *telemetryservicemocks.DockerClient) {
					target := targetWithStatus("running")
					core.EXPECT().GetRuntimeConfig(mock.Anything).Return(model.RuntimeConfig{MaxLogStreamsPerUser: 1}, nil).Once()
					core.EXPECT().GetContainerTarget(mock.Anything, containerID).Return(target, nil).Once()
					docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(apperrors.ErrNotFound).Once()
				},
			},
			{
				name: "stream logs",
				setup: func(core *telemetryservicemocks.CoreClient, docker *telemetryservicemocks.DockerClient) {
					target := targetWithStatus("running")
					core.EXPECT().GetRuntimeConfig(mock.Anything).Return(model.RuntimeConfig{MaxLogStreamsPerUser: 1}, nil).Once()
					core.EXPECT().GetContainerTarget(mock.Anything, containerID).Return(target, nil).Once()
					docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(nil).Once()
					docker.EXPECT().StreamLogs(mock.Anything, target, mock.Anything).Return(nil, apperrors.ErrUnavailable).Once()
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				core := telemetryservicemocks.NewCoreClient(t)
				docker := telemetryservicemocks.NewDockerClient(t)
				svc := telemetryservice.NewTelemetry(core, docker, 2, 2, discardLogger())
				tt.setup(core, docker)

				stream, err := svc.StreamLogs(userCtx(), containerID, model.LogOptions{})
				require.Nil(t, stream)
				require.Error(t, err)
			})
		}
	})

	t.Run("limit is released on stream close", func(t *testing.T) {
		core := telemetryservicemocks.NewCoreClient(t)
		docker := telemetryservicemocks.NewDockerClient(t)
		svc := telemetryservice.NewTelemetry(core, docker, 1, 2, discardLogger())
		target := targetWithStatus("running")
		cfg := model.RuntimeConfig{MaxLogStreamsPerUser: 1}

		core.EXPECT().GetRuntimeConfig(mock.Anything).Return(cfg, nil).Times(3)
		core.EXPECT().GetContainerTarget(mock.Anything, containerID).Return(target, nil).Times(3)
		docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(nil).Twice()
		docker.EXPECT().
			StreamLogs(mock.Anything, target, mock.Anything).
			RunAndReturn(func(context.Context, model.ContainerTarget, model.LogOptions) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader("log\n")), nil
			}).
			Twice()

		first, err := svc.StreamLogs(userCtx(), containerID, model.LogOptions{})
		require.NoError(t, err)
		require.NotNil(t, first)

		second, err := svc.StreamLogs(userCtx(), containerID, model.LogOptions{})
		require.Nil(t, second)
		require.ErrorIs(t, err, apperrors.ErrLimitExceeded)

		require.NoError(t, first.Close())
		third, err := svc.StreamLogs(userCtx(), containerID, model.LogOptions{})
		require.NoError(t, err)
		require.NoError(t, third.Close())
	})
}

func TestOpenTerminalSuccessAndRuntimeConfig(t *testing.T) {
	core := telemetryservicemocks.NewCoreClient(t)
	docker := telemetryservicemocks.NewDockerClient(t)
	session := telemetrymodelmocks.NewTerminalSession(t)
	svc := telemetryservice.NewTelemetry(core, docker, 2, 1, discardLogger())
	target := targetWithStatus("running")
	cfg := runtimeConfig()

	core.EXPECT().GetRuntimeConfig(mock.Anything).Return(cfg, nil).Once()
	core.EXPECT().GetContainerTarget(mock.Anything, containerID).Return(target, nil).Once()
	docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(nil).Once()
	docker.EXPECT().
		OpenTerminal(mock.Anything, target, mock.MatchedBy(func(opts model.TerminalOptions) bool {
			return opts.Rows == 24 && opts.Cols == 80 && assert.ObjectsAreEqual([]string{"/bin/sh", "-i"}, opts.Command)
		})).
		Return(session, nil).
		Once()
	session.EXPECT().Close().Return(nil).Once()

	got, gotCfg, err := svc.OpenTerminal(userCtx(), containerID, model.TerminalOptions{})
	require.NoError(t, err)
	require.Equal(t, cfg, gotCfg)
	require.NoError(t, got.Close())
}

func TestOpenTerminalRejectsInvalidOrUnavailableRequests(t *testing.T) {
	tests := []struct {
		name    string
		opts    model.TerminalOptions
		cfg     model.RuntimeConfig
		status  string
		wantErr error
	}{
		{name: "no configured commands", cfg: model.RuntimeConfig{}, status: "running", wantErr: apperrors.ErrConflict},
		{name: "terminal size too large", cfg: runtimeConfig(), opts: model.TerminalOptions{Rows: 201}, status: "running", wantErr: apperrors.ErrBadRequest},
		{name: "not running", cfg: runtimeConfig(), status: "exited", wantErr: apperrors.ErrConflict},
		{name: "missing scope", cfg: runtimeConfig(), status: "running", wantErr: apperrors.ErrUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core := telemetryservicemocks.NewCoreClient(t)
			docker := telemetryservicemocks.NewDockerClient(t)
			svc := telemetryservice.NewTelemetry(core, docker, 2, 1, discardLogger())
			ctx := userCtx()
			if tt.name == "missing scope" {
				ctx = context.Background()
			}

			core.EXPECT().GetRuntimeConfig(mock.Anything).Return(tt.cfg, nil).Once()
			if !errors.Is(tt.wantErr, apperrors.ErrBadRequest) && tt.name != "no configured commands" {
				core.EXPECT().GetContainerTarget(mock.Anything, containerID).Return(targetWithStatus(tt.status), nil).Once()
			}

			session, cfg, err := svc.OpenTerminal(ctx, containerID, tt.opts)
			require.Nil(t, session)
			require.Empty(t, cfg)
			require.ErrorIs(t, err, tt.wantErr)
			docker.AssertNotCalled(t, "OpenTerminal", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func TestOpenTerminalPropagatesDependencyErrorsAndReleasesLimit(t *testing.T) {
	t.Run("target and docker errors", func(t *testing.T) {
		tests := []struct {
			name  string
			setup func(core *telemetryservicemocks.CoreClient, docker *telemetryservicemocks.DockerClient)
		}{
			{
				name: "runtime config",
				setup: func(core *telemetryservicemocks.CoreClient, docker *telemetryservicemocks.DockerClient) {
					core.EXPECT().GetRuntimeConfig(mock.Anything).Return(model.RuntimeConfig{}, apperrors.ErrUnavailable).Once()
				},
			},
			{
				name: "target",
				setup: func(core *telemetryservicemocks.CoreClient, docker *telemetryservicemocks.DockerClient) {
					core.EXPECT().GetRuntimeConfig(mock.Anything).Return(runtimeConfig(), nil).Once()
					core.EXPECT().GetContainerTarget(mock.Anything, containerID).Return(model.ContainerTarget{}, apperrors.ErrNotFound).Once()
				},
			},
			{
				name: "ensure target",
				setup: func(core *telemetryservicemocks.CoreClient, docker *telemetryservicemocks.DockerClient) {
					target := targetWithStatus("running")
					core.EXPECT().GetRuntimeConfig(mock.Anything).Return(runtimeConfig(), nil).Once()
					core.EXPECT().GetContainerTarget(mock.Anything, containerID).Return(target, nil).Once()
					docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(apperrors.ErrNotFound).Once()
				},
			},
			{
				name: "open terminal",
				setup: func(core *telemetryservicemocks.CoreClient, docker *telemetryservicemocks.DockerClient) {
					target := targetWithStatus("running")
					core.EXPECT().GetRuntimeConfig(mock.Anything).Return(runtimeConfig(), nil).Once()
					core.EXPECT().GetContainerTarget(mock.Anything, containerID).Return(target, nil).Once()
					docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(nil).Once()
					docker.EXPECT().OpenTerminal(mock.Anything, target, mock.Anything).Return(nil, apperrors.ErrUnavailable).Once()
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				core := telemetryservicemocks.NewCoreClient(t)
				docker := telemetryservicemocks.NewDockerClient(t)
				svc := telemetryservice.NewTelemetry(core, docker, 2, 1, discardLogger())
				tt.setup(core, docker)

				session, cfg, err := svc.OpenTerminal(userCtx(), containerID, model.TerminalOptions{})
				require.Nil(t, session)
				require.Empty(t, cfg)
				require.Error(t, err)
			})
		}
	})

	t.Run("limit is released on session close", func(t *testing.T) {
		core := telemetryservicemocks.NewCoreClient(t)
		docker := telemetryservicemocks.NewDockerClient(t)
		firstSession := telemetrymodelmocks.NewTerminalSession(t)
		secondSession := telemetrymodelmocks.NewTerminalSession(t)
		svc := telemetryservice.NewTelemetry(core, docker, 2, 1, discardLogger())
		target := targetWithStatus("running")

		core.EXPECT().GetRuntimeConfig(mock.Anything).Return(runtimeConfig(), nil).Times(3)
		core.EXPECT().GetContainerTarget(mock.Anything, containerID).Return(target, nil).Times(3)
		docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(nil).Twice()
		docker.EXPECT().OpenTerminal(mock.Anything, target, mock.Anything).Return(firstSession, nil).Once()
		docker.EXPECT().OpenTerminal(mock.Anything, target, mock.Anything).Return(secondSession, nil).Once()
		firstSession.EXPECT().Close().Return(nil).Once()
		secondSession.EXPECT().Close().Return(nil).Once()

		first, _, err := svc.OpenTerminal(userCtx(), containerID, model.TerminalOptions{})
		require.NoError(t, err)

		second, _, err := svc.OpenTerminal(userCtx(), containerID, model.TerminalOptions{})
		require.Nil(t, second)
		require.ErrorIs(t, err, apperrors.ErrLimitExceeded)

		require.NoError(t, first.Close())
		third, _, err := svc.OpenTerminal(userCtx(), containerID, model.TerminalOptions{})
		require.NoError(t, err)
		require.NoError(t, third.Close())
	})
}

func TestNormalizeTerminalOptions(t *testing.T) {
	tests := []struct {
		name    string
		cfg     model.RuntimeConfig
		opts    model.TerminalOptions
		want    model.TerminalOptions
		wantErr error
	}{
		{
			name: "default shell command",
			cfg:  runtimeConfig(),
			want: model.TerminalOptions{Rows: 24, Cols: 80, Command: []string{"/bin/sh", "-i"}},
		},
		{
			name: "default non-shell command",
			cfg: model.RuntimeConfig{
				AllowedExecCommands: []string{"/usr/bin/top"},
				MaxCommandArgs:      2,
				MaxCommandArgBytes:  16,
			},
			want: model.TerminalOptions{Rows: 24, Cols: 80, Command: []string{"/usr/bin/top"}},
		},
		{
			name: "allowed explicit command",
			cfg:  runtimeConfig(),
			opts: model.TerminalOptions{Rows: 40, Cols: 120, Command: []string{"/usr/bin/top", "cpu"}},
			want: model.TerminalOptions{Rows: 40, Cols: 120, Command: []string{"/usr/bin/top", "cpu"}},
		},
		{name: "unknown command", cfg: runtimeConfig(), opts: model.TerminalOptions{Command: []string{"/bin/unknown"}}, wantErr: apperrors.ErrForbidden},
		{name: "shell command injection", cfg: runtimeConfig(), opts: model.TerminalOptions{Command: []string{"/bin/sh", "-c"}}, wantErr: apperrors.ErrForbidden},
		{name: "empty arg", cfg: runtimeConfig(), opts: model.TerminalOptions{Command: []string{"/usr/bin/top", ""}}, wantErr: apperrors.ErrBadRequest},
		{name: "newline arg", cfg: runtimeConfig(), opts: model.TerminalOptions{Command: []string{"/usr/bin/top", "bad\narg"}}, wantErr: apperrors.ErrBadRequest},
		{name: "too many args", cfg: runtimeConfig(), opts: model.TerminalOptions{Command: []string{"/usr/bin/top", "a", "b", "c"}}, wantErr: apperrors.ErrBadRequest},
		{name: "arg too large", cfg: runtimeConfig(), opts: model.TerminalOptions{Command: []string{"/usr/bin/top", strings.Repeat("a", 17)}}, wantErr: apperrors.ErrBadRequest},
		{name: "rows too large", cfg: runtimeConfig(), opts: model.TerminalOptions{Rows: 201}, wantErr: apperrors.ErrBadRequest},
		{name: "cols too large", cfg: runtimeConfig(), opts: model.TerminalOptions{Cols: 501}, wantErr: apperrors.ErrBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := telemetryservice.NormalizeTerminalOptions(tt.opts, tt.cfg)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeLogOptionsParseSizeLimiterAndAuditAttrs(t *testing.T) {
	logTests := []struct {
		name string
		opts model.LogOptions
		cfg  model.RuntimeConfig
		want int
	}{
		{name: "default tail", opts: model.LogOptions{}, cfg: model.RuntimeConfig{}, want: 200},
		{name: "cap tail", opts: model.LogOptions{Tail: 500}, cfg: model.RuntimeConfig{MaxLogTailLines: 100}, want: 100},
		{name: "preserve valid tail", opts: model.LogOptions{Tail: 50}, cfg: model.RuntimeConfig{MaxLogTailLines: 100}, want: 50},
	}
	for _, tt := range logTests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, telemetryservice.NormalizeLogOptions(tt.opts, tt.cfg).Tail)
		})
	}

	size, err := telemetryservice.ParseSize("42")
	require.NoError(t, err)
	require.Equal(t, uint(42), size)
	_, err = telemetryservice.ParseSize("bad")
	require.ErrorIs(t, err, apperrors.ErrBadRequest)

	limiter := telemetryservice.NewSessionLimiter(2)
	require.True(t, limiter.Acquire("user", 1))
	require.False(t, limiter.Acquire("user", 1))
	require.True(t, limiter.Acquire("admin", 1))
	require.False(t, limiter.Acquire("other", 1))
	limiter.Release("user")
	require.True(t, limiter.Acquire("other", 1))

	attrs := telemetryservice.AuditAttrs(userCtx(), containerID, "session-id", time.Now().Add(-time.Millisecond), "closed")
	require.Contains(t, attrs, "session_id")
	require.Contains(t, attrs, "session-id")
	require.Contains(t, attrs, "container_id")
	require.Contains(t, attrs, containerID)
	require.Contains(t, attrs, "reason")
	require.Contains(t, attrs, "closed")
}

func runtimeConfig() model.RuntimeConfig {
	return model.RuntimeConfig{
		MaxLogTailLines:            100,
		MaxLogStreamsPerUser:       1,
		MaxTerminalSessionsPerUser: 1,
		TerminalIdleTimeout:        time.Second,
		TerminalMaxDuration:        time.Minute,
		AllowedExecCommands:        []string{"/bin/sh", "/usr/bin/top"},
		MaxCommandArgs:             2,
		MaxCommandArgBytes:         16,
		WSReadLimitBytes:           1024,
	}
}

func targetWithStatus(status string) model.ContainerTarget {
	return model.ContainerTarget{
		ContainerID:      containerID,
		DockerID:         "docker-id",
		Status:           status,
		OwnerID:          uuid.NewString(),
		DockerGeneration: 2,
	}
}

func userCtx() context.Context {
	return accessscope.WithUserScope(context.Background(), uuid.MustParse("11111111-1111-1111-1111-111111111111"), "alice", "user")
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}
