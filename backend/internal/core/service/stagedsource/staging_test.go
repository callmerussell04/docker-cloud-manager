package stagedsource_test

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/stagedsource"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/userquota"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	stagedsourcemocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service/stagedsource"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const mb = int64(1 << 20)

// TestReserveAppliesTheOneStagedDiskRule pins the one staged disk rule both
// callers stage through: the owner's reserved disk plus staged bytes plus the
// incoming bytes must fit the disk quota (user_disk), then staged bytes plus
// the incoming bytes must fit the staged bytes cap (staged_source_bytes), in
// that order. The disk quota is the owner's effective quota in Core: the
// 1024 MB default, or a custom quota.
func TestReserveAppliesTheOneStagedDiskRule(t *testing.T) {
	scenarios := []struct {
		name          string
		reservedMB    int64
		stagedBytes   int64
		incomingBytes int64
		// customDiskMB, when set, is the owner's custom disk quota.
		customDiskMB int64
		wantLimit    apperrors.Limit
	}{
		{name: "within both limits", reservedMB: 800, stagedBytes: 100 * mb, incomingBytes: 100 * mb},
		{name: "reserved plus staged plus incoming over the quota", reservedMB: 900, stagedBytes: 100 * mb, incomingBytes: 100 * mb, wantLimit: apperrors.LimitUserDisk},
		{name: "one byte over the quota", reservedMB: 1023, incomingBytes: mb + 1, wantLimit: apperrors.LimitUserDisk},
		{name: "reserved disk fills the quota", reservedMB: 1024, incomingBytes: 1, wantLimit: apperrors.LimitUserDisk},
		{name: "within a custom quota above the default", reservedMB: 900, stagedBytes: 100 * mb, incomingBytes: 100 * mb, customDiskMB: 2048},
		{name: "over a custom quota below the default", stagedBytes: 400 * mb, incomingBytes: 200 * mb, customDiskMB: 512, wantLimit: apperrors.LimitUserDisk},
		{name: "staged bytes over the staged cap", customDiskMB: 4096, stagedBytes: 250 * mb, incomingBytes: 7 * mb, wantLimit: apperrors.LimitStagedSourceBytes},
		{name: "staged bytes exactly at the staged cap", customDiskMB: 4096, stagedBytes: 250 * mb, incomingBytes: 6 * mb},
		{name: "the disk quota is checked first", reservedMB: 1000, stagedBytes: 250 * mb, incomingBytes: 50 * mb, wantLimit: apperrors.LimitUserDisk},
	}
	kinds := []string{model.StagedObjectKindBuildArchive, model.StagedObjectKindComposeSource}
	for _, kind := range kinds {
		for _, scenario := range scenarios {
			t.Run(kind+"/"+scenario.name, func(t *testing.T) {
				owner := uuid.New()
				cfg := coretest.IntegrationConfig()
				cfg.MaxStagedSourceBytesPerUser = 256 * mb
				cfg.DefaultUserQuotaDiskMB = 1024
				repo := stagedsourcemocks.NewRepository(t)
				repo.EXPECT().ActiveBytesByOwner(mock.Anything, owner).Return(scenario.stagedBytes, nil).Once()
				store := coretest.NewUserQuotaStoreFake()
				if scenario.customDiskMB > 0 {
					require.NoError(t, store.SetCustomUserQuota(context.Background(), owner, model.UserQuota{CPUMillicores: 1000, RAMMB: 1024, DiskMB: scenario.customDiskMB}))
				}
				quotas, err := userquota.New(store, coretest.StaticConfig{Config: cfg})
				require.NoError(t, err)
				reserved := stagedsourcemocks.NewReservedDiskReader(t)
				reserved.EXPECT().ReservedDiskBytes(mock.Anything, owner).Return(scenario.reservedMB*mb, nil).Once()
				if scenario.wantLimit == "" {
					repo.EXPECT().Reserve(mock.Anything, mock.Anything).
						Run(func(_ context.Context, reservation model.StagedObjectReservation) {
							require.Equal(t, owner, reservation.OwnerID)
							require.Equal(t, "staged/source.zip", reservation.ObjectKey)
							require.Equal(t, kind, reservation.Kind)
							require.Equal(t, scenario.incomingBytes, reservation.BytesReserved)
							require.NotEqual(t, uuid.Nil, reservation.ID)
						}).Return(nil).Once()
				}
				staging := newStaging(t, stagedsource.Deps{Repository: repo, Quotas: quotas, ReservedDisk: reserved, Config: coretest.StaticConfig{Config: cfg}})

				err = staging.Reserve(context.Background(), stagedsource.Reservation{
					OwnerID:   owner,
					ObjectKey: "staged/source.zip",
					Kind:      kind,
					Bytes:     scenario.incomingBytes,
				})
				if scenario.wantLimit == "" {
					require.NoError(t, err)
					return
				}
				require.ErrorIs(t, err, apperrors.ErrQuotaExceeded)
				require.Equal(t, scenario.wantLimit, apperrors.LimitOf(err))
			})
		}
	}
}

