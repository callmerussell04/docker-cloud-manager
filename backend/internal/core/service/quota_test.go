package service_test

import (
	"context"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/userquota"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// testQuotas returns Core's effective-quota source over a quota store fake
// and cfg, or testConfig() when cfg is nil. Each entry of custom becomes that
// owner's custom quota; every other owner has cfg's default quota.
func testQuotas(t *testing.T, cfg service.ConfigManager, custom map[uuid.UUID]model.UserQuota) *userquota.Service {
	t.Helper()
	if cfg == nil {
		cfg = testConfig()
	}
	store := coretest.NewUserQuotaStoreFake()
	for ownerID, quota := range custom {
		require.NoError(t, store.SetCustomUserQuota(context.Background(), ownerID, quota))
	}
	quotas, err := userquota.New(store, cfg)
	require.NoError(t, err)
	return quotas
}
