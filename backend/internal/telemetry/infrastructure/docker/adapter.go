package docker

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/callmerussell04/docker-cloud-manager/internal/telemetry/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

type Adapter struct {
	cli *client.Client
}

func NewAdapter() (*Adapter, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Adapter{cli: cli}, nil
}

func (a *Adapter) Close() error {
	if a.cli != nil {
		return a.cli.Close()
	}
	return nil
}

func (a *Adapter) EnsureContainerTarget(ctx context.Context, target model.ContainerTarget) error {
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

func (a *Adapter) StreamLogs(ctx context.Context, target model.ContainerTarget, opts model.LogOptions) (io.ReadCloser, error) {
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

	rawLogs, err := a.cli.ContainerLogs(ctx, target.DockerID, logOpts)
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
		defer rawLogs.Close()
		if _, err := stdcopy.StdCopy(pw, pw, rawLogs); err != nil && ctx.Err() != nil {
			_ = pw.CloseWithError(ctx.Err())
			return
		}
		_ = pw.Close()
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = rawLogs.Close()
			_ = pw.CloseWithError(ctx.Err())
		case <-done:
		}
	}()

	return pr, nil
}

func (a *Adapter) OpenTerminal(ctx context.Context, target model.ContainerTarget, opts model.TerminalOptions) (model.TerminalSession, error) {
	resp, err := a.cli.ContainerExecCreate(ctx, target.DockerID, container.ExecOptions{
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

func validateLabels(labels map[string]string, target model.ContainerTarget) error {
	if labels["managed_by"] != "docker-cloud-manager" ||
		labels["dcm.resource_type"] != "container" ||
		labels["dcm.container_id"] != target.ContainerID ||
		labels["dcm.owner_id"] != target.OwnerID ||
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
		_ = s.conn.CloseWrite()
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
