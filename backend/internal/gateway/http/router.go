package http

import (
	"errors"
	"log/slog"
	"net/netip"

	"github.com/gin-gonic/gin"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/middleware"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/permissions"
)

// Config is the router configuration owned by the config package.
type Config = config.Router

type RouterDeps struct {
	AuthHandler                 *handler.AuthHandler
	CoreHandler                 *handler.CoreHandler
	UserHandler                 *handler.UserManagementHandler
	HealthHandler               *handler.HealthHandler
	TelemetryTicketHandler      *handler.TelemetryTicketHandler
	CoreHTTPProxy               gin.HandlerFunc
	TelemetryLogsProxy          gin.HandlerFunc
	TelemetryTerminalProxy      gin.HandlerFunc
	AdminTelemetryLogsProxy     gin.HandlerFunc
	AdminTelemetryTerminalProxy gin.HandlerFunc
	TokenVerifier               middleware.TokenVerifier
	Logger                      *slog.Logger
}

func NewRouter(cfg Config, deps RouterDeps) (*gin.Engine, error) {
	if err := validateTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}
	router := gin.New()
	// Traefik supplies X-Forwarded-For; do not fall back to other identity headers.
	router.RemoteIPHeaders = []string{"X-Forwarded-For"}
	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, errors.New("invalid GATEWAY_TRUSTED_PROXIES: expected proxy IP addresses or CIDRs")
	}
	limiter := middleware.NewRateLimiter()

	router.Use(logging.RequestIDMiddleware())
	router.Use(logging.AccessLogMiddleware(deps.Logger))
	router.Use(logging.RecoveryMiddleware(deps.Logger))
	router.Use(middleware.CORSMiddleware(cfg.CORSAllowedOrigins))
	router.Use(middleware.BodySizeLimit(middleware.BodyLimitConfig{MaxJSONBodyBytes: cfg.MaxJSONBodyBytes}))

	router.GET("/health/live", deps.HealthHandler.Live)
	router.GET("/health/ready", deps.HealthHandler.Ready)

	v1 := router.Group("/api/v1")
	registerAuthRoutes(v1, cfg, limiter, deps)

	protected := v1.Group("/")
	protected.Use(middleware.Auth(deps.TokenVerifier))
	registerUserRoutes(protected, cfg, limiter, deps)
	registerAdminRoutes(v1, deps)
	registerTelemetryRoutes(v1, deps)

	return router, nil
}

// registerAuthRoutes gives each purpose its own bucket under the auth limit,
// so sign-in navigation, password attempts, session calls and the provider's
// back-channel logouts never exhaust one another. Navigation routes answer a rejection with a redirect.
func registerAuthRoutes(v1 *gin.RouterGroup, cfg Config, limiter *middleware.RateLimiter, deps RouterDeps) {
	auth := v1.Group("/auth")
	credentials := middleware.RateLimit(limiter, cfg.AuthRateLimit, "auth-credentials")
	session := middleware.RateLimit(limiter, cfg.AuthRateLimit, "auth-session")
	navigation := middleware.RateLimitWith(limiter, cfg.AuthRateLimit, "auth-oidc-navigation", deps.AuthHandler.RejectRateLimitedNavigation)
	backchannel := middleware.RateLimit(limiter, cfg.AuthRateLimit, "auth-oidc-backchannel")
	auth.GET("/providers", session, deps.AuthHandler.Providers)
	auth.GET("/oidc/keycloak/start", navigation, deps.AuthHandler.StartKeycloakLogin)
	auth.GET("/oidc/keycloak/callback", navigation, deps.AuthHandler.CompleteKeycloakCallback)
	// The provider posts here from its server: no cookie and no browser origin.
	auth.POST("/oidc/keycloak/backchannel-logout", backchannel, deps.AuthHandler.KeycloakBackchannelLogout)
	auth.POST("/register", credentials, deps.AuthHandler.Register)
	auth.POST("/login", credentials, deps.AuthHandler.Login)
	auth.POST("/refresh", session, deps.AuthHandler.Refresh)
	auth.POST("/logout", session, deps.AuthHandler.Logout)
}

