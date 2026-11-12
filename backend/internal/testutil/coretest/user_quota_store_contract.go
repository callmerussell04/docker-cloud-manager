package coretest

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// UserQuotaStore is what Core stores a user's custom quota in: the Postgres
// repository and UserQuotaStoreFake.
type UserQuotaStore interface {
	GetCustomUserQuota(ctx context.Context, ownerID uuid.UUID) (model.UserQuota, bool, error)
	SetCustomUserQuota(ctx context.Context, ownerID uuid.UUID, quota model.UserQuota) error
}

// RunUserQuotaStoreContract pins the behaviour every UserQuotaStore shares.
func RunUserQuotaStoreContract(t *testing.T, newStore func(t *testing.T) UserQuotaStore) {
	t.Helper()
	t.Run("an owner without a custom quota has none", func(t *testing.T) {
		store := newStore(t)

		_, found, err := store.GetCustomUserQuota(context.Background(), uuid.New())

		require.NoError(t, err)
		require.False(t, found)
	})
	t.Run("a set quota reads back as custom", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)
		ownerID := uuid.New()

		require.NoError(t, store.SetCustomUserQuota(ctx, ownerID, model.UserQuota{CPUMillicores: 1500, RAMMB: 3072, DiskMB: 8192}))

		got, found, err := store.GetCustomUserQuota(ctx, ownerID)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, model.UserQuota{CPUMillicores: 1500, RAMMB: 3072, DiskMB: 8192, Custom: true}, got)
	})
	t.Run("setting again replaces the quota", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)
		ownerID := uuid.New()
		require.NoError(t, store.SetCustomUserQuota(ctx, ownerID, model.UserQuota{CPUMillicores: 1500, RAMMB: 3072, DiskMB: 8192}))

		require.NoError(t, store.SetCustomUserQuota(ctx, ownerID, model.UserQuota{CPUMillicores: 250, RAMMB: 512, DiskMB: 1024}))

		got, found, err := store.GetCustomUserQuota(ctx, ownerID)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, model.UserQuota{CPUMillicores: 250, RAMMB: 512, DiskMB: 1024, Custom: true}, got)
	})
	t.Run("one owner's quota is not another's", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)
		require.NoError(t, store.SetCustomUserQuota(ctx, uuid.New(), model.UserQuota{CPUMillicores: 1500, RAMMB: 3072, DiskMB: 8192}))

		_, found, err := store.GetCustomUserQuota(ctx, uuid.New())

		require.NoError(t, err)
		require.False(t, found)
	})
}
