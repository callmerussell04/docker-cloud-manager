package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/gin-gonic/gin"
)

type TokenVerifier interface {
	VerifyAccessToken(ctx context.Context, authHeader string) (domain.AuthUser, error)
	CheckPermission(ctx context.Context, authHeader, permission string) error
}

func Auth(verifier TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		user, err := verifier.VerifyAccessToken(c.Request.Context(), authHeader)
		if err != nil {
			apperrors.Respond(c, http.StatusUnauthorized, apperrors.ErrUnauthorized)
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
			statusCode := http.StatusInternalServerError
			responseErr := apperrors.ErrInternal
			switch {
			case errors.Is(err, apperrors.ErrUnauthorized):
				statusCode = http.StatusUnauthorized
				responseErr = apperrors.ErrUnauthorized
			case errors.Is(err, apperrors.ErrForbidden):
				statusCode = http.StatusForbidden
				responseErr = apperrors.ErrForbidden
			}
			apperrors.Respond(c, statusCode, responseErr)
			return
		}
		c.Next()
	}
}