func registerUserRoutes(protected *gin.RouterGroup, cfg Config, limiter *middleware.RateLimiter, deps RouterDeps) {
	containers := protected.Group("/containers")
	containers.POST("", deps.CoreHandler.CreateContainer)
	containers.GET("", deps.CoreHandler.GetContainers)
	containers.GET("/:id", deps.CoreHandler.GetContainer)
	containers.POST("/:id/action/:action", deps.CoreHandler.ActionContainer)
	containers.POST("/:id/expose", deps.CoreHandler.ExposeContainer)
	containers.GET("/:id/stats", deps.CoreHandler.GetContainerStats)
	containers.POST("/:id/logs/stream/ticket", deps.TelemetryTicketHandler.IssueLogsTicket(false))
	containers.POST("/:id/terminal/ticket", deps.TelemetryTicketHandler.IssueTerminalTicket(false))

	volumes := protected.Group("/volumes")
	volumes.POST("", deps.CoreHandler.CreateVolume)
	volumes.GET("", deps.CoreHandler.GetVolumes)
	volumes.DELETE("/:id", deps.CoreHandler.DeleteVolume)

	images := protected.Group("/images")
	images.GET("", deps.CoreHandler.GetImages)
	images.DELETE("/:id", deps.CoreHandler.DeleteImage)
	images.GET("/build/availability", deps.CoreHTTPProxy)
	images.POST("/build", middleware.RateLimit(limiter, cfg.UploadRateLimit, "image-build"), handler.AuditedProxy(deps.CoreHandler, deps.CoreHTTPProxy, auditlog.ActionBuildCreateArchive, auditlog.ResourceBuild, "upload"))
	images.POST("/build/git", middleware.RateLimit(limiter, cfg.UploadRateLimit, "image-build-git"), handler.AuditedProxy(deps.CoreHandler, deps.CoreHTTPProxy, auditlog.ActionBuildCreateGit, auditlog.ResourceBuild, "git"))

	builds := protected.Group("/builds")
	builds.GET("", deps.CoreHandler.GetBuilds)
	builds.GET("/:id", deps.CoreHandler.GetBuild)
	builds.DELETE("/:id", deps.CoreHandler.DeleteBuild)
	builds.GET("/:id/logs", deps.CoreHTTPProxy)
	builds.POST("/:id/cancel", deps.CoreHandler.CancelBuild)

	projects := protected.Group("/projects")
	projects.GET("", deps.CoreHandler.GetProjects)
	projects.GET("/:id", deps.CoreHandler.GetProject)
	projects.DELETE("/:id", deps.CoreHandler.DeleteProject)
	projects.POST("/:id/start", deps.CoreHandler.StartProject)
	projects.POST("/:id/stop", deps.CoreHandler.StopProject)
	projects.POST("/:id/cancel", deps.CoreHandler.CancelProject)
	registerComposeProxyRoutes(projects, cfg, limiter, deps)

	stats := protected.Group("/stats")
	stats.GET("", deps.CoreHandler.GetUserStats)
}

func registerComposeProxyRoutes(projects *gin.RouterGroup, cfg Config, limiter *middleware.RateLimiter, deps RouterDeps) {
	projects.POST("/compose", middleware.RateLimit(limiter, cfg.UploadRateLimit, "compose"), handler.AuditedProxy(deps.CoreHandler, deps.CoreHTTPProxy, auditlog.ActionComposeUploadDeploy, auditlog.ResourceProject, "upload"))
	projects.POST("/compose/git", middleware.RateLimit(limiter, cfg.UploadRateLimit, "compose-git"), handler.AuditedProxy(deps.CoreHandler, deps.CoreHTTPProxy, auditlog.ActionComposeGitDeploy, auditlog.ResourceProject, "git"))
}

