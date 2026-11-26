package auditlog

import (
	"context"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/google/uuid"
)

// ActorFromContext returns the audit actor stated by the request's access
// scope: the actor's user ID (nil when the scope names no user), username and
// scope kind. A request without a scope is ActorScopeUnknown.
func ActorFromContext(ctx context.Context) (*uuid.UUID, string, string) {
	scope, ok := accessscope.FromContext(ctx)
	if !ok || scope.Kind == "" {
		return nil, "", ActorScopeUnknown
	}
	return OptionalID(scope.UserID), scope.Username, string(scope.Kind)
}

// OptionalID returns a pointer to id for an optional audit field, or nil for
// uuid.Nil.
func OptionalID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}
