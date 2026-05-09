package http

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/middleware"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/callmerussell04/docker-cloud-manager/pkg/permissions"
)

type Config struct {
	CORSAllowedOrigins []string
	TrustedProxies     []string
	MaxJSONBodyBytes   int64
	AuthRateLimit      middleware.RateLimitConfig
	UploadRateLimit    middleware.RateLimitConfig
}

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

func NewRouter(cfg Config, deps RouterDeps) *gin.Engine {
	router := gin.New()
	_ = router.SetTrustedProxies(cfg.TrustedProxies)
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

	return router
}

func registerAuthRoutes(v1 *gin.RouterGroup, cfg Config, limiter *middleware.RateLimiter, deps RouterDeps) {
	auth := v1.Group("/auth")
	auth.Use(middleware.RateLimit(limiter, cfg.AuthRateLimit, "auth"))
	auth.POST("/register", deps.AuthHandler.Register)
	auth.POST("/login", deps.AuthHandler.Login)
	auth.POST("/refresh", deps.AuthHandler.Refresh)
	auth.POST("/logout", deps.AuthHandler.Logout)
}

func registerUserRoutes(protected *gin.RouterGroup, cfg Config, limiter *middleware.RateLimiter, deps RouterDeps) {
	containers := protected.Group("/containers")
	containers.POST("", deps.CoreHandler.CreateContainer)
	containers.GET("", deps.CoreHandler.GetContainers)
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
	images.POST("/build", middleware.RateLimit(limiter, cfg.UploadRateLimit, "image-build"), deps.CoreHTTPProxy)
	images.POST("/build/git", middleware.RateLimit(limiter, cfg.UploadRateLimit, "image-build-git"), deps.CoreHTTPProxy)

	builds := protected.Group("/builds")
	builds.GET("", deps.CoreHandler.GetBuilds)
	builds.DELETE("/:id", deps.CoreHandler.DeleteBuild)
	builds.GET("/:id/logs", deps.CoreHTTPProxy)
	builds.POST("/:id/cancel", deps.CoreHandler.CancelBuild)

	projects := protected.Group("/projects")
	projects.GET("", deps.CoreHandler.GetProjects)
	projects.DELETE("/:id", deps.CoreHandler.DeleteProject)
	projects.POST("/:id/start", deps.CoreHandler.StartProject)
	projects.POST("/:id/stop", deps.CoreHandler.StopProject)
	projects.POST("/:id/cancel", deps.CoreHandler.CancelProject)
	registerComposeProxyRoutes(projects, cfg, limiter, deps)

	stats := protected.Group("/stats")
	stats.GET("", deps.CoreHandler.GetUserStats)
}

func registerComposeProxyRoutes(projects *gin.RouterGroup, cfg Config, limiter *middleware.RateLimiter, deps RouterDeps) {
	projects.POST("/compose", middleware.RateLimit(limiter, cfg.UploadRateLimit, "compose"), deps.CoreHTTPProxy)
	projects.POST("/compose/git", middleware.RateLimit(limiter, cfg.UploadRateLimit, "compose-git"), deps.CoreHTTPProxy)
}

func registerAdminRoutes(v1 *gin.RouterGroup, deps RouterDeps) {
	admin := v1.Group("/admin")
	admin.GET("/config", adminPermission(deps, permissions.SystemConfigRead), deps.CoreHandler.GetSystemConfig)
	admin.PUT("/config", adminPermission(deps, permissions.SystemConfigUpdate), deps.CoreHandler.UpdateSystemConfig)

	admin.GET("/users", adminPermission(deps, permissions.UsersAdminList), deps.UserHandler.ListUsers)
	admin.GET("/users/:id", adminPermission(deps, permissions.UsersAdminRead), deps.UserHandler.GetUser)
	admin.POST("/users", adminPermission(deps, permissions.UsersAdminCreate), deps.UserHandler.CreateUser)
	admin.PUT("/users/:id", adminPermission(deps, permissions.UsersAdminUpdate), deps.UserHandler.UpdateUser)
	admin.DELETE("/users/:id", adminPermission(deps, permissions.UsersAdminDelete), deps.UserHandler.DeactivateUser)
	admin.POST("/users/:id/activate", adminPermission(deps, permissions.UsersAdminUpdate), deps.UserHandler.ReactivateUser)

	admin.GET("/containers", adminPermission(deps, permissions.ContainersAdminList), deps.CoreHandler.ListAdminContainers)
	admin.POST("/containers/:id/action/:action", adminPermission(deps, permissions.ContainersAdminAction), deps.CoreHandler.AdminActionContainer)
	admin.GET("/containers/:id/stats", adminPermission(deps, permissions.ContainersAdminStats), deps.CoreHandler.AdminGetContainerStats)
	admin.POST("/containers/:id/logs/stream/ticket", adminPermission(deps, permissions.ContainersAdminLogs), deps.TelemetryTicketHandler.IssueLogsTicket(true))
	admin.POST("/containers/:id/terminal/ticket", adminPermission(deps, permissions.ContainersAdminTerminal), deps.TelemetryTicketHandler.IssueTerminalTicket(true))

	admin.GET("/volumes", adminPermission(deps, permissions.VolumesAdminList), deps.CoreHandler.ListAdminVolumes)
	admin.DELETE("/volumes/:id", adminPermission(deps, permissions.VolumesAdminDelete), deps.CoreHandler.AdminDeleteVolume)

	admin.GET("/images", adminPermission(deps, permissions.ImagesAdminList), deps.CoreHandler.ListAdminImages)
	admin.DELETE("/images/:id", adminPermission(deps, permissions.ImagesAdminDelete), deps.CoreHandler.AdminDeleteImage)

	admin.GET("/builds", adminPermission(deps, permissions.BuildsAdminList), deps.CoreHandler.ListAdminBuilds)
	admin.GET("/builds/:id/logs", adminPermission(deps, permissions.BuildsAdminList), deps.CoreHTTPProxy)
	admin.POST("/builds/:id/cancel", adminPermission(deps, permissions.BuildsAdminDelete), deps.CoreHandler.AdminCancelBuild)
	admin.DELETE("/builds/:id", adminPermission(deps, permissions.BuildsAdminDelete), deps.CoreHandler.AdminDeleteBuild)

	admin.GET("/projects", adminPermission(deps, permissions.ProjectsAdminList), deps.CoreHandler.ListAdminProjects)
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
	return Config{
		CORSAllowedOrigins: []string{"http://localhost", "http://localhost:3000", "http://localhost:5173"},
		MaxJSONBodyBytes:   1 << 20,
		AuthRateLimit: middleware.RateLimitConfig{
			Requests: 20,
			Window:   time.Minute,
		},
		UploadRateLimit: middleware.RateLimitConfig{
			Requests: 10,
			Window:   time.Minute,
		},
	}
}
