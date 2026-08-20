package accessscope

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type Kind string

const (
	KindUser   Kind = "user"
	KindAdmin  Kind = "admin"
	KindSystem Kind = "system"
)

type Scope struct {
	Kind     Kind
	UserID   uuid.UUID
	Username string
	Role     string
}

type contextKey struct{}

func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, contextKey{}, scope)
}

func WithUserScope(ctx context.Context, userID uuid.UUID, username, role string) context.Context {
	return WithScope(ctx, Scope{Kind: KindUser, UserID: userID, Username: username, Role: role})
}

func WithAdminScope(ctx context.Context, userID uuid.UUID, username, role string) context.Context {
	return WithScope(ctx, Scope{Kind: KindAdmin, UserID: userID, Username: username, Role: role})
}

func WithSystemScope(ctx context.Context) context.Context {
	return WithScope(ctx, Scope{Kind: KindSystem})
}

func FromContext(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(contextKey{}).(Scope)
	return scope, ok
}

func RequireScope(ctx context.Context) (Scope, error) {
	scope, ok := FromContext(ctx)
	if !ok || scope.Kind == "" {
		return Scope{}, apperrors.ErrUnauthorized
	}
	return scope, nil
}

func RequireUserOwner(ctx context.Context) (uuid.UUID, error) {
	scope, err := RequireScope(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if scope.Kind != KindUser || scope.UserID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: user scope is required", apperrors.ErrForbidden)
	}
	return scope.UserID, nil
}

func RequireOwnerAccess(ctx context.Context, ownerID uuid.UUID) error {
	scope, err := RequireScope(ctx)
	if err != nil {
		return err
	}
	if scope.AllowsOwner(ownerID) {
		return nil
	}
	return apperrors.ErrNotFound
}

func (s Scope) AllowsOwner(ownerID uuid.UUID) bool {
	switch s.Kind {
	case KindAdmin:
		return true
	case KindUser:
		return s.UserID == ownerID && ownerID != uuid.Nil
	default:
		return false
	}
}

func (s Scope) OwnerFilter() *uuid.UUID {
	if s.Kind != KindUser || s.UserID == uuid.Nil {
		return nil
	}
	ownerID := s.UserID
	return &ownerID
}
