package model

import (
	"context"
	"io"
	"time"
)

// StreamConfig is the runtime configuration a container log stream or terminal
// session starts with; an open session keeps it until it ends.
type StreamConfig struct {
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

// LogOptions selects the container log lines a stream returns.
type LogOptions struct {
	Tail       int
	Since      time.Time
	Timestamps bool
	Follow     bool
}

// TerminalOptions is the command and terminal size of a terminal session.
type TerminalOptions struct {
	Command []string
	Rows    uint
	Cols    uint
}

// ExecState is the state of a terminal session's process.
type ExecState struct {
	Running  bool
	ExitCode int
}

// TerminalSession is an attached Docker exec with a TTY.
type TerminalSession interface {
	io.ReadWriteCloser
	CloseWrite() error
	Resize(ctx context.Context, rows, cols uint) error
	Inspect(ctx context.Context) (ExecState, error)
}
