// Package streams serves container log streams and terminal sessions: it
// authorizes the container through the container service, applies the stream
// limits and the terminal command allowlist, and opens the Docker stream.
package streams

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/nilcheck"
)

// ContainerTargets resolves a container to its Docker runtime under the
// caller's owner access.
type ContainerTargets interface {
	GetRuntimeTarget(ctx context.Context, containerID uuid.UUID) (model.ContainerRuntimeTarget, error)
}

// Docker opens log streams and terminal sessions on the Docker daemon.
type Docker interface {
	EnsureContainerTarget(ctx context.Context, target model.ContainerRuntimeTarget) error
	StreamContainerLogs(ctx context.Context, dockerID string, opts model.LogOptions) (io.ReadCloser, error)
	OpenTerminal(ctx context.Context, dockerID string, opts model.TerminalOptions) (model.TerminalSession, error)
}

// ConfigProvider returns the current runtime configuration.
type ConfigProvider interface {
	Get() config.SystemConfig
}

// Deps are the service's dependencies. MaxLogStreams and MaxTerminalSessions
// are the global limits across all users.
type Deps struct {
	Targets             ContainerTargets
	Docker              Docker
	Config              ConfigProvider
	MaxLogStreams       int
	MaxTerminalSessions int
	Logger              *slog.Logger
}

// Service opens container log streams and terminal sessions.
type Service struct {
	targets         ContainerTargets
	docker          Docker
	config          ConfigProvider
	logLimiter      *SessionLimiter
	terminalLimiter *SessionLimiter
	logger          *slog.Logger
}

// NewService rejects missing or typed-nil Targets, Docker and Config.
func NewService(deps Deps) (*Service, error) {
	required := []struct {
		name  string
		value any
	}{
		{"container targets", deps.Targets}, {"docker", deps.Docker}, {"config", deps.Config},
	}
	for _, dependency := range required {
		if nilcheck.IsNil(dependency.value) {
			return nil, fmt.Errorf("container streams: %s is required", dependency.name)
		}
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{
		targets:         deps.Targets,
		docker:          deps.Docker,
		config:          deps.Config,
		logLimiter:      NewSessionLimiter(deps.MaxLogStreams),
		terminalLimiter: NewSessionLimiter(deps.MaxTerminalSessions),
		logger:          logging.WithComponent(logger, "container_streams"),
	}, nil
}

// StreamLogs opens a log stream for a container the caller may access. Closing
// the stream releases its place under the limits.
func (s *Service) StreamLogs(ctx context.Context, containerID uuid.UUID, opts model.LogOptions) (io.ReadCloser, error) {
	cfg := StreamConfigFrom(s.config.Get())
	opts = NormalizeLogOptions(opts, cfg)

	target, err := s.targets.GetRuntimeTarget(ctx, containerID)
	if err != nil {
		return nil, err
	}
	if !logsAllowedForStatus(target.Status) {
		return nil, unavailableStatusError(target.Status)
	}

	key, err := actorKey(ctx)
	if err != nil {
		return nil, err
	}
	if !s.logLimiter.Acquire(key, cfg.MaxLogStreamsPerUser) {
		return nil, apperrors.New(apperrors.ErrLimitExceeded, "maximum telemetry log streams reached")
	}
	release := func() { s.logLimiter.Release(key) }

	if err := s.docker.EnsureContainerTarget(ctx, target); err != nil {
		release()
		return nil, err
	}
	stream, err := s.docker.StreamContainerLogs(ctx, target.DockerID, opts)
	if err != nil {
		release()
		return nil, err
	}
	return &releaseReadCloser{ReadCloser: stream, release: release}, nil
}

// OpenTerminal opens a terminal session in a running container the caller may
// access, and returns the configuration the session keeps until it ends.
// Closing the session releases its place under the limits.
func (s *Service) OpenTerminal(ctx context.Context, containerID uuid.UUID, opts model.TerminalOptions) (model.TerminalSession, model.StreamConfig, error) {
	cfg := StreamConfigFrom(s.config.Get())
	normalized, err := NormalizeTerminalOptions(opts, cfg)
	if err != nil {
		return nil, model.StreamConfig{}, err
	}

	target, err := s.targets.GetRuntimeTarget(ctx, containerID)
	if err != nil {
		return nil, model.StreamConfig{}, err
	}
	if target.Status != model.ContainerStatusRunning {
		return nil, model.StreamConfig{}, apperrors.New(apperrors.ErrConflict, "terminal requires a running container")
	}

	key, err := actorKey(ctx)
	if err != nil {
		return nil, model.StreamConfig{}, err
	}
	if !s.terminalLimiter.Acquire(key, cfg.MaxTerminalSessionsPerUser) {
		return nil, model.StreamConfig{}, apperrors.New(apperrors.ErrLimitExceeded, "maximum telemetry terminal sessions reached")
	}
	release := func() { s.terminalLimiter.Release(key) }

	if err := s.docker.EnsureContainerTarget(ctx, target); err != nil {
		release()
		return nil, model.StreamConfig{}, err
	}
	session, err := s.docker.OpenTerminal(ctx, target.DockerID, normalized)
	if err != nil {
		release()
		return nil, model.StreamConfig{}, err
	}
	s.logger.InfoContext(ctx, "terminal session opened", "container_id", containerID.String())
	return &releaseTerminalSession{TerminalSession: session, release: release}, cfg, nil
}

// StreamConfigFrom selects the stream settings from the runtime configuration.
func StreamConfigFrom(cfg config.SystemConfig) model.StreamConfig {
	return model.StreamConfig{
		MaxLogTailLines:            cfg.TelemetryMaxLogTailLines,
		MaxLogStreamsPerUser:       cfg.TelemetryMaxLogStreamsPerUser,
		MaxTerminalSessionsPerUser: cfg.TelemetryMaxTerminalSessionsPerUser,
		TerminalIdleTimeout:        time.Duration(cfg.TelemetryTerminalIdleTimeoutSeconds) * time.Second,
		TerminalMaxDuration:        time.Duration(cfg.TelemetryTerminalMaxDurationSeconds) * time.Second,
		AllowedExecCommands:        cfg.TelemetryAllowedExecCommands,
		MaxCommandArgs:             cfg.TelemetryMaxCommandArgs,
		MaxCommandArgBytes:         cfg.TelemetryMaxCommandArgBytes,
		WSReadLimitBytes:           cfg.TelemetryWSReadLimitBytes,
	}
}

func NormalizeLogOptions(opts model.LogOptions, cfg model.StreamConfig) model.LogOptions {
	if opts.Tail <= 0 {
		opts.Tail = 200
	}
	if cfg.MaxLogTailLines > 0 && opts.Tail > cfg.MaxLogTailLines {
		opts.Tail = cfg.MaxLogTailLines
	}
	return opts
}

func NormalizeTerminalOptions(opts model.TerminalOptions, cfg model.StreamConfig) (model.TerminalOptions, error) {
	if opts.Rows == 0 {
		opts.Rows = 24
	}
	if opts.Cols == 0 {
		opts.Cols = 80
	}
	if opts.Rows > 200 || opts.Cols > 500 {
		return model.TerminalOptions{}, apperrors.New(apperrors.ErrBadRequest, "terminal size is out of range")
	}

	command, err := normalizeCommand(opts.Command, cfg)
	if err != nil {
		return model.TerminalOptions{}, err
	}
	opts.Command = command
	return opts, nil
}

func normalizeCommand(command []string, cfg model.StreamConfig) ([]string, error) {
	if len(cfg.AllowedExecCommands) == 0 {
		return nil, apperrors.New(apperrors.ErrConflict, "terminal commands are not configured")
	}
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		cmd := cfg.AllowedExecCommands[0]
		if isShellCommand(cmd) {
			return []string{cmd, "-i"}, nil
		}
		return []string{cmd}, nil
	}

	if len(command)-1 > cfg.MaxCommandArgs {
		return nil, apperrors.New(apperrors.ErrBadRequest, "too many command arguments")
	}
	allowed := make(map[string]struct{}, len(cfg.AllowedExecCommands))
	for _, item := range cfg.AllowedExecCommands {
		allowed[item] = struct{}{}
	}
	cmd := command[0]
	if _, ok := allowed[cmd]; !ok {
		return nil, apperrors.New(apperrors.ErrForbidden, "terminal command is not allowed")
	}

	normalized := make([]string, 0, len(command))
	for i, arg := range command {
		if arg == "" || strings.ContainsAny(arg, "\x00\r\n") {
			return nil, apperrors.New(apperrors.ErrBadRequest, "invalid command argument")
		}
		if i > 0 && len(arg) > cfg.MaxCommandArgBytes {
			return nil, apperrors.New(apperrors.ErrBadRequest, "command argument is too large")
		}
		if i > 0 && isShellCommand(cmd) && !safeShellArg(arg) {
			return nil, apperrors.New(apperrors.ErrForbidden, "shell argument is not allowed")
		}
		normalized = append(normalized, arg)
	}
	return normalized, nil
}

