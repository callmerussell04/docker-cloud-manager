package service

import (
	"context"
	"fmt"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/google/uuid"
)

const bytesPerMB = 1024 * 1024

type StatsContainerRepository interface {
	GetUserReservedMemory(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetUserReservedCPU(ctx context.Context, ownerID uuid.UUID) (int64, error)
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	List(ctx context.Context, opts model.ListOptions) ([]model.Container, int, error)
	GetTotalSystemReservedMemory(ctx context.Context) (int64, error)
	GetSystemStatusCounts(ctx context.Context) (model.ContainerStatusCounts, error)
}

type StatsVolumeRepository interface {
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	GetUserReservedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetTotalUsedVolumeBytes(ctx context.Context) (int64, error)
	GetTotalVolumeSizeBytes(ctx context.Context) (int64, error)
	GetUnusedVolumePromiseBytes(ctx context.Context) (int64, error)
	CountAll(ctx context.Context) (int, error)
}

type StatsImageRepository interface {
	GetUserImageBytes(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetTotalImageBytes(ctx context.Context) (int64, error)
	List(ctx context.Context, opts model.ListOptions) ([]model.Image, int, error)
	CountAll(ctx context.Context) (int, error)
}

type StatsBuildRepository interface {
	CountAll(ctx context.Context) (int, error)
	CountActive(ctx context.Context) (int, error)
}

// StatsBorrowedMemory reports the memory running workloads borrow above
// their standard limits, as the memory pressure worker last measured it.
type StatsBorrowedMemory interface {
	BorrowedMemoryBytes() int64
}

// StatsRootfsQuota reports whether the container root filesystem cap is
// enforced on this host, as Core found at startup.
type StatsRootfsQuota interface {
	RootfsQuotaStatus() model.RootfsQuotaStatus
}

// StatsVolumeSizeMode reports the mode new volumes are created in.
type StatsVolumeSizeMode interface {
	VolumeSizeMode() model.VolumeSizeMode
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
	quotas       UserQuotaProvider
	borrowed     StatsBorrowedMemory
	rootfsQuota  StatsRootfsQuota
	volumeSize   StatsVolumeSizeMode
}

// StatsServiceDeps are what the stats service reads. An empty HostDiskPath
// measures "/".
type StatsServiceDeps struct {
	Containers     StatsContainerRepository
	Volumes        StatsVolumeRepository
	Images         StatsImageRepository
	Builds         StatsBuildRepository
	Projects       StatsProjectRepository
	Metrics        StatsMetricsProvider
	HostDiskPath   string
	Config         ConfigManager
	Quotas         UserQuotaProvider
	Borrowed       StatsBorrowedMemory
	RootfsQuota    StatsRootfsQuota
	VolumeSizeMode StatsVolumeSizeMode
}

func NewStatsService(deps StatsServiceDeps) (*StatsService, error) {
	required := []struct {
		name  string
		value any
	}{
		{"containers", deps.Containers}, {"volumes", deps.Volumes}, {"images", deps.Images},
		{"builds", deps.Builds}, {"projects", deps.Projects}, {"metrics", deps.Metrics},
		{"config", deps.Config}, {"quotas", deps.Quotas}, {"borrowed memory", deps.Borrowed},
		{"rootfs quota status", deps.RootfsQuota}, {"volume size mode", deps.VolumeSizeMode},
	}
	for _, dependency := range required {
		if dependency.value == nil || isNilServiceDependency(dependency.value) {
			return nil, fmt.Errorf("stats service: %s is required", dependency.name)
		}
	}
	hostDiskPath := deps.HostDiskPath
	if hostDiskPath == "" {
		hostDiskPath = "/"
	}
	return &StatsService{
		contRepo:     deps.Containers,
		volRepo:      deps.Volumes,
		imgRepo:      deps.Images,
		buildRepo:    deps.Builds,
		projRepo:     deps.Projects,
		metrics:      deps.Metrics,
		hostDiskPath: hostDiskPath,
		cfg:          deps.Config,
		quotas:       deps.Quotas,
		borrowed:     deps.Borrowed,
		rootfsQuota:  deps.RootfsQuota,
		volumeSize:   deps.VolumeSizeMode,
	}, nil
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

	quota, err := s.quotas.EffectiveUserQuota(ctx, ownerID)
	if err != nil {
		return model.UserStats{}, err
	}
	stats.RamQuotaBytes = quota.RAMMB * bytesPerMB
	stats.RamUsedBytes, err = s.contRepo.GetUserReservedMemory(ctx, ownerID)
	if err != nil {
		return model.UserStats{}, err
	}
	stats.CPUQuotaMillicores = quota.CPUMillicores
	stats.CPUUsedMillicores, err = s.contRepo.GetUserReservedCPU(ctx, ownerID)
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

	stats.DiskQuotaMB = int(quota.DiskMB)
	diskReserved, err := reservedDiskBytes(ctx, ownerID, s.imgRepo, s.volRepo)
	if err != nil {
		return model.UserStats{}, err
	}
	stats.DiskUsedMB = int(model.BytesToMBRoundedUp(diskReserved))

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
	activeBuilds, err := s.buildRepo.CountActive(ctx)
	if err != nil {
		return model.SystemMonitoring{}, err
	}
	cfg := s.monitoringConfig()
	reservedBuildMemory := reservedBuildSlots(int64(activeBuilds), cfg) * cfg.BuildMemoryBytes
	totalReservedMemory := reservedMemory + reservedBuildMemory

	imageUsedBytes, err := s.imgRepo.GetTotalImageBytes(ctx)
	if err != nil {
		return model.SystemMonitoring{}, err
	}
	volumeUsedBytes, err := s.volRepo.GetTotalUsedVolumeBytes(ctx)
	if err != nil {
		return model.SystemMonitoring{}, err
	}

	reservedVolumeBytes, err := s.volRepo.GetTotalVolumeSizeBytes(ctx)
	if err != nil {
		return model.SystemMonitoring{}, err
	}
	unusedVolumePromise, err := s.volRepo.GetUnusedVolumePromiseBytes(ctx)
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
		DCMBorrowedMemoryBytes:      s.borrowed.BorrowedMemoryBytes(),
		RootfsQuotaStatus:           s.rootfsQuota.RootfsQuotaStatus(),
		VolumeSizeMode:              s.volumeSize.VolumeSizeMode(),
		DCMDiskUsedBytes:            imageUsedBytes + volumeUsedBytes,
		DCMReservedVolumeBytes:      reservedVolumeBytes,
		DCMUnusedVolumePromiseBytes: unusedVolumePromise,
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
			MaxConcurrentBuilds:      2,
		}
	}
	return s.cfg.Get()
}
