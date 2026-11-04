package service_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestContainerServiceCreateUsesNameAsDefaultNetworkAlias(t *testing.T) {
	ownerID := uuid.New()
	repo, dockerAPI, svc := newContainerCreateService(t, ownerID)
	var saved model.Container

	repo.EXPECT().SaveWithMountsOperationAndOutbox(mock.Anything, mock.AnythingOfType("model.Container"), mock.Anything, mock.Anything, mock.Anything, false, false).Run(func(ctx context.Context, c model.Container, mounts []model.VolumeMount, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox, lockOwner, lockCapacity bool) {
		saved = c
	}).Return(nil)

	_, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.ContainerCreateParams{Name: "api", ImageTag: "nginx:latest"})
	require.NoError(t, err)
	require.Equal(t, "api", saved.NetworkAlias)
	dockerAPI.AssertNotCalled(t, "CreateContainer", mock.Anything, mock.Anything)
}

func TestContainerServiceCreatePreservesExplicitNetworkAlias(t *testing.T) {
	ownerID := uuid.New()
	repo, dockerAPI, svc := newContainerCreateService(t, ownerID)
	var saved model.Container

	repo.EXPECT().SaveWithMountsOperationAndOutbox(mock.Anything, mock.AnythingOfType("model.Container"), mock.Anything, mock.Anything, mock.Anything, false, false).Run(func(ctx context.Context, c model.Container, mounts []model.VolumeMount, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox, lockOwner, lockCapacity bool) {
		saved = c
	}).Return(nil)

	_, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.ContainerCreateParams{
		Name:         "project_api",
		NetworkAlias: "api",
		ImageTag:     "nginx:latest",
	})
	require.NoError(t, err)
	require.Equal(t, "api", saved.NetworkAlias)
	dockerAPI.AssertNotCalled(t, "CreateContainer", mock.Anything, mock.Anything)
}

func TestContainerServiceCleanupUserNetworkIfUnusedRemovesNetwork(t *testing.T) {
	ownerID := uuid.New()
	repo := newContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   repo,
		Docker:       dockerAPI,
		HostDiskPath: "",
		Logger:       slog.Default(),
	})
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
	repo := newContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   repo,
		Docker:       dockerAPI,
		HostDiskPath: "",
		Logger:       slog.Default(),
	})

	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(1, nil)

	require.NoError(t, svc.CleanupUserNetworkIfUnused(context.Background(), ownerID))
	dockerAPI.AssertNotCalled(t, "RemoveNetwork", mock.Anything, mock.Anything)
}

func TestContainerServiceAdminScopeDeleteCleansUserNetwork(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	store := newContainerStore()
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   store,
		Docker:       coremocks.NewContainerDockerAPI(t),
		HostDiskPath: "",
		Logger:       slog.Default(),
	})
	saveContainer(t, store, model.Container{ID: containerID, OwnerID: ownerID, DockerID: "docker-id"})

	require.NoError(t, svc.Delete(accessscope.WithAdminScope(context.Background(), uuid.New(), "", "admin"), containerID))
	require.Equal(t, model.OperationDelete, requireQueued(t, store).op.Operation)
}

func TestContainerServiceListFiltersByOwnerScopeAndProject(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := newContainerRepository(t)
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   repo,
		Images:       newContainerImageRepository(t),
		Docker:       coremocks.NewContainerDockerAPI(t),
		Metrics:      coremocks.NewHostMetricsProvider(t),
		Config:       coremocks.NewConfigManager(t),
		Users:        coremocks.NewUserInfoProvider(t),
		HostDiskPath: "",
		Logger:       slog.Default(),
	})

	repo.EXPECT().
		List(mock.Anything, mock.MatchedBy(func(opts model.ListOptions) bool {
			return opts.OwnerID != nil &&
				*opts.OwnerID == ownerID &&
				opts.ProjectID != nil &&
				*opts.ProjectID == projectID &&
				opts.Limit == 25 &&
				opts.Offset == 5
		})).
		Return([]model.Container{{OwnerID: ownerID, ProjectID: &projectID}}, 1, nil)

	containers, total, err := svc.List(accessscope.WithUserScope(context.Background(), ownerID, "", ""), 25, 5, &projectID)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, containers, 1)
}

func newContainerCreateService(t *testing.T, ownerID uuid.UUID) (*coremocks.ContainerRepository, *coremocks.ContainerDockerAPI, *service.ContainerService) {
	t.Helper()
	repo := newContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	imageRepo := newContainerImageRepository(t)
	metrics := coremocks.NewHostMetricsProvider(t)
	cfg := coremocks.NewConfigManager(t)
	users := coremocks.NewUserInfoProvider(t)
	cfg.EXPECT().Get().Return(containerCreateConfig()).Maybe()
	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil)
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaRAMMB: 1024, QuotaDiskMB: 1024, QuotaCPU: 2}, nil)
	repo.EXPECT().GetUserReservedMemory(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().GetUserReservedCPU(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().GetTotalSystemReservedMemory(mock.Anything).Return(int64(0), nil)
	repo.EXPECT().GetTotalSystemReservedCPU(mock.Anything).Return(int64(0), nil)
	metrics.EXPECT().GetTotalMemory().Return(int64(1024*1024*1024), nil)
	metrics.EXPECT().GetLogicalCPUs().Return(int64(2), nil)
	imageRepo.EXPECT().List(mock.Anything, mock.AnythingOfType("model.ListOptions")).Return([]model.Image(nil), 0, nil)
	return repo, dockerAPI, newTestContainerService(t, service.ContainerServiceDeps{
		Repository:   repo,
		Images:       imageRepo,
		Docker:       dockerAPI,
		Metrics:      metrics,
		Config:       cfg,
		Users:        users,
		HostDiskPath: "",
		Logger:       slog.Default(),
	})
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
