package service_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// An active build reserves BuildMemoryBytes of host memory. With a 1 GiB host
// pool, a 512 MiB container fits beside no build and does not fit beside one
// 768 MiB build.
func TestContainerAdmissionCountsActiveBuildMemory(t *testing.T) {
	const (
		mib              = int64(1024 * 1024)
		hostMemory       = 1024 * mib
		containerMemory  = 512 * mib
		buildMemoryBytes = 768 * mib
	)
	for _, entry := range []string{"create", "start"} {
		for _, tc := range []struct {
			name         string
			activeBuilds int
			wantErr      error
		}{
			{name: "no active build admits", activeBuilds: 0},
			{name: "active build memory rejects", activeBuilds: 1, wantErr: apperrors.ErrHostExhausted},
		} {
			t.Run(entry+"/"+tc.name, func(t *testing.T) {
				ownerID, containerID := uuid.New(), uuid.New()
				cfg := containerCreateConfig()
				cfg.ReservedSystemMemory = 0
				cfg.OvercommitFactor = 1
				cfg.BuildMemoryBytes = buildMemoryBytes
				cfg.BuildCPUQuota = 0

				repo := newContainerRepository(t)
				repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil).Maybe()
				repo.EXPECT().GetUserReservedMemory(mock.Anything, ownerID).Return(int64(0), nil).Maybe()
				repo.EXPECT().GetUserReservedCPU(mock.Anything, ownerID).Return(int64(0), nil).Maybe()
				repo.EXPECT().GetTotalSystemReservedMemory(mock.Anything).Return(int64(0), nil).Maybe()
				repo.EXPECT().GetTotalSystemReservedCPU(mock.Anything).Return(int64(0), nil).Maybe()
				repo.EXPECT().GetByID(mock.Anything, containerID).Return(model.Container{
					ID:                    containerID,
					OwnerID:               ownerID,
					DockerID:              "docker-id",
					Status:                model.ContainerStatusExited,
					BaseMemoryReservation: containerMemory,
				}, nil).Maybe()
				queued := 0
				repo.EXPECT().SaveWithMountsOperationAndOutbox(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, false, false).
					RunAndReturn(func(context.Context, model.Container, []model.VolumeMount, model.ResourceOperation, model.ContainerLifecycleOutbox, bool, bool) error {
						queued++
						return nil
					}).Maybe()
				repo.EXPECT().QueueContainerOperation(mock.Anything, containerID, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					RunAndReturn(func(context.Context, uuid.UUID, string, string, model.ResourceOperation, model.ContainerLifecycleOutbox) error {
						queued++
						return nil
					}).Maybe()

				images := newContainerImageRepository(t)
				images.EXPECT().List(mock.Anything, mock.Anything).Return(nil, 0, nil).Maybe()
				metrics := coremocks.NewHostMetricsProvider(t)
				metrics.EXPECT().GetTotalMemory().Return(hostMemory, nil).Maybe()
				metrics.EXPECT().GetLogicalCPUs().Return(int64(4), nil).Maybe()
				users := coremocks.NewUserInfoProvider(t)
				users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaRAMMB: 4096, QuotaDiskMB: 1024, QuotaCPU: 4}, nil).Maybe()
				configProvider := coremocks.NewConfigManager(t)
				configProvider.EXPECT().Get().Return(cfg).Maybe()
				builds := coremocks.NewActiveSystemBuildCounter(t)
				builds.EXPECT().CountActive(mock.Anything).Return(tc.activeBuilds, nil).Maybe()

				svc := newTestContainerService(t, service.ContainerServiceDeps{
					Repository: repo,
					Images:     images,
					Metrics:    metrics,
					Config:     configProvider,
					Users:      users,
					Builds:     builds,
					Logger:     slog.Default(),
				})
				ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")

				var err error
				switch entry {
				case "create":
					_, err = svc.Create(ctx, model.ContainerCreateParams{Name: "web", ImageTag: "nginx:latest", RequestedMemoryMB: containerMemory / mib})
				case "start":
					err = svc.Start(ctx, containerID)
				}

				if tc.wantErr != nil {
					require.ErrorIs(t, err, tc.wantErr)
					require.Zero(t, queued, "rejected work must not be queued")
					return
				}
				require.NoError(t, err)
				require.Equal(t, 1, queued)
			})
		}
	}
}
