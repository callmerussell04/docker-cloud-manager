package service

import (
	"context"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"
)

// admissionRequest is the memory, CPU and disk one piece of work asks for.
type admissionRequest struct {
	memoryBytes   int64
	cpuMillicores int64
	// chargesUser reports whether the memory and CPU count against the
	// owner's quotas. Builds reserve host capacity only.
	chargesUser bool
	// diskWriteBytes is what the work is projected to write to host disk.
	diskWriteBytes int64
}

// reservationLedger is a snapshot of the memory, CPU and disk already promised
// to an owner and to the host. Admission reads it while holding the capacity
// lock, so nothing it counts changes before the request's reservation is
// durable.
type reservationLedger struct {
	userMemoryQuotaBytes      int64
	userReservedMemoryBytes   int64
	userCPUQuotaMillicores    int64
	userReservedCPUMillicores int64

	// userDiskCounted is false when no image repository counts disk usage.
	userDiskCounted bool
	userDiskQuotaMB int64
	userUsedDiskMB  int64

	systemReservedMemoryBytes   int64
	systemReservedCPUMillicores int64
	activeBuilds                int64

	// hostCounted is false when no host metrics are wired; host memory and
	// CPU are then not checked.
	hostCounted      bool
	hostMemoryBytes  int64
	hostLogicalCPUs  int64
	diskFloorCounted bool
	diskFreeBytes    int64
}

// admissionLimit names the first limit a request exceeds.
type admissionLimit string

const (
	limitUserMemory admissionLimit = "user_memory"
	limitUserCPU    admissionLimit = "user_cpu"
	limitUserDisk   admissionLimit = "user_disk"
	limitHostMemory admissionLimit = "host_memory"
	limitHostCPU    admissionLimit = "host_cpu"
	limitDiskFloor  admissionLimit = "disk_floor"
)

// admissionVerdict is either admitted, with no limit, or rejected with the
// first limit the request exceeds and the figures that exceeded it.
type admissionVerdict struct {
	limit     admissionLimit
	projected int64
	capacity  int64
}

func (v admissionVerdict) err() error {
	switch v.limit {
	case "":
		return nil
	case limitUserMemory, limitUserCPU:
		return apperrors.ErrQuotaExceeded
	case limitUserDisk:
		return apperrors.New(apperrors.ErrQuotaExceeded, "user disk quota exceeded")
	default:
		return apperrors.ErrHostExhausted
	}
}

// checkAdmission decides whether req fits ledger. It checks, in order, user
// memory, user CPU, user disk, host memory including builds, host CPU
// including builds, and the host disk floor, and rejects at the first limit
// exceeded.
func checkAdmission(ledger reservationLedger, req admissionRequest, cfg config.SystemConfig) admissionVerdict {
	memory, cpu := max(req.memoryBytes, 0), max(req.cpuMillicores, 0)
	if req.chargesUser {
		if projected := ledger.userReservedMemoryBytes + memory; projected > ledger.userMemoryQuotaBytes {
			return admissionVerdict{limit: limitUserMemory, projected: projected, capacity: ledger.userMemoryQuotaBytes}
		}
		if projected := ledger.userReservedCPUMillicores + cpu; projected > ledger.userCPUQuotaMillicores {
			return admissionVerdict{limit: limitUserCPU, projected: projected, capacity: ledger.userCPUQuotaMillicores}
		}
	}
	if ledger.userDiskCounted && ledger.userUsedDiskMB >= ledger.userDiskQuotaMB {
		return admissionVerdict{limit: limitUserDisk, projected: ledger.userUsedDiskMB, capacity: ledger.userDiskQuotaMB}
	}
	if ledger.hostCounted {
		pool := hostMemoryPool(ledger.hostMemoryBytes, cfg.ReservedSystemMemory, cfg.OvercommitFactor)
		projected := ledger.systemReservedMemoryBytes + ledger.activeBuilds*cfg.BuildMemoryBytes + memory
		if pool <= 0 || projected > pool {
			return admissionVerdict{limit: limitHostMemory, projected: projected, capacity: pool}
		}
		pool = hostCPUPool(ledger.hostLogicalCPUs, cfg.ReservedSystemCPU, cfg.CPUOvercommitFactor)
		projected = ledger.systemReservedCPUMillicores + ledger.activeBuilds*buildCPUMillicores(cfg) + cpu
		if pool <= 0 || projected > pool {
			return admissionVerdict{limit: limitHostCPU, projected: projected, capacity: pool}
		}
	}
	if ledger.diskFloorCounted {
		remaining := ledger.diskFreeBytes - max(req.diskWriteBytes, 0)
		if remaining < cfg.HostMinFreeDiskBytes {
			return admissionVerdict{limit: limitDiskFloor, projected: remaining, capacity: cfg.HostMinFreeDiskBytes}
		}
	}
	return admissionVerdict{}
}

