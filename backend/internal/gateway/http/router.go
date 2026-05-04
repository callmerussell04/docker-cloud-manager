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

func NewRouter(cfg Config, authHandler *handler.AuthHandler, coreHandler *handler.CoreHandler, userHandler *handler.UserManagementHandler, healthHandler *handler.HealthHandler, coreHttpProxy gin.HandlerFunc, telemetryLogsProxy gin.HandlerFunc, telemetryTerminalProxy gin.HandlerFunc, adminTelemetryLogsProxy gin.HandlerFunc, adminTelemetryTerminalProxy gin.HandlerFunc, telemetryTicketHandler *handler.TelemetryTicketHandler, tokenVerifier middleware.TokenVerifier, logger *slog.Logger) *gin.Engine {
	router := gin.New()
	_ = router.SetTrustedProxies(cfg.TrustedProxies)
	limiter := middleware.NewRateLimiter()

	router.Use(logging.RequestIDMiddleware())
	router.Use(logging.AccessLogMiddleware(logger))
	router.Use(logging.RecoveryMiddleware(logger))
	router.Use(middleware.CORSMiddleware(cfg.CORSAllowedOrigins))
	router.Use(middleware.BodySizeLimit(middleware.BodyLimitConfig{MaxJSONBodyBytes: cfg.MaxJSONBodyBytes}))

	router.GET("/health/live", healthHandler.Live)
	router.GET("/health/ready", healthHandler.Ready)

	v1 := router.Group("/api/v1")
	{
		auth := v1.Group("/auth")
		auth.Use(middleware.RateLimit(limiter, cfg.AuthRateLimit, "auth"))
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			auth.POST("/refresh", authHandler.Refresh)
			auth.POST("/logout", authHandler.Logout)
		}

		protected := v1.Group("/")
		protected.Use(middleware.Auth(tokenVerifier))
		{
			containers := protected.Group("/containers")
			{
				containers.POST("", coreHandler.CreateContainer)
				containers.GET("", coreHandler.GetContainers)
				containers.POST("/:id/action/:action", coreHandler.ActionContainer)
				containers.POST("/:id/expose", coreHandler.ExposeContainer)
				containers.GET("/:id/stats", coreHandler.GetContainerStats)
				containers.POST("/:id/logs/stream/ticket", telemetryTicketHandler.IssueLogsTicket(false))
				containers.POST("/:id/terminal/ticket", telemetryTicketHandler.IssueTerminalTicket(false))
			}

			volumes := protected.Group("/volumes")
			{
				volumes.POST("", coreHandler.CreateVolume)
				volumes.GET("", coreHandler.GetVolumes)
				volumes.DELETE("/:id", coreHandler.DeleteVolume)
			}

			images := protected.Group("/images")
			{
				images.GET("", coreHandler.GetImages)
				images.DELETE("/:id", coreHandler.DeleteImage)
				images.GET("/build/availability", coreHandler.GetImageBuildAvailability)
				images.POST("/build", middleware.RateLimit(limiter, cfg.UploadRateLimit, "image-build"), coreHandler.BuildImage)
			}

			builds := protected.Group("/builds")
			{
				builds.GET("", coreHandler.GetBuilds)
				builds.DELETE("/:id", coreHandler.DeleteBuild)
				builds.GET("/:id/logs", coreHandler.GetBuildLogs)
				builds.POST("/:id/cancel", coreHandler.CancelBuild)
			}

			projects := protected.Group("/projects")
			{
				projects.GET("", coreHandler.GetProjects)
				projects.DELETE("/:id", coreHandler.DeleteProject)
				projects.POST("/:id/start", coreHandler.StartProject)
				projects.POST("/:id/stop", coreHandler.StopProject)
				projects.POST("/:id/cancel", coreHandler.CancelProject)
				projects.POST("/compose", middleware.RateLimit(limiter, cfg.UploadRateLimit, "compose"), coreHttpProxy)
			}
		}
		admin := v1.Group("/admin")
		{
			admin.GET("/config", middleware.RequirePermission(tokenVerifier, permissions.SystemConfigRead), coreHandler.GetSystemConfig)
			admin.PUT("/config", middleware.RequirePermission(tokenVerifier, permissions.SystemConfigUpdate), coreHandler.UpdateSystemConfig)

			admin.GET("/users", middleware.RequirePermission(tokenVerifier, permissions.UsersAdminList), userHandler.ListUsers)
			admin.GET("/users/:id", middleware.RequirePermission(tokenVerifier, permissions.UsersAdminRead), userHandler.GetUser)
			admin.POST("/users", middleware.RequirePermission(tokenVerifier, permissions.UsersAdminCreate), userHandler.CreateUser)
			admin.PUT("/users/:id", middleware.RequirePermission(tokenVerifier, permissions.UsersAdminUpdate), userHandler.UpdateUser)
			admin.DELETE("/users/:id", middleware.RequirePermission(tokenVerifier, permissions.UsersAdminDelete), userHandler.DeactivateUser)
			admin.POST("/users/:id/activate", middleware.RequirePermission(tokenVerifier, permissions.UsersAdminUpdate), userHandler.ReactivateUser)

			admin.GET("/containers", middleware.RequirePermission(tokenVerifier, permissions.ContainersAdminList), coreHandler.GetAllContainers)
			admin.POST("/containers/:id/action/:action", middleware.RequirePermission(tokenVerifier, permissions.ContainersAdminAction), coreHandler.AdminActionContainer)
			admin.GET("/containers/:id/stats", middleware.RequirePermission(tokenVerifier, permissions.ContainersAdminStats), coreHandler.AdminGetContainerStats)
			admin.POST("/containers/:id/logs/stream/ticket", middleware.RequirePermission(tokenVerifier, permissions.ContainersAdminLogs), telemetryTicketHandler.IssueLogsTicket(true))
			admin.POST("/containers/:id/terminal/ticket", middleware.RequirePermission(tokenVerifier, permissions.ContainersAdminTerminal), telemetryTicketHandler.IssueTerminalTicket(true))

			admin.GET("/volumes", middleware.RequirePermission(tokenVerifier, permissions.VolumesAdminList), coreHandler.GetAllVolumes)
			admin.DELETE("/volumes/:id", middleware.RequirePermission(tokenVerifier, permissions.VolumesAdminDelete), coreHandler.AdminDeleteVolume)

			admin.GET("/images", middleware.RequirePermission(tokenVerifier, permissions.ImagesAdminList), coreHandler.GetAllImages)
			admin.DELETE("/images/:id", middleware.RequirePermission(tokenVerifier, permissions.ImagesAdminDelete), coreHandler.AdminDeleteImage)

			admin.GET("/builds", middleware.RequirePermission(tokenVerifier, permissions.BuildsAdminList), coreHandler.GetAllBuilds)
			admin.GET("/builds/:id/logs", middleware.RequirePermission(tokenVerifier, permissions.BuildsAdminList), coreHandler.AdminGetBuildLogs)
			admin.POST("/builds/:id/cancel", middleware.RequirePermission(tokenVerifier, permissions.BuildsAdminDelete), coreHandler.AdminCancelBuild)
			admin.DELETE("/builds/:id", middleware.RequirePermission(tokenVerifier, permissions.BuildsAdminDelete), coreHandler.AdminDeleteBuild)

			admin.GET("/projects", middleware.RequirePermission(tokenVerifier, permissions.ProjectsAdminList), coreHandler.GetAllProjects)
			admin.DELETE("/projects/:id", middleware.RequirePermission(tokenVerifier, permissions.ProjectsAdminDelete), coreHandler.AdminDeleteProject)
			admin.POST("/projects/:id/start", middleware.RequirePermission(tokenVerifier, permissions.ProjectsAdminStart), coreHandler.AdminStartProject)
			admin.POST("/projects/:id/stop", middleware.RequirePermission(tokenVerifier, permissions.ProjectsAdminStop), coreHandler.AdminStopProject)
			admin.POST("/projects/:id/cancel", middleware.RequirePermission(tokenVerifier, permissions.ProjectsAdminStop), coreHandler.AdminCancelProject)
		}
		stats := protected.Group("/stats")
		{
			stats.GET("", coreHandler.GetUserStats)
		}

		v1.GET("/containers/:id/logs/stream", telemetryLogsProxy)
		v1.GET("/containers/:id/terminal", telemetryTerminalProxy)
		v1.GET("/admin/containers/:id/logs/stream", adminTelemetryLogsProxy)
		v1.GET("/admin/containers/:id/terminal", adminTelemetryTerminalProxy)
	}

	return router
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
