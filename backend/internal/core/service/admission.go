package service

import (
	"context"
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"
)

// admissionRequest is the memory, CPU and disk one piece of work asks for.
type admissionRequest struct {
	memoryBytes   int64
	cpuMillicores int64
	// builds is how many image builds the request queues. A build asks for no
	// memoryBytes or cpuMillicores of its own: it takes a build slot, and
	// every occupied slot then holds its memory and CPU. The slot's
	// BuildMaxDiskBytes of disk is held at the claim (buildClaimCap), not
	// here, so a build waits for disk instead of being refused.
	builds int64
	// chargesUser reports whether the memory and CPU count against the
	// owner's quotas. Builds reserve host capacity only.
	chargesUser bool
	// reservationHeld reports that the work's memory and CPU are already in the
	// ledger, as for the start of a container that reserves them. It asks for
	// none and meets no memory or CPU limit, only the disk ones: the ledger
	// already counts its reservation, so a limit it now exceeds was exceeded
	// before the request.
	reservationHeld bool
	// volumeBytes is the size of the volumes the request creates. It is
	// reserved against the owner's disk quota and promised by the host disk:
	// with the unused volume promise of every existing volume it must leave
	// the disk floor free. With none, the request is rejected only when the
	// owner's reserved disk already fills the quota, and the unused volume
	// promise is not read.
	volumeBytes int64
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
	userDiskCounted       bool
	userDiskQuotaBytes    int64
	userReservedDiskBytes int64

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
	// unusedVolumePromiseBytes is what every owner's volumes were promised
	// and have not used yet. It is read only for a request that creates
	// volumes and only when the disk floor is counted.
	unusedVolumePromiseBytes int64
}

// admissionVerdict is either admitted, with no limit, or rejected with the
// first limit the request exceeds and the figures that exceeded it. The limit
// is one of the six admission limits of apperrors: user memory, CPU and disk,
// host memory and CPU, and the disk floor.
type admissionVerdict struct {
	limit     apperrors.Limit
	projected int64
	capacity  int64
}

// err is nil for an admitted request, and otherwise the rejection that names
// the limit: a quota error for a user limit, a host-exhausted error for a host
// limit or the disk floor.
func (v admissionVerdict) err() error {
	if v.limit == "" {
		return nil
	}
	return apperrors.Exceeded(v.limit)
}

