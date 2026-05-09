package http

import (
	"log/slog"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
)

func TestRouterRegistersGatewayRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(DefaultConfig(), RouterDeps{
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
		Logger:                      slog.Default(),
	})

	routes := map[string]struct{}{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	expected := []string{
		"GET /health/live",
		"GET /health/ready",
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
		if _, ok := routes[route]; !ok {
			t.Fatalf("route %q is not registered", route)
		}
	}
}

func noopHandler(c *gin.Context) {}
