package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
)

type TelemetryTicketConsumer interface {
	Consume(ctx context.Context, token, expectedContainerID, expectedStreamType string, expectedAdminRoute bool) (model.TelemetryTicketClaims, error)
}

type TelemetryProxyAuthOptions struct {
	Tickets    TelemetryTicketConsumer
	StreamType string
	AdminRoute bool
}

func authenticateTelemetryProxy(c *gin.Context, auth TelemetryProxyAuthOptions, containerID string) bool {
	ticket := c.Query("ticket")
	if ticket == "" || auth.Tickets == nil {
		httpresponse.Respond(c, http.StatusUnauthorized, apperrors.ErrUnauthorized)
		return false
	}
	claims, err := auth.Tickets.Consume(c.Request.Context(), ticket, containerID, auth.StreamType, auth.AdminRoute)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return false
	}
	c.Request = c.Request.WithContext(accessscope.WithScope(c.Request.Context(), claims.Scope))
	setScopeGinValues(c, claims.Scope)
	removeQueryParam(c, "ticket")
	return true
}

func setScopeGinValues(c *gin.Context, scope accessscope.Scope) {
	if scope.UserID != uuid.Nil {
		c.Set("user_id", scope.UserID.String())
	}
	if scope.Username != "" {
		c.Set("username", scope.Username)
	}
	if scope.Role != "" {
		c.Set("role", scope.Role)
	}
}

func removeQueryParam(c *gin.Context, key string) {
	query := c.Request.URL.Query()
	query.Del(key)
	c.Request.URL.RawQuery = query.Encode()
}
