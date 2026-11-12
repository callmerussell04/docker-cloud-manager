package userquota_test

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/userquota"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewRejectsAMissingStoreOrConfig(t *testing.T) {
	cfg := coretest.StaticConfig{Config: coretest.SystemConfig()}
	for _, tc := range []struct {
		name  string
		store userquota.Store
		cfg   userquota.ConfigProvider
	}{
		{name: "nil store", cfg: cfg},
		{name: "typed nil store", store: (*coretest.UserQuotaStoreFake)(nil), cfg: cfg},
		{name: "nil config", store: coretest.NewUserQuotaStoreFake()},
		{name: "typed nil config", store: coretest.NewUserQuotaStoreFake(), cfg: (*coretest.StaticConfig)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := userquota.New(tc.store, tc.cfg)
			require.Error(t, err)
		})
	}
}

// A custom quota wins over the defaults; any other owner gets the defaults,
// with the CPU default rounded to whole millicores.
func TestEffectiveUserQuota(t *testing.T) {
	ctx := context.Background()
	store := coretest.NewUserQuotaStoreFake()
	customID := uuid.New()
	require.NoError(t, store.SetCustomUserQuota(ctx, customID, model.UserQuota{CPUMillicores: 250, RAMMB: 512, DiskMB: 1024}))
	cfg := coretest.SystemConfig()
	cfg.DefaultUserQuotaCPU, cfg.DefaultUserQuotaRAMMB, cfg.DefaultUserQuotaDiskMB = 0.29, 2048, 5120
	quotas, err := userquota.New(store, coretest.StaticConfig{Config: cfg})
	require.NoError(t, err)

	custom, err := quotas.EffectiveUserQuota(ctx, customID)
	require.NoError(t, err)
	require.Equal(t, model.UserQuota{CPUMillicores: 250, RAMMB: 512, DiskMB: 1024, Custom: true}, custom)

	byDefault, err := quotas.EffectiveUserQuota(ctx, uuid.New())
	require.NoError(t, err)
	require.Equal(t, model.UserQuota{CPUMillicores: 290, RAMMB: 2048, DiskMB: 5120}, byDefault)
}

func TestEffectiveUserQuotaReportsAStoreFailure(t *testing.T) {
	failure := errors.New("quota store unavailable")
	store := coretest.NewUserQuotaStoreFake()
	store.SetFaults(coretest.UserQuotaStoreFaults{GetCustomUserQuota: coretest.FailAlways(failure)})
	quotas, err := userquota.New(store, coretest.StaticConfig{Config: coretest.SystemConfig()})
	require.NoError(t, err)

	_, err = quotas.EffectiveUserQuota(context.Background(), uuid.New())

	require.ErrorIs(t, err, failure)
}
