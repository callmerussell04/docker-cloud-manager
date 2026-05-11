package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

type StagedObjectRepository interface {
	Reserve(ctx context.Context, reservation model.StagedObjectReservation, maxBytesPerUser int64) error
	UpdateBytes(ctx context.Context, objectKey string, bytesReserved int64) error
	Release(ctx context.Context, objectKey string) error
	ActiveBytesByOwner(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type HostDiskMetricsProvider interface {
	GetDiskUsage(path string) (model.HostDiskStats, error)
}

func ensureHostDiskFloor(metrics HostDiskMetricsProvider, path string, minFreeBytes int64) error {
	if metrics == nil || minFreeBytes <= 0 {
		return nil
	}
	if path == "" {
		path = "/"
	}
	stats, err := metrics.GetDiskUsage(path)
	if err != nil {
		return err
	}
	if stats.FreeBytes < minFreeBytes {
		return apperrors.ErrHostExhausted
	}
	return nil
}

func stagedBytesToMB(bytes int64) int64 {
	return bytesToMBRoundedUp(bytes)
}
