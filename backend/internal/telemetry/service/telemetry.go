package service

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

	"github.com/callmerussell04/docker-cloud-manager/internal/telemetry/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
)

const (
	containerStatusCreated = "created"
	containerStatusRunning = "running"
	containerStatusExited  = "exited"
	containerStatusError   = "error"
	containerStatusMissing = "missing"
)

type CoreClient interface {
	GetContainerTarget(ctx context.Context, containerID string) (model.ContainerTarget, error)
	GetRuntimeConfig(ctx context.Context) (model.RuntimeConfig, error)
}

type DockerClient interface {
	EnsureContainerTarget(ctx context.Context, target model.ContainerTarget) error
	StreamLogs(ctx context.Context, target model.ContainerTarget, opts model.LogOptions) (io.ReadCloser, error)
	OpenTerminal(ctx context.Context, target model.ContainerTarget, opts model.TerminalOptions) (model.TerminalSession, error)
	Close() error
}

type Telemetry struct {
	core            CoreClient
	docker          DockerClient
	logLimiter      *SessionLimiter
	terminalLimiter *SessionLimiter
	logger          *slog.Logger
}

func NewTelemetry(core CoreClient, docker DockerClient, maxLogStreams, maxTerminalSessions int, logger *slog.Logger) *Telemetry {
	return &Telemetry{
		core:            core,
		docker:          docker,
		logLimiter:      NewSessionLimiter(maxLogStreams),
		terminalLimiter: NewSessionLimiter(maxTerminalSessions),
		logger:          logging.WithComponent(logger, "telemetry_service"),
	}
}

func (s *Telemetry) StreamLogs(ctx context.Context, containerID string, opts model.LogOptions) (io.ReadCloser, error) {
	cfg, err := s.core.GetRuntimeConfig(ctx)
	if err != nil {
		return nil, err
	}
	opts = NormalizeLogOptions(opts, cfg)

	target, err := s.core.GetContainerTarget(ctx, containerID)
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
	stream, err := s.docker.StreamLogs(ctx, target, opts)
	if err != nil {
		release()
		return nil, err
	}
	return &releaseReadCloser{ReadCloser: stream, release: release}, nil
}

func (s *Telemetry) OpenTerminal(ctx context.Context, containerID string, opts model.TerminalOptions) (model.TerminalSession, model.RuntimeConfig, error) {
	cfg, err := s.core.GetRuntimeConfig(ctx)
	if err != nil {
		return nil, model.RuntimeConfig{}, err
	}
	normalized, err := NormalizeTerminalOptions(opts, cfg)
	if err != nil {
		return nil, model.RuntimeConfig{}, err
	}

	target, err := s.core.GetContainerTarget(ctx, containerID)
	if err != nil {
		return nil, model.RuntimeConfig{}, err
	}
	if target.Status != containerStatusRunning {
		return nil, model.RuntimeConfig{}, apperrors.New(apperrors.ErrConflict, "terminal requires a running container")
	}

	key, err := actorKey(ctx)
	if err != nil {
		return nil, model.RuntimeConfig{}, err
	}
	if !s.terminalLimiter.Acquire(key, cfg.MaxTerminalSessionsPerUser) {
		return nil, model.RuntimeConfig{}, apperrors.New(apperrors.ErrLimitExceeded, "maximum telemetry terminal sessions reached")
	}
	release := func() { s.terminalLimiter.Release(key) }

	if err := s.docker.EnsureContainerTarget(ctx, target); err != nil {
		release()
		return nil, model.RuntimeConfig{}, err
	}
	session, err := s.docker.OpenTerminal(ctx, target, normalized)
	if err != nil {
		release()
		return nil, model.RuntimeConfig{}, err
	}
	s.logger.InfoContext(ctx, "terminal session opened", "container_id", containerID)
	return &releaseTerminalSession{TerminalSession: session, release: release}, cfg, nil
}

func NormalizeLogOptions(opts model.LogOptions, cfg model.RuntimeConfig) model.LogOptions {
	if opts.Tail <= 0 {
		opts.Tail = 200
	}
	if cfg.MaxLogTailLines > 0 && opts.Tail > cfg.MaxLogTailLines {
		opts.Tail = cfg.MaxLogTailLines
	}
	return opts
}

func NormalizeTerminalOptions(opts model.TerminalOptions, cfg model.RuntimeConfig) (model.TerminalOptions, error) {
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

func normalizeCommand(command []string, cfg model.RuntimeConfig) ([]string, error) {
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
	case containerStatusCreated, containerStatusRunning, containerStatusExited, containerStatusError:
		return true
	case containerStatusMissing:
		return false
	default:
		return false
	}
}

func unavailableStatusError(status string) error {
	if status == containerStatusMissing {
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

func AuditAttrs(ctx context.Context, containerID, sessionID string, started time.Time, reason string) []any {
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
