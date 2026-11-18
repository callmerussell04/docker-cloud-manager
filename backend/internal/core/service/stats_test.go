package service_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/stretchr/testify/require"
)

const bytesPerMBForTest = 1024 * 1024

func TestNewStatsServiceRejectsMissingDependencies(t *testing.T) {
	valid := func(t *testing.T) service.StatsServiceDeps {
		return service.StatsServiceDeps{
			Containers:     coremocks.NewStatsContainerRepository(t),
			Volumes:        coremocks.NewStatsVolumeRepository(t),
			Images:         coremocks.NewStatsImageRepository(t),
			Builds:         coremocks.NewStatsBuildRepository(t),
			Projects:       coremocks.NewStatsProjectRepository(t),
			Metrics:        coremocks.NewStatsMetricsProvider(t),
			Config:         testConfig(),
			Quotas:         testQuotas(t, nil, nil),
			Borrowed:       coremocks.NewStatsBorrowedMemory(t),
			RootfsQuota:    coremocks.NewStatsRootfsQuota(t),
			VolumeSizeMode: coremocks.NewStatsVolumeSizeMode(t),
		}
	}
	depstest.RequireRejectsMissing(t, valid, service.NewStatsService, map[string]string{
		"Containers":     "containers",
		"Volumes":        "volumes",
		"Images":         "images",
		"Builds":         "builds",
		"Projects":       "projects",
		"Metrics":        "metrics",
		"Config":         "config",
		"Quotas":         "quotas",
		"Borrowed":       "borrowed memory",
		"RootfsQuota":    "rootfs quota status",
		"VolumeSizeMode": "volume size mode",
	})
}

