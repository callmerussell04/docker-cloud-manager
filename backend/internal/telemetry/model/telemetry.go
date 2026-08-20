package model

import (
	"context"
	"io"
	"time"
)

type RuntimeConfig struct {
	MaxLogTailLines            int
	MaxLogStreamsPerUser       int
	MaxTerminalSessionsPerUser int
	TerminalIdleTimeout        time.Duration
	TerminalMaxDuration        time.Duration
	AllowedExecCommands        []string
	MaxCommandArgs             int
	MaxCommandArgBytes         int
	WSReadLimitBytes           int64
}

type ContainerTarget struct {
	ContainerID      string
	DockerID         string
	Status           string
	OwnerID          string
	DockerGeneration int
}

type LogOptions struct {
	Tail       int
	Since      time.Time
	Timestamps bool
	Follow     bool
}

type TerminalOptions struct {
	Command []string
	Rows    uint
	Cols    uint
}

type ExecState struct {
	Running  bool
	ExitCode int
}

type TerminalSession interface {
	io.ReadWriteCloser
	CloseWrite() error
	Resize(ctx context.Context, rows, cols uint) error
	Inspect(ctx context.Context) (ExecState, error)
}
