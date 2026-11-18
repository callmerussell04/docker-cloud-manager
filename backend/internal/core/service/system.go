package service

import (
	"context"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
)

// SystemVolumeSizeSupport asks the host whether it can enforce volume sizes.
// A false answer with no error means Docker refused a volume size.
type SystemVolumeSizeSupport interface {
	CheckVolumeSizeSupport(ctx context.Context) (bool, error)
}

type SystemService struct {
	cfgManager        *config.Manager
	volumeSizeSupport SystemVolumeSizeSupport
	hostCapacity      SystemHostCapacity
}

// SystemServiceDeps are what the system service needs; all are required.
type SystemServiceDeps struct {
	Config            *config.Manager
	VolumeSizeSupport SystemVolumeSizeSupport
	HostCapacity      SystemHostCapacity
}

func NewSystemService(deps SystemServiceDeps) (*SystemService, error) {
	required := []struct {
		name  string
		value any
	}{
		{"config", deps.Config}, {"volume size support", deps.VolumeSizeSupport},
		{"host capacity", deps.HostCapacity},
	}
	for _, dependency := range required {
		if dependency.value == nil || isNilServiceDependency(dependency.value) {
			return nil, fmt.Errorf("system service: %s is required", dependency.name)
		}
	}
	return &SystemService{
		cfgManager: deps.Config, volumeSizeSupport: deps.VolumeSizeSupport, hostCapacity: deps.HostCapacity,
	}, nil
}

func (s *SystemService) GetConfig(ctx context.Context) config.SystemConfig {
	return s.cfgManager.Get()
}

// UpdateConfig validates and applies newConfig, saving nothing when it
// rejects it. An update that changes a capacity setting is checked against the
// host: a system reservation that leaves no pool, or build slots above it, are
// a bad request. Turning volume size enforcement on first checks that the host
// supports volume sizes and rejects the update when it does not; turning it
// off or leaving it unchanged checks nothing. A host check that cannot run is
// unavailable.
func (s *SystemService) UpdateConfig(ctx context.Context, newConfig config.SystemConfig) error {
	if err := config.ValidateSystemConfig(newConfig); err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	current := s.cfgManager.Get()
	if err := checkHostCapacitySettings(s.hostCapacity, current, newConfig); err != nil {
		return err
	}
	if newConfig.VolumeSizeEnforcement && !current.VolumeSizeEnforcement {
		supported, err := s.volumeSizeSupport.CheckVolumeSizeSupport(ctx)
		if err != nil {
			return apperrors.Wrap(apperrors.ErrUnavailable,
				"volume size enforcement cannot be turned on: the host check could not run; try again", err)
		}
		if !supported {
			return apperrors.New(apperrors.ErrBadRequest,
				"volume size enforcement cannot be turned on: this host does not support volume sizes "+
					"(Docker's data directory needs a filesystem with project quotas)")
		}
	}
	if err := s.cfgManager.Update(newConfig); err != nil {
		return fmt.Errorf("failed to update system config: %w", err)
	}
	return nil
}
