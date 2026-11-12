package stagedsource_test

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/stagedsource"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	stagedsourcemocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service/stagedsource"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const mb = int64(1 << 20)

// TestReserveAppliesTheChosenQuotaRule pins the two staged disk quota rules: a
// build counts the owner's reserved disk plus staged bytes against the disk
// quota, and a Compose source counts staged bytes only.
func TestReserveAppliesTheChosenQuotaRule(t *testing.T) {
	scenarios := []struct {
		name          string
		rule          stagedsource.QuotaRule
		reservedMB    int64
		stagedBytes   int64
		incomingBytes int64
		wantRejected  bool
	}{
		{name: "build within quota", rule: stagedsource.CountReservedAndStaged, reservedMB: 800, stagedBytes: 100 * mb, incomingBytes: 100 * mb},
		{name: "build rejected by reserved plus staged", rule: stagedsource.CountReservedAndStaged, reservedMB: 900, stagedBytes: 100 * mb, incomingBytes: 100 * mb, wantRejected: true},
		{name: "build rounds staged bytes up to whole MB", rule: stagedsource.CountReservedAndStaged, reservedMB: 1023, incomingBytes: mb + 1, wantRejected: true},
		{name: "compose ignores reserved disk", rule: stagedsource.CountStagedOnly, reservedMB: 900, stagedBytes: 100 * mb, incomingBytes: 100 * mb},
		{name: "compose rejected by staged bytes", rule: stagedsource.CountStagedOnly, stagedBytes: 1000 * mb, incomingBytes: 100 * mb, wantRejected: true},
		{name: "compose rounds staged bytes up to whole MB", rule: stagedsource.CountStagedOnly, stagedBytes: 1024 * mb, incomingBytes: 1, wantRejected: true},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			owner := uuid.New()
			cfg := coretest.IntegrationConfig()
			cfg.MaxStagedSourceBytesPerUser = 4096 * mb
			repo := stagedsourcemocks.NewRepository(t)
			repo.EXPECT().ActiveBytesByOwner(mock.Anything, owner).Return(scenario.stagedBytes, nil).Once()
			users := stagedsourcemocks.NewUserInfoProvider(t)
			users.EXPECT().GetUser(mock.Anything, owner).Return(model.UserInfo{ID: owner, QuotaDiskMB: 1024}, nil).Once()
			reserved := stagedsourcemocks.NewReservedDiskReader(t)
			if scenario.rule == stagedsource.CountReservedAndStaged {
				reserved.EXPECT().ReservedDiskMB(mock.Anything, owner).Return(scenario.reservedMB, nil).Once()
			}
			if !scenario.wantRejected {
				repo.EXPECT().Reserve(mock.Anything, mock.Anything, cfg.MaxStagedSourceBytesPerUser).
					Run(func(_ context.Context, reservation model.StagedObjectReservation, _ int64) {
						require.Equal(t, owner, reservation.OwnerID)
						require.Equal(t, "staged/source.zip", reservation.ObjectKey)
						require.Equal(t, model.StagedObjectKindBuildArchive, reservation.Kind)
						require.Equal(t, scenario.incomingBytes, reservation.BytesReserved)
						require.NotEqual(t, uuid.Nil, reservation.ID)
					}).Return(nil).Once()
			}
			staging := stagedsource.New(stagedsource.Deps{
				Repository:   repo,
				Users:        users,
				ReservedDisk: reserved,
				Config:       coretest.StaticConfig{Config: cfg},
			})

			err := staging.Reserve(context.Background(), stagedsource.Reservation{
				OwnerID:   owner,
				ObjectKey: "staged/source.zip",
				Kind:      model.StagedObjectKindBuildArchive,
				Bytes:     scenario.incomingBytes,
				Rule:      scenario.rule,
			})
			if !scenario.wantRejected {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, apperrors.ErrQuotaExceeded)
			require.Equal(t, "user disk quota exceeded", apperrors.SafeMessage(err))
		})
	}
}

func TestReserveWithoutRepositoryOrKeyReservesNothing(t *testing.T) {
	cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
	reservation := stagedsource.Reservation{OwnerID: uuid.New(), ObjectKey: "staged/source.zip", Bytes: mb, Rule: stagedsource.CountStagedOnly}

	require.NoError(t, stagedsource.New(stagedsource.Deps{Config: cfg}).Reserve(context.Background(), reservation))

	reservation.ObjectKey = ""
	staging := stagedsource.New(stagedsource.Deps{Repository: stagedsourcemocks.NewRepository(t), Config: cfg})
	require.NoError(t, staging.Reserve(context.Background(), reservation))
}

func TestReserveWithoutUserDirectoryCountsNoQuota(t *testing.T) {
	owner := uuid.New()
	repo := stagedsourcemocks.NewRepository(t)
	repo.EXPECT().ActiveBytesByOwner(mock.Anything, owner).Return(1<<40, nil).Once()
	repo.EXPECT().Reserve(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	staging := stagedsource.New(stagedsource.Deps{Repository: repo, Config: coretest.StaticConfig{Config: coretest.IntegrationConfig()}})

	err := staging.Reserve(context.Background(), stagedsource.Reservation{OwnerID: owner, ObjectKey: "staged/source.zip", Bytes: 1 << 40, Rule: stagedsource.CountStagedOnly})

	require.NoError(t, err)
}

func TestReserveClampsNegativeBytesToZero(t *testing.T) {
	owner := uuid.New()
	repo := stagedsourcemocks.NewRepository(t)
	repo.EXPECT().ActiveBytesByOwner(mock.Anything, owner).Return(int64(0), nil).Once()
	repo.EXPECT().Reserve(mock.Anything, mock.MatchedBy(func(r model.StagedObjectReservation) bool { return r.BytesReserved == 0 }), mock.Anything).Return(nil).Once()
	staging := stagedsource.New(stagedsource.Deps{Repository: repo, Config: coretest.StaticConfig{Config: coretest.IntegrationConfig()}})

	err := staging.Reserve(context.Background(), stagedsource.Reservation{OwnerID: owner, ObjectKey: "staged/source.zip", Bytes: -5, Rule: stagedsource.CountStagedOnly})

	require.NoError(t, err)
}

func TestReserveRejectsAnUnchosenRule(t *testing.T) {
	staging := stagedsource.New(stagedsource.Deps{Repository: stagedsourcemocks.NewRepository(t), Config: coretest.StaticConfig{Config: coretest.IntegrationConfig()}})

	err := staging.Reserve(context.Background(), stagedsource.Reservation{OwnerID: uuid.New(), ObjectKey: "staged/source.zip", Bytes: mb})

	require.Error(t, err)
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
			staging := stagedsource.New(stagedsource.Deps{Repository: repo})

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
	off := stagedsource.New(stagedsource.Deps{})
	require.NoError(t, off.Resize(context.Background(), "staged/source.zip", 42))
	require.NoError(t, off.Release(context.Background(), "staged/source.zip"))

	staging := stagedsource.New(stagedsource.Deps{Repository: stagedsourcemocks.NewRepository(t)})
	require.NoError(t, staging.Resize(context.Background(), "", 42))
	require.NoError(t, staging.Release(context.Background(), ""))
}
