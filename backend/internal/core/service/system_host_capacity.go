package service

import (
	"context"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
)

// SystemHostCapacity reads the host figures the capacity settings are checked
// against.
type SystemHostCapacity interface {
	GetTotalMemory() (int64, error)
	GetLogicalCPUs() (int64, error)
}

// The host setting conflicts. Each is a fixed message that names the conflict
// and no figure; it reaches the administrator as a bad request.
const (
	conflictNoHostMemoryPool = "the system memory reservation leaves no host memory for containers"
	conflictNoHostCPUPool    = "the system cpu reservation leaves no host cpu for containers"
	conflictBuildSlotsMemory = "the build slots need more host memory than the server can reserve"
	conflictBuildSlotsCPU    = "the build slots need more host cpu than the server can reserve"
	hostCapacityCheckFailed  = "the host capacity check could not run"
	settingsNotSavedPrefix   = "settings cannot be saved: "
)

// readsHostCapacitySettings reports whether an update from current to next
// changes a setting the host capacity check reads.
func readsHostCapacitySettings(current, next config.SystemConfig) bool {
	return current.ReservedSystemMemory != next.ReservedSystemMemory ||
		current.OvercommitFactor != next.OvercommitFactor ||
		current.ReservedSystemCPU != next.ReservedSystemCPU ||
		current.CPUOvercommitFactor != next.CPUOvercommitFactor ||
		current.MaxConcurrentBuilds != next.MaxConcurrentBuilds ||
		current.BuildMemoryBytes != next.BuildMemoryBytes ||
		current.BuildCPUMillicores != next.BuildCPUMillicores
}

// hostSettingConflicts lists the capacity settings of cfg the host cannot
// hold, at most one per resource: a system reservation that leaves no pool,
// or else MaxConcurrentBuilds build slots above the pool. The pools are the
// ones admission computes. An error means the host could not be read.
func hostSettingConflicts(host SystemHostCapacity, cfg config.SystemConfig) ([]string, error) {
	memory, err := host.GetTotalMemory()
	if err != nil {
		return nil, err
	}
	cpus, err := host.GetLogicalCPUs()
	if err != nil {
		return nil, err
	}
	slots := int64(max(cfg.MaxConcurrentBuilds, 0))
	var conflicts []string
	if pool := hostMemoryPool(memory, cfg.ReservedSystemMemory, cfg.OvercommitFactor); pool <= 0 {
		conflicts = append(conflicts, conflictNoHostMemoryPool)
	} else if exceedsPool(slots, cfg.BuildMemoryBytes, pool) {
		conflicts = append(conflicts, conflictBuildSlotsMemory)
	}
	if pool := hostCPUPool(cpus, cfg.ReservedSystemCPU, cfg.CPUOvercommitFactor); pool <= 0 {
		conflicts = append(conflicts, conflictNoHostCPUPool)
	} else if exceedsPool(slots, cfg.BuildCPUMillicores, pool) {
		conflicts = append(conflicts, conflictBuildSlotsCPU)
	}
	return conflicts, nil
}

// exceedsPool reports whether slots of perSlot each exceed a positive pool,
// without the overflow of multiplying them: for integers, slots*perSlot >
// pool exactly when slots > pool/perSlot.
func exceedsPool(slots, perSlot, pool int64) bool {
	if slots <= 0 || perSlot <= 0 {
		return false
	}
	return slots > pool/perSlot
}

// checkHostCapacitySettings rejects next when it changes a capacity setting
// the host cannot hold: a bad request naming the first conflict, or
// unavailable when the host could not be read. An update that changes none of
// those settings is not checked, so it saves on any host.
func checkHostCapacitySettings(host SystemHostCapacity, current, next config.SystemConfig) error {
	if !readsHostCapacitySettings(current, next) {
		return nil
	}
	conflicts, err := hostSettingConflicts(host, next)
	if err != nil {
		return apperrors.Wrap(apperrors.ErrUnavailable,
			settingsNotSavedPrefix+hostCapacityCheckFailed+"; try again", err)
	}
	if len(conflicts) > 0 {
		return apperrors.New(apperrors.ErrBadRequest, settingsNotSavedPrefix+conflicts[0])
	}
	return nil
}

// ReportHostSettingConflicts checks the capacity settings against the host at
// startup and logs one error for each conflict, or one for a host that could
// not be read. It never fails startup: the settings stay as they are and
// admission refuses what the host cannot hold.
func ReportHostSettingConflicts(ctx context.Context, host SystemHostCapacity, cfg config.SystemConfig, logger *slog.Logger) {
	conflicts, err := hostSettingConflicts(host, cfg)
	if err != nil {
		logger.ErrorContext(ctx, "capacity settings were not checked against the host",
			"operation", "check_host_capacity_settings", "conflict", hostCapacityCheckFailed, "error", err)
		return
	}
	for _, conflict := range conflicts {
		logger.ErrorContext(ctx, "a capacity setting does not fit this host; admission refuses what it cannot hold",
			"operation", "check_host_capacity_settings", "conflict", conflict)
	}
}
