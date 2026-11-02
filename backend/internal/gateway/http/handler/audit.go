package handler

import (
	"context"
	"fmt"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/internal/platform/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/internal/platform/logging"
	"github.com/gin-gonic/gin"
)

type auditInput struct {
	Action       string
	ResourceType string
	ResourceID   string
	ResourceName string
	OwnerID      string
	Outcome      string
	ErrorCode    string
	DetailsJSON  string
}

func (h *CoreHandler) recordAudit(c *gin.Context, input auditInput) {
	if input.Action == "" {
		return
	}
	scope, _ := accessscope.FromContext(c.Request.Context())
	actorScope := string(scope.Kind)
	if actorScope == "" {
		actorScope = auditlog.ActorScopeUnknown
	}
	outcome := input.Outcome
	if outcome == "" {
		outcome = auditlog.OutcomeSuccess
	}
	detailsJSON := input.DetailsJSON
	if detailsJSON == "" {
		detailsJSON = "{}"
	}
	actorUserID := ""
	if scope.UserID.String() != "00000000-0000-0000-0000-000000000000" {
		actorUserID = scope.UserID.String()
	}
	// Audit is best effort: a failed write never changes the response. It is
	// attached to the request so the access log records it.
	err := h.service.RecordAuditEvent(context.WithoutCancel(c.Request.Context()), model.AuditEvent{
		OccurredAt:    time.Now().UTC().Unix(),
		ActorUserID:   actorUserID,
		ActorUsername: scope.Username,
		ActorScope:    actorScope,
		Action:        input.Action,
		Outcome:       outcome,
		ResourceType:  input.ResourceType,
		ResourceID:    input.ResourceID,
		ResourceName:  input.ResourceName,
		OwnerID:       input.OwnerID,
		RequestID:     logging.RequestIDFromContext(c.Request.Context()),
		ClientIP:      c.ClientIP(),
		UserAgent:     c.Request.UserAgent(),
		ErrorCode:     input.ErrorCode,
		DetailsJSON:   detailsJSON,
	})
	if err != nil {
		_ = c.Error(fmt.Errorf("record audit event: %w", err)) // c.Error returns its argument
	}
}

func auditOutcome(err error) (string, string) {
	if err == nil {
		return auditlog.OutcomeSuccess, ""
	}
	return auditlog.OutcomeFailure, apperrors.SafeMessage(err)
}
