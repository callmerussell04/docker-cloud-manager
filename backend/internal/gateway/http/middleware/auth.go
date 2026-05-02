package middleware

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

		if !setAuthUser(c, user, accessscope.KindUser) {
			httpresponse.Respond(c, httpresponse.Status(apperrors.ErrUnauthorized), apperrors.ErrUnauthorized)
			return
		}

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
		if !setAuthUser(c, user, accessscope.KindAdmin) {
			httpresponse.Respond(c, httpresponse.Status(apperrors.ErrUnauthorized), apperrors.ErrUnauthorized)
			return
		}
		c.Next()
	}
}

func setAuthUser(c *gin.Context, user model.AuthUser, kind accessscope.Kind) bool {
	c.Set("user_id", user.UserID)
	c.Set("username", user.Username)
	c.Set("role", user.Role)
	userID, err := uuid.Parse(user.UserID)
	if err != nil {
		return false
	}
	switch kind {
	case accessscope.KindAdmin:
		c.Request = c.Request.WithContext(accessscope.WithAdminScope(c.Request.Context(), userID, user.Username, user.Role))
	default:
		c.Request = c.Request.WithContext(accessscope.WithUserScope(c.Request.Context(), userID, user.Username, user.Role))
	}
	return true
}
