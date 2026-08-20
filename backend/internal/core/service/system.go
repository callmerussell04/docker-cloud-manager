package service

import (
	"context"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type SystemService struct {
	cfgManager *config.Manager
	rebalancer configRebalancer
}

func NewSystemService(cfgManager *config.Manager) *SystemService {
	return &SystemService{cfgManager: cfgManager}
}

type configRebalancer interface {
	RequestRebalance()
}

func (s *SystemService) SetRebalancer(rebalancer configRebalancer) {
	s.rebalancer = rebalancer
}

func (s *SystemService) GetConfig(ctx context.Context) config.SystemConfig {
	return s.cfgManager.Get()
}

func (s *SystemService) UpdateConfig(ctx context.Context, newConfig config.SystemConfig) error {
	oldConfig := s.cfgManager.Get()
	if err := config.ValidateSystemConfig(newConfig); err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	if err := s.cfgManager.Update(newConfig); err != nil {
		return fmt.Errorf("failed to update system config: %w", err)
	}
	if s.rebalancer != nil && resourceConfigChanged(oldConfig, newConfig) {
		s.rebalancer.RequestRebalance()
	}
	return nil
}

func resourceConfigChanged(oldConfig, newConfig config.SystemConfig) bool {
	return oldConfig.DefaultMemoryReservation != newConfig.DefaultMemoryReservation ||
		oldConfig.ReservedSystemMemory != newConfig.ReservedSystemMemory ||
		oldConfig.MaxBurstMultiplier != newConfig.MaxBurstMultiplier ||
		oldConfig.ContainerMemorySwapMultiplier != newConfig.ContainerMemorySwapMultiplier ||
		oldConfig.DefaultCPUReservation != newConfig.DefaultCPUReservation ||
		oldConfig.ReservedSystemCPU != newConfig.ReservedSystemCPU ||
		oldConfig.CPUOvercommitFactor != newConfig.CPUOvercommitFactor ||
		oldConfig.MaxCPUBurstMultiplier != newConfig.MaxCPUBurstMultiplier ||
		oldConfig.ContainerCPUPeriod != newConfig.ContainerCPUPeriod ||
		oldConfig.DefaultCPUShares != newConfig.DefaultCPUShares ||
		oldConfig.HighLoadCPUShares != newConfig.HighLoadCPUShares ||
		oldConfig.HighLoadContainerCount != newConfig.HighLoadContainerCount ||
		oldConfig.BuildMemoryBytes != newConfig.BuildMemoryBytes ||
		oldConfig.BuildCPUQuota != newConfig.BuildCPUQuota ||
		oldConfig.BuildCPUPeriod != newConfig.BuildCPUPeriod
}
