package middleware

import (
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/jwtutils"
	"github.com/gin-gonic/gin"
)

type TokenParser interface {
	ParseToken(authHeader string) (jwtutils.UserClaims, error)
}

func Auth(parser TokenParser) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		claims, err := parser.ParseToken(authHeader)
		if err != nil {
			apperrors.Respond(c, http.StatusUnauthorized, apperrors.ErrUnauthorized)
			return
		}

		c.Set("user_id", claims.UserID.String())
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)

		c.Next()
	}
}
