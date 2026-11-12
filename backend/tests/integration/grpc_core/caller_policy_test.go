package grpc_core_test

import (
	"context"
	"testing"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	builderclient "github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/grpc/client"
	buildermodel "github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coregrpcmocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/grpc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestBuilderCredentialMakesOnlyItsFourCalls serves every Core API behind
// Core's production caller policies over strict mocks: a refused call that
// reached a handler would fail the test as an unexpected mock call.
func TestBuilderCredentialMakesOnlyItsFourCalls(t *testing.T) {
	buildID := uuid.New()
	imageID := uuid.New()
	builds := coregrpcmocks.NewMockBuildLogic(t)
	system := coregrpcmocks.NewMockSystemLogic(t)
	reg := coretest.CoreGRPCRegistration{
		Containers: coregrpcmocks.NewMockContainerLogic(t),
		Volumes:    coregrpcmocks.NewMockVolumeLogic(t),
		Images:     coregrpcmocks.NewMockImageLogic(t),
		Builds:     builds,
		Projects:   coregrpcmocks.NewMockProjectLogic(t),
		System:     system,
		Stats:      coregrpcmocks.NewMockStatsLogic(t),
		Reports:    coregrpcmocks.NewMockReportLogic(t),
		Users:      coregrpcmocks.NewMockUserDirectory(t),
	}
	conn := coretest.NewCoreGRPCConn(t, reg)
	builderConn := coretest.NewCoreGRPCConn(t, reg, grpc.WithUnaryInterceptor(internalauth.SystemScopeUnaryClientInterceptor(coretest.BuilderCredential)))

	system.EXPECT().GetConfig(mock.Anything).Return(coretest.IntegrationConfig()).Once()
	builds.EXPECT().StartBuildRecord(mock.Anything, buildID).
		Return(model.Build{ID: buildID, ImageID: imageID, Status: model.BuildStatusRunning}, true, nil).Once()
	builds.EXPECT().GetBuild(mock.Anything, buildID).
		Return(model.Build{ID: buildID, ImageID: imageID, Status: model.BuildStatusRunning}, nil).Once()
	builds.EXPECT().CompleteBuildRecord(mock.Anything, buildID, imageID, "success", 3).Return(nil).Once()

	t.Run("its four calls with system scope", func(t *testing.T) {
		// Builder's production client, which states system scope itself.
		core := builderclient.NewCoreClient(builderConn)
		ctx := context.Background()
		_, err := core.GetBuilderConfig(ctx)
		require.NoError(t, err)
		_, started, _, err := core.StartBuildRecord(ctx, buildID.String())
		require.NoError(t, err)
		require.True(t, started)
		got, err := core.GetBuildStatus(ctx, buildID.String())
		require.NoError(t, err)
		require.Equal(t, buildermodel.BuildStatus("running"), got)
		require.NoError(t, core.CompleteBuildRecord(ctx, buildID.String(), imageID.String(), "success", 3))
	})

	builderSystem := coretest.InternalGRPCContext(context.Background(), coretest.BuilderCredential, accessscope.Scope{Kind: accessscope.KindSystem})
	t.Run("refused every other call", func(t *testing.T) {
		for name, call := range map[string]func(context.Context) error{
			"list containers": func(ctx context.Context) error {
				_, err := coreapi.NewContainerAPIClient(conn).ListContainers(ctx, &coreapi.PaginationRequest{})
				return err
			},
			"list volumes": func(ctx context.Context) error {
				_, err := coreapi.NewVolumeAPIClient(conn).ListVolumes(ctx, &coreapi.PaginationRequest{})
				return err
			},
			"list projects": func(ctx context.Context) error {
				_, err := coreapi.NewProjectAPIClient(conn).ListProjects(ctx, &coreapi.PaginationRequest{})
				return err
			},
			"reports overview": func(ctx context.Context) error {
				_, err := coreapi.NewReportAPIClient(conn).GetReportsOverview(ctx, &coreapi.ReportRangeRequest{})
				return err
			},
			"read config": func(ctx context.Context) error {
				_, err := coreapi.NewSystemAPIClient(conn).GetConfig(ctx, &coreapi.Empty{})
				return err
			},
			"cancel a build": func(ctx context.Context) error {
				_, err := coreapi.NewImageAPIClient(conn).CancelBuildRecord(ctx, &coreapi.BuildActionRequest{BuildId: buildID.String()})
				return err
			},
			"list builds": func(ctx context.Context) error {
				_, err := coreapi.NewImageAPIClient(conn).ListBuilds(ctx, &coreapi.PaginationRequest{})
				return err
			},
		} {
			require.Equal(t, codes.PermissionDenied, status.Code(call(builderSystem)), name)
		}
	})

	t.Run("refused a user or admin scope", func(t *testing.T) {
		for _, scope := range []accessscope.Scope{
			{Kind: accessscope.KindUser, UserID: uuid.New(), Username: "alice", Role: "user"},
			{Kind: accessscope.KindAdmin, UserID: uuid.New(), Username: "root", Role: "admin"},
		} {
			ctx := coretest.InternalGRPCContext(context.Background(), coretest.BuilderCredential, scope)
			_, err := coreapi.NewImageAPIClient(conn).StartBuildRecord(ctx, &coreapi.BuildActionRequest{BuildId: buildID.String()})
			require.Equal(t, codes.PermissionDenied, status.Code(err), string(scope.Kind))
			_, err = coreapi.NewSystemAPIClient(conn).GetBuilderRuntimeConfig(ctx, &coreapi.Empty{})
			require.Equal(t, codes.PermissionDenied, status.Code(err), string(scope.Kind))
		}
	})

	t.Run("Gateway refused Builder's calls", func(t *testing.T) {
		gatewaySystem := coretest.InternalGRPCContext(context.Background(), coretest.GatewayCredential, accessscope.Scope{Kind: accessscope.KindSystem})
		_, err := coreapi.NewImageAPIClient(conn).StartBuildRecord(gatewaySystem, &coreapi.BuildActionRequest{BuildId: buildID.String()})
		require.Equal(t, codes.PermissionDenied, status.Code(err))
		_, err = coreapi.NewSystemAPIClient(conn).GetBuilderRuntimeConfig(gatewaySystem, &coreapi.Empty{})
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	})
}
