package service_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestStatsServiceGetUserStatsAggregatesUserResources(t *testing.T) {
	ownerID := uuid.New()
	contRepo := coremocks.NewStatsContainerRepository(t)
	volRepo := coremocks.NewStatsVolumeRepository(t)
	imgRepo := coremocks.NewStatsImageRepository(t)
	buildRepo := coremocks.NewStatsBuildRepository(t)
	projRepo := coremocks.NewStatsProjectRepository(t)
	users := coremocks.NewUserInfoProvider(t)
	cfg := coremocks.NewConfigManager(t)
	var containerListOpts model.ListOptions

	cfg.EXPECT().Get().Return(staticConfig{}.Get())
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaRAMMB: 512, QuotaDiskMB: 100}, nil)
	contRepo.EXPECT().GetUserReservedMemory(mock.Anything, ownerID).Return(int64(256), nil)
	contRepo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(3, nil)
	contRepo.EXPECT().
		List(mock.Anything, mock.AnythingOfType("model.ListOptions")).
		Run(func(ctx context.Context, opts model.ListOptions) {
			containerListOpts = opts
		}).
		Return([]model.Container{
			{Status: model.ContainerStatusRunning},
			{Status: model.ContainerStatusExited},
			{Status: model.ContainerStatusRunning},
		}, 3, nil)
	imgRepo.EXPECT().GetUserUsedDiskSpace(mock.Anything, ownerID).Return(int64(10), nil)
	volRepo.EXPECT().GetUserUsedVolumeBytes(mock.Anything, ownerID).Return(int64(1024*1024+1), nil)
	imgRepo.EXPECT().List(mock.Anything, mock.AnythingOfType("model.ListOptions")).Return([]model.Image(nil), 4, nil)
	volRepo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(2, nil)
	projRepo.EXPECT().List(mock.Anything, mock.AnythingOfType("model.ListOptions")).Return([]model.Project(nil), 5, nil)
	svc := NewStatsService(contRepo, volRepo, imgRepo, buildRepo, projRepo, nil, "", cfg, users)

	stats, err := svc.GetUserStats(accessscope.WithUserScope(context.Background(), ownerID, "", ""))
	require.NoError(t, err)
	require.Equal(t, 3, stats.ContainersTotal)
	require.Equal(t, 2, stats.ContainersRunning)
	require.Equal(t, staticConfig{}.Get().MaxContainersPerUser, stats.ContainersQuota)
	require.EqualValues(t, 256, stats.RamUsedBytes)
	require.EqualValues(t, 512*1024*1024, stats.RamQuotaBytes)
	require.Equal(t, 12, stats.DiskUsedMB)
	require.Equal(t, 100, stats.DiskQuotaMB)
	require.Equal(t, 2, stats.VolumesTotal)
	require.Equal(t, staticConfig{}.Get().MaxVolumesPerUser, stats.VolumesQuota)
	require.Equal(t, 4, stats.ImagesTotal)
	require.Equal(t, 5, stats.ProjectsTotal)
	require.NotNil(t, containerListOpts.OwnerID)
	require.Equal(t, ownerID, *containerListOpts.OwnerID)
}
