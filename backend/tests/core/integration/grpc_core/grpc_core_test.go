package grpc_core_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	coregrpcmocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/grpc"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/coretest"
	"github.com/google/uuid"
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

	users := coremocks.NewUserInfoProvider(t)
	users.EXPECT().
		GetUser(mock.Anything, mock.AnythingOfType("uuid.UUID")).
		RunAndReturn(func(ctx context.Context, id uuid.UUID) (model.UserInfo, error) {
			return model.UserInfo{ID: id, Username: "user-" + id.String()[:8], QuotaRAMMB: 4096, QuotaDiskMB: 4096, QuotaCPU: 4}, nil
		}).
		Maybe()
	metrics := coremocks.NewHostMetricsProvider(t)
	metrics.EXPECT().GetTotalMemory().Return(int64(64<<30), nil).Maybe()
	containerDocker := coremocks.NewContainerDockerAPI(t)
	containerDocker.EXPECT().RemoveNetwork(mock.Anything, mock.Anything).Return(nil).Maybe()
	containers := service.NewContainerService(repos.Containers, repos.Volumes, repos.Images, containerDocker, metrics, cfg, users, "", coretest.DiscardLogger())
	volumes := service.NewVolumeService(repos.Volumes, coremocks.NewVolumeDockerAPI(t), cfg, service.VolumeServiceDeps{Users: users, ImageRepo: repos.Images})

	runtimeID := uuid.New()
	require.NoError(t, repos.Containers.Save(ctx, model.Container{
		ID:                    runtimeID,
		OwnerID:               ownerID,
		DockerID:              "docker-runtime",
		Name:                  "runtime",
		ImageTag:              "nginx:latest",
		Status:                model.ContainerStatusRunning,
		DesiredStatus:         model.ContainerStatusRunning,
		BaseMemoryReservation: 128,
		DockerGeneration:      3,
		EnvVars:               []byte(`{}`),
	}))

	userDirectory := coregrpcmocks.NewMockUserDirectory(t)
	userDirectory.EXPECT().
		GetUsers(mock.Anything, []uuid.UUID{ownerID}).
		Return(map[uuid.UUID]model.UserInfo{ownerID: {ID: ownerID, Username: "alice"}}, nil).
		Maybe()
	conn := coretest.NewCoreGRPCConn(t, "core-token", coretest.CoreGRPCRegistration{
		Containers: containers,
		Volumes:    volumes,
		Users:      userDirectory,
	})
	client := coreapi.NewContainerAPIClient(conn)

	_, err := client.ListContainers(context.Background(), &coreapi.PaginationRequest{Limit: 10})
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	wrongTokenCtx := coretest.InternalGRPCContext(context.Background(), "wrong", accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID})
	_, err = client.ListContainers(wrongTokenCtx, &coreapi.PaginationRequest{Limit: 10})
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	userCtx := coretest.InternalGRPCContext(context.Background(), "core-token", accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID, Username: "alice", Role: "user"})
	createResp, err := client.CreateContainer(userCtx, &coreapi.CreateContainerRequest{Name: "created", ImageTag: "nginx:latest"})
	require.NoError(t, err)
	require.NotEmpty(t, createResp.ContainerId)

	listResp, err := client.ListContainers(userCtx, &coreapi.PaginationRequest{Limit: 20})
	require.NoError(t, err)
	require.Equal(t, int32(2), listResp.TotalCount)

	targetResp, err := client.GetContainerRuntimeTarget(userCtx, &coreapi.ContainerRuntimeTargetRequest{ContainerId: runtimeID.String()})
	require.NoError(t, err)
	require.Equal(t, "docker-runtime", targetResp.DockerId)
	require.Equal(t, int32(3), targetResp.DockerGeneration)
	require.Equal(t, ownerID.String(), targetResp.OwnerId)

	otherCtx := coretest.InternalGRPCContext(context.Background(), "core-token", accessscope.Scope{Kind: accessscope.KindUser, UserID: otherOwnerID, Username: "bob", Role: "user"})
	_, err = client.GetContainerRuntimeTarget(otherCtx, &coreapi.ContainerRuntimeTargetRequest{ContainerId: runtimeID.String()})
	require.Equal(t, codes.NotFound, status.Code(err))

	_, err = client.StartContainer(userCtx, &coreapi.ContainerActionRequest{ContainerId: runtimeID.String()})
	require.NoError(t, err)
	require.Len(t, mustContainerOutbox(t, repos.DB), 2)
}

func mustContainerOutbox(t *testing.T, db *sql.DB) []map[string]any {
	t.Helper()
	rows, err := db.Query(`SELECT payload FROM container_lifecycle_queue_outbox ORDER BY created_at, id`)
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
