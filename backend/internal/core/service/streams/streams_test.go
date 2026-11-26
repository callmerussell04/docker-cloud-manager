package streams_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/streams"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	coremodelmocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/model"
	streamsmocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service/streams"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var containerID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

func TestNewServiceRejectsMissingDependencies(t *testing.T) {
	valid := func(t *testing.T) streams.Deps {
		return streams.Deps{
			Targets: streamsmocks.NewContainerTargets(t),
			Docker:  streamsmocks.NewDocker(t),
			Config:  coretest.StaticConfig{},
		}
	}
	depstest.RequireRejectsMissing(t, valid, streams.NewService, map[string]string{
		"Targets": "container targets",
		"Docker":  "docker",
		"Config":  "config",
	})
}

func TestStreamLogsAllowsExpectedStatusesAndNormalizesOptions(t *testing.T) {
	statuses := []string{"created", "running", "exited", "error"}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			targets := streamsmocks.NewContainerTargets(t)
			docker := streamsmocks.NewDocker(t)
			svc := newService(t, targets, docker, model.StreamConfig{MaxLogTailLines: 100, MaxLogStreamsPerUser: 1}, 2, 2)
			target := targetWithStatus(status)
			stream := &trackingReadCloser{Reader: strings.NewReader("line\n")}

			targetCall := targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(target, nil).Once()
			ensureCall := docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(nil).Once()
			streamCall := docker.EXPECT().
				StreamContainerLogs(mock.Anything, target.DockerID, mock.MatchedBy(func(opts model.LogOptions) bool {
					return opts.Tail == 100 && opts.Follow && opts.Timestamps
				})).
				Return(stream, nil).
				Once()
			mock.InOrder(targetCall, ensureCall, streamCall)

			got, err := svc.StreamLogs(userCtx(), containerID, model.LogOptions{Tail: 500, Follow: true, Timestamps: true})
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
			targets := streamsmocks.NewContainerTargets(t)
			docker := streamsmocks.NewDocker(t)
			svc := newService(t, targets, docker, model.StreamConfig{MaxLogStreamsPerUser: 1}, 2, 2)

			targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(targetWithStatus(tt.status), nil).Once()

			stream, err := svc.StreamLogs(userCtx(), containerID, model.LogOptions{})
			require.Nil(t, stream)
			require.ErrorIs(t, err, apperrors.ErrConflict)
		})
	}
}

