//go:build unix

package compose_test

import (
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func mkfifo(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, syscall.Mkfifo(path, 0o600))
}
