package service

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type AuditRecorder interface {
	RecordAuditEvent(ctx context.Context, event model.AuditEvent) error
}

func auditActorFromContext(ctx context.Context) (*uuid.UUID, string, string) {
	scope, ok := accessscope.FromContext(ctx)
	if !ok || scope.Kind == "" {
		return nil, "", auditlog.ActorScopeUnknown
	}
	var actorID *uuid.UUID
	if scope.UserID != uuid.Nil {
		value := scope.UserID
		actorID = &value
	}
	return actorID, scope.Username, string(scope.Kind)
}

func ownerPtr(ownerID uuid.UUID) *uuid.UUID {
	if ownerID == uuid.Nil {
		return nil
	}
	value := ownerID
	return &value
}

func requestIDFromContext(ctx context.Context) string {
	return logging.RequestIDFromContext(ctx)
}
