package middleware

import (
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CORSMiddleware(origins []string) gin.HandlerFunc {
	allowedOrigins := map[string]struct{}{}
	for _, origin := range origins {
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
			if len(allowedOrigins) == 0 {
				return false
			}
			_, ok := allowedOrigins[origin]
			return ok
		},

		MaxAge: 12 * time.Hour,
	})
}
