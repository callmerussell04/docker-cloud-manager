package handler

import (
	"context"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
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
	_ = h.service.RecordAuditEvent(context.Background(), model.AuditEvent{
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
}

func auditOutcome(err error) (string, string) {
	if err == nil {
		return auditlog.OutcomeSuccess, ""
	}
	return auditlog.OutcomeFailure, apperrors.SafeMessage(err)
}
