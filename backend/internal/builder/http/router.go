package http

import (
	"github.com/callmerussell04/docker-cloud-manager/internal/builder/http/handler"
	"github.com/gin-gonic/gin"
)

func NewRouter(buildHandler *handler.BuildHandler) *gin.Engine {
	router := gin.Default()

	v1 := router.Group("/api/v1")
	{
		images := v1.Group("/images")
		{
			images.POST("/build", buildHandler.BuildImage)
		}
	}

	return router
}
