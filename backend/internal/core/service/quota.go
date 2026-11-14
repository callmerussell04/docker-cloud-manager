package service

import (
	"context"

	"github.com/google/uuid"
)

type diskQuotaImageRepository interface {
	GetUserImageBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type diskQuotaVolumeRepository interface {
	GetUserReservedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

// reservedDiskBytes is the disk reserved by ownerID, in bytes: the disk their
// images use plus, for every volume not being deleted, its size or its
// measured usage when that is larger. Admission, builds and user stats all
// read it here.
func reservedDiskBytes(ctx context.Context, ownerID uuid.UUID, imageRepo diskQuotaImageRepository, volumeRepo diskQuotaVolumeRepository) (int64, error) {
	reserved, err := imageRepo.GetUserImageBytes(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	if volumeRepo != nil {
		volumeBytes, err := volumeRepo.GetUserReservedVolumeBytes(ctx, ownerID)
		if err != nil {
			return 0, err
		}
		reserved += volumeBytes
	}
	return reserved, nil
}

// reservedDiskSource reads reservedDiskBytes for the staged disk quota check.
type reservedDiskSource struct {
	images  diskQuotaImageRepository
	volumes diskQuotaVolumeRepository
}

func (r reservedDiskSource) ReservedDiskBytes(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	return reservedDiskBytes(ctx, ownerID, r.images, r.volumes)
}
