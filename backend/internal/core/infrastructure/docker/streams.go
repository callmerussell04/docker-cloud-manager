package docker

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// EnsureContainerTarget checks that the Docker container still carries the
// identity labels of the record it was resolved from, so a recorded Docker ID
// that now names another container is never streamed or attached to.
func (a *Adapter) EnsureContainerTarget(ctx context.Context, target model.ContainerRuntimeTarget) error {
	inspect, err := a.cli.ContainerInspect(ctx, target.DockerID)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return apperrors.ErrNotFound
		}
		return err
	}
	if inspect.Config == nil {
		return apperrors.New(apperrors.ErrNotFound, "container target verification failed")
	}
	if err := validateLabels(inspect.Config.Labels, target); err != nil {
		return err
	}
	return nil
}

// StreamContainerLogs streams a container's stdout and stderr as plain text.
// Cancelling ctx ends the stream.
func (a *Adapter) StreamContainerLogs(ctx context.Context, dockerID string, opts model.LogOptions) (io.ReadCloser, error) {
	logOpts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     opts.Follow,
		Timestamps: opts.Timestamps,
		Tail:       strconv.Itoa(opts.Tail),
	}
	if !opts.Since.IsZero() {
		logOpts.Since = strconv.FormatInt(opts.Since.Unix(), 10)
	}

	rawLogs, err := a.cli.ContainerLogs(ctx, dockerID, logOpts)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil, apperrors.ErrNotFound
		}
		return nil, err
	}

	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = rawLogs.Close() }() // read-only stream; close failure is not actionable
		if _, err := stdcopy.StdCopy(pw, pw, rawLogs); err != nil && ctx.Err() != nil {
			_ = pw.CloseWithError(ctx.Err()) // always nil; it hands the reader the cancellation
			return
		}
		_ = pw.Close() // always nil; it hands the reader EOF
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = rawLogs.Close()              // read-only stream; closing unblocks the copy
			_ = pw.CloseWithError(ctx.Err()) // always nil; it hands the reader the cancellation
		case <-done:
		}
	}()

	return pr, nil
}

// OpenTerminal attaches to a new exec of opts.Command with a TTY. The exec is
// never privileged and gets no environment or user override.
func (a *Adapter) OpenTerminal(ctx context.Context, dockerID string, opts model.TerminalOptions) (model.TerminalSession, error) {
	resp, err := a.cli.ContainerExecCreate(ctx, dockerID, container.ExecOptions{
		Cmd:          opts.Command,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Privileged:   false,
		ConsoleSize:  &[2]uint{opts.Rows, opts.Cols},
	})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil, apperrors.ErrNotFound
		}
		return nil, err
	}

	hijacked, err := a.cli.ContainerExecAttach(ctx, resp.ID, container.ExecAttachOptions{
		Tty:         true,
		ConsoleSize: &[2]uint{opts.Rows, opts.Cols},
	})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil, apperrors.ErrNotFound
		}
		return nil, err
	}

	return &terminalSession{
		cli:    a.cli,
		execID: resp.ID,
		conn:   &hijacked,
	}, nil
}

func validateLabels(labels map[string]string, target model.ContainerRuntimeTarget) error {
	if labels["managed_by"] != "docker-cloud-manager" ||
		labels["dcm.resource_type"] != "container" ||
		labels["dcm.container_id"] != target.ContainerID.String() ||
		labels["dcm.owner_id"] != target.OwnerID.String() ||
		labels["dcm.generation"] != strconv.Itoa(target.DockerGeneration) {
		return apperrors.New(apperrors.ErrNotFound, "container target verification failed")
	}
	return nil
}

type terminalSession struct {
	cli    *client.Client
	execID string
	conn   *types.HijackedResponse
}

func (s *terminalSession) Read(p []byte) (int, error) {
	return s.conn.Reader.Read(p)
}

func (s *terminalSession) Write(p []byte) (int, error) {
	return s.conn.Conn.Write(p)
}

func (s *terminalSession) Close() error {
	if s.conn != nil {
		_ = s.conn.CloseWrite() // teardown; the connection is closed next
		s.conn.Close()
	}
	return nil
}

func (s *terminalSession) CloseWrite() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.CloseWrite()
}

func (s *terminalSession) Resize(ctx context.Context, rows, cols uint) error {
	if rows == 0 || cols == 0 {
		return fmt.Errorf("%w: invalid terminal size", apperrors.ErrBadRequest)
	}
	return s.cli.ContainerExecResize(ctx, s.execID, container.ResizeOptions{
		Height: rows,
		Width:  cols,
	})
}

func (s *terminalSession) Inspect(ctx context.Context) (model.ExecState, error) {
	inspect, err := s.cli.ContainerExecInspect(ctx, s.execID)
	if err != nil {
		return model.ExecState{}, err
	}
	return model.ExecState{
		Running:  inspect.Running,
		ExitCode: inspect.ExitCode,
	}, nil
}
