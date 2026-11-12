package resource_flow_test

import (
	"context"
	"encoding/json"
	"testing"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
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

func TestResourceFlowUsesScopeAndPersistsLifecycleOutbox(t *testing.T) {
	ctx := context.Background()
	repos := coretest.OpenCoreRepositories(t)
	cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
	ownerID := uuid.New()
	otherOwnerID := uuid.New()
	adminID := uuid.New()

	// Every owner has the default quota in Core's store.
	quotas := repos.UserQuotas(t, cfg)
	volumeDocker := coremocks.NewVolumeDockerAPI(t)
	volumeDocker.EXPECT().
		CreateVolume(mock.Anything, mock.AnythingOfType("model.VolumeRuntimeSpec")).
		Return("docker-volume", nil).
		Once()
	volumeDocker.EXPECT().RemoveVolume(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	containerDocker := coremocks.NewContainerDockerAPI(t)
	containerDocker.EXPECT().RemoveNetwork(mock.Anything, mock.Anything).Return(nil).Maybe()
	metrics := coremocks.NewHostMetricsProvider(t)
	metrics.EXPECT().GetTotalMemory().Return(int64(64<<30), nil).Maybe()
	metrics.EXPECT().GetLogicalCPUs().Return(int64(16), nil).Maybe()
	statsMetrics := coremocks.NewStatsMetricsProvider(t)
	statsMetrics.EXPECT().GetCPULoad().Return(1.0, nil).Maybe()
	statsMetrics.EXPECT().GetMemoryStats().Return(model.HostMemoryStats{TotalBytes: 64 << 30, AvailableBytes: 32 << 30}, nil).Maybe()
	statsMetrics.EXPECT().GetDiskUsage(mock.Anything).Return(model.HostDiskStats{TotalBytes: 1 << 40, FreeBytes: 1 << 39}, nil).Maybe()

	lifecycle := repository.NewResourceLifecycleRepository(repos.DB)
	volumes, err := service.NewVolumeService(service.VolumeServiceDeps{
		Repository:   repos.Volumes,
		Docker:       volumeDocker,
		Config:       cfg,
		Lifecycle:    lifecycle,
		DiskMetrics:  coremocks.NewHostDiskMetricsProvider(t),
		Quotas:       quotas,
		ImageRepo:    repos.Images,
		CapacityLock: repos.Containers,
		SizeMode:     coretest.MeasuredVolumeSizeMode(t),
	})
	require.NoError(t, err)
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
	images, err := service.NewImageService(service.ImageServiceDeps{
		Repository:   repos.Images,
		Docker:       coremocks.NewImageDockerAPI(t),
		Registry:     coremocks.NewImageRegistryAPI(t),
		Containers:   repos.Containers,
		Config:       cfg,
		Lifecycle:    lifecycle,
		RegistryAuth: repos.RegistryAuthorization(t),
	})
	require.NoError(t, err)
	borrowed := coremocks.NewStatsBorrowedMemory(t)
	borrowed.EXPECT().BorrowedMemoryBytes().Return(0).Maybe()
	rootfsQuota := coremocks.NewStatsRootfsQuota(t)
	rootfsQuota.EXPECT().RootfsQuotaStatus().Return(model.RootfsQuotaUnknown).Maybe()
	stats, err := service.NewStatsService(service.StatsServiceDeps{
		Containers: repos.Containers, Volumes: repos.Volumes, Images: repos.Images, Builds: repos.Builds, Projects: repos.Projects,
		Metrics: statsMetrics, Config: cfg, Quotas: quotas, Borrowed: borrowed, RootfsQuota: rootfsQuota,
		VolumeSizeMode: coretest.MeasuredVolumeSizeMode(t),
	})
	require.NoError(t, err)

	ownerCtx := coretest.UserContext(ownerID)
	volumeID, err := volumes.Create(ownerCtx, model.VolumeCreateParams{Name: "data"})
	require.NoError(t, err)
	volume, err := repos.Volumes.GetByID(ctx, volumeID)
	require.NoError(t, err)
	require.Equal(t, ownerID, volume.OwnerID)
	require.Equal(t, model.VolumeStatusCreating, volume.Status)
	volumeDocker.AssertNotCalled(t, "CreateVolume", mock.Anything, mock.Anything)

	var payload []byte
	require.NoError(t, repos.DB.QueryRow(ctx, `SELECT payload FROM resource_lifecycle_queue_outbox`).Scan(&payload))
	var message queue.ResourceLifecycleMessage
	require.NoError(t, json.Unmarshal(payload, &message))
	require.Equal(t, volumeID.String(), message.ResourceID)
	require.Equal(t, ownerID.String(), message.OwnerID)
	require.Equal(t, model.ResourceTypeVolume, message.ResourceType)
	require.Equal(t, model.OperationCreate, message.Operation)
	worker := service.NewResourceLifecycleWorker(volumes, images, coretest.DiscardLogger())
	require.NoError(t, worker.HandleMessage(ctx, message))
	operation, err := lifecycle.GetOperationByID(ctx, uuid.MustParse(message.OperationID))
	require.NoError(t, err)
	require.Equal(t, model.OperationStatusDone, operation.Status)
	volume, err = volumes.GetByID(ownerCtx, volumeID)
	require.NoError(t, err)
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

	userContainers, total, err := containers.List(ownerCtx, 20, 0, nil)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, userContainers, 1)

	otherContainers, total, err := containers.List(coretest.UserContext(otherOwnerID), 20, 0, nil)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, otherContainers)

	adminContainers, total, err := containers.List(coretest.AdminContext(adminID), 20, 0, nil)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, adminContainers, 1)

	_, err = containers.Create(ownerCtx, model.ContainerCreateParams{Name: "web2", ImageTag: "nginx:latest", DomainPrefix: "web", InternalPort: 8081})
	require.ErrorIs(t, err, apperrors.ErrAlreadyExists)
	require.Equal(t, "subdomain web.example.test is already in use", apperrors.SafeMessage(err))

	missingVolumeID := uuid.New()
	require.NoError(t, repos.Volumes.Save(ctx, model.Volume{ID: missingVolumeID, OwnerID: ownerID, Name: "missing", DockerName: "vol_missing", Status: model.VolumeStatusMissing, SizeBytes: 1 << 20}))
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
	conn := coretest.NewCoreGRPCConn(t, coretest.CoreGRPCRegistration{
		Containers: containers,
		Volumes:    volumes,
		Images:     images,
		Stats:      stats,
		Users:      userDirectory,
	})
	client := coreapi.NewContainerAPIClient(conn)
	grpcCtx := coretest.InternalGRPCContext(context.Background(), coretest.GatewayCredential, accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID, Username: "alice", Role: "user"})

	listResp, err := client.ListContainers(grpcCtx, &coreapi.PaginationRequest{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, int32(1), listResp.TotalCount)
	require.Equal(t, "alice", listResp.Containers[0].OwnerUsername)

	target, err := containers.GetRuntimeTarget(accessscope.WithUserScope(context.Background(), ownerID, "alice", "user"), containerID)
	require.NoError(t, err)
	require.Equal(t, ownerID, target.OwnerID)
	require.NotEmpty(t, target.DockerID)

	_, err = client.ListContainers(context.Background(), &coreapi.PaginationRequest{Limit: 10})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestResourceFlowQuotaExceeded(t *testing.T) {
	repos := coretest.OpenCoreRepositories(t)
	cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
	ownerID := uuid.New()
	repos.SetCustomQuota(t, ownerID, model.UserQuota{CPUMillicores: 4000, RAMMB: 1, DiskMB: 4096})
	quotas := repos.UserQuotas(t, cfg)
	metrics := coremocks.NewHostMetricsProvider(t)
	metrics.EXPECT().GetTotalMemory().Return(int64(64<<30), nil).Maybe()
	metrics.EXPECT().GetLogicalCPUs().Return(int64(16), nil).Maybe()

	containers, err := service.NewContainerService(service.ContainerServiceDeps{
		Repository:   repos.Containers,
		Builds:       repos.Builds,
		DiskMetrics:  coremocks.NewHostDiskMetricsProvider(t),
		Volumes:      repos.Volumes,
		Images:       repos.Images,
		Docker:       coremocks.NewContainerDockerAPI(t),
		Metrics:      metrics,
		Config:       cfg,
		Quotas:       quotas,
		HostDiskPath: "",
		Logger:       coretest.DiscardLogger(),
	})
	require.NoError(t, err)

	_, err = containers.Create(coretest.UserContext(ownerID), model.ContainerCreateParams{
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

func mustResourceOperations(t *testing.T, db *pgxpool.Pool) []operationRow {
	t.Helper()
	rows, err := db.Query(context.Background(), `SELECT operation, status FROM resource_operations WHERE resource_type = 'container' ORDER BY created_at, id`)
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

func mustContainerOutbox(t *testing.T, db *pgxpool.Pool) []queue.ContainerLifecycleMessage {
	t.Helper()
	rows, err := db.Query(context.Background(), `SELECT payload FROM container_lifecycle_queue_outbox ORDER BY created_at, id`)
	require.NoError(t, err)
	defer rows.Close()
	var messages []queue.ContainerLifecycleMessage
	for rows.Next() {
		var payload []byte
		require.NoError(t, rows.Scan(&payload))
		var msg queue.ContainerLifecycleMessage
		require.NoError(t, json.Unmarshal(payload, &msg))
		messages = append(messages, msg)
	}
	require.NoError(t, rows.Err())
	return messages
}

func countTable(t *testing.T, db *pgxpool.Pool, table string) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+table).Scan(&count))
	return count
}
