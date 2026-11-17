package grpc_core_test

import (
	"context"
	"encoding/json"
	"testing"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coregrpcmocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/grpc"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCoreGRPCInternalAuthScopeAndRuntimeTarget(t *testing.T) {
	ctx := context.Background()
	repos := coretest.OpenCoreRepositories(t)
	cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
	ownerID := uuid.New()
	otherOwnerID := uuid.New()

	quotas := repos.UserQuotas(t, cfg)
	metrics := coremocks.NewHostMetricsProvider(t)
	metrics.EXPECT().GetTotalMemory().Return(int64(64<<30), nil).Maybe()
	metrics.EXPECT().GetLogicalCPUs().Return(int64(16), nil).Maybe()
	containerDocker := coremocks.NewContainerDockerAPI(t)
	containerDocker.EXPECT().RemoveNetwork(mock.Anything, mock.Anything).Return(nil).Maybe()
	containers, err := service.NewContainerService(service.ContainerServiceDeps{
		Repository:   repos.Containers,
		Builds:       repos.Builds,
		DiskMetrics:  coremocks.NewHostDiskMetricsProvider(t),
		Volumes:      repos.Volumes,
		Images:       repos.Images,
		Docker:       containerDocker,
		Metrics:      metrics,
		Config:       cfg,
		Quotas:       quotas,
		HostDiskPath: "",
		Logger:       coretest.DiscardLogger(),
	})
	require.NoError(t, err)
	volumes, err := service.NewVolumeService(service.VolumeServiceDeps{
		Repository:   repos.Volumes,
		Docker:       coremocks.NewVolumeDockerAPI(t),
		Config:       cfg,
		Lifecycle:    repository.NewResourceLifecycleRepository(repos.DB),
		DiskMetrics:  coremocks.NewHostDiskMetricsProvider(t),
		Quotas:       quotas,
		ImageRepo:    repos.Images,
		CapacityLock: repos.Containers,
		SizeMode:     coretest.MeasuredVolumeSizeMode(t),
	})
	require.NoError(t, err)

	runtimeID := uuid.New()
	require.NoError(t, repos.Containers.Save(ctx, model.Container{
		ID:                    runtimeID,
		OwnerID:               ownerID,
		DockerID:              "docker-runtime",
		Name:                  "runtime",
		ImageTag:              "nginx:latest",
		Status:                model.ContainerStatusExited,
		DesiredStatus:         model.ContainerStatusExited,
		BaseMemoryReservation: 128,
		DockerGeneration:      3,
		EnvVars:               []byte(`{}`),
	}))

	userDirectory := coregrpcmocks.NewMockUserDirectory(t)
	userDirectory.EXPECT().
		GetUsers(mock.Anything, []uuid.UUID{ownerID}).
		Return(map[uuid.UUID]model.UserInfo{ownerID: {ID: ownerID, Username: "alice"}}, nil).
		Maybe()
	conn := coretest.NewCoreGRPCConn(t, coretest.CoreGRPCRegistration{
		Containers: containers,
		Volumes:    volumes,
		Users:      userDirectory,
	})
	client := coreapi.NewContainerAPIClient(conn)

	_, err = client.ListContainers(context.Background(), &coreapi.PaginationRequest{Limit: 10})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	wrongTokenCtx := coretest.InternalGRPCContext(context.Background(), "wrong", accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID})
	_, err = client.ListContainers(wrongTokenCtx, &coreapi.PaginationRequest{Limit: 10})
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	userCtx := coretest.InternalGRPCContext(context.Background(), coretest.GatewayCredential, accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID, Username: "alice", Role: "user"})
	createResp, err := client.CreateContainer(userCtx, &coreapi.CreateContainerRequest{Name: "created", ImageTag: "nginx:latest"})
	require.NoError(t, err)
	require.NotEmpty(t, createResp.ContainerId)

	listResp, err := client.ListContainers(userCtx, &coreapi.PaginationRequest{Limit: 20})
	require.NoError(t, err)
	require.Equal(t, int32(2), listResp.TotalCount)

	// Container streams resolve their target in-process, under the same owner access.
	target, err := containers.GetRuntimeTarget(accessscope.WithUserScope(context.Background(), ownerID, "alice", "user"), runtimeID)
	require.NoError(t, err)
	require.Equal(t, "docker-runtime", target.DockerID)
	require.Equal(t, 3, target.DockerGeneration)
	require.Equal(t, ownerID, target.OwnerID)

	_, err = containers.GetRuntimeTarget(accessscope.WithUserScope(context.Background(), otherOwnerID, "bob", "user"), runtimeID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)

	_, err = client.StartContainer(userCtx, &coreapi.ContainerActionRequest{ContainerId: runtimeID.String()})
	require.NoError(t, err)
	require.Len(t, mustContainerOutbox(t, repos.DB), 2)
}

