package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/google/uuid"
)

type StagedObjectRepository interface {
	Reserve(ctx context.Context, reservation model.StagedObjectReservation) error
	UpdateBytes(ctx context.Context, objectKey string, bytesReserved int64) error
	Release(ctx context.Context, objectKey string) error
	ActiveBytesByOwner(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type HostDiskMetricsProvider interface {
	GetDiskUsage(path string) (model.HostDiskStats, error)
}