func TestStreamLogsErrorsAndLimiterRelease(t *testing.T) {
	t.Run("missing scope returns unauthorized after target lookup", func(t *testing.T) {
		targets := streamsmocks.NewContainerTargets(t)
		docker := streamsmocks.NewDocker(t)
		svc := newService(t, targets, docker, model.StreamConfig{MaxLogStreamsPerUser: 1}, 2, 2)

		targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(targetWithStatus("running"), nil).Once()

		stream, err := svc.StreamLogs(context.Background(), containerID, model.LogOptions{})
		require.Nil(t, stream)
		require.ErrorIs(t, err, apperrors.ErrUnauthorized)
	})

	t.Run("target and docker errors are propagated", func(t *testing.T) {
		tests := []struct {
			name  string
			setup func(targets *streamsmocks.ContainerTargets, docker *streamsmocks.Docker)
		}{
			{
				name: "target",
				setup: func(targets *streamsmocks.ContainerTargets, docker *streamsmocks.Docker) {
					targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(model.ContainerRuntimeTarget{}, apperrors.ErrNotFound).Once()
				},
			},
			{
				name: "ensure target",
				setup: func(targets *streamsmocks.ContainerTargets, docker *streamsmocks.Docker) {
					target := targetWithStatus("running")
					targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(target, nil).Once()
					docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(apperrors.ErrNotFound).Once()
				},
			},
			{
				name: "stream logs",
				setup: func(targets *streamsmocks.ContainerTargets, docker *streamsmocks.Docker) {
					target := targetWithStatus("running")
					targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(target, nil).Once()
					docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(nil).Once()
					docker.EXPECT().StreamContainerLogs(mock.Anything, target.DockerID, mock.Anything).Return(nil, apperrors.ErrUnavailable).Once()
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				targets := streamsmocks.NewContainerTargets(t)
				docker := streamsmocks.NewDocker(t)
				// One stream per user: a leaked limiter slot would refuse the next test's stream.
				svc := newService(t, targets, docker, model.StreamConfig{MaxLogStreamsPerUser: 1}, 2, 2)
				tt.setup(targets, docker)

				stream, err := svc.StreamLogs(userCtx(), containerID, model.LogOptions{})
				require.Nil(t, stream)
				require.Error(t, err)
			})
		}
	})

	t.Run("limit is released on stream close", func(t *testing.T) {
		targets := streamsmocks.NewContainerTargets(t)
		docker := streamsmocks.NewDocker(t)
		svc := newService(t, targets, docker, model.StreamConfig{MaxLogStreamsPerUser: 1}, 1, 2)
		target := targetWithStatus("running")

		targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(target, nil).Times(3)
		docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(nil).Twice()
		docker.EXPECT().
			StreamContainerLogs(mock.Anything, target.DockerID, mock.Anything).
			RunAndReturn(func(context.Context, string, model.LogOptions) (io.ReadCloser, error) {
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

	t.Run("global limit counts every user", func(t *testing.T) {
		targets := streamsmocks.NewContainerTargets(t)
		docker := streamsmocks.NewDocker(t)
		svc := newService(t, targets, docker, model.StreamConfig{MaxLogStreamsPerUser: 5}, 1, 2)
		target := targetWithStatus("running")

		targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(target, nil).Twice()
		docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(nil).Once()
		docker.EXPECT().StreamContainerLogs(mock.Anything, target.DockerID, mock.Anything).Return(io.NopCloser(strings.NewReader("")), nil).Once()

		first, err := svc.StreamLogs(userCtx(), containerID, model.LogOptions{})
		require.NoError(t, err)
		defer func() { _ = first.Close() }() // test teardown

		other := accessscope.WithUserScope(context.Background(), uuid.New(), "bob", "user")
		second, err := svc.StreamLogs(other, containerID, model.LogOptions{})
		require.Nil(t, second)
		require.ErrorIs(t, err, apperrors.ErrLimitExceeded)
	})
}

// Each new stream reads the runtime configuration afresh.
func TestStreamLogsReadsRuntimeConfigForEachStream(t *testing.T) {
	targets := streamsmocks.NewContainerTargets(t)
	docker := streamsmocks.NewDocker(t)
	cfg := &mutableConfig{cfg: systemConfig(model.StreamConfig{MaxLogTailLines: 100, MaxLogStreamsPerUser: 2})}
	svc, err := streams.NewService(streams.Deps{Targets: targets, Docker: docker, Config: cfg, MaxLogStreams: 2, MaxTerminalSessions: 2, Logger: discardLogger()})
	require.NoError(t, err)
	target := targetWithStatus("running")

	targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(target, nil).Twice()
	docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(nil).Twice()
	var tails []int
	docker.EXPECT().
		StreamContainerLogs(mock.Anything, target.DockerID, mock.Anything).
		RunAndReturn(func(_ context.Context, _ string, opts model.LogOptions) (io.ReadCloser, error) {
			tails = append(tails, opts.Tail)
			return io.NopCloser(strings.NewReader("")), nil
		}).
		Twice()

	first, err := svc.StreamLogs(userCtx(), containerID, model.LogOptions{Tail: 500})
	require.NoError(t, err)
	require.NoError(t, first.Close())
	cfg.cfg.TelemetryMaxLogTailLines = 10
	second, err := svc.StreamLogs(userCtx(), containerID, model.LogOptions{Tail: 500})
	require.NoError(t, err)
	require.NoError(t, second.Close())

	require.Equal(t, []int{100, 10}, tails)
}

func TestOpenTerminalSuccessAndRuntimeConfig(t *testing.T) {
	targets := streamsmocks.NewContainerTargets(t)
	docker := streamsmocks.NewDocker(t)
	session := coremodelmocks.NewMockTerminalSession(t)
	cfg := runtimeConfig()
	svc := newService(t, targets, docker, cfg, 2, 1)
	target := targetWithStatus("running")

	targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(target, nil).Once()
	docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(nil).Once()
	docker.EXPECT().
		OpenTerminal(mock.Anything, target.DockerID, mock.MatchedBy(func(opts model.TerminalOptions) bool {
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
		cfg     model.StreamConfig
		status  string
		wantErr error
	}{
		{name: "no configured commands", cfg: model.StreamConfig{}, status: "running", wantErr: apperrors.ErrConflict},
		{name: "terminal size too large", cfg: runtimeConfig(), opts: model.TerminalOptions{Rows: 201}, status: "running", wantErr: apperrors.ErrBadRequest},
		{name: "not running", cfg: runtimeConfig(), status: "exited", wantErr: apperrors.ErrConflict},
		{name: "missing scope", cfg: runtimeConfig(), status: "running", wantErr: apperrors.ErrUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targets := streamsmocks.NewContainerTargets(t)
			docker := streamsmocks.NewDocker(t)
			svc := newService(t, targets, docker, tt.cfg, 2, 1)
			ctx := userCtx()
			if tt.name == "missing scope" {
				ctx = context.Background()
			}

			if !errors.Is(tt.wantErr, apperrors.ErrBadRequest) && tt.name != "no configured commands" {
				targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(targetWithStatus(tt.status), nil).Once()
			}

			session, cfg, err := svc.OpenTerminal(ctx, containerID, tt.opts)
			require.Nil(t, session)
			require.Empty(t, cfg)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestOpenTerminalPropagatesDependencyErrorsAndReleasesLimit(t *testing.T) {
	t.Run("target and docker errors", func(t *testing.T) {
		tests := []struct {
			name  string
			setup func(targets *streamsmocks.ContainerTargets, docker *streamsmocks.Docker)
		}{
			{
				name: "target",
				setup: func(targets *streamsmocks.ContainerTargets, docker *streamsmocks.Docker) {
					targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(model.ContainerRuntimeTarget{}, apperrors.ErrNotFound).Once()
				},
			},
			{
				name: "ensure target",
				setup: func(targets *streamsmocks.ContainerTargets, docker *streamsmocks.Docker) {
					target := targetWithStatus("running")
					targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(target, nil).Once()
					docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(apperrors.ErrNotFound).Once()
				},
			},
			{
				name: "open terminal",
				setup: func(targets *streamsmocks.ContainerTargets, docker *streamsmocks.Docker) {
					target := targetWithStatus("running")
					targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(target, nil).Once()
					docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(nil).Once()
					docker.EXPECT().OpenTerminal(mock.Anything, target.DockerID, mock.Anything).Return(nil, apperrors.ErrUnavailable).Once()
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				targets := streamsmocks.NewContainerTargets(t)
				docker := streamsmocks.NewDocker(t)
				svc := newService(t, targets, docker, runtimeConfig(), 2, 1)
				tt.setup(targets, docker)

				session, cfg, err := svc.OpenTerminal(userCtx(), containerID, model.TerminalOptions{})
				require.Nil(t, session)
				require.Empty(t, cfg)
				require.Error(t, err)
			})
		}
	})

	t.Run("limit is released on session close", func(t *testing.T) {
		targets := streamsmocks.NewContainerTargets(t)
		docker := streamsmocks.NewDocker(t)
		firstSession := coremodelmocks.NewMockTerminalSession(t)
		secondSession := coremodelmocks.NewMockTerminalSession(t)
		svc := newService(t, targets, docker, runtimeConfig(), 2, 1)
		target := targetWithStatus("running")

		targets.EXPECT().GetRuntimeTarget(mock.Anything, containerID).Return(target, nil).Times(3)
		docker.EXPECT().EnsureContainerTarget(mock.Anything, target).Return(nil).Twice()
		docker.EXPECT().OpenTerminal(mock.Anything, target.DockerID, mock.Anything).Return(firstSession, nil).Once()
		docker.EXPECT().OpenTerminal(mock.Anything, target.DockerID, mock.Anything).Return(secondSession, nil).Once()
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
		cfg     model.StreamConfig
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
			cfg: model.StreamConfig{
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
			got, err := streams.NormalizeTerminalOptions(tt.opts, tt.cfg)
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
		cfg  model.StreamConfig
		want int
	}{
		{name: "default tail", opts: model.LogOptions{}, cfg: model.StreamConfig{}, want: 200},
		{name: "cap tail", opts: model.LogOptions{Tail: 500}, cfg: model.StreamConfig{MaxLogTailLines: 100}, want: 100},
		{name: "preserve valid tail", opts: model.LogOptions{Tail: 50}, cfg: model.StreamConfig{MaxLogTailLines: 100}, want: 50},
	}
	for _, tt := range logTests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, streams.NormalizeLogOptions(tt.opts, tt.cfg).Tail)
		})
	}

	size, err := streams.ParseSize("42")
	require.NoError(t, err)
	require.Equal(t, uint(42), size)
	_, err = streams.ParseSize("bad")
	require.ErrorIs(t, err, apperrors.ErrBadRequest)

	limiter := streams.NewSessionLimiter(2)
	require.True(t, limiter.Acquire("user", 1))
	require.False(t, limiter.Acquire("user", 1))
	require.True(t, limiter.Acquire("admin", 1))
	require.False(t, limiter.Acquire("other", 1))
	limiter.Release("user")
	require.True(t, limiter.Acquire("other", 1))

	attrs := streams.SessionLogAttrs(userCtx(), containerID.String(), "session-id", time.Now().Add(-time.Millisecond), "closed")
	require.Contains(t, attrs, "session_id")
	require.Contains(t, attrs, "session-id")
	require.Contains(t, attrs, "container_id")
	require.Contains(t, attrs, containerID.String())
	require.Contains(t, attrs, "reason")
	require.Contains(t, attrs, "closed")
}

func TestStreamConfigSelectsTheTelemetrySettings(t *testing.T) {
	require.Equal(t, runtimeConfig(), streams.StreamConfigFrom(systemConfig(runtimeConfig())))
}

func newService(t *testing.T, targets *streamsmocks.ContainerTargets, docker *streamsmocks.Docker, cfg model.StreamConfig, maxLogStreams, maxTerminalSessions int) *streams.Service {
	t.Helper()
	svc, err := streams.NewService(streams.Deps{
		Targets:             targets,
		Docker:              docker,
		Config:              coretest.StaticConfig{Config: systemConfig(cfg)},
		MaxLogStreams:       maxLogStreams,
		MaxTerminalSessions: maxTerminalSessions,
		Logger:              discardLogger(),
	})
	require.NoError(t, err)
	return svc
}

// systemConfig is the runtime configuration whose stream settings are cfg.
func systemConfig(cfg model.StreamConfig) config.SystemConfig {
	return config.SystemConfig{
		TelemetryTerminalIdleTimeoutSeconds: int64(cfg.TerminalIdleTimeout / time.Second),
		TelemetryTerminalMaxDurationSeconds: int64(cfg.TerminalMaxDuration / time.Second),
		TelemetryAllowedExecCommands:        cfg.AllowedExecCommands,
		Fixed: config.Fixed{
			TelemetryMaxLogTailLines:            cfg.MaxLogTailLines,
			TelemetryMaxLogStreamsPerUser:       cfg.MaxLogStreamsPerUser,
			TelemetryMaxTerminalSessionsPerUser: cfg.MaxTerminalSessionsPerUser,
			TelemetryMaxCommandArgs:             cfg.MaxCommandArgs,
			TelemetryMaxCommandArgBytes:         cfg.MaxCommandArgBytes,
			TelemetryWSReadLimitBytes:           cfg.WSReadLimitBytes,
		},
	}
}

type mutableConfig struct {
	cfg config.SystemConfig
}

func (c *mutableConfig) Get() config.SystemConfig {
	return c.cfg
}

func runtimeConfig() model.StreamConfig {
	return model.StreamConfig{
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

func targetWithStatus(status string) model.ContainerRuntimeTarget {
	return model.ContainerRuntimeTarget{
		ContainerID:      containerID,
		DockerID:         "docker-id",
		Status:           status,
		OwnerID:          uuid.New(),
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
