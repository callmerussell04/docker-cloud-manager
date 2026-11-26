package handler

import (
	"context"
	"fmt"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

// AuditRecorder records an audit event in Core.
type AuditRecorder interface {
	RecordAuditEvent(ctx context.Context, event model.AuditEvent) error
}

func (h *CoreHandler) recordAudit(c *gin.Context, input auditInput) {
	recordAudit(c, h.service, input)
}

// recordAudit attributes the event to the request's scope.
func recordAudit(c *gin.Context, recorder AuditRecorder, input auditInput) {
	recordAuditIn(c.Request.Context(), c, recorder, input)
}

// recordAuditAs attributes the event to scope instead of the request's: a
// public auth route has no scope, and learns its actor only from the result.
func recordAuditAs(c *gin.Context, recorder AuditRecorder, scope accessscope.Scope, input auditInput) {
	recordAuditIn(accessscope.WithScope(c.Request.Context(), scope), c, recorder, input)
}

// recordAuditIn records the event under the scope ctx carries, which is also
// the scope Core takes the actor from.
func recordAuditIn(ctx context.Context, c *gin.Context, recorder AuditRecorder, input auditInput) {
	if input.Action == "" {
		return
	}
	scope, _ := accessscope.FromContext(ctx)
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
	if scope.UserID != uuid.Nil {
		actorUserID = scope.UserID.String()
	}
	// Audit is best effort: a failed write never changes the response. It is
	// attached to the request so the access log records it.
	err := recorder.RecordAuditEvent(context.WithoutCancel(ctx), model.AuditEvent{
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
		RequestID:     logging.RequestIDFromContext(ctx),
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
