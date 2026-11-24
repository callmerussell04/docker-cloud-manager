package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/nilcheck"
	"github.com/google/uuid"
)

type StatsContainerRepository interface {
	GetUserReservedMemory(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetUserReservedCPU(ctx context.Context, ownerID uuid.UUID) (int64, error)
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	List(ctx context.Context, opts model.ListOptions) ([]model.Container, int, error)
	GetTotalSystemReservedMemory(ctx context.Context) (int64, error)
	GetTotalSystemReservedCPU(ctx context.Context) (int64, error)
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

// StatsMetricsProvider measures the host. GetTotalMemory, GetLogicalCPUs and
// GetDiskUsage are also what admission reads, through the reservation ledger.
type StatsMetricsProvider interface {
	GetCPULoad() (float64, error)
	GetMemoryStats() (model.HostMemoryStats, error)
	HostMetricsProvider
	HostDiskMetricsProvider
}

type StatsService struct {
	contRepo       StatsContainerRepository
	volRepo        StatsVolumeRepository
	imgRepo        StatsImageRepository
	buildRepo      StatsBuildRepository
	projRepo       StatsProjectRepository
	metrics        StatsMetricsProvider
	dockerDataPath string
	cfg            ConfigManager
	quotas         UserQuotaProvider
	borrowed       StatsBorrowedMemory
	rootfsQuota    StatsRootfsQuota
	volumeSize     StatsVolumeSizeMode
	// hostLedger reads the host part of the reservation ledger the way
	// admission does, for monitoring's admission status.
	hostLedger admissionSources
}

// StatsServiceDeps are what the stats service reads. Monitoring's disk figures
// describe the filesystem of DockerDataPath, Docker's data directory as Core
// sees it; empty measures "/". Logger is optional.
type StatsServiceDeps struct {
	Containers     StatsContainerRepository
	Volumes        StatsVolumeRepository
	Images         StatsImageRepository
	Builds         StatsBuildRepository
	Projects       StatsProjectRepository
	Metrics        StatsMetricsProvider
	DockerDataPath string
	Config         ConfigManager
	Quotas         UserQuotaProvider
	Borrowed       StatsBorrowedMemory
	RootfsQuota    StatsRootfsQuota
	VolumeSizeMode StatsVolumeSizeMode
	Logger         *slog.Logger
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
		if nilcheck.IsNil(dependency.value) {
			return nil, fmt.Errorf("stats service: %s is required", dependency.name)
		}
	}
	dockerDataPath := deps.DockerDataPath
	if dockerDataPath == "" {
		dockerDataPath = "/"
	}
	return &StatsService{
		contRepo:       deps.Containers,
		volRepo:        deps.Volumes,
		imgRepo:        deps.Images,
		buildRepo:      deps.Builds,
		projRepo:       deps.Projects,
		metrics:        deps.Metrics,
		dockerDataPath: dockerDataPath,
		cfg:            deps.Config,
		quotas:         deps.Quotas,
		borrowed:       deps.Borrowed,
		rootfsQuota:    deps.RootfsQuota,
		volumeSize:     deps.VolumeSizeMode,
		hostLedger: admissionSources{
			system:         deps.Containers,
			builds:         deps.Builds,
			hostMetrics:    deps.Metrics,
			diskMetrics:    deps.Metrics,
			dockerDataPath: dockerDataPath,
			logger:         logging.WithComponent(deps.Logger, "stats"),
		},
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
	stats.RamQuotaBytes = quota.RAMMB * model.BytesPerMB
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
		return model.SystemMonitoring{}, fmt.Errorf("system monitoring: GetCPULoad: %w", err)
	}

	memory, err := s.metrics.GetMemoryStats()
	if err != nil {
		return model.SystemMonitoring{}, fmt.Errorf("system monitoring: GetMemoryStats: %w", err)
	}

	disk, err := s.metrics.GetDiskUsage(s.dockerDataPath)
	if err != nil {
		return model.SystemMonitoring{}, fmt.Errorf("system monitoring: GetDiskUsage: %w", err)
	}

	cfg := s.monitoringConfig()
	var ledger reservationLedger
	if err := s.hostLedger.readHostLedger(ctx, &ledger, admissionRequest{}, cfg); err != nil {
		return model.SystemMonitoring{}, fmt.Errorf("system monitoring: read reservation ledger: %w", err)
	}
	buildSlots := reservedBuildSlots(ledger.activeBuilds, cfg)

	imageUsedBytes, err := s.imgRepo.GetTotalImageBytes(ctx)
	if err != nil {
		return model.SystemMonitoring{}, fmt.Errorf("system monitoring: GetTotalImageBytes: %w", err)
	}
	volumeUsedBytes, err := s.volRepo.GetTotalUsedVolumeBytes(ctx)
	if err != nil {
		return model.SystemMonitoring{}, fmt.Errorf("system monitoring: GetTotalUsedVolumeBytes: %w", err)
	}

	reservedVolumeBytes, err := s.volRepo.GetTotalVolumeSizeBytes(ctx)
	if err != nil {
		return model.SystemMonitoring{}, fmt.Errorf("system monitoring: GetTotalVolumeSizeBytes: %w", err)
	}
	unusedVolumePromise, err := s.volRepo.GetUnusedVolumePromiseBytes(ctx)
	if err != nil {
		return model.SystemMonitoring{}, fmt.Errorf("system monitoring: GetUnusedVolumePromiseBytes: %w", err)
	}

	containerCounts, err := s.contRepo.GetSystemStatusCounts(ctx)
	if err != nil {
		return model.SystemMonitoring{}, fmt.Errorf("system monitoring: GetSystemStatusCounts: %w", err)
	}
	volumesTotal, err := s.volRepo.CountAll(ctx)
	if err != nil {
		return model.SystemMonitoring{}, fmt.Errorf("system monitoring: count volumes: %w", err)
	}
	imagesTotal, err := s.imgRepo.CountAll(ctx)
	if err != nil {
		return model.SystemMonitoring{}, fmt.Errorf("system monitoring: count images: %w", err)
	}
	buildsTotal, err := s.buildRepo.CountAll(ctx)
	if err != nil {
		return model.SystemMonitoring{}, fmt.Errorf("system monitoring: count builds: %w", err)
	}
	projectsTotal, err := s.projRepo.CountAll(ctx)
	if err != nil {
		return model.SystemMonitoring{}, fmt.Errorf("system monitoring: count projects: %w", err)
	}

	admissionStatus, admissionReasons := admissionState(ledger, cfg)

	return model.SystemMonitoring{
		CPUPercent:                    cpuPercent,
		MemoryTotalBytes:              memory.TotalBytes,
		MemoryUsedBytes:               memory.UsedBytes,
		MemoryAvailableBytes:          memory.AvailableBytes,
		DiskTotalBytes:                disk.TotalBytes,
		DiskUsedBytes:                 disk.UsedBytes,
		DiskFreeBytes:                 disk.FreeBytes,
		DCMReservedMemoryBytes:        ledger.systemReservedMemoryBytes,
		DCMReservedBuildMemoryBytes:   buildSlots * cfg.BuildMemoryBytes,
		DCMReservedCPUMillicores:      ledger.systemReservedCPUMillicores,
		DCMReservedBuildCPUMillicores: buildSlots * max(cfg.BuildCPUMillicores, 0),
		DCMBorrowedMemoryBytes:        s.borrowed.BorrowedMemoryBytes(),
		RootfsQuotaStatus:             s.rootfsQuota.RootfsQuotaStatus(),
		VolumeSizeMode:                s.volumeSize.VolumeSizeMode(),
		DCMDiskUsedBytes:              imageUsedBytes + volumeUsedBytes,
		DCMReservedVolumeBytes:        reservedVolumeBytes,
		DCMUnusedVolumePromiseBytes:   unusedVolumePromise,
		HostMinFreeDiskBytes:          cfg.HostMinFreeDiskBytes,
		BuildClaimCap:                 buildClaimCap(cfg, disk.FreeBytes),
		MaxConcurrentBuilds:           cfg.MaxConcurrentBuilds,
		BuildsActive:                  int(ledger.activeBuilds),
		CPUTotalMillicores:            ledger.hostLogicalCPUs * 1000,
		AdmissionStatus:               admissionStatus,
		AdmissionReasons:              admissionReasons,
		ContainersTotal:               containerCounts.Total,
		ContainersRunning:             containerCounts.Running,
		ContainersStopped:             containerCounts.Stopped,
		ContainersError:               containerCounts.Error,
		ContainersMissing:             containerCounts.Missing,
		VolumesTotal:                  volumesTotal,
		ImagesTotal:                   imagesTotal,
		BuildsTotal:                   buildsTotal,
		ProjectsTotal:                 projectsTotal,
		ObservedAt:                    time.Now(),
	}, nil
}

// admissionState is what the admission check says of one more container with
// the default reservations, charged to no owner, over ledger: "open", or
// "blocked" with the host limit it meets first as the reason.
func admissionState(ledger reservationLedger, cfg config.SystemConfig) (string, []string) {
	verdict := checkAdmission(ledger, admissionRequest{
		memoryBytes:   cfg.DefaultMemoryReservation,
		cpuMillicores: cfg.DefaultCPUReservation,
	}, cfg)
	reason, blocked := admissionReasons[verdict.limit]
	if !blocked {
		return "open", []string{}
	}
	return "blocked", []string{reason}
}

// admissionReasons is the monitoring reason for each limit a request charged
// to no owner can meet.
var admissionReasons = map[apperrors.Limit]string{
	apperrors.LimitHostMemory: "host_memory_exhausted",
	apperrors.LimitHostCPU:    "host_cpu_exhausted",
	apperrors.LimitDiskFloor:  "host_disk_floor",
}

func (s *StatsService) monitoringConfig() config.SystemConfig {
	if s.cfg == nil {
		return config.SystemConfig{
			DefaultMemoryReservation: 256 * model.BytesPerMB,
			OvercommitFactor:         1,
			BuildMemoryBytes:         512 * model.BytesPerMB,
			MaxConcurrentBuilds:      2,
		}
	}
	return s.cfg.Get()
}
