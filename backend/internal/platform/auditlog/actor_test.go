package auditlog_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/auditlog"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestActorFromContext(t *testing.T) {
	userID := uuid.New()

	t.Run("user scope", func(t *testing.T) {
		ctx := accessscope.WithUserScope(context.Background(), userID, "alice", "")
		actorID, username, scope := auditlog.ActorFromContext(ctx)
		require.Equal(t, &userID, actorID)
		require.Equal(t, "alice", username)
		require.Equal(t, auditlog.ActorScopeUser, scope)
	})
	t.Run("scope without user", func(t *testing.T) {
		ctx := accessscope.WithScope(context.Background(), accessscope.Scope{Kind: accessscope.KindSystem})
		actorID, username, scope := auditlog.ActorFromContext(ctx)
		require.Nil(t, actorID)
		require.Empty(t, username)
		require.Equal(t, string(accessscope.KindSystem), scope)
	})
	t.Run("no scope", func(t *testing.T) {
		actorID, username, scope := auditlog.ActorFromContext(context.Background())
		require.Nil(t, actorID)
		require.Empty(t, username)
		require.Equal(t, auditlog.ActorScopeUnknown, scope)
	})
}

func TestOptionalID(t *testing.T) {
	id := uuid.New()
	require.Equal(t, &id, auditlog.OptionalID(id))
	require.Nil(t, auditlog.OptionalID(uuid.Nil))
}
