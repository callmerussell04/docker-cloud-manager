package service_test

import (
	"context"
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
