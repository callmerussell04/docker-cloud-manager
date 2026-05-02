package handler

import (
	"context"
	"net/http"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/gin-gonic/gin"
)

type TelemetryTicketIssuer interface {
	Issue(ctx context.Context, claims model.TelemetryTicketClaims) (model.TelemetryTicket, error)
}

type TelemetryTicketHandler struct {
	store TelemetryTicketIssuer
}

func NewTelemetryTicketHandler(store TelemetryTicketIssuer) *TelemetryTicketHandler {
	return &TelemetryTicketHandler{store: store}
}

func (h *TelemetryTicketHandler) IssueLogsTicket(adminRoute bool) gin.HandlerFunc {
	return h.issue(model.TelemetryStreamLogs, adminRoute)
}

func (h *TelemetryTicketHandler) IssueTerminalTicket(adminRoute bool) gin.HandlerFunc {
	return h.issue(model.TelemetryStreamTerminal, adminRoute)
}

func (h *TelemetryTicketHandler) issue(streamType string, adminRoute bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		containerID, ok := pathUUID(c, "id")
		if !ok {
			return
		}
		scope, ok := accessscope.FromContext(c.Request.Context())
		if !ok || scope.Kind == "" {
			httpresponse.Respond(c, http.StatusUnauthorized, apperrors.ErrUnauthorized)
			return
		}
		ticket, err := h.store.Issue(c.Request.Context(), model.TelemetryTicketClaims{
			ContainerID: containerID,
			StreamType:  streamType,
			AdminRoute:  adminRoute,
			Scope:       scope,
		})
		if err != nil {
			httpresponse.Respond(c, httpresponse.Status(err), err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{
			"ticket":     ticket.Ticket,
			"expires_at": ticket.ExpiresAt.Unix(),
		})
	}
}
