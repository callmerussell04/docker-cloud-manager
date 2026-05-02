package accessscope

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

func TestScopeAllowsOwner(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()

	userScope := Scope{Kind: KindUser, UserID: ownerID}
	if !userScope.AllowsOwner(ownerID) {
		t.Fatal("user scope did not allow its own owner id")
	}
	if userScope.AllowsOwner(otherID) {
		t.Fatal("user scope allowed a different owner id")
	}

	if !((Scope{Kind: KindAdmin, UserID: otherID}).AllowsOwner(ownerID)) {
		t.Fatal("admin scope did not allow owner access")
	}
	if !((Scope{Kind: KindSystem}).AllowsOwner(ownerID)) {
		t.Fatal("system scope did not allow owner access")
	}
}

func TestRequireUserOwner(t *testing.T) {
	ownerID := uuid.New()
	got, err := RequireUserOwner(WithUserScope(context.Background(), ownerID, "user", "user"))
	if err != nil {
		t.Fatalf("RequireUserOwner returned error: %v", err)
	}
	if got != ownerID {
		t.Fatalf("owner id = %s, want %s", got, ownerID)
	}

	_, err = RequireUserOwner(WithAdminScope(context.Background(), uuid.New(), "admin", "admin"))
	if !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatalf("admin RequireUserOwner error = %v, want forbidden", err)
	}
}

func TestRequireOwnerAccess(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()

	err := RequireOwnerAccess(WithUserScope(context.Background(), ownerID, "", ""), otherID)
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("cross-owner error = %v, want not found", err)
	}

	if err := RequireOwnerAccess(WithAdminScope(context.Background(), uuid.New(), "", "admin"), otherID); err != nil {
		t.Fatalf("admin owner access returned error: %v", err)
	}
}
