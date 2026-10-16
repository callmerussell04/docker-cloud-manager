package grpc_core_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	grpcclient "github.com/callmerussell04/docker-cloud-manager/internal/gateway/grpc/client"
	gatewayhttp "github.com/callmerussell04/docker-cloud-manager/internal/gateway/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
	gatewaymodel "github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	gatewayservice "github.com/callmerussell04/docker-cloud-manager/internal/gateway/service"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/permissions"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	gatewaymocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/gateway/http/middleware"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/coretest"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestGatewayReportsAndAuditReachCoreWithAuthenticatedScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reports := coremocks.NewReportsRepository(t)
	svc := service.NewReportService(reports, nil, nil, nil, coretest.DiscardLogger())
	conn := coretest.NewCoreGRPCConn(t, "reports-token", coretest.CoreGRPCRegistration{Reports: svc}, grpc.WithUnaryInterceptor(internalauth.UnaryClientInterceptor("reports-token")))
	coreHandler := handler.NewCoreHandler(gatewayservice.NewCore(grpcclient.NewCoreClient(conn)))
	verifier := gatewaymocks.NewMockTokenVerifier(t)
	actorID := uuid.New()
	admin := gatewaymodel.AuthUser{UserID: actorID.String(), Username: "admin", Role: "admin"}
	verifier.EXPECT().CheckPermission(mock.Anything, "Bearer admin", permissions.ReportsAdminRead).Return(admin, nil).Times(5)
	verifier.EXPECT().CheckPermission(mock.Anything, "Bearer denied", permissions.ReportsAdminRead).Return(gatewaymodel.AuthUser{}, apperrors.ErrForbidden).Once()
	reports.EXPECT().GetReportsOverview(mock.Anything, mock.Anything, mock.Anything).Return(model.ReportsOverview{AuditEventsTotal: 3}, nil).Once()
	reports.EXPECT().GetLatestUsageSnapshotCollectedAt(mock.Anything).Return(nil, nil).Once()
	reports.EXPECT().ListUserUsageReport(mock.Anything, mock.Anything, mock.Anything, "disk", "", 20, 0).Return(nil, 0, nil).Once()
	reports.EXPECT().GetUserUsageTimeline(mock.Anything, actorID, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil).Once()
	reports.EXPECT().ListAuditEvents(mock.Anything, mock.Anything).Return(nil, 0, nil).Once()
	noop := func(c *gin.Context) {}
	router, err := gatewayhttp.NewRouter(gatewayhttp.DefaultConfig(), gatewayhttp.RouterDeps{
		CoreHandler: coreHandler, TokenVerifier: verifier, Logger: coretest.DiscardLogger(),
		CoreHTTPProxy:      func(c *gin.Context) { c.Status(http.StatusAccepted) },
		TelemetryLogsProxy: noop, TelemetryTerminalProxy: noop, AdminTelemetryLogsProxy: noop, AdminTelemetryTerminalProxy: noop,
	})
	require.NoError(t, err)
	for _, path := range []string{"overview", "users", "users/" + actorID.String() + "/usage", "audit-events", "usage-snapshots/refresh"} {
		method, want := http.MethodGet, http.StatusOK
		if path == "usage-snapshots/refresh" {
			method, want = http.MethodPost, http.StatusAccepted
		}
		req := httptest.NewRequest(method, "/api/v1/admin/reports/"+path, nil)
		req.Header.Set("Authorization", "Bearer admin")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		require.Equal(t, want, resp.Code, resp.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports/overview", nil)
	req.Header.Set("Authorization", "Bearer denied")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusForbidden, resp.Code)

	// Audit writes survive request cancellation and ClickHouse failure, preserving actor identity.
	verifier.EXPECT().VerifyAccessToken(mock.Anything, "Bearer user").Return(gatewaymodel.AuthUser{UserID: actorID.String(), Username: "alice", Role: "user"}, nil).Once()
	reports.EXPECT().RecordAuditEvent(mock.Anything, mock.MatchedBy(func(event model.AuditEvent) bool {
		return event.ActorUserID != nil && *event.ActorUserID == actorID && event.ActorUsername == "alice" && event.ActorScope == "user"
	})).Return(errors.New("private ClickHouse failure")).Once()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/images/build", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer user")
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusAccepted, resp.Code, resp.Body.String())

	verifier.EXPECT().CheckPermission(mock.Anything, "Bearer admin", permissions.ReportsAdminRead).Return(admin, nil).Once()
	reports.EXPECT().GetReportsOverview(mock.Anything, mock.Anything, mock.Anything).Return(model.ReportsOverview{}, errors.New("private ClickHouse failure")).Once()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports/overview", nil)
	req.Header.Set("Authorization", "Bearer admin")
	resp = httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusServiceUnavailable, resp.Code)
	require.NotContains(t, resp.Body.String(), "private ClickHouse failure")
}
