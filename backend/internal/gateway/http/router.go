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

func NewRouter(cfg Config, authHandler *handler.AuthHandler, coreHandler *handler.CoreHandler, healthHandler *handler.HealthHandler, builderProxy gin.HandlerFunc, buildProxy gin.HandlerFunc, coreHttpProxy gin.HandlerFunc, tokenVerifier middleware.TokenVerifier, logger *slog.Logger) *gin.Engine {
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
				images.POST("/build", middleware.RateLimit(limiter, cfg.UploadRateLimit, "image-build"), builderProxy)
			}

			builds := protected.Group("/builds")
			{
				builds.GET("", coreHandler.GetBuilds)
				builds.DELETE("/:id", coreHandler.DeleteBuild)
				builds.GET("/:id/logs", buildProxy)
				builds.POST("/:id/cancel", buildProxy)
			}

			projects := protected.Group("/projects")
			{
				projects.GET("", coreHandler.GetProjects)
				projects.DELETE("/:id", coreHandler.DeleteProject)
				projects.POST("/:id/start", coreHandler.StartProject)
				projects.POST("/:id/stop", coreHandler.StopProject)
				projects.POST("/compose", middleware.RateLimit(limiter, cfg.UploadRateLimit, "compose"), coreHttpProxy)
			}
		}
		admin := v1.Group("/admin")
		{
			admin.GET("/config", middleware.RequirePermission(tokenVerifier, permissions.SystemConfigRead), coreHandler.GetSystemConfig)
			admin.PUT("/config", middleware.RequirePermission(tokenVerifier, permissions.SystemConfigUpdate), coreHandler.UpdateSystemConfig)

			admin.GET("/containers", middleware.RequirePermission(tokenVerifier, permissions.ContainersAdminList), coreHandler.GetAllContainers)
			admin.POST("/containers/:id/action/:action", middleware.RequirePermission(tokenVerifier, permissions.ContainersAdminAction), coreHandler.AdminActionContainer)
			admin.GET("/containers/:id/stats", middleware.RequirePermission(tokenVerifier, permissions.ContainersAdminStats), coreHandler.AdminGetContainerStats)

			admin.GET("/volumes", middleware.RequirePermission(tokenVerifier, permissions.VolumesAdminList), coreHandler.GetAllVolumes)
			admin.DELETE("/volumes/:id", middleware.RequirePermission(tokenVerifier, permissions.VolumesAdminDelete), coreHandler.AdminDeleteVolume)

			admin.GET("/images", middleware.RequirePermission(tokenVerifier, permissions.ImagesAdminList), coreHandler.GetAllImages)
			admin.DELETE("/images/:id", middleware.RequirePermission(tokenVerifier, permissions.ImagesAdminDelete), coreHandler.AdminDeleteImage)

			admin.GET("/builds", middleware.RequirePermission(tokenVerifier, permissions.BuildsAdminList), coreHandler.GetAllBuilds)
			admin.DELETE("/builds/:id", middleware.RequirePermission(tokenVerifier, permissions.BuildsAdminDelete), coreHandler.AdminDeleteBuild)

			admin.GET("/projects", middleware.RequirePermission(tokenVerifier, permissions.ProjectsAdminList), coreHandler.GetAllProjects)
			admin.DELETE("/projects/:id", middleware.RequirePermission(tokenVerifier, permissions.ProjectsAdminDelete), coreHandler.AdminDeleteProject)
			admin.POST("/projects/:id/start", middleware.RequirePermission(tokenVerifier, permissions.ProjectsAdminStart), coreHandler.AdminStartProject)
			admin.POST("/projects/:id/stop", middleware.RequirePermission(tokenVerifier, permissions.ProjectsAdminStop), coreHandler.AdminStopProject)
		}
		stats := protected.Group("/stats")
		{
			stats.GET("", coreHandler.GetUserStats)
		}
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
