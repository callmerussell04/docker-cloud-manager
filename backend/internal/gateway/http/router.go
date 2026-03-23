package http

import (
	"github.com/gin-gonic/gin"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/middleware"
)

func NewRouter(authHandler *handler.AuthHandler, tokenParser middleware.TokenParser) *gin.Engine {
	router := gin.Default()

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
			protected.GET("/ping", func(c *gin.Context) {
				userID := c.GetString("user_id")
				c.JSON(200, gin.H{"message": "pong", "user_id": userID})
			})
		}
	}

	return router
}
