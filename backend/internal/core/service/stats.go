package service

import (
	"context"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/google/uuid"
)

const bytesPerMB = 1024 * 1024

type StatsContainerRepository interface {
	GetUserReservedMemory(ctx context.Context, ownerID uuid.UUID) (int64, error)
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	List(ctx context.Context, opts model.ListOptions) ([]model.Container, int, error)
	GetTotalSystemReservedMemory(ctx context.Context) (int64, error)
	GetSystemStatusCounts(ctx context.Context) (model.ContainerStatusCounts, error)
}

type StatsVolumeRepository interface {
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	GetUserUsedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetTotalUsedVolumeBytes(ctx context.Context) (int64, error)
	CountAll(ctx context.Context) (int, error)
}

type StatsImageRepository interface {
	GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetTotalUsedDiskSpace(ctx context.Context) (int64, error)
	List(ctx context.Context, opts model.ListOptions) ([]model.Image, int, error)
	CountAll(ctx context.Context) (int, error)
}

type StatsBuildRepository interface {
	CountAll(ctx context.Context) (int, error)
}

type statsActiveBuildRepository interface {
	CountActive(ctx context.Context) (int, error)
}

type StatsProjectRepository interface {
	List(ctx context.Context, opts model.ListOptions) ([]model.Project, int, error)
	CountAll(ctx context.Context) (int, error)
}

type StatsMetricsProvider interface {
	GetCPULoad() (float64, error)
	GetMemoryStats() (model.HostMemoryStats, error)
	GetDiskUsage(path string) (model.HostDiskStats, error)
}

type StatsService struct {
	contRepo     StatsContainerRepository
	volRepo      StatsVolumeRepository
	imgRepo      StatsImageRepository
	buildRepo    StatsBuildRepository
	projRepo     StatsProjectRepository
	metrics      StatsMetricsProvider
	hostDiskPath string
	cfg          ConfigManager
	users        UserInfoProvider
}

func NewStatsService(
	contRepo StatsContainerRepository,
	volRepo StatsVolumeRepository,
	imgRepo StatsImageRepository,
	buildRepo StatsBuildRepository,
	projRepo StatsProjectRepository,
	metrics StatsMetricsProvider,
	hostDiskPath string,
	cfg ConfigManager,
	users UserInfoProvider,
) *StatsService {
	if hostDiskPath == "" {
		hostDiskPath = "/"
	}
	return &StatsService{
		contRepo:     contRepo,
		volRepo:      volRepo,
		imgRepo:      imgRepo,
		buildRepo:    buildRepo,
		projRepo:     projRepo,
		metrics:      metrics,
		hostDiskPath: hostDiskPath,
		cfg:          cfg,
		users:        users,
	}
}

func (s *StatsService) GetUserStats(ctx context.Context) (model.UserStats, error) {
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return model.UserStats{}, err
	}
	var stats model.UserStats

	cfg := s.monitoringConfig()
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
	volumeBytes, err := s.volRepo.GetUserUsedVolumeBytes(ctx, ownerID)
	if err != nil {
		return model.UserStats{}, err
	}
	diskUsed += bytesToMBRoundedUp(volumeBytes)
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

