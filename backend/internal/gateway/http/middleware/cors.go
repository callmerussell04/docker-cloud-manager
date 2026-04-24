package middleware

import (
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CORSMiddleware() gin.HandlerFunc {
	allowedOrigins := map[string]struct{}{}
	origins := os.Getenv("CORS_ALLOWED_ORIGINS")
	if origins == "" {
		origins = "http://localhost,http://localhost:3000,http://localhost:5173"
	}
	for _, origin := range strings.Split(origins, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowedOrigins[origin] = struct{}{}
		}
	}

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
			if origin == "" {
				return true
			}
			_, ok := allowedOrigins[origin]
			return ok
		},

		MaxAge: 12 * time.Hour,
	})
}
