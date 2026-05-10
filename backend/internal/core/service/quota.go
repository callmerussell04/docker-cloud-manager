package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

type diskQuotaImageRepository interface {
	GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type diskQuotaVolumeRepository interface {
	GetUserUsedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

func usedDiskMB(ctx context.Context, ownerID uuid.UUID, imageRepo diskQuotaImageRepository, volumeRepo diskQuotaVolumeRepository) (int64, error) {
	usedMB, err := imageRepo.GetUserUsedDiskSpace(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	if volumeRepo != nil {
		usedBytes, err := volumeRepo.GetUserUsedVolumeBytes(ctx, ownerID)
		if err != nil {
			return 0, err
		}
		usedMB += bytesToMBRoundedUp(usedBytes)
	}
	return usedMB, nil
}

func ensureDiskQuotaAvailable(ctx context.Context, ownerID uuid.UUID, users UserInfoProvider, imageRepo diskQuotaImageRepository, volumeRepo diskQuotaVolumeRepository) error {
	if users == nil || imageRepo == nil {
		return nil
	}
	user, err := users.GetUser(ctx, ownerID)
	if err != nil {
		return err
	}
	usedMB, err := usedDiskMB(ctx, ownerID, imageRepo, volumeRepo)
	if err != nil {
		return err
	}
	if usedMB >= user.QuotaDiskMB {
		return apperrors.New(apperrors.ErrQuotaExceeded, "user disk quota exceeded")
	}
	return nil
}
