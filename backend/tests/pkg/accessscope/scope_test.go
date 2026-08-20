package accessscope_test

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestScopeAllowsOwner(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()

	userScope := accessscope.Scope{Kind: accessscope.KindUser, UserID: ownerID}
	require.True(t, userScope.AllowsOwner(ownerID))
	require.False(t, userScope.AllowsOwner(otherID))
	require.True(t, (accessscope.Scope{Kind: accessscope.KindAdmin, UserID: otherID}).AllowsOwner(ownerID))
	require.False(t, (accessscope.Scope{Kind: accessscope.KindSystem}).AllowsOwner(ownerID))
}

func TestRequireUserOwner(t *testing.T) {
	ownerID := uuid.New()
	got, err := accessscope.RequireUserOwner(accessscope.WithUserScope(context.Background(), ownerID, "user", "user"))
	require.NoError(t, err)
	require.Equal(t, ownerID, got)

	_, err = accessscope.RequireUserOwner(accessscope.WithAdminScope(context.Background(), uuid.New(), "admin", "admin"))
	require.True(t, errors.Is(err, apperrors.ErrForbidden))
}

func TestRequireOwnerAccess(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()

	err := accessscope.RequireOwnerAccess(accessscope.WithUserScope(context.Background(), ownerID, "", ""), otherID)
	require.True(t, errors.Is(err, apperrors.ErrNotFound))
	require.NoError(t, accessscope.RequireOwnerAccess(accessscope.WithAdminScope(context.Background(), uuid.New(), "", "admin"), otherID))
	err = accessscope.RequireOwnerAccess(accessscope.WithSystemScope(context.Background()), ownerID)
	require.True(t, errors.Is(err, apperrors.ErrNotFound))
}
