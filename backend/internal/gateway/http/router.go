package http

import (
	"github.com/gin-gonic/gin"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/middleware"
)

func NewRouter(authHandler *handler.AuthHandler, coreHandler *handler.CoreHandler, builderProxy gin.HandlerFunc, coreHttpProxy gin.HandlerFunc, tokenParser middleware.TokenParser) *gin.Engine {
	router := gin.Default()

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
		protected.Use(middleware.Auth(tokenParser))
		{
			containers := protected.Group("/containers")
			{
				containers.POST("", coreHandler.CreateContainer)
				containers.GET("", coreHandler.GetContainers)
				containers.POST("/:id/action/:action", coreHandler.ActionContainer)
				containers.POST("/:id/expose", coreHandler.ExposeContainer)
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
				builds.GET("/:id/logs", builderProxy)
			}

			projects := protected.Group("/projects")
			{
				projects.GET("", coreHandler.GetProjects)
				projects.DELETE("/:id", coreHandler.DeleteProject)
				projects.POST("/:id/stop", coreHandler.StopProject)
				projects.POST("/compose", coreHttpProxy)
			}
		}
	}

	return router
}
