package userquota_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/userquota"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
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

func adminContext() context.Context {
	return accessscope.WithAdminScope(context.Background(), uuid.New(), "root", "admin")
}

func newQuotas(t *testing.T) (*userquota.Service, *coretest.UserQuotaStoreFake) {
	t.Helper()
	store := coretest.NewUserQuotaStoreFake()
	cfg := coretest.SystemConfig()
	cfg.DefaultUserQuotaCPU, cfg.DefaultUserQuotaRAMMB, cfg.DefaultUserQuotaDiskMB = 1, 2048, 5120
	quotas, err := userquota.New(store, coretest.StaticConfig{Config: cfg})
	require.NoError(t, err)
	return quotas, store
}

// An administrator sets a custom quota, reads it back marked custom beside
// a user on the default quota, and resets it to the default.
func TestAdministratorSetsReadsAndResetsAQuota(t *testing.T) {
	ctx := adminContext()
	quotas, _ := newQuotas(t)
	customID, defaultID := uuid.New(), uuid.New()

	set, err := quotas.SetUserQuota(ctx, customID, model.UserQuotaLimits{CPUCores: 2.5, RAMMB: 4096, DiskMB: 10240})
	require.NoError(t, err)
	require.Equal(t, model.UserQuota{CPUMillicores: 2500, RAMMB: 4096, DiskMB: 10240, Custom: true}, set)

	got, err := quotas.UserQuotas(ctx, []uuid.UUID{customID, defaultID})
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]model.UserQuota{
		customID:  {CPUMillicores: 2500, RAMMB: 4096, DiskMB: 10240, Custom: true},
		defaultID: {CPUMillicores: 1000, RAMMB: 2048, DiskMB: 5120},
	}, got)

	reset, err := quotas.ResetUserQuota(ctx, customID)
	require.NoError(t, err)
	require.Equal(t, model.UserQuota{CPUMillicores: 1000, RAMMB: 2048, DiskMB: 5120}, reset)
	effective, err := quotas.EffectiveUserQuota(context.Background(), customID)
	require.NoError(t, err)
	require.Equal(t, reset, effective)
}

func TestQuotaAdministrationRequiresAdminScope(t *testing.T) {
	quotas, store := newQuotas(t)
	ownerID := uuid.New()
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want error
	}{
		{name: "no scope", ctx: context.Background(), want: apperrors.ErrUnauthorized},
		{name: "user scope", ctx: accessscope.WithUserScope(context.Background(), ownerID, "alice", "user"), want: apperrors.ErrForbidden},
		{name: "system scope", ctx: accessscope.WithSystemScope(context.Background()), want: apperrors.ErrForbidden},
		{name: "admin scope without an actor", ctx: accessscope.WithAdminScope(context.Background(), uuid.Nil, "", "admin"), want: apperrors.ErrForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := quotas.UserQuotas(tc.ctx, []uuid.UUID{ownerID})
			require.ErrorIs(t, err, tc.want)
			_, err = quotas.SetUserQuota(tc.ctx, ownerID, model.UserQuotaLimits{CPUCores: 1, RAMMB: 1, DiskMB: 1})
			require.ErrorIs(t, err, tc.want)
			_, err = quotas.ResetUserQuota(tc.ctx, ownerID)
			require.ErrorIs(t, err, tc.want)
		})
	}
	_, found, err := store.GetCustomUserQuota(context.Background(), ownerID)
	require.NoError(t, err)
	require.False(t, found, "a refused set must store nothing")
}

// A rejected value is a bad request whose message never repeats it.
func TestSetUserQuotaRejectsValuesOutOfRangeWithoutEchoingThem(t *testing.T) {
	quotas, store := newQuotas(t)
	ownerID := uuid.New()
	for _, tc := range []struct {
		name   string
		limits model.UserQuotaLimits
		echo   string
	}{
		{name: "zero cpu", limits: model.UserQuotaLimits{CPUCores: 0, RAMMB: 1024, DiskMB: 1024}},
		{name: "negative cpu", limits: model.UserQuotaLimits{CPUCores: -7.25, RAMMB: 1024, DiskMB: 1024}, echo: "7.25"},
		{name: "cpu below a millicore", limits: model.UserQuotaLimits{CPUCores: 0.0004, RAMMB: 1024, DiskMB: 1024}, echo: "0.0004"},
		{name: "cpu too large", limits: model.UserQuotaLimits{CPUCores: 2_000_000, RAMMB: 1024, DiskMB: 1024}, echo: "2000000"},
		{name: "cpu not a number", limits: model.UserQuotaLimits{CPUCores: math.NaN(), RAMMB: 1024, DiskMB: 1024}, echo: "NaN"},
		{name: "cpu infinite", limits: model.UserQuotaLimits{CPUCores: math.Inf(1), RAMMB: 1024, DiskMB: 1024}, echo: "Inf"},
		{name: "zero memory", limits: model.UserQuotaLimits{CPUCores: 1, RAMMB: 0, DiskMB: 1024}},
		{name: "negative memory", limits: model.UserQuotaLimits{CPUCores: 1, RAMMB: -4242, DiskMB: 1024}, echo: "4242"},
		{name: "negative disk", limits: model.UserQuotaLimits{CPUCores: 1, RAMMB: 1024, DiskMB: -9191}, echo: "9191"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := quotas.SetUserQuota(adminContext(), ownerID, tc.limits)

			require.ErrorIs(t, err, apperrors.ErrBadRequest)
			if tc.echo != "" {
				require.NotContains(t, apperrors.SafeMessage(err), tc.echo)
			}
		})
	}
	_, found, err := store.GetCustomUserQuota(context.Background(), ownerID)
	require.NoError(t, err)
	require.False(t, found)
}

func TestUserQuotasRejectsTooManyOwners(t *testing.T) {
	quotas, _ := newQuotas(t)
	ownerIDs := make([]uuid.UUID, userquota.MaxOwnersPerRead+1)
	for i := range ownerIDs {
		ownerIDs[i] = uuid.New()
	}

	_, err := quotas.UserQuotas(adminContext(), ownerIDs)

	require.ErrorIs(t, err, apperrors.ErrBadRequest)
}

func TestQuotaAdministrationReportsAStoreFailure(t *testing.T) {
	failure := errors.New("quota store unavailable")
	quotas, store := newQuotas(t)
	store.SetFaults(coretest.UserQuotaStoreFaults{
		GetCustomUserQuotas:   coretest.FailAlways(failure),
		SetCustomUserQuota:    coretest.FailAlways(failure),
		DeleteCustomUserQuota: coretest.FailAlways(failure),
	})
	ownerID := uuid.New()

	_, err := quotas.UserQuotas(adminContext(), []uuid.UUID{ownerID})
	require.ErrorIs(t, err, failure)
	_, err = quotas.SetUserQuota(adminContext(), ownerID, model.UserQuotaLimits{CPUCores: 1, RAMMB: 1, DiskMB: 1})
	require.ErrorIs(t, err, failure)
	_, err = quotas.ResetUserQuota(adminContext(), ownerID)
	require.ErrorIs(t, err, failure)
}