// Core owns quotas, so admission never asks the identity service: with every
// call to it failing, container create, container start and volume create
// are still admitted or rejected on their merits.
func TestCoreAdmitsWithTheIdentityServiceUnreachable(t *testing.T) {
	repos := coretest.OpenCoreRepositories(t)
	cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
	roomyID, limitedID := uuid.New(), uuid.New()
	// roomyID has the default quota; limitedID a custom quota too small for
	// any request.
	repos.SetCustomQuota(t, limitedID, model.UserQuota{CPUMillicores: 1, RAMMB: 1, DiskMB: 1})
	quotas := repos.UserQuotas(t, cfg)

	unreachable := coregrpcmocks.NewMockUserDirectory(t)
	unreachable.EXPECT().GetUsers(mock.Anything, mock.Anything).Return(nil, apperrors.ErrUnavailable).Maybe()
	metrics := coremocks.NewHostMetricsProvider(t)
	metrics.EXPECT().GetTotalMemory().Return(int64(64<<30), nil).Maybe()
	metrics.EXPECT().GetLogicalCPUs().Return(int64(16), nil).Maybe()
	containers, err := service.NewContainerService(service.ContainerServiceDeps{
		Repository:  repos.Containers,
		Builds:      repos.Builds,
		DiskMetrics: coremocks.NewHostDiskMetricsProvider(t),
		Volumes:     repos.Volumes,
		Images:      repos.Images,
		Docker:      coremocks.NewContainerDockerAPI(t),
		Metrics:     metrics,
		Config:      cfg,
		Quotas:      quotas,
		Logger:      coretest.DiscardLogger(),
	})
	require.NoError(t, err)
	volumes, err := service.NewVolumeService(service.VolumeServiceDeps{
		Repository:   repos.Volumes,
		Docker:       coremocks.NewVolumeDockerAPI(t),
		Config:       cfg,
		Lifecycle:    repository.NewResourceLifecycleRepository(repos.DB),
		DiskMetrics:  coremocks.NewHostDiskMetricsProvider(t),
		Quotas:       quotas,
		ImageRepo:    repos.Images,
		CapacityLock: repos.Containers,
		SizeMode:     coretest.MeasuredVolumeSizeMode(t),
	})
	require.NoError(t, err)
	conn := coretest.NewCoreGRPCConn(t, coretest.CoreGRPCRegistration{
		Containers: containers,
		Volumes:    volumes,
		Users:      unreachable,
	})
	containerClient := coreapi.NewContainerAPIClient(conn)
	volumeClient := coreapi.NewVolumeAPIClient(conn)

	for _, tc := range []struct {
		name    string
		ownerID uuid.UUID
		want    codes.Code
	}{
		{name: "an owner within the default quota is admitted", ownerID: roomyID, want: codes.OK},
		{name: "an owner over a custom quota is rejected", ownerID: limitedID, want: codes.ResourceExhausted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := coretest.InternalGRPCContext(context.Background(), coretest.GatewayCredential, accessscope.Scope{Kind: accessscope.KindUser, UserID: tc.ownerID, Username: "owner", Role: "user"})
			stopped := uuid.New()
			require.NoError(t, repos.Containers.Save(context.Background(), model.Container{
				ID:                    stopped,
				OwnerID:               tc.ownerID,
				DockerID:              "docker-" + stopped.String()[:12],
				Name:                  "stopped-" + stopped.String()[:8],
				ImageTag:              "nginx:latest",
				Status:                model.ContainerStatusExited,
				DesiredStatus:         model.ContainerStatusExited,
				BaseMemoryReservation: 128 << 20,
				BaseCPUReservation:    250,
				EnvVars:               []byte(`{}`),
			}))

			_, err := containerClient.CreateContainer(ctx, &coreapi.CreateContainerRequest{Name: "created", ImageTag: "nginx:latest"})
			require.Equal(t, tc.want, status.Code(err), "container create: %v", err)
			_, err = containerClient.StartContainer(ctx, &coreapi.ContainerActionRequest{ContainerId: stopped.String()})
			require.Equal(t, tc.want, status.Code(err), "container start: %v", err)
			_, err = volumeClient.CreateVolume(ctx, &coreapi.CreateVolumeRequest{Name: "data"})
			require.Equal(t, tc.want, status.Code(err), "volume create: %v", err)
		})
	}
}

func mustContainerOutbox(t *testing.T, db *pgxpool.Pool) []map[string]any {
	t.Helper()
	rows, err := db.Query(context.Background(), `SELECT payload FROM container_lifecycle_queue_outbox ORDER BY created_at, id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var payload []byte
		require.NoError(t, rows.Scan(&payload))
		var decoded map[string]any
		require.NoError(t, json.Unmarshal(payload, &decoded))
		out = append(out, decoded)
	}
	require.NoError(t, rows.Err())
	return out
}