// TestReserveHoldsTheOwnerCapacityLock shows the check and the insert run
// under the owner capacity lock, released only after the insert, and that a
// lock that cannot be taken stops the reservation before anything is read.
func TestReserveHoldsTheOwnerCapacityLock(t *testing.T) {
	owner := uuid.New()
	var events []string
	lock := stagedsourcemocks.NewCapacityLock(t)
	lock.EXPECT().AcquireOwnerCapacityLock(mock.Anything, owner).RunAndReturn(func(context.Context, uuid.UUID) (func(), error) {
		events = append(events, "lock")
		return func() { events = append(events, "unlock") }, nil
	}).Once()
	reserved := stagedsourcemocks.NewReservedDiskReader(t)
	reserved.EXPECT().ReservedDiskBytes(mock.Anything, owner).RunAndReturn(func(context.Context, uuid.UUID) (int64, error) {
		events = append(events, "read reserved disk")
		return 0, nil
	}).Once()
	repo := stagedsourcemocks.NewRepository(t)
	repo.EXPECT().ActiveBytesByOwner(mock.Anything, owner).RunAndReturn(func(context.Context, uuid.UUID) (int64, error) {
		events = append(events, "read staged bytes")
		return 0, nil
	}).Once()
	repo.EXPECT().Reserve(mock.Anything, mock.Anything).RunAndReturn(func(context.Context, model.StagedObjectReservation) error {
		events = append(events, "insert")
		return nil
	}).Once()
	staging := newStaging(t, stagedsource.Deps{Repository: repo, CapacityLock: lock, ReservedDisk: reserved})

	err := staging.Reserve(context.Background(), stagedsource.Reservation{OwnerID: owner, ObjectKey: "staged/source.zip", Bytes: mb})

	require.NoError(t, err)
	require.Equal(t, []string{"lock", "read reserved disk", "read staged bytes", "insert", "unlock"}, events)

	lockFailure := errors.New("lock pool unavailable")
	failing := stagedsourcemocks.NewCapacityLock(t)
	failing.EXPECT().AcquireOwnerCapacityLock(mock.Anything, owner).Return(nil, lockFailure).Once()
	staging = newStaging(t, stagedsource.Deps{Repository: stagedsourcemocks.NewRepository(t), CapacityLock: failing, ReservedDisk: stagedsourcemocks.NewReservedDiskReader(t)})

	err = staging.Reserve(context.Background(), stagedsource.Reservation{OwnerID: owner, ObjectKey: "staged/source.zip", Bytes: mb})

	require.ErrorIs(t, err, lockFailure)
}

// TestNewRequiresTheLockWhenStaging pins the construction contract: staging
// with a repository cannot be built without its capacity lock, reserved disk
// reader or config, typed nils included; staging that is off needs none.
func TestNewRequiresTheLockWhenStaging(t *testing.T) {
	valid := func(t *testing.T) stagedsource.Deps {
		return stagedsource.Deps{
			Repository:   stagedsourcemocks.NewRepository(t),
			CapacityLock: stagedsourcemocks.NewCapacityLock(t),
			ReservedDisk: stagedsourcemocks.NewReservedDiskReader(t),
			Config:       coretest.StaticConfig{Config: coretest.IntegrationConfig()},
		}
	}
	depstest.RequireRejectsMissing(t, valid, stagedsource.New, map[string]string{
		"CapacityLock": "capacity lock",
		"ReservedDisk": "reserved disk reader",
		"Config":       "config",
	})

	staging, err := stagedsource.New(valid(t))
	require.NoError(t, err)
	require.NotNil(t, staging)
	off, err := stagedsource.New(stagedsource.Deps{})
	require.NoError(t, err)
	require.NotNil(t, off)
}

func TestReserveWithoutRepositoryOrKeyReservesNothing(t *testing.T) {
	cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
	reservation := stagedsource.Reservation{OwnerID: uuid.New(), ObjectKey: "staged/source.zip", Bytes: mb}

	require.NoError(t, newStaging(t, stagedsource.Deps{Config: cfg}).Reserve(context.Background(), reservation))

	reservation.ObjectKey = ""
	staging := newStaging(t, stagedsource.Deps{Repository: stagedsourcemocks.NewRepository(t), CapacityLock: stagedsourcemocks.NewCapacityLock(t), ReservedDisk: stagedsourcemocks.NewReservedDiskReader(t), Config: cfg})
	require.NoError(t, staging.Reserve(context.Background(), reservation))
}

