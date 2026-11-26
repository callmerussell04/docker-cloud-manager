package coretest_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUserQuotaStoreFakeContract(t *testing.T) {
	coretest.RunUserQuotaStoreContract(t, func(*testing.T) coretest.UserQuotaStore {
		return coretest.NewUserQuotaStoreFake()
	})
}

func TestUserQuotaStoreFakeFaultFailsReadsWithoutLosingTheQuota(t *testing.T) {
	ctx := context.Background()
	store := coretest.NewUserQuotaStoreFake()
	ownerID := uuid.New()
	quota := model.UserQuota{CPUMillicores: 500, RAMMB: 1024, DiskMB: 2048}
	require.NoError(t, store.SetCustomUserQuota(ctx, ownerID, quota))

	store.SetFaults(coretest.UserQuotaStoreFaults{GetCustomUserQuota: coretest.FailNext(1, errInjected)})
	_, _, err := store.GetCustomUserQuota(ctx, ownerID)
	require.ErrorIs(t, err, errInjected)

	got, found, err := store.GetCustomUserQuota(ctx, ownerID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(1024), got.RAMMB)
}