func TestStatsServiceGetSystemMonitoring(t *testing.T) {
	ctx := context.Background()
	contRepo := coremocks.NewStatsContainerRepository(t)
	volRepo := coremocks.NewStatsVolumeRepository(t)
	imgRepo := coremocks.NewStatsImageRepository(t)
	buildRepo := coremocks.NewStatsBuildRepository(t)
	projRepo := coremocks.NewStatsProjectRepository(t)
	metrics := coremocks.NewStatsMetricsProvider(t)
	cfgData := testSystemConfig()
	cfgData.DefaultMemoryReservation = 1
	cfgData.OvercommitFactor = 1
	cfgData.BuildMemoryBytes = 100
	cfgData.MaxConcurrentBuilds = 2
	cfgData.HostMinFreeDiskBytes = 100
	// 500 free less the 100 floor holds one build slot of 300.
	cfgData.BuildMaxDiskBytes = 300
	cfg := coretest.StaticConfig{Config: cfgData}

	contRepo.EXPECT().GetTotalSystemReservedMemory(ctx).Return(int64(3*bytesPerMBForTest), nil)
	contRepo.EXPECT().GetSystemStatusCounts(ctx).Return(model.ContainerStatusCounts{
		Total:   7,
		Running: 3,
		Stopped: 2,
		Error:   1,
		Missing: 1,
	}, nil)
	volRepo.EXPECT().GetTotalUsedVolumeBytes(ctx).Return(int64(512), nil)
	volRepo.EXPECT().GetTotalVolumeSizeBytes(ctx).Return(int64(3<<30), nil)
	volRepo.EXPECT().GetUnusedVolumePromiseBytes(ctx).Return(int64(2<<30), nil)
	volRepo.EXPECT().CountAll(ctx).Return(4, nil)
	imgRepo.EXPECT().GetTotalImageBytes(ctx).Return(int64(2<<20), nil)
	imgRepo.EXPECT().CountAll(ctx).Return(5, nil)
	// Three builds are active, but only the two Builder can run hold memory.
	buildRepo.EXPECT().CountActive(ctx).Return(3, nil)
	buildRepo.EXPECT().CountAll(ctx).Return(6, nil)
	projRepo.EXPECT().CountAll(ctx).Return(2, nil)
	metrics.EXPECT().GetCPULoad().Return(42.5, nil)
	metrics.EXPECT().GetMemoryStats().Return(model.HostMemoryStats{
		TotalBytes:     10 * bytesPerMBForTest,
		UsedBytes:      600,
		AvailableBytes: 400,
	}, nil)
	metrics.EXPECT().GetDiskUsage("/host").Return(model.HostDiskStats{
		TotalBytes: 2000,
		UsedBytes:  1500,
		FreeBytes:  500,
	}, nil)

	borrowed := coremocks.NewStatsBorrowedMemory(t)
	borrowed.EXPECT().BorrowedMemoryBytes().Return(1234)
	rootfsQuota := coremocks.NewStatsRootfsQuota(t)
	rootfsQuota.EXPECT().RootfsQuotaStatus().Return(model.RootfsQuotaNotEnforced)
	volumeSize := coremocks.NewStatsVolumeSizeMode(t)
	volumeSize.EXPECT().VolumeSizeMode().Return(model.VolumeSizeEnforced)
	svc, err := service.NewStatsService(service.StatsServiceDeps{
		Containers: contRepo, Volumes: volRepo, Images: imgRepo, Builds: buildRepo, Projects: projRepo,
		Metrics: metrics, HostDiskPath: "/host", Config: cfg, Quotas: testQuotas(t, nil, nil), Borrowed: borrowed,
		RootfsQuota: rootfsQuota, VolumeSizeMode: volumeSize,
	})
	require.NoError(t, err)

	stats, err := svc.GetSystemMonitoring(ctx)
	require.NoError(t, err)
	require.Equal(t, 42.5, stats.CPUPercent)
	require.EqualValues(t, 10*bytesPerMBForTest, stats.MemoryTotalBytes)
	require.EqualValues(t, 600, stats.MemoryUsedBytes)
	require.EqualValues(t, 400, stats.MemoryAvailableBytes)
	require.EqualValues(t, 2000, stats.DiskTotalBytes)
	require.EqualValues(t, 1500, stats.DiskUsedBytes)
	require.EqualValues(t, 500, stats.DiskFreeBytes)
	require.EqualValues(t, 3*bytesPerMBForTest, stats.DCMReservedMemoryBytes)
	require.EqualValues(t, 200, stats.DCMReservedBuildMemoryBytes)
	require.EqualValues(t, 1234, stats.DCMBorrowedMemoryBytes, "borrowed memory as the memory pressure worker last measured it")
	require.Equal(t, model.RootfsQuotaNotEnforced, stats.RootfsQuotaStatus, "root filesystem cap status as Core found it at startup")
	require.EqualValues(t, 2*bytesPerMBForTest+512, stats.DCMDiskUsedBytes)
	require.EqualValues(t, 3<<30, stats.DCMReservedVolumeBytes, "the sum of volume sizes")
	require.EqualValues(t, 2<<30, stats.DCMUnusedVolumePromiseBytes, "what volumes were promised and have not used")
	require.Equal(t, model.VolumeSizeEnforced, stats.VolumeSizeMode)
	require.EqualValues(t, 100, stats.HostMinFreeDiskBytes)
	require.Equal(t, 1, stats.BuildClaimCap, "the disk holds one build slot, below max_concurrent_builds")
	require.Equal(t, 2, stats.MaxConcurrentBuilds)
	require.Equal(t, "open", stats.AdmissionStatus)
	require.Empty(t, stats.AdmissionReasons)
	require.Equal(t, 7, stats.ContainersTotal)
	require.Equal(t, 3, stats.ContainersRunning)
	require.Equal(t, 2, stats.ContainersStopped)
	require.Equal(t, 1, stats.ContainersError)
	require.Equal(t, 1, stats.ContainersMissing)
	require.Equal(t, 4, stats.VolumesTotal)
	require.Equal(t, 5, stats.ImagesTotal)
	require.Equal(t, 6, stats.BuildsTotal)
	require.Equal(t, 2, stats.ProjectsTotal)
	require.False(t, stats.ObservedAt.IsZero())
}

var (
	_ service.StatsBuildRepository = (*repository.BuildRepository)(nil)
	_ service.StatsBorrowedMemory  = (*service.MemoryPressureWorker)(nil)
)
