package middleware

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/gin-gonic/gin"
)

type TokenVerifier interface {
	VerifyAccessToken(ctx context.Context, authHeader string) (model.AuthUser, error)
	CheckPermission(ctx context.Context, authHeader, permission string) (model.AuthUser, error)
}

func Auth(verifier TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		user, err := verifier.VerifyAccessToken(c.Request.Context(), authHeader)
		if err != nil {
			httpresponse.Respond(c, httpresponse.Status(err), err)
			return
		}

		setAuthUser(c, user)

		c.Next()
	}
}

func RequirePermission(verifier TokenVerifier, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		user, err := verifier.CheckPermission(c.Request.Context(), authHeader, permission)
		if err != nil {
			httpresponse.Respond(c, httpresponse.Status(err), err)
			return
		}
		setAuthUser(c, user)
		c.Next()
	}
}

func setAuthUser(c *gin.Context, user model.AuthUser) {
	c.Set("user_id", user.UserID)
	c.Set("username", user.Username)
	c.Set("role", user.Role)
}
