package service

import "context"

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

	availableForBurst := totalMem - cfg.ReservedSystemMemory
	var totalReserved int64
	for _, c := range runningContainers {
		totalReserved += c.BaseMemoryReservation
	}

	burstFactor := 1.0
	if totalReserved > 0 && availableForBurst > totalReserved {
		burstFactor = float64(availableForBurst) / float64(totalReserved)
	}

	for _, c := range runningContainers {
		newMemoryLimit := int64(float64(c.BaseMemoryReservation) * burstFactor)
		maxAllowedBurst := c.BaseMemoryReservation * cfg.MaxBurstMultiplier
		if newMemoryLimit > maxAllowedBurst {
			newMemoryLimit = maxAllowedBurst
		}

		cpuShares := cfg.DefaultCPUShares
		if len(runningContainers) > cfg.HighLoadContainerCount {
			cpuShares = cfg.HighLoadCPUShares
		}

		err := s.dockerAPI.UpdateContainerResources(ctx, c.DockerID, newMemoryLimit, c.BaseMemoryReservation, cpuShares, cfg.ContainerMemorySwapMultiplier)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to update container resources", "container_id", c.ID, "docker_id", c.DockerID, "error", err)
		}
	}
	s.logger.InfoContext(ctx, "containers rebalanced", "container_count", len(runningContainers), "burst_factor", burstFactor)
}

func (s *ContainerService) RequestRebalance() {
	select {
	case s.rebalanceCh <- struct{}{}:
	default:
	}
}

func (s *ContainerService) RunRebalancer(ctx context.Context) {
	s.logger.InfoContext(ctx, "resource rebalancer started")
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
