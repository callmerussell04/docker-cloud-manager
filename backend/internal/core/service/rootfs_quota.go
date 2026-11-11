package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
)

// rootfsQuotaReadTimeout bounds the startup read so a stalled Docker daemon
// cannot hold Core's startup.
const rootfsQuotaReadTimeout = 10 * time.Second

// RootfsQuotaReader reads from Docker whether it enforces the container root
// filesystem cap on this host.
type RootfsQuotaReader interface {
	ReadRootfsQuotaStatus(ctx context.Context) (model.RootfsQuotaStatus, error)
}

// RootfsQuotaCheck holds whether the container root filesystem cap
// (container_disk_quota) is enforced on this host, as read once at startup.
// Nothing changes behaviour with it; it is reported to administrators.
type RootfsQuotaCheck struct {
	status model.RootfsQuotaStatus
}

// CheckRootfsQuota reads the root filesystem cap status once and logs a
// warning unless it is enforced. A failed read is RootfsQuotaUnknown and never
// stops startup.
func CheckRootfsQuota(ctx context.Context, reader RootfsQuotaReader, logger *slog.Logger) *RootfsQuotaCheck {
	readCtx, cancel := context.WithTimeout(ctx, rootfsQuotaReadTimeout)
	defer cancel()
	status, err := reader.ReadRootfsQuotaStatus(readCtx)
	if err != nil {
		status = model.RootfsQuotaUnknown
	}
	if status != model.RootfsQuotaEnforced {
		attrs := []any{"operation", "check_rootfs_quota", "rootfs_quota_status", string(status)}
		if err != nil {
			attrs = append(attrs, "error", err)
		}
		logger.Warn("container root filesystem cap (container_disk_quota) is not known to be enforced on this host; "+
			"Docker may accept it without applying it", attrs...)
	}
	return &RootfsQuotaCheck{status: status}
}

// RootfsQuotaStatus returns the status read at startup.
func (c *RootfsQuotaCheck) RootfsQuotaStatus() model.RootfsQuotaStatus {
	return c.status
}
