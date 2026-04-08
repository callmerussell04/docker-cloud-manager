package middleware

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CORSMiddleware() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},

		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Content-Length",
			"Accept-Encoding",
			"X-CSRF-Token",
			"Authorization",
		},

		ExposeHeaders: []string{"Content-Length"},

		AllowCredentials: true,

		AllowOriginFunc: func(origin string) bool {
			return true
		},

		MaxAge: 12 * time.Hour,
	})
}
