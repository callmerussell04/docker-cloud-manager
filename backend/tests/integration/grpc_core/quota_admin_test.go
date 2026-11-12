package grpc_core_test

import (
	"context"
	"testing"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coregrpcmocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/grpc"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// quotaAdminServer serves the quota and container APIs over the test
// database, so a quota an administrator sets decides the next admission.
func quotaAdminServer(t *testing.T) *grpc.ClientConn {
	t.Helper()
	repos := coretest.OpenCoreRepositories(t)
	cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
	quotas := repos.UserQuotas(t, cfg)
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
	return coretest.NewCoreGRPCConn(t, coretest.CoreGRPCRegistration{
		Containers: containers,
		Quotas:     quotas,
		Users:      coregrpcmocks.NewMockUserDirectory(t),
	})
}

func adminGRPCContext() context.Context {
	return coretest.InternalGRPCContext(context.Background(), coretest.GatewayCredential, accessscope.Scope{Kind: accessscope.KindAdmin, UserID: uuid.New(), Username: "root", Role: "admin"})
}

// An administrator sets a custom quota, sees it marked custom, and resets it
// to the default; each change decides the user's next admission.
func TestAdministratorSetsAndResetsAQuotaThatAdmissionUses(t *testing.T) {
	conn := quotaAdminServer(t)
	quotaClient := coreapi.NewQuotaAPIClient(conn)
	containerClient := coreapi.NewContainerAPIClient(conn)
	admin := adminGRPCContext()
	ownerID, otherID := uuid.New(), uuid.New()
	defaults := coretest.IntegrationConfig()
	owner := coretest.InternalGRPCContext(context.Background(), coretest.GatewayCredential, accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID, Username: "alice", Role: "user"})
	create := func(name string) codes.Code {
		_, err := containerClient.CreateContainer(owner, &coreapi.CreateContainerRequest{Name: name, ImageTag: "nginx:latest"})
		return status.Code(err)
	}

	set, err := quotaClient.SetUserQuota(admin, &coreapi.SetUserQuotaRequest{UserId: ownerID.String(), Cpu: 0.001, RamMb: 1, DiskMb: 1})
	require.NoError(t, err)
	require.Equal(t, &coreapi.UserQuotaData{UserId: ownerID.String(), Cpu: 0.001, RamMb: 1, DiskMb: 1, Custom: true}, stripped(set))

	got, err := quotaClient.GetUserQuotas(admin, &coreapi.GetUserQuotasRequest{UserIds: []string{ownerID.String(), otherID.String()}})
	require.NoError(t, err)
	require.Len(t, got.GetQuotas(), 2)
	require.Equal(t, &coreapi.UserQuotaData{UserId: ownerID.String(), Cpu: 0.001, RamMb: 1, DiskMb: 1, Custom: true}, stripped(got.GetQuotas()[0]))
	require.Equal(t, &coreapi.UserQuotaData{UserId: otherID.String(), Cpu: defaults.DefaultUserQuotaCPU, RamMb: defaults.DefaultUserQuotaRAMMB, DiskMb: defaults.DefaultUserQuotaDiskMB}, stripped(got.GetQuotas()[1]))

	require.Equal(t, codes.ResourceExhausted, create("first"), "the custom quota admits nothing")

	reset, err := quotaClient.ResetUserQuota(admin, &coreapi.UserQuotaActionRequest{UserId: ownerID.String()})
	require.NoError(t, err)
	require.Equal(t, &coreapi.UserQuotaData{UserId: ownerID.String(), Cpu: defaults.DefaultUserQuotaCPU, RamMb: defaults.DefaultUserQuotaRAMMB, DiskMb: defaults.DefaultUserQuotaDiskMB}, stripped(reset))

	require.Equal(t, codes.OK, create("second"), "the default quota admits the container")
}

func TestQuotaRPCsRefuseEveryScopeButAdmin(t *testing.T) {
	quotaClient := coreapi.NewQuotaAPIClient(quotaAdminServer(t))
	ownerID := uuid.New()
	for name, scope := range map[string]accessscope.Scope{
		"user":   {Kind: accessscope.KindUser, UserID: ownerID, Username: "alice", Role: "user"},
		"system": {Kind: accessscope.KindSystem},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := coretest.InternalGRPCContext(context.Background(), coretest.GatewayCredential, scope)

			_, err := quotaClient.GetUserQuotas(ctx, &coreapi.GetUserQuotasRequest{UserIds: []string{ownerID.String()}})
			require.Equal(t, codes.PermissionDenied, status.Code(err), "get: %v", err)
			_, err = quotaClient.SetUserQuota(ctx, &coreapi.SetUserQuotaRequest{UserId: ownerID.String(), Cpu: 4, RamMb: 4096, DiskMb: 4096})
			require.Equal(t, codes.PermissionDenied, status.Code(err), "set: %v", err)
			_, err = quotaClient.ResetUserQuota(ctx, &coreapi.UserQuotaActionRequest{UserId: ownerID.String()})
			require.Equal(t, codes.PermissionDenied, status.Code(err), "reset: %v", err)
		})
	}
	got, err := quotaClient.GetUserQuotas(adminGRPCContext(), &coreapi.GetUserQuotasRequest{UserIds: []string{ownerID.String()}})
	require.NoError(t, err)
	require.False(t, got.GetQuotas()[0].GetCustom(), "a refused set must store nothing")
}

func TestSetUserQuotaRejectsANonPositiveValueWithoutEchoingIt(t *testing.T) {
	quotaClient := coreapi.NewQuotaAPIClient(quotaAdminServer(t))
	ownerID := uuid.New().String()
	for name, req := range map[string]*coreapi.SetUserQuotaRequest{
		"cpu":    {UserId: ownerID, Cpu: -3.75, RamMb: 1024, DiskMb: 1024},
		"memory": {UserId: ownerID, Cpu: 1, RamMb: -3751, DiskMb: 1024},
		"disk":   {UserId: ownerID, Cpu: 1, RamMb: 1024, DiskMb: -3752},
		"zero":   {UserId: ownerID, Cpu: 1, RamMb: 0, DiskMb: 1024},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := quotaClient.SetUserQuota(adminGRPCContext(), req)

			require.Equal(t, codes.InvalidArgument, status.Code(err), "%v", err)
			require.NotContains(t, status.Convert(err).Message(), "375")
		})
	}
	_, err := quotaClient.SetUserQuota(adminGRPCContext(), &coreapi.SetUserQuotaRequest{UserId: "not-a-uuid", Cpu: 1, RamMb: 1, DiskMb: 1})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// stripped drops protobuf's internal state so messages compare by value.
func stripped(data *coreapi.UserQuotaData) *coreapi.UserQuotaData {
	return &coreapi.UserQuotaData{UserId: data.GetUserId(), Cpu: data.GetCpu(), RamMb: data.GetRamMb(), DiskMb: data.GetDiskMb(), Custom: data.GetCustom()}
}
