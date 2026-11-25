package service_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// The dashboard shows the owner's effective quota from Core: a custom quota,
// or else the default quota.
func TestStatsServiceGetUserStatsAggregatesUserResources(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		custom                   bool
		wantRAMQuotaMB, wantDisk int64
		wantCPUQuotaMillicores   int64
	}{
		{name: "custom quota", custom: true, wantRAMQuotaMB: 512, wantDisk: 100, wantCPUQuotaMillicores: 1000},
		{name: "default quota", wantRAMQuotaMB: 768, wantDisk: 200, wantCPUQuotaMillicores: 1500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testUserStats(t, tc.custom, tc.wantRAMQuotaMB, tc.wantDisk, tc.wantCPUQuotaMillicores)
		})
	}
}

func testUserStats(t *testing.T, custom bool, wantRAMQuotaMB, wantDiskQuotaMB, wantCPUQuotaMillicores int64) {
	t.Helper()
	ownerID := uuid.New()
	contRepo := coremocks.NewStatsContainerRepository(t)
	volRepo := coremocks.NewStatsVolumeRepository(t)
	imgRepo := coremocks.NewStatsImageRepository(t)
	buildRepo := coremocks.NewStatsBuildRepository(t)
	projRepo := coremocks.NewStatsProjectRepository(t)
	var containerListOpts model.ContainerListOptions

	cfgValue := testSystemConfig()
	cfgValue.DefaultUserQuotaRAMMB, cfgValue.DefaultUserQuotaDiskMB = 768, 200
	cfgValue.DefaultUserQuotaCPU = 1.5
	cfg := coretest.StaticConfig{Config: cfgValue}
	customQuotas := map[uuid.UUID]model.UserQuota{}
	if custom {
		customQuotas[ownerID] = model.UserQuota{CPUMillicores: 1000, RAMMB: 512, DiskMB: 100}
	}
	contRepo.EXPECT().GetUserReservedMemory(mock.Anything, ownerID).Return(int64(256), nil)
	contRepo.EXPECT().GetUserReservedCPU(mock.Anything, ownerID).Return(int64(750), nil)
	contRepo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(3, nil)
	contRepo.EXPECT().
		List(mock.Anything, mock.AnythingOfType("model.ContainerListOptions")).
		Run(func(ctx context.Context, opts model.ContainerListOptions) {
			containerListOpts = opts
		}).
		Return(model.ContainerPage{Counts: model.ContainerFilterCounts{All: 3, Running: 2, Stopped: 1}}, nil)
	imgRepo.EXPECT().GetUserImageBytes(mock.Anything, ownerID).Return(int64(10<<20), nil)
	volRepo.EXPECT().GetUserReservedVolumeBytes(mock.Anything, ownerID).Return(int64(1024*1024+1), nil)
	imgRepo.EXPECT().List(mock.Anything, mock.AnythingOfType("model.ListOptions")).Return([]model.Image(nil), 4, nil)
	volRepo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(2, nil)
	projRepo.EXPECT().List(mock.Anything, mock.AnythingOfType("model.ListOptions")).Return([]model.Project(nil), 5, nil)
	svc, err := service.NewStatsService(service.StatsServiceDeps{
		Containers: contRepo, Volumes: volRepo, Images: imgRepo, Builds: buildRepo, Projects: projRepo,
		Metrics: coremocks.NewStatsMetricsProvider(t), Config: cfg, Quotas: testQuotas(t, cfg, customQuotas), Borrowed: coremocks.NewStatsBorrowedMemory(t), RootfsQuota: coremocks.NewStatsRootfsQuota(t), VolumeSizeMode: coremocks.NewStatsVolumeSizeMode(t),
	})
	require.NoError(t, err)

	stats, err := svc.GetUserStats(accessscope.WithUserScope(context.Background(), ownerID, "", ""))
	require.NoError(t, err)
	require.Equal(t, 3, stats.ContainersTotal)
	require.Equal(t, 2, stats.ContainersRunning)
	require.Equal(t, testSystemConfig().MaxContainersPerUser, stats.ContainersQuota)
	require.EqualValues(t, 256, stats.RamUsedBytes)
	require.Equal(t, wantRAMQuotaMB*1024*1024, stats.RamQuotaBytes)
	require.EqualValues(t, 750, stats.CPUUsedMillicores)
	require.Equal(t, wantCPUQuotaMillicores, stats.CPUQuotaMillicores)
	require.Equal(t, 12, stats.DiskUsedMB)
	require.Equal(t, int(wantDiskQuotaMB), stats.DiskQuotaMB)
	require.Equal(t, 2, stats.VolumesTotal)
	require.Equal(t, testSystemConfig().MaxVolumesPerUser, stats.VolumesQuota)
	require.Equal(t, 4, stats.ImagesTotal)
	require.Equal(t, 5, stats.ProjectsTotal)
	require.NotNil(t, containerListOpts.OwnerID)
	require.Equal(t, ownerID, *containerListOpts.OwnerID)
}
