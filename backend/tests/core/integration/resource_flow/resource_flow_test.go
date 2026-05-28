package resource_flow_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/containerqueue"
	coregrpcmocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/grpc"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/coretest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestResourceFlowUsesScopeAndPersistsLifecycleOutbox(t *testing.T) {
	ctx := context.Background()
	repos := coretest.OpenCoreRepositories(t)
	cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
	ownerID := uuid.New()
	otherOwnerID := uuid.New()
	adminID := uuid.New()

	users := coremocks.NewUserInfoProvider(t)
	users.EXPECT().
		GetUser(mock.Anything, mock.AnythingOfType("uuid.UUID")).
		RunAndReturn(func(ctx context.Context, id uuid.UUID) (model.UserInfo, error) {
			return model.UserInfo{ID: id, Username: "user-" + id.String()[:8], QuotaRAMMB: 4096, QuotaDiskMB: 4096, QuotaCPU: 4}, nil
		}).
		Maybe()
	volumeDocker := coremocks.NewVolumeDockerAPI(t)
	volumeDocker.EXPECT().
		CreateVolume(mock.Anything, mock.AnythingOfType("model.VolumeRuntimeSpec")).
		Return("docker-volume", nil).
		Maybe()
	volumeDocker.EXPECT().RemoveVolume(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	containerDocker := coremocks.NewContainerDockerAPI(t)
	containerDocker.EXPECT().RemoveNetwork(mock.Anything, mock.Anything).Return(nil).Maybe()
	metrics := coremocks.NewHostMetricsProvider(t)
	metrics.EXPECT().GetTotalMemory().Return(int64(64<<30), nil).Maybe()
	statsMetrics := coremocks.NewStatsMetricsProvider(t)
	statsMetrics.EXPECT().GetCPULoad().Return(1.0, nil).Maybe()
	statsMetrics.EXPECT().GetMemoryStats().Return(model.HostMemoryStats{TotalBytes: 64 << 30, AvailableBytes: 32 << 30}, nil).Maybe()
	statsMetrics.EXPECT().GetDiskUsage(mock.Anything).Return(model.HostDiskStats{TotalBytes: 1 << 40, FreeBytes: 1 << 39}, nil).Maybe()

	volumes := service.NewVolumeService(repos.Volumes, volumeDocker, cfg, service.VolumeServiceDeps{
		Users:     users,
		ImageRepo: repos.Images,
	})
	containers := service.NewContainerService(repos.Containers, repos.Volumes, repos.Images, containerDocker, metrics, cfg, users, "", coretest.DiscardLogger())
	images := service.NewImageService(repos.Images, coremocks.NewImageDockerAPI(t), coremocks.NewImageRegistryAPI(t), repos.Containers, cfg)
	stats := service.NewStatsService(repos.Containers, repos.Volumes, repos.Images, repos.Builds, repos.Projects, statsMetrics, "", cfg, users)

	ownerCtx := coretest.UserContext(ownerID)
	volumeID, err := volumes.Create(ownerCtx, model.VolumeCreateParams{Name: "data"})
	require.NoError(t, err)
	volume, err := repos.Volumes.GetByID(ctx, volumeID)
	require.NoError(t, err)
	require.Equal(t, ownerID, volume.OwnerID)
	require.Equal(t, model.VolumeStatusAvailable, volume.Status)

	containerID, err := containers.Create(ownerCtx, model.ContainerCreateParams{
		Name:         "web",
		ImageTag:     "nginx:latest",
		DomainPrefix: "web",
		InternalPort: 8080,
		VolumeMounts: []model.VolumeMountParams{{VolumeID: volumeID, MountPath: "/data"}},
	})
	require.NoError(t, err)

	container, err := repos.Containers.GetByID(ctx, containerID)
	require.NoError(t, err)
	require.Equal(t, ownerID, container.OwnerID)
	require.Equal(t, model.ContainerStatusPending, container.Status)
	require.Equal(t, model.ContainerStatusCreated, container.DesiredStatus)
	require.Equal(t, "nginx:latest", container.ImageTag)
	require.Len(t, mustContainerOutbox(t, repos.DB), 1)

	userContainers, total, err := containers.List(ownerCtx, 20, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, userContainers, 1)

	otherContainers, total, err := containers.List(coretest.UserContext(otherOwnerID), 20, 0)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, otherContainers)

	adminContainers, total, err := containers.List(coretest.AdminContext(adminID), 20, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, adminContainers, 1)

	_, err = containers.Create(ownerCtx, model.ContainerCreateParams{Name: "web2", ImageTag: "nginx:latest", DomainPrefix: "web", InternalPort: 8081})
	require.ErrorIs(t, err, apperrors.ErrAlreadyExists)
	require.Equal(t, "subdomain web.localhost is already in use", apperrors.SafeMessage(err))

	missingVolumeID := uuid.New()
	require.NoError(t, repos.Volumes.Save(ctx, model.Volume{ID: missingVolumeID, OwnerID: ownerID, Name: "missing", DockerName: "vol_missing", Status: model.VolumeStatusMissing}))
	_, err = containers.Create(ownerCtx, model.ContainerCreateParams{
		Name:         "missing-volume",
		ImageTag:     "nginx:latest",
		VolumeMounts: []model.VolumeMountParams{{VolumeID: missingVolumeID, MountPath: "/data"}},
	})
	require.ErrorIs(t, err, apperrors.ErrConflict)

	_, err = containers.Create(coretest.UserContext(otherOwnerID), model.ContainerCreateParams{
		Name:         "foreign-volume",
		ImageTag:     "nginx:latest",
		VolumeMounts: []model.VolumeMountParams{{VolumeID: volumeID, MountPath: "/data"}},
	})
	require.ErrorIs(t, err, apperrors.ErrNotFound)

	require.NoError(t, repos.Containers.UpdateDockerIDAndStatus(ctx, containerID, "docker-"+containerID.String()[:8], model.ContainerStatusExited))
	require.NoError(t, repos.Containers.CompleteLatestOperation(ctx, model.ResourceTypeContainer, containerID, model.OperationStatusDone, nil))
	require.NoError(t, containers.Start(ownerCtx, containerID))

	operations := mustResourceOperations(t, repos.DB)
	require.Len(t, operations, 2)
	require.Equal(t, model.OperationStart, operations[1].Operation)
	require.Equal(t, model.OperationStatusPending, operations[1].Status)
	require.Len(t, mustContainerOutbox(t, repos.DB), 2)

	userDirectory := coregrpcmocks.NewMockUserDirectory(t)
	userDirectory.EXPECT().
		GetUsers(mock.Anything, []uuid.UUID{ownerID}).
		Return(map[uuid.UUID]model.UserInfo{ownerID: {ID: ownerID, Username: "alice"}}, nil).
		Maybe()
	conn := coretest.NewCoreGRPCConn(t, "core-token", coretest.CoreGRPCRegistration{
		Containers: containers,
		Volumes:    volumes,
		Images:     images,
		Stats:      stats,
		Users:      userDirectory,
	})
	client := coreapi.NewContainerAPIClient(conn)
	grpcCtx := coretest.InternalGRPCContext(context.Background(), "core-token", accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID, Username: "alice", Role: "user"})

	listResp, err := client.ListContainers(grpcCtx, &coreapi.PaginationRequest{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, int32(1), listResp.TotalCount)
	require.Equal(t, "alice", listResp.Containers[0].OwnerUsername)

	targetResp, err := client.GetContainerRuntimeTarget(grpcCtx, &coreapi.ContainerRuntimeTargetRequest{ContainerId: containerID.String()})
	require.NoError(t, err)
	require.Equal(t, ownerID.String(), targetResp.OwnerId)
	require.NotEmpty(t, targetResp.DockerId)

	_, err = client.GetContainerRuntimeTarget(context.Background(), &coreapi.ContainerRuntimeTargetRequest{ContainerId: containerID.String()})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestResourceFlowQuotaExceeded(t *testing.T) {
	repos := coretest.OpenCoreRepositories(t)
	cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
	ownerID := uuid.New()
	users := coremocks.NewUserInfoProvider(t)
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{
		ID: ownerID, Username: "limited", QuotaRAMMB: 1, QuotaDiskMB: 4096,
	}, nil).Maybe()
	metrics := coremocks.NewHostMetricsProvider(t)
	metrics.EXPECT().GetTotalMemory().Return(int64(64<<30), nil).Maybe()

	containers := service.NewContainerService(
		repos.Containers,
		repos.Volumes,
		repos.Images,
		coremocks.NewContainerDockerAPI(t),
		metrics,
		cfg,
		users,
		"",
		coretest.DiscardLogger(),
	)

	_, err := containers.Create(coretest.UserContext(ownerID), model.ContainerCreateParams{
		Name:              "too-big",
		ImageTag:          "nginx:latest",
		RequestedMemoryMB: 2,
	})
	require.ErrorIs(t, err, apperrors.ErrQuotaExceeded)
	require.Zero(t, countTable(t, repos.DB, "containers"))
}

type operationRow struct {
	Operation string
	Status    string
}

func mustResourceOperations(t *testing.T, db *sql.DB) []operationRow {
	t.Helper()
	rows, err := db.Query(`SELECT operation, status FROM resource_operations ORDER BY created_at, id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []operationRow
	for rows.Next() {
		var row operationRow
		require.NoError(t, rows.Scan(&row.Operation, &row.Status))
		out = append(out, row)
	}
	require.NoError(t, rows.Err())
	return out
}

func mustContainerOutbox(t *testing.T, db *sql.DB) []containerqueue.LifecycleMessage {
	t.Helper()
	rows, err := db.Query(`SELECT payload FROM container_lifecycle_queue_outbox ORDER BY created_at, id`)
	require.NoError(t, err)
	defer rows.Close()
	var messages []containerqueue.LifecycleMessage
	for rows.Next() {
		var payload []byte
		require.NoError(t, rows.Scan(&payload))
		var msg containerqueue.LifecycleMessage
		require.NoError(t, json.Unmarshal(payload, &msg))
		messages = append(messages, msg)
	}
	require.NoError(t, rows.Err())
	return messages
}

func countTable(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&count))
	return count
}
