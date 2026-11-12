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
	GetCustomUserQuotas(ctx context.Context, ownerIDs []uuid.UUID) (map[uuid.UUID]model.UserQuota, error)
	SetCustomUserQuota(ctx context.Context, ownerID uuid.UUID, quota model.UserQuota) error
	DeleteCustomUserQuota(ctx context.Context, ownerID uuid.UUID) error
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
	t.Run("a batch read returns only the owners with a custom quota", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)
		customID, defaultID := uuid.New(), uuid.New()
		require.NoError(t, store.SetCustomUserQuota(ctx, customID, model.UserQuota{CPUMillicores: 1500, RAMMB: 3072, DiskMB: 8192}))

		got, err := store.GetCustomUserQuotas(ctx, []uuid.UUID{customID, defaultID})

		require.NoError(t, err)
		require.Equal(t, map[uuid.UUID]model.UserQuota{
			customID: {CPUMillicores: 1500, RAMMB: 3072, DiskMB: 8192, Custom: true},
		}, got)
	})
	t.Run("a batch read of no owners is empty", func(t *testing.T) {
		got, err := newStore(t).GetCustomUserQuotas(context.Background(), nil)

		require.NoError(t, err)
		require.Empty(t, got)
	})
	t.Run("deleting returns the owner to the default quota", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)
		ownerID, otherID := uuid.New(), uuid.New()
		require.NoError(t, store.SetCustomUserQuota(ctx, ownerID, model.UserQuota{CPUMillicores: 1500, RAMMB: 3072, DiskMB: 8192}))
		require.NoError(t, store.SetCustomUserQuota(ctx, otherID, model.UserQuota{CPUMillicores: 250, RAMMB: 512, DiskMB: 1024}))

		require.NoError(t, store.DeleteCustomUserQuota(ctx, ownerID))

		_, found, err := store.GetCustomUserQuota(ctx, ownerID)
		require.NoError(t, err)
		require.False(t, found)
		_, found, err = store.GetCustomUserQuota(ctx, otherID)
		require.NoError(t, err)
		require.True(t, found)
	})
	t.Run("deleting an owner on the default quota changes nothing", func(t *testing.T) {
		require.NoError(t, newStore(t).DeleteCustomUserQuota(context.Background(), uuid.New()))
	})
}
