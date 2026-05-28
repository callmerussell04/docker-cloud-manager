package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
)

func (s *ContainerService) RebalanceResources(ctx context.Context) {
	cfg := s.config.Get()
	runningContainers, err := s.repo.GetRunning(ctx)
	if err != nil || len(runningContainers) == 0 {
		return
	}

	totalMem, err := s.metrics.GetTotalMemory()
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to read system memory for rebalancing", "error", err)
		return
	}
	logicalCPUs, err := s.metrics.GetLogicalCPUs()
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to read system cpu count for rebalancing", "error", err)
		return
	}

	activeBuildMemory, activeBuildCPU := s.activeBuildReservations(ctx, cfg)
	availableForBurst := totalMem - cfg.ReservedSystemMemory - activeBuildMemory
	availableCPUForBurst := logicalCPUs*1000 - cfg.ReservedSystemCPU - activeBuildCPU
	var totalReservedMemory int64
	var totalReservedCPU int64
	for _, c := range runningContainers {
		totalReservedMemory += normalizeMemoryReservation(c.BaseMemoryReservation, cfg)
		totalReservedCPU += normalizeCPUReservation(c.BaseCPUReservation, cfg)
	}

	memoryBurstFactor := 1.0
	if totalReservedMemory > 0 && availableForBurst > totalReservedMemory {
		memoryBurstFactor = float64(availableForBurst) / float64(totalReservedMemory)
	}
	cpuBurstFactor := 1.0
	if totalReservedCPU > 0 && availableCPUForBurst > totalReservedCPU {
		cpuBurstFactor = float64(availableCPUForBurst) / float64(totalReservedCPU)
	}

	for _, c := range runningContainers {
		if c.DockerID == "" {
			continue
		}
		memoryReservation := normalizeMemoryReservation(c.BaseMemoryReservation, cfg)
		cpuReservation := normalizeCPUReservation(c.BaseCPUReservation, cfg)
		newMemoryLimit := int64(float64(memoryReservation) * memoryBurstFactor)
		maxAllowedBurst := memoryReservation * cfg.MaxBurstMultiplier
		if newMemoryLimit > maxAllowedBurst {
			newMemoryLimit = maxAllowedBurst
		}

		targetCPU := int64(float64(cpuReservation) * cpuBurstFactor)
		maxAllowedCPU := cpuReservation * cfg.MaxCPUBurstMultiplier
		if targetCPU > maxAllowedCPU {
			targetCPU = maxAllowedCPU
		}

		err := s.dockerAPI.UpdateContainerResources(ctx, c.DockerID, model.ContainerResourceUpdate{
			MemoryLimitBytes:     newMemoryLimit,
			MemoryReservation:    memoryReservation,
			MemorySwapMultiplier: cfg.ContainerMemorySwapMultiplier,
			CPUShares:            cpuSharesForReservation(cpuReservation, len(runningContainers), cfg),
			CPUQuota:             cpuQuotaFromMillicores(targetCPU, cfg.ContainerCPUPeriod),
			CPUPeriod:            cfg.ContainerCPUPeriod,
		})
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to update container resources", "container_id", c.ID, "docker_id", c.DockerID, "error", err)
		}
	}
	s.logger.InfoContext(ctx, "containers rebalanced", "container_count", len(runningContainers), "memory_burst_factor", memoryBurstFactor, "cpu_burst_factor", cpuBurstFactor)
}

func (s *ContainerService) RequestRebalance() {
	select {
	case s.rebalanceCh <- struct{}{}:
	default:
	}
}

func (s *ContainerService) RunRebalancer(ctx context.Context) {
	s.logger.InfoContext(ctx, "resource rebalancer started")
	s.RequestRebalance()
	for {
		select {
		case <-ctx.Done():
			s.logger.InfoContext(ctx, "resource rebalancer stopped")
			return
		case <-s.rebalanceCh:
			s.RebalanceResources(ctx)
		}
	}
}

func (s *ContainerService) activeBuildReservations(ctx context.Context, cfg config.SystemConfig) (int64, int64) {
	if s.builds == nil {
		return 0, 0
	}
	activeBuilds, err := s.builds.CountActive(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to count active builds for rebalancing", "error", err)
		return 0, 0
	}
	return int64(activeBuilds) * cfg.BuildMemoryBytes, int64(activeBuilds) * buildCPUMillicores(cfg)
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

func cpuSharesForReservation(cpuReservation int64, runningCount int, cfg config.SystemConfig) int64 {
	baseShares := cfg.DefaultCPUShares
	if runningCount > cfg.HighLoadContainerCount {
		baseShares = cfg.HighLoadCPUShares
	}
	if cfg.DefaultCPUReservation > 0 && cpuReservation > 0 {
		baseShares = baseShares * cpuReservation / cfg.DefaultCPUReservation
	}
	if baseShares < 2 {
		return 2
	}
	return baseShares
}
