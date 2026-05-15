package http_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gatewayhttp "github.com/callmerussell04/docker-cloud-manager/internal/gateway/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/middleware"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/permissions"
	gatewayhandlermocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/gateway/http/handler"
	gatewaymiddlewaremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/gateway/http/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestRouterRegistersGatewayRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gatewayhttp.NewRouter(gatewayhttp.DefaultConfig(), gatewayhttp.RouterDeps{
		AuthHandler:                 (*handler.AuthHandler)(nil),
		CoreHandler:                 (*handler.CoreHandler)(nil),
		UserHandler:                 (*handler.UserManagementHandler)(nil),
		HealthHandler:               (*handler.HealthHandler)(nil),
		TelemetryTicketHandler:      (*handler.TelemetryTicketHandler)(nil),
		CoreHTTPProxy:               noopHandler,
		TelemetryLogsProxy:          noopHandler,
		TelemetryTerminalProxy:      noopHandler,
		AdminTelemetryLogsProxy:     noopHandler,
		AdminTelemetryTerminalProxy: noopHandler,
		Logger:                      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	routes := map[string]struct{}{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	expected := []string{
		"GET /health/live",
		"GET /health/ready",
		"GET /api/v1/auth/providers",
		"GET /api/v1/auth/oidc/keycloak/start",
		"GET /api/v1/auth/oidc/keycloak/callback",
		"POST /api/v1/auth/register",
		"POST /api/v1/auth/login",
		"POST /api/v1/auth/refresh",
		"POST /api/v1/auth/logout",
		"POST /api/v1/containers",
		"GET /api/v1/containers",
		"POST /api/v1/containers/:id/action/:action",
		"POST /api/v1/containers/:id/expose",
		"GET /api/v1/containers/:id/stats",
		"POST /api/v1/containers/:id/logs/stream/ticket",
		"POST /api/v1/containers/:id/terminal/ticket",
		"POST /api/v1/volumes",
		"GET /api/v1/volumes",
		"DELETE /api/v1/volumes/:id",
		"GET /api/v1/images",
		"DELETE /api/v1/images/:id",
		"GET /api/v1/images/build/availability",
		"POST /api/v1/images/build",
		"POST /api/v1/images/build/git",
		"GET /api/v1/builds",
		"DELETE /api/v1/builds/:id",
		"GET /api/v1/builds/:id/logs",
		"POST /api/v1/builds/:id/cancel",
		"GET /api/v1/projects",
		"DELETE /api/v1/projects/:id",
		"POST /api/v1/projects/:id/start",
		"POST /api/v1/projects/:id/stop",
		"POST /api/v1/projects/:id/cancel",
		"POST /api/v1/projects/compose",
		"POST /api/v1/projects/compose/git",
		"GET /api/v1/stats",
		"GET /api/v1/admin/config",
		"PUT /api/v1/admin/config",
		"GET /api/v1/admin/monitoring",
		"GET /api/v1/admin/reports/overview",
		"GET /api/v1/admin/reports/users",
		"GET /api/v1/admin/reports/users/:id/usage",
		"GET /api/v1/admin/reports/audit-events",
		"POST /api/v1/admin/reports/usage-snapshots/refresh",
		"GET /api/v1/admin/users",
		"GET /api/v1/admin/users/:id",
		"POST /api/v1/admin/users",
		"PUT /api/v1/admin/users/:id",
		"DELETE /api/v1/admin/users/:id",
		"POST /api/v1/admin/users/:id/activate",
		"GET /api/v1/admin/containers",
		"POST /api/v1/admin/containers/:id/action/:action",
		"GET /api/v1/admin/containers/:id/stats",
		"POST /api/v1/admin/containers/:id/logs/stream/ticket",
		"POST /api/v1/admin/containers/:id/terminal/ticket",
		"GET /api/v1/admin/volumes",
		"DELETE /api/v1/admin/volumes/:id",
		"GET /api/v1/admin/images",
		"DELETE /api/v1/admin/images/:id",
		"GET /api/v1/admin/builds",
		"GET /api/v1/admin/builds/:id/logs",
		"POST /api/v1/admin/builds/:id/cancel",
		"DELETE /api/v1/admin/builds/:id",
		"GET /api/v1/admin/projects",
		"DELETE /api/v1/admin/projects/:id",
		"POST /api/v1/admin/projects/:id/start",
		"POST /api/v1/admin/projects/:id/stop",
		"POST /api/v1/admin/projects/:id/cancel",
		"GET /api/v1/containers/:id/logs/stream",
		"GET /api/v1/containers/:id/terminal",
		"GET /api/v1/admin/containers/:id/logs/stream",
		"GET /api/v1/admin/containers/:id/terminal",
	}
	for _, route := range expected {
		require.Contains(t, routes, route)
	}
}

func TestRouterAuthBoundariesAndUploadProxyChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := newRouterDeps(t)
	cfg := gatewayhttp.DefaultConfig()
	cfg.AuthRateLimit = middleware.RateLimitConfig{}
	cfg.UploadRateLimit = middleware.RateLimitConfig{Requests: 1, Window: 0}
	router := gatewayhttp.NewRouter(cfg, deps.routerDeps())
	userID := uuid.NewString()

	deps.auth.EXPECT().GetAuthConfig(mock.Anything).Return(model.AuthConfig{LocalLoginEnabled: true}, nil)
	resp := routerRequest(router, http.MethodGet, "/api/v1/auth/providers", "", nil)
	require.Equal(t, http.StatusOK, resp.Code)

	deps.verifier.EXPECT().
		VerifyAccessToken(mock.Anything, "Bearer user").
		Return(model.AuthUser{UserID: userID, Username: "alice", Role: "user"}, nil)
	deps.core.EXPECT().GetUserStats(mock.Anything).Return(model.UserStats{ContainersTotal: 1}, nil)
	resp = routerRequest(router, http.MethodGet, "/api/v1/stats", "", map[string]string{"Authorization": "Bearer user"})
	require.Equal(t, http.StatusOK, resp.Code)

	deps.verifier.EXPECT().
		CheckPermission(mock.Anything, "Bearer admin", permissions.SystemConfigRead).
		Return(model.AuthUser{UserID: userID, Username: "admin", Role: "admin"}, nil)
	deps.core.EXPECT().GetSystemConfig(mock.Anything).Return(model.SystemConfig{BaseDomain: "example.test"}, nil)
	resp = routerRequest(router, http.MethodGet, "/api/v1/admin/config", "", map[string]string{"Authorization": "Bearer admin"})
	require.Equal(t, http.StatusOK, resp.Code)

	deps.verifier.EXPECT().
		VerifyAccessToken(mock.Anything, "Bearer upload").
		Return(model.AuthUser{UserID: userID, Username: "alice", Role: "user"}, nil)
	deps.core.EXPECT().RecordAuditEvent(mock.Anything, mock.MatchedBy(func(event model.AuditEvent) bool {
		return event.ActorUserID == userID && event.Action != "" && event.Outcome == "success"
	})).Return(nil)
	resp = routerRequest(router, http.MethodPost, "/api/v1/images/build", "archive", map[string]string{"Authorization": "Bearer upload"})
	require.Equal(t, http.StatusAccepted, resp.Code)
	require.Equal(t, 1, deps.proxyCalls)
}

func newRouterDeps(t *testing.T) *routerDeps {
	t.Helper()
	auth := gatewayhandlermocks.NewMockAuthService(t)
	core := gatewayhandlermocks.NewMockCoreService(t)
	users := gatewayhandlermocks.NewMockUserManagementService(t)
	tickets := gatewayhandlermocks.NewMockTelemetryTicketIssuer(t)
	readiness := gatewayhandlermocks.NewMockReadinessService(t)
	verifier := gatewaymiddlewaremocks.NewMockTokenVerifier(t)
	return &routerDeps{auth: auth, core: core, users: users, tickets: tickets, readiness: readiness, verifier: verifier}
}

type routerDeps struct {
	auth       *gatewayhandlermocks.MockAuthService
	core       *gatewayhandlermocks.MockCoreService
	users      *gatewayhandlermocks.MockUserManagementService
	tickets    *gatewayhandlermocks.MockTelemetryTicketIssuer
	readiness  *gatewayhandlermocks.MockReadinessService
	verifier   *gatewaymiddlewaremocks.MockTokenVerifier
	proxyCalls int
}

func (d *routerDeps) routerDeps() gatewayhttp.RouterDeps {
	coreHandler := handler.NewCoreHandler(d.core)
	return gatewayhttp.RouterDeps{
		AuthHandler:            handler.NewAuthHandler(d.auth, handler.CookieConfig{Name: "refresh_token"}, handler.AuthRedirectConfig{}),
		CoreHandler:            coreHandler,
		UserHandler:            handler.NewUserManagementHandler(d.users),
		HealthHandler:          handler.NewHealthHandler(d.readiness),
		TelemetryTicketHandler: handler.NewTelemetryTicketHandler(d.tickets),
		CoreHTTPProxy: func(c *gin.Context) {
			d.proxyCalls++
			c.Status(http.StatusAccepted)
		},
		TelemetryLogsProxy:          noopHandler,
		TelemetryTerminalProxy:      noopHandler,
		AdminTelemetryLogsProxy:     noopHandler,
		AdminTelemetryTerminalProxy: noopHandler,
		TokenVerifier:               d.verifier,
		Logger:                      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func routerRequest(router http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func noopHandler(c *gin.Context) {}
