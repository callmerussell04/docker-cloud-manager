package grpc_core_test

import (
	"context"
	"testing"

	coreapi "github.com/callmerussell04/docker-cloud-manager/backend/api/core"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/tests/mocks/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/tests/testutil/coretest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReportGRPCRequiresAdminScope(t *testing.T) {
	operations := []struct {
		name string
		call func(context.Context, coreapi.ReportAPIClient) error
	}{
		{"overview", func(ctx context.Context, c coreapi.ReportAPIClient) error {
			_, err := c.GetReportsOverview(ctx, &coreapi.ReportRangeRequest{})
			return err
		}},
		{"usage_list", func(ctx context.Context, c coreapi.ReportAPIClient) error {
			_, err := c.ListUserUsageReport(ctx, &coreapi.ListUserUsageReportRequest{})
			return err
		}},
		{"timeline", func(ctx context.Context, c coreapi.ReportAPIClient) error {
			_, err := c.GetUserUsageTimeline(ctx, &coreapi.GetUserUsageTimelineRequest{OwnerId: uuid.NewString()})
			return err
		}},
		{"audit_reads", func(ctx context.Context, c coreapi.ReportAPIClient) error {
			_, err := c.ListAuditEvents(ctx, &coreapi.ListAuditEventsRequest{})
			return err
		}},
		{"refresh", func(ctx context.Context, c coreapi.ReportAPIClient) error {
			_, err := c.RefreshUsageSnapshots(ctx, &coreapi.Empty{})
			return err
		}},
	}
	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			for _, kind := range []accessscope.Kind{"", accessscope.KindUser, accessscope.KindSystem, accessscope.KindAdmin} {
				t.Run("scope_"+string(kind), func(t *testing.T) {
					reports := coremocks.NewReportsRepository(t)
					reports.EXPECT().GetReportsOverview(mock.Anything, mock.Anything, mock.Anything).Return(model.ReportsOverview{}, nil).Maybe()
					reports.EXPECT().GetLatestUsageSnapshotCollectedAt(mock.Anything).Return(nil, nil).Maybe()
					reports.EXPECT().ListUserUsageReport(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, 0, nil).Maybe()
					reports.EXPECT().GetUserUsageTimeline(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil).Maybe()
					reports.EXPECT().ListAuditEvents(mock.Anything, mock.Anything).Return(nil, 0, nil).Maybe()
					svc := service.NewReportService(reports, nil, nil, nil, coretest.DiscardLogger())
					conn := coretest.NewCoreGRPCConn(t, "reports-token", coretest.CoreGRPCRegistration{Reports: svc})
					ctx := coretest.InternalGRPCContext(context.Background(), "reports-token", accessscope.Scope{Kind: kind, UserID: uuid.New(), Username: "alice"})
					err := op.call(ctx, coreapi.NewReportAPIClient(conn))
					want := codes.PermissionDenied
					if kind == accessscope.KindAdmin {
						want = codes.OK
					}
					require.Equal(t, want, status.Code(err))
					if kind != accessscope.KindAdmin {
						require.Empty(t, reports.Calls, "denied requests must not read storage")
					}
				})
			}
		})
	}
}

func TestAuditGRPCBindsActorToTrustedScope(t *testing.T) {
	for _, kind := range []accessscope.Kind{"", accessscope.KindUser, accessscope.KindAdmin, accessscope.KindSystem} {
		t.Run("scope_"+string(kind), func(t *testing.T) {
			reports := coremocks.NewReportsRepository(t)
			actorID := uuid.New()
			var recorded model.AuditEvent
			reports.EXPECT().RecordAuditEvent(mock.Anything, mock.Anything).Run(func(ctx context.Context, event model.AuditEvent) { recorded = event }).Return(nil).Once()
			svc := service.NewReportService(reports, nil, nil, nil, coretest.DiscardLogger())
			conn := coretest.NewCoreGRPCConn(t, "reports-token", coretest.CoreGRPCRegistration{Reports: svc})
			ctx := coretest.InternalGRPCContext(context.Background(), "reports-token", accessscope.Scope{Kind: kind, UserID: actorID, Username: "alice"})
			_, err := coreapi.NewReportAPIClient(conn).RecordAuditEvent(ctx, &coreapi.AuditEventData{
				Action: "containers.create", ActorScope: "admin", ActorUserId: uuid.NewString(), ActorUsername: "forged-admin",
			})
			require.NoError(t, err)
			if kind == "" || kind == accessscope.KindSystem {
				require.Equal(t, "system", recorded.ActorScope)
				require.Nil(t, recorded.ActorUserID)
				require.Empty(t, recorded.ActorUsername)
			} else {
				require.Equal(t, string(kind), recorded.ActorScope)
				require.Equal(t, &actorID, recorded.ActorUserID)
				require.Equal(t, "alice", recorded.ActorUsername)
			}
		})
	}
}

func TestReportGRPCRejectsUntrustedTransport(t *testing.T) {
	reports := coremocks.NewReportsRepository(t)
	svc := service.NewReportService(reports, nil, nil, nil, coretest.DiscardLogger())
	conn := coretest.NewCoreGRPCConn(t, "reports-token", coretest.CoreGRPCRegistration{Reports: svc})
	client := coreapi.NewReportAPIClient(conn)
	for _, token := range []string{"", "wrong-token"} {
		ctx := coretest.InternalGRPCContext(context.Background(), token, accessscope.Scope{Kind: accessscope.KindAdmin, UserID: uuid.New()})
		_, err := client.RecordAuditEvent(ctx, &coreapi.AuditEventData{Action: "containers.create"})
		require.Equal(t, codes.Unauthenticated, status.Code(err))
		_, err = client.GetReportsOverview(ctx, &coreapi.ReportRangeRequest{})
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	}
	require.Empty(t, reports.Calls)
}
