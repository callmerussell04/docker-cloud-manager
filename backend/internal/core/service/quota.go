package service

import (
	"context"

	"github.com/google/uuid"
)

// DiskQuotaImageRepository reads the disk an owner's images use.
type DiskQuotaImageRepository interface {
	GetUserImageBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

// DiskQuotaVolumeRepository reads the disk an owner's volumes reserve.
type DiskQuotaVolumeRepository interface {
	GetUserReservedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

// reservedDiskBytes is the disk reserved by ownerID, in bytes: the disk their
// images use plus, for every volume not being deleted, its size or its
// measured usage when that is larger. Admission, builds and user stats all
// read it here.
func reservedDiskBytes(ctx context.Context, ownerID uuid.UUID, imageRepo DiskQuotaImageRepository, volumeRepo DiskQuotaVolumeRepository) (int64, error) {
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

// ReservedDiskReader reads reservedDiskBytes for the staged disk quota check,
// which builds and Compose ingestion both make through the staged-source
// module.
type ReservedDiskReader struct {
	images  DiskQuotaImageRepository
	volumes DiskQuotaVolumeRepository
}

// NewReservedDiskReader reads reserved disk from images and volumes. A nil
// volumes repository counts images only.
func NewReservedDiskReader(images DiskQuotaImageRepository, volumes DiskQuotaVolumeRepository) ReservedDiskReader {
	return ReservedDiskReader{images: images, volumes: volumes}
}

// ReservedDiskBytes is the disk ownerID reserves, in bytes.
func (r ReservedDiskReader) ReservedDiskBytes(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	return reservedDiskBytes(ctx, ownerID, r.images, r.volumes)
}
