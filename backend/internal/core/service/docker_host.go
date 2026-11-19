package service

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
)

// dockerHostReadTimeout bounds the startup read so a stalled Docker daemon
// cannot hold Core's startup.
const dockerHostReadTimeout = 10 * time.Second

// DockerHostReader reads from Docker, in one call, its data directory and
// whether it enforces the container root filesystem cap on this host.
type DockerHostReader interface {
	ReadDockerHost(ctx context.Context) (model.DockerHost, error)
}

// DockerDataDir is Docker's data directory: Root on the host (the
// DOCKER_DATA_ROOT setting) and Path, where Core reads it.
type DockerDataDir struct {
	Root string
	Path string
}

// RootfsQuotaCheck holds whether the container root filesystem cap
// (container_disk_quota) is enforced on this host, as read once at startup.
// Nothing changes behaviour with it; it is reported to administrators.
type RootfsQuotaCheck struct {
	status model.RootfsQuotaStatus
}

// Both errors name the setting and never a path: the paths are deployment
// layout, and the stat error would quote one.
var (
	errDockerDataRootNotDaemons = &envconfig.InvalidError{
		Key:    "DOCKER_DATA_ROOT",
		Reason: "the Docker daemon's root directory is neither this directory nor below it",
	}
	errDockerDataRootNotMounted = &envconfig.InvalidError{
		Key:    "DOCKER_DATA_ROOT",
		Reason: "is not a directory under CORE_HOST_DISK_PATH inside Core; mount it read-only there",
	}
)

// CheckDockerHost reads Docker once at startup. It fails when the daemon's
// root directory is neither dataDir.Root nor below it, or when dataDir.Path is
// not a directory inside Core: host disk figures and volume usage scans would
// then read the wrong files. A daemon that cannot be read is logged once and
// startup continues, with the cap status RootfsQuotaUnknown. Otherwise it logs
// a warning unless the cap is enforced.
func CheckDockerHost(ctx context.Context, reader DockerHostReader, dataDir DockerDataDir, logger *slog.Logger) (*RootfsQuotaCheck, error) {
	readCtx, cancel := context.WithTimeout(ctx, dockerHostReadTimeout)
	defer cancel()
	host, readErr := reader.ReadDockerHost(readCtx)
	if readErr == nil && host.RootDir != "" && !withinDir(dataDir.Root, host.RootDir) {
		return nil, errDockerDataRootNotDaemons
	}
	if info, err := os.Stat(dataDir.Path); err != nil || !info.IsDir() {
		return nil, errDockerDataRootNotMounted
	}

	status := host.RootfsQuota
	if readErr != nil {
		status = model.RootfsQuotaUnknown
	}
	// At most one warning covers both findings.
	attrs := []any{"operation", "check_docker_host", "rootfs_quota_status", string(status)}
	switch {
	case readErr != nil:
		logger.Warn("docker info could not be read at startup: the container root filesystem cap (container_disk_quota) "+
			"is not known to be enforced and DOCKER_DATA_ROOT is not checked against the daemon", append(attrs, "error", readErr)...)
	case host.RootDir == "":
		logger.Warn("docker info reported no root directory, so DOCKER_DATA_ROOT is not checked against the daemon; "+
			"the container root filesystem cap (container_disk_quota) is "+string(status), attrs...)
	case status != model.RootfsQuotaEnforced:
		logger.Warn("container root filesystem cap (container_disk_quota) is not known to be enforced on this host; "+
			"Docker may accept it without applying it", attrs...)
	}
	return &RootfsQuotaCheck{status: status}, nil
}

// withinDir reports whether path is dir or below it. Both are host paths.
func withinDir(dir, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// RootfsQuotaStatus returns the status read at startup.
func (c *RootfsQuotaCheck) RootfsQuotaStatus() model.RootfsQuotaStatus {
	return c.status
}
