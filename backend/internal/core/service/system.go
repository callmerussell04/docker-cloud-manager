package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/validation"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type SystemService struct {
	cfgManager *config.Manager
}

func NewSystemService(cfgManager *config.Manager) *SystemService {
	return &SystemService{cfgManager: cfgManager}
}

func (s *SystemService) GetConfig(ctx context.Context) config.SystemConfig {
	return s.cfgManager.Get()
}

func (s *SystemService) UpdateConfig(ctx context.Context, newConfig config.SystemConfig) error {
	if err := validateSystemConfig(newConfig); err != nil {
		return err
	}
	return s.cfgManager.Update(newConfig)
}

func validateSystemConfig(cfg config.SystemConfig) error {
	if cfg.BaseDomain == "" || strings.ContainsAny(cfg.BaseDomain, " `/\\") {
		return fmt.Errorf("%w: invalid base domain", apperrors.ErrBadRequest)
	}
	if cfg.DefaultMemoryReservation <= 0 || cfg.ReservedSystemMemory < 0 {
		return fmt.Errorf("%w: memory limits must be positive", apperrors.ErrBadRequest)
	}
	if cfg.OvercommitFactor <= 0 || cfg.OvercommitFactor > 10 {
		return fmt.Errorf("%w: overcommit factor must be between 0 and 10", apperrors.ErrBadRequest)
	}
	if cfg.MaxBurstMultiplier <= 0 || cfg.MaxBurstMultiplier > 10 {
		return fmt.Errorf("%w: max burst multiplier must be between 1 and 10", apperrors.ErrBadRequest)
	}
	if cfg.DefaultCPUShares <= 0 || cfg.HighLoadCPUShares <= 0 {
		return fmt.Errorf("%w: cpu shares must be positive", apperrors.ErrBadRequest)
	}
	if cfg.HighLoadContainerCount <= 0 || cfg.ContainerStopTimeout <= 0 {
		return fmt.Errorf("%w: container counters and timeouts must be positive", apperrors.ErrBadRequest)
	}
	if err := validation.DockerSize(cfg.MaxLogSize); err != nil {
		return fmt.Errorf("%w: invalid max log size", apperrors.ErrBadRequest)
	}
	if err := validation.PositiveNumberString(cfg.MaxLogFiles); err != nil {
		return fmt.Errorf("%w: invalid max log files", apperrors.ErrBadRequest)
	}
	if err := validation.DockerSize(cfg.ContainerDiskQuota); err != nil {
		return fmt.Errorf("%w: invalid container disk quota", apperrors.ErrBadRequest)
	}
	if cfg.MaxVolumesPerUser <= 0 || cfg.MaxContainersPerUser <= 0 {
		return fmt.Errorf("%w: user resource limits must be positive", apperrors.ErrBadRequest)
	}
	if cfg.RegistryAPIURL == "" || cfg.RegistryPublicURL == "" {
		return fmt.Errorf("%w: registry urls are required", apperrors.ErrBadRequest)
	}
	if cfg.ContainerTTL < 0 {
		return fmt.Errorf("%w: container ttl must not be negative", apperrors.ErrBadRequest)
	}
	return nil
}
