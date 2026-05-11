package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

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

func (s *ContainerService) checkHostCapacity(ctx context.Context, requestedRam int64) error {
	totalMem, err := s.metrics.GetTotalMemory()
	if err != nil {
		s.logger.Error("failed to get system memory", "error", err)
		return apperrors.ErrInternal
	}

	availablePool := float64(totalMem-s.config.Get().ReservedSystemMemory) * s.config.Get().OvercommitFactor
	if availablePool <= 0 {
		return apperrors.ErrHostExhausted
	}

	totalRunningReserved, err := s.repo.GetTotalSystemReservedMemory(ctx)
	if err != nil {
		s.logger.Error("failed to calculate total system reserved memory", "error", err)
		return apperrors.ErrInternal
	}

	projectedRequiredMem := totalRunningReserved + requestedRam
	if projectedRequiredMem > int64(availablePool) {
		s.logger.Warn(
			"container request rejected by host capacity",
			"projected_memory_mb", projectedRequiredMem/1024/1024,
			"max_pool_mb", int64(availablePool)/1024/1024,
		)
		return apperrors.ErrHostExhausted
	}

	return nil
}

func (s *ContainerService) checkHostDiskCapacity() error {
	diskMetrics, ok := s.metrics.(HostDiskMetricsProvider)
	if !ok {
		return nil
	}
	return ensureHostDiskFloor(diskMetrics, s.hostDiskPath, s.config.Get().HostMinFreeDiskBytes)
}
