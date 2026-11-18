package service

import (
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
)

// Docker's bounds for a container's CPU shares.
const (
	minCPUShares = 2
	maxCPUShares = 262144
)

// containerResources is the memory and CPU a container runs with. The
// reservation is what admission promised: Docker protects that much memory
// from reclaim and weighs CPU by it under contention. The limits are the
// reservation times the burst multipliers. Both depend only on the
// reservation and cfg, so create and every start apply the same values. The
// memory limit is the container's standard limit: while it runs, only the
// memory pressure worker moves it, between the standard limit and the boost
// ceiling.
func containerResources(c model.Container, cfg config.SystemConfig) model.ContainerResourceUpdate {
	memory := normalizeMemoryReservation(c.BaseMemoryReservation, cfg)
	cpu := normalizeCPUReservation(c.BaseCPUReservation, cfg)
	return model.ContainerResourceUpdate{
		MemoryLimitBytes:     memory * max(cfg.MaxBurstMultiplier, 1),
		MemoryReservation:    memory,
		MemorySwapMultiplier: cfg.ContainerMemorySwapMultiplier,
		CPUShares:            cpuSharesForReservation(cpu, cfg),
		CPUQuota:             cpuQuotaFromMillicores(cpu*max(cfg.MaxCPUBurstMultiplier, 1), cfg.ContainerCPUPeriod),
		CPUPeriod:            cfg.ContainerCPUPeriod,
	}
}

// memoryBoostCeiling is the highest memory limit memory boost may give a
// running container: its reservation times MemoryBoostMultiplier.
func memoryBoostCeiling(c model.Container, cfg config.SystemConfig) int64 {
	return normalizeMemoryReservation(c.BaseMemoryReservation, cfg) * cfg.MemoryBoostMultiplier
}

// cpuBoostCeiling is the highest CPU quota CPU boost may give a running
// container: its reservation times CPUBoostMultiplier.
func cpuBoostCeiling(c model.Container, cfg config.SystemConfig) int64 {
	return cpuQuotaFromMillicores(normalizeCPUReservation(c.BaseCPUReservation, cfg)*cfg.CPUBoostMultiplier, cfg.ContainerCPUPeriod)
}

func normalizeMemoryReservation(value int64, cfg config.SystemConfig) int64 {
	if value > 0 {
		return value
	}
	return cfg.DefaultMemoryReservation
}

func normalizeCPUReservation(value int64, cfg config.SystemConfig) int64 {
	if value > 0 {
		return value
	}
	return cfg.DefaultCPUReservation
}

func cpuQuotaFromMillicores(millicores, period int64) int64 {
	if millicores <= 0 || period <= 0 {
		return 0
	}
	return millicores * period / 1000
}

// cpuSharesForReservation gives the default reservation DefaultCPUShares and
// scales other reservations in proportion, within Docker's bounds.
func cpuSharesForReservation(cpuReservation int64, cfg config.SystemConfig) int64 {
	shares := cfg.DefaultCPUShares
	if cfg.DefaultCPUReservation > 0 && cpuReservation > 0 {
		shares = shares * cpuReservation / cfg.DefaultCPUReservation
	}
	return min(max(shares, minCPUShares), maxCPUShares)
}

// chosenReservation returns the memory, in bytes, and the CPU, in millicores,
// a new container reserves: the owner's figures, or the defaults of cfg for a
// figure of zero. A figure outside the reservation bounds is a bad request
// whose message does not echo it. A figure above the owner's quota is within
// the bounds and left to admission.
func chosenReservation(params model.ContainerCreateParams, cfg config.SystemConfig) (memoryBytes, cpuMillicores int64, err error) {
	memoryBytes, cpuMillicores = cfg.DefaultMemoryReservation, cfg.DefaultCPUReservation
	if mb := params.MemoryReservationMB; mb != 0 {
		if mb < config.MinMemoryReservationBytes/bytesPerMB || mb > config.MaxMemoryReservationBytes/bytesPerMB {
			return 0, 0, apperrors.New(apperrors.ErrBadRequest, "memory reservation must be between 16 MiB and 1 TiB")
		}
		memoryBytes = mb * bytesPerMB
	}
	if mc := params.CPUReservationMillicores; mc != 0 {
		if mc < config.MinCPUReservationMillicores || mc > config.MaxCPUReservationMillicores {
			return 0, 0, apperrors.New(apperrors.ErrBadRequest, "cpu reservation must be between 10 millicores and the largest cpu quota")
		}
		cpuMillicores = mc
	}
	return memoryBytes, cpuMillicores, nil
}
