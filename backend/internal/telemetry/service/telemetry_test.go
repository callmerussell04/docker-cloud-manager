package service

import (
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/telemetry/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

func TestNormalizeLogOptionsCapsTail(t *testing.T) {
	got := NormalizeLogOptions(model.LogOptions{Tail: 5000}, model.RuntimeConfig{MaxLogTailLines: 1000})
	if got.Tail != 1000 {
		t.Fatalf("Tail = %d, want 1000", got.Tail)
	}
}

func TestNormalizeTerminalOptionsValidatesCommand(t *testing.T) {
	cfg := model.RuntimeConfig{
		AllowedExecCommands: []string{"/bin/sh", "/usr/bin/top"},
		MaxCommandArgs:      2,
		MaxCommandArgBytes:  16,
	}

	got, err := NormalizeTerminalOptions(model.TerminalOptions{}, cfg)
	if err != nil {
		t.Fatalf("NormalizeTerminalOptions() error = %v", err)
	}
	if len(got.Command) != 2 || got.Command[0] != "/bin/sh" || got.Command[1] != "-i" {
		t.Fatalf("default command = %+v, want /bin/sh -i", got.Command)
	}

	_, err = NormalizeTerminalOptions(model.TerminalOptions{Command: []string{"/bin/sh", "-c"}}, cfg)
	if !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatalf("shell -c error = %v, want ErrForbidden", err)
	}

	_, err = NormalizeTerminalOptions(model.TerminalOptions{Command: []string{"/bin/unknown"}}, cfg)
	if !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatalf("unknown command error = %v, want ErrForbidden", err)
	}

	_, err = NormalizeTerminalOptions(model.TerminalOptions{Command: []string{"/usr/bin/top", "aaaaaaaaaaaaaaaaa"}}, cfg)
	if !errors.Is(err, apperrors.ErrBadRequest) {
		t.Fatalf("large arg error = %v, want ErrBadRequest", err)
	}
}

func TestSessionLimiter(t *testing.T) {
	limiter := NewSessionLimiter(2)
	if !limiter.Acquire("user", 1) {
		t.Fatal("first acquire failed")
	}
	if limiter.Acquire("user", 1) {
		t.Fatal("second acquire for same user succeeded")
	}
	if !limiter.Acquire("admin", 1) {
		t.Fatal("second global acquire failed")
	}
	if limiter.Acquire("other", 1) {
		t.Fatal("third global acquire succeeded")
	}
	limiter.Release("user")
	if !limiter.Acquire("other", 1) {
		t.Fatal("acquire after release failed")
	}
}
