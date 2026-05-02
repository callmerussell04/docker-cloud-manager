package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/google/uuid"
)

type StatsImageRepository interface {
	List(ctx context.Context, opts model.ListOptions) ([]model.Image, int, error)
	GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error)
}

type StatsService struct {
	contRepo ContainerRepository
	volRepo  VolumeRepository
	imgRepo  StatsImageRepository
	projRepo ProjectRepository
	cfg      ConfigManager
	users    UserInfoProvider
}

func NewStatsService(
	contRepo ContainerRepository,
	volRepo VolumeRepository,
	imgRepo StatsImageRepository,
	projRepo ProjectRepository,
	cfg ConfigManager,
	users UserInfoProvider,
) *StatsService {
	return &StatsService{
		contRepo: contRepo,
		volRepo:  volRepo,
		imgRepo:  imgRepo,
		projRepo: projRepo,
		cfg:      cfg,
		users:    users,
	}
}

func (s *StatsService) GetUserStats(ctx context.Context) (model.UserStats, error) {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return model.UserStats{}, err
	}
	var stats model.UserStats

	cfg := s.cfg.Get()
	stats.ContainersQuota = cfg.MaxContainersPerUser
	stats.VolumesQuota = cfg.MaxVolumesPerUser

	user, err := s.users.GetUser(ctx, ownerID)
	if err != nil {
		return model.UserStats{}, err
	}
	stats.RamQuotaBytes = user.QuotaRAMMB * 1024 * 1024
	stats.RamUsedBytes, err = s.contRepo.GetUserReservedMemory(ctx, ownerID)
	if err != nil {
		return model.UserStats{}, err
	}

	stats.ContainersTotal, err = s.contRepo.CountByOwnerID(ctx, ownerID)
	if err != nil {
		return model.UserStats{}, err
	}
	containers, _, err := s.contRepo.List(ctx, model.ListOptions{OwnerID: &ownerID})
	if err != nil {
		return model.UserStats{}, err
	}
	for _, c := range containers {
		if c.Status == model.ContainerStatusRunning {
			stats.ContainersRunning++
		}
	}

	stats.DiskQuotaMB = int(user.QuotaDiskMB)
	diskUsed, err := s.imgRepo.GetUserUsedDiskSpace(ctx, ownerID)
	if err != nil {
		return model.UserStats{}, err
	}
	if volumeRepo, ok := s.volRepo.(volumeDiskUsageRepository); ok {
		volumeBytes, err := volumeRepo.GetUserUsedVolumeBytes(ctx, ownerID)
		if err != nil {
			return model.UserStats{}, err
		}
		diskUsed += bytesToMBRoundedUp(volumeBytes)
	}
	stats.DiskUsedMB = int(diskUsed)

	_, imagesTotal, err := s.imgRepo.List(ctx, model.ListOptions{OwnerID: &ownerID})
	if err != nil {
		return model.UserStats{}, err
	}
	stats.ImagesTotal = imagesTotal

	stats.VolumesTotal, err = s.volRepo.CountByOwnerID(ctx, ownerID)
	if err != nil {
		return model.UserStats{}, err
	}

	_, projectsTotal, err := s.projRepo.List(ctx, model.ListOptions{OwnerID: &ownerID})
	if err != nil {
		return model.UserStats{}, err
	}
	stats.ProjectsTotal = projectsTotal

	return stats, nil
}
