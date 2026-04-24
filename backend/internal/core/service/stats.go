package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/google/uuid"
)

type StatsService struct {
	contRepo ContainerRepository
	volRepo  VolumeRepository
	imgRepo  ImageRepository
	projRepo ProjectRepository
	cfg      ConfigManager
	users    UserInfoProvider
}

func NewStatsService(
	contRepo ContainerRepository,
	volRepo VolumeRepository,
	imgRepo ImageRepository,
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

func (s *StatsService) GetUserStats(ctx context.Context, ownerID uuid.UUID) (domain.UserStats, error) {
	var stats domain.UserStats

	cfg := s.cfg.Get()
	stats.ContainersQuota = cfg.MaxContainersPerUser
	stats.VolumesQuota = cfg.MaxVolumesPerUser

	user, err := s.users.GetUser(ctx, ownerID)
	if err != nil {
		return domain.UserStats{}, err
	}
	stats.RamQuotaBytes = user.QuotaRAMMB * 1024 * 1024
	stats.RamUsedBytes, _ = s.contRepo.GetUserReservedMemory(ctx, ownerID)

	stats.ContainersTotal, _ = s.contRepo.CountByOwnerID(ctx, ownerID)
	containers, _ := s.contRepo.GetByOwnerID(ctx, ownerID)
	for _, c := range containers {
		if c.Status == domain.ContainerStatusRunning {
			stats.ContainersRunning++
		}
	}

	stats.DiskQuotaMB = int(user.QuotaDiskMB)
	diskUsed, _ := s.imgRepo.GetUserUsedDiskSpace(ctx, ownerID)
	stats.DiskUsedMB = int(diskUsed)

	images, _ := s.imgRepo.GetByOwnerID(ctx, ownerID)
	stats.ImagesTotal = len(images)

	stats.VolumesTotal, _ = s.volRepo.CountByOwnerID(ctx, ownerID)

	projects, _ := s.projRepo.GetByOwnerID(ctx, ownerID)
	stats.ProjectsTotal = len(projects)

	return stats, nil
}