func safeShellArg(arg string) bool {
	switch arg {
	case "-i", "-l", "--login":
		return true
	default:
		return false
	}
}

func isShellCommand(command string) bool {
	switch path.Base(command) {
	case "sh", "bash", "ash":
		return true
	default:
		return false
	}
}

func logsAllowedForStatus(status string) bool {
	switch status {
	case model.ContainerStatusCreated, model.ContainerStatusRunning, model.ContainerStatusExited, model.ContainerStatusError:
		return true
	default:
		return false
	}
}

func unavailableStatusError(status string) error {
	if status == model.ContainerStatusMissing {
		return apperrors.New(apperrors.ErrConflict, "container is missing in Docker and can only be deleted")
	}
	return apperrors.New(apperrors.ErrConflict, "container is not available for telemetry")
}

func actorKey(ctx context.Context) (string, error) {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return "", err
	}
	if scope.UserID.String() != "" {
		return string(scope.Kind) + ":" + scope.UserID.String(), nil
	}
	return string(scope.Kind), nil
}

// SessionLogAttrs are the lifecycle attributes of a closed terminal session's log
// line. They carry no terminal input or output.
func SessionLogAttrs(ctx context.Context, containerID, sessionID string, started time.Time, reason string) []any {
	scope, _ := accessscope.FromContext(ctx)
	return []any{
		"session_id", sessionID,
		"user_id", scope.UserID.String(),
		"scope", string(scope.Kind),
		"container_id", containerID,
		"duration_ms", time.Since(started).Milliseconds(),
		"reason", reason,
	}
}

func ParseSize(value string) (uint, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid terminal size", apperrors.ErrBadRequest)
	}
	return uint(parsed), nil
}

type releaseReadCloser struct {
	io.ReadCloser
	release func()
	once    sync.Once
}

func (r *releaseReadCloser) Close() error {
	r.once.Do(r.release)
	return r.ReadCloser.Close()
}

type releaseTerminalSession struct {
	model.TerminalSession
	release func()
	once    sync.Once
}

func (s *releaseTerminalSession) Close() error {
	s.once.Do(s.release)
	return s.TerminalSession.Close()
}