// checkAdmission decides whether req fits ledger. It checks, in order, user
// memory, user CPU, user disk, host memory including builds, host CPU
// including builds, and the host disk floor including the unused volume
// promise for a volume request, and rejects at the first limit exceeded. A
// request whose memory and CPU are already held meets only the user disk quota and the
// disk floor.
func checkAdmission(ledger reservationLedger, req admissionRequest, cfg config.SystemConfig) admissionVerdict {
	memory, cpu := max(req.memoryBytes, 0), max(req.cpuMillicores, 0)
	if req.chargesUser {
		if projected := ledger.userReservedMemoryBytes + memory; projected > ledger.userMemoryQuotaBytes {
			return admissionVerdict{limit: apperrors.LimitUserMemory, projected: projected, capacity: ledger.userMemoryQuotaBytes}
		}
		if projected := ledger.userReservedCPUMillicores + cpu; projected > ledger.userCPUQuotaMillicores {
			return admissionVerdict{limit: apperrors.LimitUserCPU, projected: projected, capacity: ledger.userCPUQuotaMillicores}
		}
	}
	if ledger.userDiskCounted {
		if req.volumeBytes > 0 {
			if projected := ledger.userReservedDiskBytes + req.volumeBytes; projected > ledger.userDiskQuotaBytes {
				return admissionVerdict{limit: apperrors.LimitUserDisk, projected: projected, capacity: ledger.userDiskQuotaBytes}
			}
		} else if ledger.userReservedDiskBytes >= ledger.userDiskQuotaBytes {
			return admissionVerdict{limit: apperrors.LimitUserDisk, projected: ledger.userReservedDiskBytes, capacity: ledger.userDiskQuotaBytes}
		}
	}
	if ledger.hostCounted && !req.reservationHeld {
		pool := hostMemoryPool(ledger.hostMemoryBytes, cfg.ReservedSystemMemory, cfg.OvercommitFactor)
		builds := reservedBuildSlots(ledger.activeBuilds+max(req.builds, 0), cfg)
		projected := ledger.systemReservedMemoryBytes + builds*cfg.BuildMemoryBytes + memory
		if pool <= 0 || projected > pool {
			return admissionVerdict{limit: apperrors.LimitHostMemory, projected: projected, capacity: pool}
		}
		pool = hostCPUPool(ledger.hostLogicalCPUs, cfg.ReservedSystemCPU, cfg.CPUOvercommitFactor)
		projected = ledger.systemReservedCPUMillicores + builds*max(cfg.BuildCPUMillicores, 0) + cpu
		if pool <= 0 || projected > pool {
			return admissionVerdict{limit: apperrors.LimitHostCPU, projected: projected, capacity: pool}
		}
	}
	if ledger.diskFloorCounted {
		remaining := ledger.diskFreeBytes - projectedVolumePromiseBytes(ledger, req)
		if remaining < cfg.HostMinFreeDiskBytes {
			return admissionVerdict{limit: apperrors.LimitDiskFloor, projected: remaining, capacity: cfg.HostMinFreeDiskBytes}
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

// volumePromiseReader sums what every owner's volumes were promised and have
// not used yet.
type volumePromiseReader interface {
	GetUnusedVolumePromiseBytes(ctx context.Context) (int64, error)
}

// admissionSources are where a reservation ledger is read from. quotas is
// required; userReservations is read only for requests charged to the owner,
// system and builds only when host metrics are wired, and volumePromise only
// for requests that create volumes while the disk floor is counted. Absent
// images, volumes, host or disk metrics leave that part of the ledger
// uncounted.
type admissionSources struct {
	quotas           UserQuotaProvider
	userReservations userReservationReader
	system           systemReservationReader
	builds           activeSystemBuildCounter
	images           DiskQuotaImageRepository
	volumes          DiskQuotaVolumeRepository
	volumePromise    volumePromiseReader
	hostMetrics      HostMetricsProvider
	diskMetrics      HostDiskMetricsProvider
	dockerDataPath   string
	logger           *slog.Logger
}

// admit reads the reservation ledger for ownerID and checks req against it.
// The caller holds the capacity lock. operation names the work in logs.
func (s admissionSources) admit(ctx context.Context, ownerID uuid.UUID, req admissionRequest, cfg config.SystemConfig, operation string) error {
	ledger, err := s.readLedger(ctx, ownerID, req, cfg)
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

// readLedger reads what req is checked against. It reads the disk only when
// cfg enables the disk floor.
func (s admissionSources) readLedger(ctx context.Context, ownerID uuid.UUID, req admissionRequest, cfg config.SystemConfig) (reservationLedger, error) {
	var ledger reservationLedger
	quota, err := s.quotas.EffectiveUserQuota(ctx, ownerID)
	if err != nil {
		return ledger, err
	}
	if req.chargesUser {
		ledger.userMemoryQuotaBytes = quota.RAMMB * model.BytesPerMB
		ledger.userCPUQuotaMillicores = quota.CPUMillicores
		if ledger.userReservedMemoryBytes, err = s.userReservations.GetUserReservedMemory(ctx, ownerID); err != nil {
			return ledger, err
		}
		if ledger.userReservedCPUMillicores, err = s.userReservations.GetUserReservedCPU(ctx, ownerID); err != nil {
			return ledger, err
		}
	}
	if s.images != nil {
		ledger.userDiskCounted = true
		ledger.userDiskQuotaBytes = quota.DiskBytes()
		if ledger.userReservedDiskBytes, err = reservedDiskBytes(ctx, ownerID, s.images, s.volumes); err != nil {
			return ledger, err
		}
	}
	return ledger, s.readHostLedger(ctx, &ledger, req, cfg)
}

// readHostLedger reads the host part of the ledger into ledger: system
// reservations, active builds and host memory and CPU when host metrics are
// wired, and free disk, with the unused volume promise for a request that
// creates volumes, when cfg enables the disk floor. Monitoring reads its
// ledger here too, so its admission status cannot drift from a request's.
func (s admissionSources) readHostLedger(ctx context.Context, ledger *reservationLedger, req admissionRequest, cfg config.SystemConfig) error {
	var err error
	if s.hostMetrics != nil {
		ledger.hostCounted = true
		if ledger.systemReservedMemoryBytes, err = s.system.GetTotalSystemReservedMemory(ctx); err != nil {
			s.logReadFailure(ctx, "failed to calculate total system reserved memory", err)
			return apperrors.ErrInternal
		}
		if ledger.systemReservedCPUMillicores, err = s.system.GetTotalSystemReservedCPU(ctx); err != nil {
			s.logReadFailure(ctx, "failed to calculate total system reserved cpu", err)
			return apperrors.ErrInternal
		}
		activeBuilds, err := s.builds.CountActive(ctx)
		if err != nil {
			return err
		}
		ledger.activeBuilds = int64(activeBuilds)
		if ledger.hostMemoryBytes, err = s.hostMetrics.GetTotalMemory(); err != nil {
			s.logReadFailure(ctx, "failed to get system memory", err)
			return apperrors.ErrInternal
		}
		if ledger.hostLogicalCPUs, err = s.hostMetrics.GetLogicalCPUs(); err != nil {
			s.logReadFailure(ctx, "failed to get system cpu count", err)
			return apperrors.ErrInternal
		}
	}
	if s.diskMetrics != nil && cfg.HostMinFreeDiskBytes > 0 {
		path := s.dockerDataPath
		if path == "" {
			path = "/"
		}
		stats, err := s.diskMetrics.GetDiskUsage(path)
		if err != nil {
			return err
		}
		ledger.diskFloorCounted = true
		ledger.diskFreeBytes = stats.FreeBytes
		if req.volumeBytes > 0 && s.volumePromise != nil {
			if ledger.unusedVolumePromiseBytes, err = s.volumePromise.GetUnusedVolumePromiseBytes(ctx); err != nil {
				return err
			}
		}
	}
	return nil
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

// reservedBuildSlots is how many of activeBuilds, pending and running, hold
// host memory and CPU. Builder runs at most MaxConcurrentBuilds at once, so
// builds queued behind those reserve nothing until a slot frees.
func reservedBuildSlots(activeBuilds int64, cfg config.SystemConfig) int64 {
	return max(min(activeBuilds, int64(cfg.MaxConcurrentBuilds)), 0)
}

// buildClaimCap is how many builds may run at once: MaxConcurrentBuilds, or
// fewer when the free disk above HostMinFreeDiskBytes cannot hold that many
// build slots of BuildMaxDiskBytes each. It is never below zero. With a zero
// floor diskFreeBytes is not read and the cap is MaxConcurrentBuilds. The
// projection is conservative: diskFreeBytes already lacks what running builds
// wrote, and each of them is counted again as a whole slot.
func buildClaimCap(cfg config.SystemConfig, diskFreeBytes int64) int {
	limit := max(cfg.MaxConcurrentBuilds, 0)
	if cfg.HostMinFreeDiskBytes <= 0 || cfg.BuildMaxDiskBytes <= 0 {
		return limit
	}
	slots := max(diskFreeBytes-cfg.HostMinFreeDiskBytes, 0) / cfg.BuildMaxDiskBytes
	return int(min(slots, int64(limit)))
}

// projectedVolumePromiseBytes is the disk a volume request projects onto the
// host: the unused promise of every existing volume and the new volumes'
// sizes. Work that creates no volume projects none.
func projectedVolumePromiseBytes(ledger reservationLedger, req admissionRequest) int64 {
	if req.volumeBytes <= 0 {
		return 0
	}
	return ledger.unusedVolumePromiseBytes + req.volumeBytes
}
