package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/stagedsource"
	"github.com/google/uuid"
)

type CapacityChecker interface {
	CheckCapacity(ctx context.Context, ownerID uuid.UUID, requestedRam int64, requestedCPU int64, projectedDiskWriteBytes int64) error
}

// CheckCapacity admits memory, CPU and a projected disk write for ownerID
// under the owner capacity lock, and releases the lock: it reserves nothing.
// Project start preflight and Compose deployment use it.
func (s *ContainerService) CheckCapacity(ctx context.Context, ownerID uuid.UUID, requestedRam int64, requestedCPU int64, projectedDiskWriteBytes int64) error {
	unlock, err := s.acquireOwnerCapacityLock(ctx, ownerID)
	if err != nil {
		return err
	}
	if unlock != nil {
		defer unlock()
	}
	return s.admit(ctx, ownerID, admissionRequest{
		memoryBytes:    requestedRam,
		cpuMillicores:  requestedCPU,
		chargesUser:    true,
		diskWriteBytes: projectedDiskWriteBytes,
	})
}

// admit checks a request charged to ownerID. The caller holds the owner
// capacity lock.
func (s *ContainerService) admit(ctx context.Context, ownerID uuid.UUID, req admissionRequest) error {
	sources := admissionSources{
		quotas:           s.quotas,
		userReservations: s.repo,
		system:           s.repo,
		builds:           s.builds,
		images:           s.imageRepo,
		volumes:          s.volumeRepo,
		hostMetrics:      s.metrics,
		diskMetrics:      s.diskMetrics,
		hostDiskPath:     s.hostDiskPath,
		logger:           s.logger,
	}
	return sources.admit(ctx, ownerID, req, s.config.Get(), "container")
}

func (s *ContainerService) checkHostDiskCapacity() error {
	return stagedsource.CheckHostDiskFloor(s.diskMetrics, s.hostDiskPath, s.config.Get().HostMinFreeDiskBytes, 0)
}