func registerAdminRoutes(v1 *gin.RouterGroup, deps RouterDeps) {
	admin := v1.Group("/admin")
	admin.GET("/config", adminPermission(deps, permissions.SystemConfigRead), deps.CoreHandler.GetSystemConfig)
	admin.PUT("/config", adminPermission(deps, permissions.SystemConfigUpdate), deps.CoreHandler.UpdateSystemConfig)
	admin.GET("/monitoring", adminPermission(deps, permissions.SystemMonitoringRead), deps.CoreHandler.GetSystemMonitoring)
	admin.GET("/reports/overview", adminPermission(deps, permissions.ReportsAdminRead), deps.CoreHandler.GetReportsOverview)
	admin.GET("/reports/users", adminPermission(deps, permissions.ReportsAdminRead), deps.CoreHandler.ListUserUsageReport)
	admin.GET("/reports/users/:id/usage", adminPermission(deps, permissions.ReportsAdminRead), deps.CoreHandler.GetUserUsageTimeline)
	admin.GET("/reports/audit-events", adminPermission(deps, permissions.ReportsAdminRead), deps.CoreHandler.ListAuditEvents)
	admin.POST("/reports/usage-snapshots/refresh", adminPermission(deps, permissions.ReportsAdminRead), deps.CoreHandler.RefreshUsageSnapshots)

	admin.GET("/users", adminPermission(deps, permissions.UsersAdminList), deps.UserHandler.ListUsers)
	admin.GET("/users/:id", adminPermission(deps, permissions.UsersAdminRead), deps.UserHandler.GetUser)
	admin.POST("/users", adminPermission(deps, permissions.UsersAdminCreate), deps.UserHandler.CreateUser)
	admin.PUT("/users/:id", adminPermission(deps, permissions.UsersAdminUpdate), deps.UserHandler.UpdateUser)
	admin.DELETE("/users/:id", adminPermission(deps, permissions.UsersAdminDelete), deps.UserHandler.DeactivateUser)
	admin.POST("/users/:id/activate", adminPermission(deps, permissions.UsersAdminUpdate), deps.UserHandler.ReactivateUser)
	admin.PUT("/users/:id/quota", adminPermission(deps, permissions.UsersAdminUpdate), deps.UserHandler.SetUserQuota)
	admin.DELETE("/users/:id/quota", adminPermission(deps, permissions.UsersAdminUpdate), deps.UserHandler.ResetUserQuota)

	admin.GET("/containers", adminPermission(deps, permissions.ContainersAdminList), deps.CoreHandler.ListAdminContainers)
	admin.GET("/containers/:id", adminPermission(deps, permissions.ContainersAdminList), deps.CoreHandler.AdminGetContainer)
	admin.POST("/containers/:id/action/:action", adminPermission(deps, permissions.ContainersAdminAction), deps.CoreHandler.AdminActionContainer)
	admin.GET("/containers/:id/stats", adminPermission(deps, permissions.ContainersAdminStats), deps.CoreHandler.AdminGetContainerStats)
	admin.POST("/containers/:id/logs/stream/ticket", adminPermission(deps, permissions.ContainersAdminLogs), deps.TelemetryTicketHandler.IssueLogsTicket(true))
	admin.POST("/containers/:id/terminal/ticket", adminPermission(deps, permissions.ContainersAdminTerminal), deps.TelemetryTicketHandler.IssueTerminalTicket(true))

	admin.GET("/volumes", adminPermission(deps, permissions.VolumesAdminList), deps.CoreHandler.ListAdminVolumes)
	admin.DELETE("/volumes/:id", adminPermission(deps, permissions.VolumesAdminDelete), deps.CoreHandler.AdminDeleteVolume)

	admin.GET("/images", adminPermission(deps, permissions.ImagesAdminList), deps.CoreHandler.ListAdminImages)
	admin.DELETE("/images/:id", adminPermission(deps, permissions.ImagesAdminDelete), deps.CoreHandler.AdminDeleteImage)

	admin.GET("/builds", adminPermission(deps, permissions.BuildsAdminList), deps.CoreHandler.ListAdminBuilds)
	admin.GET("/builds/:id", adminPermission(deps, permissions.BuildsAdminList), deps.CoreHandler.AdminGetBuild)
	admin.GET("/builds/:id/logs", adminPermission(deps, permissions.BuildsAdminList), deps.CoreHTTPProxy)
	admin.POST("/builds/:id/cancel", adminPermission(deps, permissions.BuildsAdminDelete), deps.CoreHandler.AdminCancelBuild)
	admin.DELETE("/builds/:id", adminPermission(deps, permissions.BuildsAdminDelete), deps.CoreHandler.AdminDeleteBuild)

	admin.GET("/projects", adminPermission(deps, permissions.ProjectsAdminList), deps.CoreHandler.ListAdminProjects)
	admin.GET("/projects/:id", adminPermission(deps, permissions.ProjectsAdminList), deps.CoreHandler.AdminGetProject)
	admin.DELETE("/projects/:id", adminPermission(deps, permissions.ProjectsAdminDelete), deps.CoreHandler.AdminDeleteProject)
	admin.POST("/projects/:id/start", adminPermission(deps, permissions.ProjectsAdminStart), deps.CoreHandler.AdminStartProject)
	admin.POST("/projects/:id/stop", adminPermission(deps, permissions.ProjectsAdminStop), deps.CoreHandler.AdminStopProject)
	admin.POST("/projects/:id/cancel", adminPermission(deps, permissions.ProjectsAdminStop), deps.CoreHandler.AdminCancelProject)
}

func registerTelemetryRoutes(v1 *gin.RouterGroup, deps RouterDeps) {
	v1.GET("/containers/:id/logs/stream", deps.TelemetryLogsProxy)
	v1.GET("/containers/:id/terminal", deps.TelemetryTerminalProxy)
	v1.GET("/admin/containers/:id/logs/stream", deps.AdminTelemetryLogsProxy)
	v1.GET("/admin/containers/:id/terminal", deps.AdminTelemetryTerminalProxy)
}

func adminPermission(deps RouterDeps, permission string) gin.HandlerFunc {
	return middleware.RequirePermission(deps.TokenVerifier, permission)
}

func DefaultConfig() Config {
	return config.DefaultRouter()
}

func validateTrustedProxies(proxies []string) error {
	for _, proxy := range proxies {
		prefix, err := netip.ParsePrefix(proxy)
		if err != nil {
			addr, addrErr := netip.ParseAddr(proxy)
			if addrErr != nil {
				return errors.New("invalid GATEWAY_TRUSTED_PROXIES: expected proxy IP addresses or CIDRs")
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		addr := prefix.Addr()
		if prefix.Masked().Addr().IsUnspecified() || addr.IsMulticast() || addr.Is4In6() || addr.Zone() != "" {
			return errors.New("unsafe GATEWAY_TRUSTED_PROXIES: use explicit unicast proxy addresses or restricted CIDRs")
		}
	}
	return nil
}
