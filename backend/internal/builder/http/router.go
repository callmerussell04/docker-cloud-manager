package http

import (
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/gin-gonic/gin"
)

func NewRouter(buildHandler *handler.BuildHandler, internalToken string, logger *slog.Logger) *gin.Engine {
	router := gin.New()

	router.Use(logging.RequestIDMiddleware())
	router.Use(logging.AccessLogMiddleware(logger))
	router.Use(logging.RecoveryMiddleware(logger))

	v1 := router.Group("/api/v1")
	v1.Use(internalauth.Middleware(internalToken))
	{
		images := v1.Group("/images")
		{
			images.POST("/build", buildHandler.BuildImage)
		}
		builds := v1.Group("/builds")
		{
			builds.GET("/:id/logs", buildHandler.GetLogs)
		}
	}

	return router
}
