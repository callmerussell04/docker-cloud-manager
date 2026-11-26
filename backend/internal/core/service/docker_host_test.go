package service_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dockertest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
)

// Core reads the root filesystem cap status once at startup and keeps it. An
// administrator sees a warning in the log unless the cap is enforced, and Core
// starts whatever Docker answers about the cap.
func TestCheckDockerHostKeepsTheRootfsQuotaStatus(t *testing.T) {
	tests := []struct {
		name      string
		read      model.RootfsQuotaStatus
		readErr   error
		want      model.RootfsQuotaStatus
		wantWarns int
	}{
		{name: "enforced", read: model.RootfsQuotaEnforced, want: model.RootfsQuotaEnforced, wantWarns: 0},
		{name: "not enforced", read: model.RootfsQuotaNotEnforced, want: model.RootfsQuotaNotEnforced, wantWarns: 1},
		{name: "unknown", read: model.RootfsQuotaUnknown, want: model.RootfsQuotaUnknown, wantWarns: 1},
		{
			name: "failed read", read: model.RootfsQuotaUnknown, readErr: errors.New("docker unavailable"),
			want: model.RootfsQuotaUnknown, wantWarns: 1,
		},
		{
			name: "failed read reporting another status still counts as unknown", read: model.RootfsQuotaEnforced,
			readErr: errors.New("docker unavailable"), want: model.RootfsQuotaUnknown, wantWarns: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := service.DockerDataDir{Root: "/var/lib/docker", Path: t.TempDir()}
			reader := coremocks.NewDockerHostReader(t)
			reader.EXPECT().ReadDockerHost(mock.Anything).Return(model.DockerHost{RootfsQuota: tt.read, RootDir: dataDir.Root}, tt.readErr).Once()
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))

			check, err := service.CheckDockerHost(context.Background(), reader, dataDir, logger)

			require.NoError(t, err)
			require.Equal(t, tt.want, check.RootfsQuotaStatus())
			warnings := startupWarnings(t, &logs)
			require.Len(t, warnings, tt.wantWarns)
			if tt.wantWarns > 0 {
				require.Equal(t, string(tt.want), warnings[0]["rootfs_quota_status"])
			}
		})
	}
}

// Core sees only DOCKER_DATA_ROOT of the host and measures host disk there, so
// it refuses to start when the daemon keeps its data elsewhere: disk admission
// would measure the wrong filesystem and volume scans would find nothing. The
// error names the setting and never a path.
func TestCheckDockerHostComparesDockerDataRootWithTheDaemonRootDirectory(t *testing.T) {
	tests := []struct {
		name      string
		info      dockertest.Response
		wantErr   bool
		wantWarns int
	}{
		{name: "equal", info: dockertest.OK(`{"Driver":"btrfs","DockerRootDir":"/srv/docker"}`)},
		{name: "equal once cleaned", info: dockertest.OK(`{"Driver":"btrfs","DockerRootDir":"/srv/docker/"}`)},
		{name: "below", info: dockertest.OK(`{"Driver":"btrfs","DockerRootDir":"/srv/docker/daemon"}`)},
		{name: "different", info: dockertest.OK(`{"Driver":"btrfs","DockerRootDir":"/var/lib/docker"}`), wantErr: true},
		{name: "a sibling sharing the prefix", info: dockertest.OK(`{"Driver":"btrfs","DockerRootDir":"/srv/docker-old"}`), wantErr: true},
		{name: "above", info: dockertest.OK(`{"Driver":"btrfs","DockerRootDir":"/srv"}`), wantErr: true},
		{name: "unreadable", info: dockertest.Error(500, "daemon is starting"), wantWarns: 1},
		{name: "no root directory reported", info: dockertest.OK(`{"Driver":"vfs"}`), wantWarns: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dockertest.Serve(t, map[string]dockertest.Response{"GET /info": tt.info})
			adapter, err := docker.NewAdapter()
			require.NoError(t, err)
			t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))

			_, err = service.CheckDockerHost(context.Background(), adapter, service.DockerDataDir{Root: "/srv/docker", Path: t.TempDir()}, logger)

			if tt.wantErr {
				assertNamesDockerDataRootOnly(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, startupWarnings(t, &logs), tt.wantWarns)
		})
	}
}

// The mount is what makes Docker's data visible inside Core. Without it Core
// would read the disk of its own container and scan empty volume directories.
func TestCheckDockerHostRequiresTheDataDirectoryInsideCore(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	for name, path := range map[string]string{
		"missing":       filepath.Join(t.TempDir(), "srv", "docker"),
		"not directory": file,
	} {
		t.Run(name, func(t *testing.T) {
			for _, info := range []dockertest.Response{
				dockertest.OK(`{"Driver":"btrfs","DockerRootDir":"/srv/docker"}`),
				dockertest.Error(500, "daemon is starting"),
			} {
				dockertest.Serve(t, map[string]dockertest.Response{"GET /info": info})
				adapter, err := docker.NewAdapter()
				require.NoError(t, err)
				t.Cleanup(func() { _ = adapter.Close() }) // test teardown; close failure is not actionable

				_, err = service.CheckDockerHost(context.Background(), adapter, service.DockerDataDir{Root: "/srv/docker", Path: path}, slog.New(slog.DiscardHandler))

				assertNamesDockerDataRootOnly(t, err)
				require.NotContains(t, err.Error(), path)
			}
		})
	}
}

func assertNamesDockerDataRootOnly(t *testing.T, err error) {
	t.Helper()
	var invalid *envconfig.InvalidError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, "DOCKER_DATA_ROOT", invalid.Key)
	require.NotContains(t, err.Error(), "/srv")
	require.NotContains(t, err.Error(), "/var/lib")
}

func startupWarnings(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var warnings []map[string]any
	for _, entry := range decodeLogs(t, logs) {
		require.NotEqual(t, "ERROR", entry["level"], "a warning never stops startup")
		if entry["level"] == "WARN" {
			warnings = append(warnings, entry)
		}
	}
	return warnings
}

var _ service.StatsRootfsQuota = (*service.RootfsQuotaCheck)(nil)
