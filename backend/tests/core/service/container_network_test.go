package service_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestContainerServiceCreateUsesNameAsDefaultNetworkAlias(t *testing.T) {
	ownerID := uuid.New()
	repo, dockerAPI, svc := newContainerCreateService(t, ownerID)
	var saved model.Container
	var createdParams model.ContainerRuntimeSpec

	repo.EXPECT().Save(mock.Anything, mock.AnythingOfType("model.Container")).Run(func(ctx context.Context, c model.Container) {
		saved = c
	}).Return(nil)
	dockerAPI.EXPECT().CreateContainer(mock.Anything, mock.AnythingOfType("model.ContainerRuntimeSpec")).Run(func(ctx context.Context, params model.ContainerRuntimeSpec) {
		createdParams = params
	}).Return("docker-id", nil)
	repo.EXPECT().UpdateDockerIDAndStatus(mock.Anything, mock.AnythingOfType("uuid.UUID"), "docker-id", model.ContainerStatusCreated).Return(nil)

	_, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.ContainerCreateParams{Name: "api", ImageTag: "nginx:latest"})
	require.NoError(t, err)
	require.Equal(t, "api", saved.NetworkAlias)
	require.Equal(t, "api", createdParams.NetworkAlias)
}

func TestContainerServiceCreatePreservesExplicitNetworkAlias(t *testing.T) {
	ownerID := uuid.New()
	repo, dockerAPI, svc := newContainerCreateService(t, ownerID)
	var saved model.Container
	var createdParams model.ContainerRuntimeSpec

	repo.EXPECT().Save(mock.Anything, mock.AnythingOfType("model.Container")).Run(func(ctx context.Context, c model.Container) {
		saved = c
	}).Return(nil)
	dockerAPI.EXPECT().CreateContainer(mock.Anything, mock.AnythingOfType("model.ContainerRuntimeSpec")).Run(func(ctx context.Context, params model.ContainerRuntimeSpec) {
		createdParams = params
	}).Return("docker-id", nil)
	repo.EXPECT().UpdateDockerIDAndStatus(mock.Anything, mock.AnythingOfType("uuid.UUID"), "docker-id", model.ContainerStatusCreated).Return(nil)

	_, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.ContainerCreateParams{
		Name:         "project_api",
		NetworkAlias: "api",
		ImageTag:     "nginx:latest",
	})
	require.NoError(t, err)
	require.Equal(t, "api", saved.NetworkAlias)
	require.Equal(t, "api", createdParams.NetworkAlias)
}

func TestContainerServiceCleanupUserNetworkIfUnusedRemovesNetwork(t *testing.T) {
	ownerID := uuid.New()
	repo := coremocks.NewContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, nil, nil, nil, "", slog.Default())
	var removed []string

	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil)
	dockerAPI.EXPECT().RemoveNetwork(mock.Anything, "net_user_"+ownerID.String()).Run(func(ctx context.Context, networkName string) {
		removed = append(removed, networkName)
	}).Return(nil)

	require.NoError(t, svc.CleanupUserNetworkIfUnused(context.Background(), ownerID))
	require.Equal(t, []string{"net_user_" + ownerID.String()}, removed)
}

func TestContainerServiceCleanupUserNetworkIfUnusedKeepsNetwork(t *testing.T) {
	ownerID := uuid.New()
	repo := coremocks.NewContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, nil, nil, nil, "", slog.Default())

	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(1, nil)

	require.NoError(t, svc.CleanupUserNetworkIfUnused(context.Background(), ownerID))
	dockerAPI.AssertNotCalled(t, "RemoveNetwork", mock.Anything, mock.Anything)
}

func TestContainerServiceAdminScopeDeleteCleansUserNetwork(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	repo := coremocks.NewContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	svc := NewContainerService(repo, nil, nil, dockerAPI, nil, nil, nil, "", slog.Default())

	repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{ID: containerID, OwnerID: ownerID, DockerID: "docker-id"}, nil)
	dockerAPI.EXPECT().RemoveContainer(mock.Anything, "docker-id", true).Return(nil)
	repo.EXPECT().Delete(mock.Anything, containerID).Return(nil)
	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil)
	dockerAPI.EXPECT().RemoveNetwork(mock.Anything, "net_user_"+ownerID.String()).Return(nil)

	require.NoError(t, svc.Delete(accessscope.WithAdminScope(context.Background(), uuid.New(), "", "admin"), containerID))
}

func newContainerCreateService(t *testing.T, ownerID uuid.UUID) (*coremocks.ContainerRepository, *coremocks.ContainerDockerAPI, *ContainerService) {
	t.Helper()
	repo := coremocks.NewContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	imageRepo := coremocks.NewContainerImageRepository(t)
	metrics := coremocks.NewHostMetricsProvider(t)
	cfg := coremocks.NewConfigManager(t)
	users := coremocks.NewUserInfoProvider(t)
	cfg.EXPECT().Get().Return(containerCreateConfig()).Maybe()
	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil)
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaRAMMB: 1024, QuotaDiskMB: 1024}, nil)
	repo.EXPECT().GetUserReservedMemory(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().GetTotalSystemReservedMemory(mock.Anything).Return(int64(0), nil)
	metrics.EXPECT().GetTotalMemory().Return(int64(1024*1024*1024), nil)
	metrics.EXPECT().GetFreeMemory().Return(int64(1024*1024*1024), nil).Maybe()
	imageRepo.EXPECT().List(mock.Anything, mock.AnythingOfType("model.ListOptions")).Return([]model.Image(nil), 0, nil)
	dockerAPI.EXPECT().ImageExists(mock.Anything, "nginx:latest").Return(true, nil)
	dockerAPI.EXPECT().EnsureUserNetwork(mock.Anything, "net_user_"+ownerID.String()).Return("net_user_"+ownerID.String(), nil)
	return repo, dockerAPI, NewContainerService(repo, nil, imageRepo, dockerAPI, metrics, cfg, users, "", slog.Default())
}

func containerCreateConfig() config.SystemConfig {
	cfg := staticConfig{}.Get()
	cfg.MaxContainersPerUser = 10
	cfg.DefaultMemoryReservation = 128 * 1024 * 1024
	cfg.OvercommitFactor = 1
	cfg.MaxLogSize = "10m"
	cfg.MaxLogFiles = "3"
	cfg.ContainerDiskQuota = "1G"
	return cfg
}
