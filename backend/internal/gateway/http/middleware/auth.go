package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/gin-gonic/gin"
)

type TokenVerifier interface {
	VerifyAccessToken(ctx context.Context, authHeader string) (model.AuthUser, error)
	CheckPermission(ctx context.Context, authHeader, permission string) error
}

func Auth(verifier TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		user, err := verifier.VerifyAccessToken(c.Request.Context(), authHeader)
		if err != nil {
			apperrors.Respond(c, apperrors.HTTPStatus(err), err)
			return
		}

		c.Set("user_id", user.UserID)
		c.Set("username", user.Username)
		c.Set("role", user.Role)

		c.Next()
	}
}

func RequirePermission(verifier TokenVerifier, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if err := verifier.CheckPermission(c.Request.Context(), authHeader, permission); err != nil {
			statusCode := apperrors.HTTPStatus(err)
			if statusCode >= http.StatusInternalServerError {
				slog.ErrorContext(c.Request.Context(), "permission check failed", "permission", permission, "error", err)
			}
			apperrors.Respond(c, statusCode, err)
			return
		}
		c.Next()
	}
}