// userReservationReader sums the memory and CPU reserved by one owner.
type userReservationReader interface {
	GetUserReservedMemory(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetUserReservedCPU(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

// systemReservationReader sums the memory and CPU reserved by every container.
type systemReservationReader interface {
	GetTotalSystemReservedMemory(ctx context.Context) (int64, error)
	GetTotalSystemReservedCPU(ctx context.Context) (int64, error)
}

// admissionSources are where a reservation ledger is read from. users and
// system are required; userReservations is read only for requests charged to
// the owner. Absent images, volumes, host or disk metrics leave that part of
// the ledger uncounted.
type admissionSources struct {
	users            UserInfoProvider
	userReservations userReservationReader
	system           systemReservationReader
	builds           activeSystemBuildCounter
	images           diskQuotaImageRepository
	volumes          diskQuotaVolumeRepository
	hostMetrics      HostMetricsProvider
	diskMetrics      HostDiskMetricsProvider
	hostDiskPath     string
	logger           *slog.Logger
}

// admit reads the reservation ledger for ownerID and checks req against it.
// The caller holds the capacity lock. operation names the work in logs.
func (s admissionSources) admit(ctx context.Context, ownerID uuid.UUID, req admissionRequest, cfg config.SystemConfig, operation string) error {
	ledger, err := s.readLedger(ctx, ownerID, req.chargesUser, cfg)
	if err != nil {
		return err
	}
	verdict := checkAdmission(ledger, req, cfg)
	if verdict.limit != "" && s.logger != nil {
		s.logger.WarnContext(ctx, "request rejected by admission",
			"operation", operation,
			"limit", string(verdict.limit),
			"projected", verdict.projected,
			"capacity", verdict.capacity,
		)
	}
	return verdict.err()
}

// readLedger reads the disk only when cfg enables the disk floor.
func (s admissionSources) readLedger(ctx context.Context, ownerID uuid.UUID, chargesUser bool, cfg config.SystemConfig) (reservationLedger, error) {
	var ledger reservationLedger
	user, err := s.users.GetUser(ctx, ownerID)
	if err != nil {
		return ledger, err
	}
	if chargesUser {
		ledger.userMemoryQuotaBytes = user.QuotaRAMMB * bytesPerMB
		ledger.userCPUQuotaMillicores = int64(user.QuotaCPU * 1000)
		if ledger.userReservedMemoryBytes, err = s.userReservations.GetUserReservedMemory(ctx, ownerID); err != nil {
			return ledger, err
		}
		if ledger.userReservedCPUMillicores, err = s.userReservations.GetUserReservedCPU(ctx, ownerID); err != nil {
			return ledger, err
		}
	}
	if s.images != nil {
		ledger.userDiskCounted = true
		ledger.userDiskQuotaMB = user.QuotaDiskMB
		if ledger.userUsedDiskMB, err = usedDiskMB(ctx, ownerID, s.images, s.volumes); err != nil {
			return ledger, err
		}
	}
	if ledger.systemReservedMemoryBytes, err = s.system.GetTotalSystemReservedMemory(ctx); err != nil {
		s.logReadFailure(ctx, "failed to calculate total system reserved memory", err)
		return ledger, apperrors.ErrInternal
	}
	if ledger.systemReservedCPUMillicores, err = s.system.GetTotalSystemReservedCPU(ctx); err != nil {
		s.logReadFailure(ctx, "failed to calculate total system reserved cpu", err)
		return ledger, apperrors.ErrInternal
	}
	activeBuilds, err := s.builds.CountActive(ctx)
	if err != nil {
		return ledger, err
	}
	ledger.activeBuilds = int64(activeBuilds)
	if s.hostMetrics != nil {
		ledger.hostCounted = true
		if ledger.hostMemoryBytes, err = s.hostMetrics.GetTotalMemory(); err != nil {
			s.logReadFailure(ctx, "failed to get system memory", err)
			return ledger, apperrors.ErrInternal
		}
		if ledger.hostLogicalCPUs, err = s.hostMetrics.GetLogicalCPUs(); err != nil {
			s.logReadFailure(ctx, "failed to get system cpu count", err)
			return ledger, apperrors.ErrInternal
		}
	}
	if s.diskMetrics != nil && cfg.HostMinFreeDiskBytes > 0 {
		path := s.hostDiskPath
		if path == "" {
			path = "/"
		}
		stats, err := s.diskMetrics.GetDiskUsage(path)
		if err != nil {
			return ledger, err
		}
		ledger.diskFloorCounted = true
		ledger.diskFreeBytes = stats.FreeBytes
	}
	return ledger, nil
}

func (s admissionSources) logReadFailure(ctx context.Context, msg string, err error) {
	if s.logger != nil {
		s.logger.ErrorContext(ctx, msg, "error", err)
	}
}

func hostMemoryPool(totalMemoryBytes, reservedSystemMemoryBytes int64, overcommitFactor float64) int64 {
	if overcommitFactor <= 0 {
		return 0
	}
	available := totalMemoryBytes - reservedSystemMemoryBytes
	if available <= 0 {
		return 0
	}
	return int64(float64(available) * overcommitFactor)
}

// ensureHostMemoryAdmission is admin monitoring's host memory check. Request
// admission goes through checkAdmission.
func ensureHostMemoryAdmission(totalMemoryBytes int64, cfg config.SystemConfig, reservedMemoryBytes, requestedMemoryBytes int64) error {
	if requestedMemoryBytes < 0 {
		requestedMemoryBytes = 0
	}
	pool := hostMemoryPool(totalMemoryBytes, cfg.ReservedSystemMemory, cfg.OvercommitFactor)
	if pool <= 0 {
		return apperrors.ErrHostExhausted
	}
	if reservedMemoryBytes+requestedMemoryBytes > pool {
		return apperrors.ErrHostExhausted
	}
	return nil
}

func hostCPUPool(logicalCPUs, reservedSystemCPUMillicores int64, overcommitFactor float64) int64 {
	if logicalCPUs <= 0 || overcommitFactor <= 0 {
		return 0
	}
	available := logicalCPUs*1000 - reservedSystemCPUMillicores
	if available <= 0 {
		return 0
	}
	return int64(float64(available) * overcommitFactor)
}

func buildCPUMillicores(cfg config.SystemConfig) int64 {
	if cfg.BuildCPUQuota <= 0 || cfg.BuildCPUPeriod <= 0 {
		return 0
	}
	return cfg.BuildCPUQuota * 1000 / cfg.BuildCPUPeriod
}
