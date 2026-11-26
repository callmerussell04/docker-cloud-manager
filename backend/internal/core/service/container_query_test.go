package service_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

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

// One container is readable by its owner and by an administrator. To any
// other user it does not exist, so the answer never confirms the ID.
func TestContainerServiceGetAppliesOwnerAccess(t *testing.T) {
	owner := uuid.New()
	c := model.Container{ID: uuid.New(), OwnerID: owner, Name: "web", Status: model.ContainerStatusRunning}
	tests := []struct {
		name    string
		ctx     context.Context
		wantErr error
	}{
		{name: "owner", ctx: accessscope.WithUserScope(context.Background(), owner, "", "")},
		{name: "administrator", ctx: accessscope.WithAdminScope(context.Background(), uuid.New(), "", "")},
		{name: "another user", ctx: accessscope.WithUserScope(context.Background(), uuid.New(), "", ""), wantErr: apperrors.ErrNotFound},
		{name: "system scope", ctx: accessscope.WithSystemScope(context.Background()), wantErr: apperrors.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := coremocks.NewContainerRepository(t)
			repo.EXPECT().GetByID(mock.Anything, c.ID).Return(c, nil)
			svc := newTestContainerService(t, service.ContainerServiceDeps{Repository: repo})

			got, err := svc.Get(tt.ctx, c.ID)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Equal(t, model.Container{}, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, c, got)
		})
	}
}

func TestContainerServiceGetReportsAnUnknownContainer(t *testing.T) {
	id := uuid.New()
	repo := coremocks.NewContainerRepository(t)
	repo.EXPECT().GetByID(mock.Anything, id).Return(model.Container{}, apperrors.ErrNotFound)
	svc := newTestContainerService(t, service.ContainerServiceDeps{Repository: repo})

	_, err := svc.Get(accessscope.WithUserScope(context.Background(), uuid.New(), "", ""), id)

	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

// Container stats carry the standard limit beside the current one, so the
// owner can see when the container runs on borrowed memory. The standard
// limit is the reservation times the burst multiplier in force now.
func TestContainerServiceGetStatsReportsTheStandardLimit(t *testing.T) {
	ctx := context.Background()
	repo := coremocks.NewContainerRepository(t)
	owner := uuid.New()
	c := model.Container{
		ID: uuid.New(), OwnerID: owner, Name: "web", DockerID: "docker-web", DockerGeneration: 1,
		Status: model.ContainerStatusRunning, DesiredStatus: model.ContainerStatusRunning,
		BaseMemoryReservation: 256 * pressureMiB,
	}
	repo.EXPECT().GetByID(mock.Anything, c.ID).Return(c, nil)
	docker := coremocks.NewContainerDockerAPI(t)
	docker.EXPECT().GetContainerStats(mock.Anything, "docker-web").
		Return(model.ContainerStats{MemoryUsageBytes: 900 * pressureMiB, MemoryLimitBytes: 1536 * pressureMiB}, nil)
	cfg := testSystemConfig()
	cfg.MaxBurstMultiplier = 4
	svc := newTestContainerService(t, service.ContainerServiceDeps{Repository: repo, Docker: docker, Config: coretest.StaticConfig{Config: cfg}})

	stats, err := svc.GetStats(accessscope.WithUserScope(ctx, owner, "", ""), c.ID)

	require.NoError(t, err)
	require.Equal(t, model.ContainerStats{
		MemoryUsageBytes:         900 * pressureMiB,
		MemoryLimitBytes:         1536 * pressureMiB,
		MemoryStandardLimitBytes: 1024 * pressureMiB,
	}, stats)
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
