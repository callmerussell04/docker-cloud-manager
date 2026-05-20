package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

type CapacityChecker interface {
	CheckCapacity(ctx context.Context, ownerID uuid.UUID, requestedRam int64, projectedDiskWriteBytes int64) error
}

func (s *ContainerService) checkUserQuota(ctx context.Context, ownerID uuid.UUID, requestedRam int64) error {
	user, err := s.users.GetUser(ctx, ownerID)
	if err != nil {
		return err
	}
	userQuota := user.QuotaRAMMB * 1024 * 1024

	usedRam, err := s.repo.GetUserReservedMemory(ctx, ownerID)
	if err != nil {
		return err
	}

	if usedRam+requestedRam > userQuota {
		return apperrors.ErrQuotaExceeded
	}
	return nil
}

func (s *ContainerService) checkUserDiskQuota(ctx context.Context, ownerID uuid.UUID) error {
	imageRepo, _ := s.imageRepo.(containerImageDiskRepository)
	volumeRepo, _ := s.volumeRepo.(volumeDiskUsageRepository)
	return ensureDiskQuotaAvailable(ctx, ownerID, s.users, imageRepo, volumeRepo)
}

func (s *ContainerService) CheckCapacity(ctx context.Context, ownerID uuid.UUID, requestedRam int64, projectedDiskWriteBytes int64) error {
	unlock, err := s.acquireOwnerCapacityLock(ctx, ownerID)
	if err != nil {
		return err
	}
	if unlock != nil {
		defer unlock()
	}
	if err := s.checkUserQuota(ctx, ownerID, requestedRam); err != nil {
		return err
	}
	if err := s.checkUserDiskQuota(ctx, ownerID); err != nil {
		return err
	}
	if err := s.checkHostCapacity(ctx, requestedRam); err != nil {
		return err
	}
	return s.checkHostDiskCapacityForWrite(projectedDiskWriteBytes)
}

func (s *ContainerService) checkHostCapacity(ctx context.Context, requestedRam int64) error {
	totalRunningReserved, err := s.repo.GetTotalSystemReservedMemory(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to calculate total system reserved memory", "error", err)
		return apperrors.ErrInternal
	}
	return ensureHostMemoryCapacity(ctx, s.metrics, s.config.Get(), totalRunningReserved, requestedRam, s.logger, "container")
}

func (s *ContainerService) checkHostDiskCapacity() error {
	return s.checkHostDiskCapacityForWrite(0)
}

func (s *ContainerService) checkHostDiskCapacityForWrite(projectedWriteBytes int64) error {
	diskMetrics, ok := s.metrics.(HostDiskMetricsProvider)
	if !ok {
		return nil
	}
	return ensureHostDiskFloorProjected(diskMetrics, s.hostDiskPath, s.config.Get().HostMinFreeDiskBytes, projectedWriteBytes)
}
