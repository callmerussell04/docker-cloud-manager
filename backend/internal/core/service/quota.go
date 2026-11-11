package service

import (
	"context"

	"github.com/google/uuid"
)

type diskQuotaImageRepository interface {
	GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type diskQuotaVolumeRepository interface {
	GetUserReservedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

// reservedDiskMB is the disk reserved by ownerID, in MB: the disk their images
// use plus, for every volume not being deleted, its size or its measured usage
// when that is larger. Admission, builds and user stats all read it here.
func reservedDiskMB(ctx context.Context, ownerID uuid.UUID, imageRepo diskQuotaImageRepository, volumeRepo diskQuotaVolumeRepository) (int64, error) {
	reservedMB, err := imageRepo.GetUserUsedDiskSpace(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	if volumeRepo != nil {
		volumeBytes, err := volumeRepo.GetUserReservedVolumeBytes(ctx, ownerID)
		if err != nil {
			return 0, err
		}
		reservedMB += bytesToMBRoundedUp(volumeBytes)
	}
	return reservedMB, nil
}
