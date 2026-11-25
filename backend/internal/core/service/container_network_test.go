package service_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestContainerServiceCreateUsesNameAsDefaultNetworkAlias(t *testing.T) {
	ownerID := uuid.New()
	repo, svc := newContainerCreateService(t, ownerID)
	var saved model.Container

	repo.EXPECT().SaveWithMountsOperationAndOutbox(mock.Anything, mock.AnythingOfType("model.Container"), mock.Anything, mock.Anything, mock.Anything, false, false).Run(func(ctx context.Context, c model.Container, mounts []model.VolumeMount, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox, lockOwner, lockCapacity bool) {
		saved = c
	}).Return(nil)

	_, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.ContainerCreateParams{Name: "api", ImageTag: "nginx:latest"})
	require.NoError(t, err)
	require.Equal(t, "api", saved.NetworkAlias)
}

func TestContainerServiceCreatePreservesExplicitNetworkAlias(t *testing.T) {
	ownerID := uuid.New()
	repo, svc := newContainerCreateService(t, ownerID)
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
}

func TestContainerServiceCleanupUserNetworkIfUnusedRemovesNetwork(t *testing.T) {
	ownerID := uuid.New()
	repo := newContainerRepository(t)
	dockerAPI := coremocks.NewContainerDockerAPI(t)
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:     repo,
		Docker:         dockerAPI,
		DockerDataPath: "",
		Logger:         slog.Default(),
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
		Repository:     repo,
		Docker:         dockerAPI,
		DockerDataPath: "",
		Logger:         slog.Default(),
	})

	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(1, nil)

	require.NoError(t, svc.CleanupUserNetworkIfUnused(context.Background(), ownerID))
}

func TestContainerServiceAdminScopeDeleteCleansUserNetwork(t *testing.T) {
	ownerID := uuid.New()
	containerID := uuid.New()
	store := newContainerStore()
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository:     store,
		Docker:         coremocks.NewContainerDockerAPI(t),
		DockerDataPath: "",
		Logger:         slog.Default(),
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
		Repository:     repo,
		Images:         newContainerImageRepository(t),
		Docker:         coremocks.NewContainerDockerAPI(t),
		Metrics:        coremocks.NewHostMetricsProvider(t),
		Config:         testConfig(),
		Quotas:         testQuotas(t, nil, nil),
		DockerDataPath: "",
		Logger:         slog.Default(),
	})

	repo.EXPECT().
		List(mock.Anything, model.ContainerListOptions{
			ListOptions: model.ListOptions{OwnerID: &ownerID, ProjectID: &projectID, Limit: 25, Offset: 5},
			// Omitted values take the defaults and the search is trimmed.
			ContainerListFilter: model.ContainerListFilter{Status: model.ContainerFilterAll, Sort: model.ContainerSortStatus, Order: model.SortAscending, Search: "web"},
		}).
		Return(model.ContainerPage{Containers: []model.Container{{OwnerID: ownerID, ProjectID: &projectID}}, Total: 1}, nil)

	page, err := svc.List(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.ContainerListQuery{
		ProjectID: &projectID, Limit: 25, Offset: 5,
		ContainerListFilter: model.ContainerListFilter{Search: "  web "},
	})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Len(t, page.Containers, 1)
}

// An unknown status filter, sort key or order, or an overlong search, is a
// bad request that does not repeat the value and never reaches the store.
func TestContainerServiceListRefusesAnInvalidFilter(t *testing.T) {
	ctx := accessscope.WithUserScope(context.Background(), uuid.New(), "", "")
	for _, tc := range []struct {
		name   string
		filter model.ContainerListFilter
		value  string
	}{
		{name: "status", filter: model.ContainerListFilter{Status: "failing"}, value: "failing"},
		{name: "sort", filter: model.ContainerListFilter{Sort: "owner"}, value: "owner"},
		{name: "order", filter: model.ContainerListFilter{Order: "up"}, value: "up"},
		{name: "search", filter: model.ContainerListFilter{Search: strings.Repeat("x", model.ContainerSearchMaxLength+1)}, value: strings.Repeat("x", model.ContainerSearchMaxLength+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestContainerService(t, service.ContainerServiceDeps{
				Repository: newContainerRepository(t),
				Images:     newContainerImageRepository(t),
				Docker:     coremocks.NewContainerDockerAPI(t),
				Metrics:    coremocks.NewHostMetricsProvider(t),
				Config:     testConfig(),
				Quotas:     testQuotas(t, nil, nil),
				Logger:     slog.Default(),
			})

			_, err := svc.List(ctx, model.ContainerListQuery{Limit: 20, ContainerListFilter: tc.filter})

			require.ErrorIs(t, err, apperrors.ErrBadRequest)
			require.NotContains(t, apperrors.SafeMessage(err), tc.value)
		})
	}
}

// newContainerCreateService builds a service whose create path is admitted.
// Its strict Docker mock fails the test if a create request reaches Docker.
func newContainerCreateService(t *testing.T, ownerID uuid.UUID) (*coremocks.ContainerRepository, *service.ContainerService) {
	t.Helper()
	repo := newContainerRepository(t)
	imageRepo := newContainerImageRepository(t)
	metrics := coremocks.NewHostMetricsProvider(t)
	cfg := coretest.StaticConfig{Config: containerCreateConfig()}
	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil)
	repo.EXPECT().GetUserReservedMemory(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().GetUserReservedCPU(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().GetTotalSystemReservedMemory(mock.Anything).Return(int64(0), nil)
	repo.EXPECT().GetTotalSystemReservedCPU(mock.Anything).Return(int64(0), nil)
	metrics.EXPECT().GetTotalMemory().Return(int64(1024*1024*1024), nil)
	metrics.EXPECT().GetLogicalCPUs().Return(int64(2), nil)
	imageRepo.EXPECT().FindByOwnerAndTag(mock.Anything, mock.Anything, mock.Anything).Return(model.Image{}, false, nil)
	return repo, newTestContainerService(t, service.ContainerServiceDeps{
		Repository:     repo,
		Images:         imageRepo,
		Docker:         coremocks.NewContainerDockerAPI(t),
		Metrics:        metrics,
		Config:         cfg,
		Quotas:         testQuotas(t, cfg, map[uuid.UUID]model.UserQuota{ownerID: {CPUMillicores: 2000, RAMMB: 1024, DiskMB: 1024}}),
		DockerDataPath: "",
		Logger:         slog.Default(),
	})
}

func containerCreateConfig() config.SystemConfig {
	cfg := testSystemConfig()
	cfg.MaxContainersPerUser = 10
	cfg.DefaultMemoryReservation = 128 * 1024 * 1024
	cfg.OvercommitFactor = 1
	cfg.MaxLogSize = "10m"
	cfg.MaxLogFiles = "3"
	cfg.ContainerDiskQuota = "1G"
	return cfg
}