func TestReserveWithoutQuotaProviderCountsNoQuota(t *testing.T) {
	owner := uuid.New()
	cfg := coretest.IntegrationConfig()
	cfg.MaxStagedSourceBytesPerUser = 0
	repo := stagedsourcemocks.NewRepository(t)
	repo.EXPECT().ActiveBytesByOwner(mock.Anything, owner).Return(1<<40, nil).Once()
	repo.EXPECT().Reserve(mock.Anything, mock.Anything).Return(nil).Once()
	reserved := stagedsourcemocks.NewReservedDiskReader(t)
	reserved.EXPECT().ReservedDiskBytes(mock.Anything, owner).Return(1<<40, nil).Once()
	staging := newStaging(t, stagedsource.Deps{Repository: repo, ReservedDisk: reserved, Config: coretest.StaticConfig{Config: cfg}})

	err := staging.Reserve(context.Background(), stagedsource.Reservation{OwnerID: owner, ObjectKey: "staged/source.zip", Bytes: 1 << 40})

	require.NoError(t, err)
}

func TestReserveClampsNegativeBytesToZero(t *testing.T) {
	owner := uuid.New()
	repo := stagedsourcemocks.NewRepository(t)
	repo.EXPECT().ActiveBytesByOwner(mock.Anything, owner).Return(int64(0), nil).Once()
	repo.EXPECT().Reserve(mock.Anything, mock.MatchedBy(func(r model.StagedObjectReservation) bool { return r.BytesReserved == 0 })).Return(nil).Once()
	reserved := stagedsourcemocks.NewReservedDiskReader(t)
	reserved.EXPECT().ReservedDiskBytes(mock.Anything, owner).Return(int64(0), nil).Once()
	staging := newStaging(t, stagedsource.Deps{Repository: repo, ReservedDisk: reserved})

	err := staging.Reserve(context.Background(), stagedsource.Reservation{OwnerID: owner, ObjectKey: "staged/source.zip", Bytes: -5})

	require.NoError(t, err)
}

// newStaging builds staging for a test. With a repository it fills a missing
// config with the integration config and a missing capacity lock with a lock
// that is always free.
func newStaging(t *testing.T, deps stagedsource.Deps) *stagedsource.Staging {
	t.Helper()
	if deps.Repository != nil {
		if deps.Config == nil {
			deps.Config = coretest.StaticConfig{Config: coretest.IntegrationConfig()}
		}
		if deps.CapacityLock == nil {
			deps.CapacityLock = coretest.NewContainerStoreFake()
		}
	}
	staging, err := stagedsource.New(deps)
	require.NoError(t, err)
	return staging
}

func TestResizeAndReleaseIgnoreAMissingReservation(t *testing.T) {
	storeFailure := errors.New("staged store unavailable")
	for name, scenario := range map[string]struct {
		repoErr error
		wantErr error
	}{
		"done":                {},
		"reservation missing": {repoErr: apperrors.ErrNotFound},
		"store fails":         {repoErr: storeFailure, wantErr: storeFailure},
	} {
		t.Run(name, func(t *testing.T) {
			repo := stagedsourcemocks.NewRepository(t)
			repo.EXPECT().UpdateBytes(mock.Anything, "staged/source.zip", int64(42)).Return(scenario.repoErr).Once()
			repo.EXPECT().Release(mock.Anything, "staged/source.zip").Return(scenario.repoErr).Once()
			staging := newStaging(t, stagedsource.Deps{Repository: repo, ReservedDisk: stagedsourcemocks.NewReservedDiskReader(t)})

			resizeErr := staging.Resize(context.Background(), "staged/source.zip", 42)
			releaseErr := staging.Release(context.Background(), "staged/source.zip")

			if scenario.wantErr == nil {
				require.NoError(t, resizeErr)
				require.NoError(t, releaseErr)
				return
			}
			require.ErrorIs(t, resizeErr, scenario.wantErr)
			require.ErrorIs(t, releaseErr, scenario.wantErr)
		})
	}
}

func TestResizeAndReleaseWithoutRepositoryOrKeyDoNothing(t *testing.T) {
	off := newStaging(t, stagedsource.Deps{})
	require.NoError(t, off.Resize(context.Background(), "staged/source.zip", 42))
	require.NoError(t, off.Release(context.Background(), "staged/source.zip"))

	staging := newStaging(t, stagedsource.Deps{Repository: stagedsourcemocks.NewRepository(t), ReservedDisk: stagedsourcemocks.NewReservedDiskReader(t)})
	require.NoError(t, staging.Resize(context.Background(), "", 42))
	require.NoError(t, staging.Release(context.Background(), ""))
}