func (s *StatsService) GetSystemMonitoring(ctx context.Context) (model.SystemMonitoring, error) {
	cpuPercent, err := s.metrics.GetCPULoad()
	if err != nil {
		return model.SystemMonitoring{}, err
	}

	memory, err := s.metrics.GetMemoryStats()
	if err != nil {
		return model.SystemMonitoring{}, err
	}

	disk, err := s.metrics.GetDiskUsage(s.hostDiskPath)
	if err != nil {
		return model.SystemMonitoring{}, err
	}

	reservedMemory, err := s.contRepo.GetTotalSystemReservedMemory(ctx)
	if err != nil {
		return model.SystemMonitoring{}, err
	}
	activeBuilds := 0
	if activeRepo, ok := s.buildRepo.(statsActiveBuildRepository); ok {
		activeBuilds, err = activeRepo.CountActive(ctx)
		if err != nil {
			return model.SystemMonitoring{}, err
		}
	}
	cfg := s.monitoringConfig()
	reservedBuildMemory := int64(activeBuilds) * cfg.BuildMemoryBytes
	totalReservedMemory := reservedMemory + reservedBuildMemory

	imageUsedMB, err := s.imgRepo.GetTotalUsedDiskSpace(ctx)
	if err != nil {
		return model.SystemMonitoring{}, err
	}
	volumeUsedBytes, err := s.volRepo.GetTotalUsedVolumeBytes(ctx)
	if err != nil {
		return model.SystemMonitoring{}, err
	}

	containerCounts, err := s.contRepo.GetSystemStatusCounts(ctx)
	if err != nil {
		return model.SystemMonitoring{}, err
	}
	volumesTotal, err := s.volRepo.CountAll(ctx)
	if err != nil {
		return model.SystemMonitoring{}, err
	}
	imagesTotal, err := s.imgRepo.CountAll(ctx)
	if err != nil {
		return model.SystemMonitoring{}, err
	}
	buildsTotal, err := s.buildRepo.CountAll(ctx)
	if err != nil {
		return model.SystemMonitoring{}, err
	}
	projectsTotal, err := s.projRepo.CountAll(ctx)
	if err != nil {
		return model.SystemMonitoring{}, err
	}

	admissionStatus, admissionReasons := admissionState(memory.TotalBytes, disk.FreeBytes, cfg, totalReservedMemory)

	return model.SystemMonitoring{
		CPUPercent:                  cpuPercent,
		MemoryTotalBytes:            memory.TotalBytes,
		MemoryUsedBytes:             memory.UsedBytes,
		MemoryAvailableBytes:        memory.AvailableBytes,
		DiskTotalBytes:              disk.TotalBytes,
		DiskUsedBytes:               disk.UsedBytes,
		DiskFreeBytes:               disk.FreeBytes,
		DCMReservedMemoryBytes:      reservedMemory,
		DCMReservedBuildMemoryBytes: reservedBuildMemory,
		DCMDiskUsedBytes:            imageUsedMB*bytesPerMB + volumeUsedBytes,
		HostMinFreeDiskBytes:        cfg.HostMinFreeDiskBytes,
		AdmissionStatus:             admissionStatus,
		AdmissionReasons:            admissionReasons,
		ContainersTotal:             containerCounts.Total,
		ContainersRunning:           containerCounts.Running,
		ContainersStopped:           containerCounts.Stopped,
		ContainersError:             containerCounts.Error,
		ContainersMissing:           containerCounts.Missing,
		VolumesTotal:                volumesTotal,
		ImagesTotal:                 imagesTotal,
		BuildsTotal:                 buildsTotal,
		ProjectsTotal:               projectsTotal,
		ObservedAt:                  time.Now(),
	}, nil
}

func admissionState(totalMemoryBytes, diskFreeBytes int64, cfg config.SystemConfig, reservedMemoryBytes int64) (string, []string) {
	reasons := make([]string, 0, 2)
	if err := ensureHostMemoryAdmission(totalMemoryBytes, cfg, reservedMemoryBytes, cfg.DefaultMemoryReservation); err != nil {
		reasons = append(reasons, "host_memory_exhausted")
	}
	if cfg.HostMinFreeDiskBytes > 0 && diskFreeBytes < cfg.HostMinFreeDiskBytes {
		reasons = append(reasons, "host_disk_floor")
	}
	if len(reasons) > 0 {
		return "blocked", reasons
	}
	return "open", reasons
}

func (s *StatsService) monitoringConfig() config.SystemConfig {
	if s.cfg == nil {
		return config.SystemConfig{
			DefaultMemoryReservation: 256 * bytesPerMB,
			OvercommitFactor:         1,
			BuildMemoryBytes:         512 * bytesPerMB,
		}
	}
	return s.cfg.Get()
}
