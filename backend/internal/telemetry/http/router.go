package http

import (
	"log/slog"

	"github.com/callmerussell04/docker-cloud-manager/internal/internalauth"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/gin-gonic/gin"
)

func NewRouter(handler *Handler, internalToken string, logger *slog.Logger) *gin.Engine {
	router := gin.New()
	router.Use(logging.RequestIDMiddleware())
	router.Use(logging.AccessLogMiddleware(logger))
	router.Use(logging.RecoveryMiddleware(logger))

	v1 := router.Group("/api/v1")
	v1.Use(internalauth.Middleware(internalToken))
	{
		containers := v1.Group("/containers")
		{
			containers.GET("/:id/logs/stream", handler.StreamLogs)
			containers.GET("/:id/terminal", handler.OpenTerminal)
		}
		adminContainers := v1.Group("/admin/containers")
		{
			adminContainers.GET("/:id/logs/stream", handler.StreamLogs)
			adminContainers.GET("/:id/terminal", handler.OpenTerminal)
		}
	}

	return router
}
