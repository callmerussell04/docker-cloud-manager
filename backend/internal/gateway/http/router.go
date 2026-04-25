package http

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/middleware"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
)

const (
	permissionSystemConfigRead   = "system.config.read"
	permissionSystemConfigUpdate = "system.config.update"

	permissionContainersAdminList   = "containers.admin.list"
	permissionContainersAdminAction = "containers.admin.action"
	permissionContainersAdminStats  = "containers.admin.stats"

	permissionVolumesAdminList   = "volumes.admin.list"
	permissionVolumesAdminDelete = "volumes.admin.delete"

	permissionImagesAdminList   = "images.admin.list"
	permissionImagesAdminDelete = "images.admin.delete"

	permissionBuildsAdminList   = "builds.admin.list"
	permissionBuildsAdminDelete = "builds.admin.delete"

	permissionProjectsAdminList   = "projects.admin.list"
	permissionProjectsAdminDelete = "projects.admin.delete"
	permissionProjectsAdminStop   = "projects.admin.stop"
)

func NewRouter(authHandler *handler.AuthHandler, coreHandler *handler.CoreHandler, builderProxy gin.HandlerFunc, builderLogsProxy gin.HandlerFunc, coreHttpProxy gin.HandlerFunc, tokenVerifier middleware.TokenVerifier, logger *slog.Logger) *gin.Engine {
	router := gin.New()

	router.Use(logging.RequestIDMiddleware())
	router.Use(logging.AccessLogMiddleware(logger))
	router.Use(logging.RecoveryMiddleware(logger))
	router.Use(middleware.CORSMiddleware())

	v1 := router.Group("/api/v1")
	{
		auth := v1.Group("/auth")
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
				images.POST("/build", builderProxy)
			}

			builds := protected.Group("/builds")
			{
				builds.GET("", coreHandler.GetBuilds)
				builds.DELETE("/:id", coreHandler.DeleteBuild)
				builds.GET("/:id/logs", builderLogsProxy)
			}

			projects := protected.Group("/projects")
			{
				projects.GET("", coreHandler.GetProjects)
				projects.DELETE("/:id", coreHandler.DeleteProject)
				projects.POST("/:id/stop", coreHandler.StopProject)
				projects.POST("/compose", coreHttpProxy)
			}
		}
		admin := protected.Group("/admin")
		{
			admin.GET("/config", middleware.RequirePermission(tokenVerifier, permissionSystemConfigRead), coreHandler.GetSystemConfig)
			admin.PUT("/config", middleware.RequirePermission(tokenVerifier, permissionSystemConfigUpdate), coreHandler.UpdateSystemConfig)

			admin.GET("/containers", middleware.RequirePermission(tokenVerifier, permissionContainersAdminList), coreHandler.GetAllContainers)
			admin.POST("/containers/:id/action/:action", middleware.RequirePermission(tokenVerifier, permissionContainersAdminAction), coreHandler.AdminActionContainer)
			admin.GET("/containers/:id/stats", middleware.RequirePermission(tokenVerifier, permissionContainersAdminStats), coreHandler.AdminGetContainerStats)

			admin.GET("/volumes", middleware.RequirePermission(tokenVerifier, permissionVolumesAdminList), coreHandler.GetAllVolumes)
			admin.DELETE("/volumes/:id", middleware.RequirePermission(tokenVerifier, permissionVolumesAdminDelete), coreHandler.AdminDeleteVolume)

			admin.GET("/images", middleware.RequirePermission(tokenVerifier, permissionImagesAdminList), coreHandler.GetAllImages)
			admin.DELETE("/images/:id", middleware.RequirePermission(tokenVerifier, permissionImagesAdminDelete), coreHandler.AdminDeleteImage)

			admin.GET("/builds", middleware.RequirePermission(tokenVerifier, permissionBuildsAdminList), coreHandler.GetAllBuilds)
			admin.DELETE("/builds/:id", middleware.RequirePermission(tokenVerifier, permissionBuildsAdminDelete), coreHandler.AdminDeleteBuild)

			admin.GET("/projects", middleware.RequirePermission(tokenVerifier, permissionProjectsAdminList), coreHandler.GetAllProjects)
			admin.DELETE("/projects/:id", middleware.RequirePermission(tokenVerifier, permissionProjectsAdminDelete), coreHandler.AdminDeleteProject)
			admin.POST("/projects/:id/stop", middleware.RequirePermission(tokenVerifier, permissionProjectsAdminStop), coreHandler.AdminStopProject)
		}
		stats := protected.Group("/stats")
		{
			stats.GET("", coreHandler.GetUserStats)
		}
	}

	return router
}
