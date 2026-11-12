package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Build creation holds the capacity lock until its queued build is durable,
// and rejects work over the per-owner build queue limit. Host and quota
// admission is tested with the other entry points in admission_test.go.
func TestBuildServiceAdmission(t *testing.T) {
	failure := errors.New("admission storage unavailable")
	scenarios := []struct {
		name                                              string
		queued                                            int
		lockErr, countErr, activeErr, reserveErr, wantErr error
		queueDisabled                                     bool
	}{
		{name: "reservation succeeds"},
		{name: "reservation fails", reserveErr: failure, wantErr: failure},
		{name: "lock fails", lockErr: failure, wantErr: failure},
		{name: "queue full", queued: 2, wantErr: apperrors.ErrLimitExceeded},
		{name: "queue counter fails", countErr: failure, wantErr: failure},
		{name: "active counter fails", activeErr: failure, wantErr: failure},
		{name: "queue limit disabled", queued: 100, queueDisabled: true},
	}
	for _, project := range []bool{false, true} {
		name := "user"
		if project {
			name = "project"
		}
		t.Run(name, func(t *testing.T) {
			for _, scenario := range scenarios {
				t.Run(scenario.name, func(t *testing.T) {
					deps := buildServiceDeps(t)
					cfg := coretest.IntegrationConfig()
					cfg.MaxQueuedBuildsPerUser = 2
					if scenario.queueDisabled {
						cfg.MaxQueuedBuildsPerUser = 0
					}
					deps.Config = coretest.StaticConfig{Config: cfg}
					owner := uuid.New()
					deps.Quotas = testQuotas(t, deps.Config, map[uuid.UUID]model.UserQuota{owner: {CPUMillicores: 1000, RAMMB: 1024, DiskMB: 1024}})
					images := coremocks.NewBuildImageRepository(t)
					images.EXPECT().GetUserUsedDiskSpace(mock.Anything, owner).Return(int64(0), nil).Maybe()
					deps.Images = images
					repo := coremocks.NewBuildRepository(t)
					deps.Repository = repo
					locked := false
					repo.EXPECT().AcquireCapacityLock(mock.Anything).Return(func() { locked = false }, scenario.lockErr).
						Run(func(context.Context) { locked = scenario.lockErr == nil }).Once()
					repo.EXPECT().CountActiveByOwner(mock.Anything, owner).Return(scenario.queued, scenario.countErr).Maybe()
					repo.EXPECT().CountActive(mock.Anything).Return(0, scenario.activeErr).Maybe()
					reserved := false
					repo.EXPECT().CreateQueuedBuild(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
						Run(func(_ context.Context, _ model.Image, build model.Build, _ model.BuildQueueOutbox) {
							reserved = true
							require.True(t, locked, "reservation must be durable before releasing capacity")
							require.Equal(t, owner, build.OwnerID)
							require.Equal(t, project, build.ProjectID != nil)
							require.Equal(t, model.BuildStatusPending, build.Status)
						}).Return(scenario.reserveErr).Maybe()
					svc, err := service.NewBuildService(deps)
					require.NoError(t, err)
					ctx := accessscope.WithUserScope(context.Background(), owner, "", "")
					var buildID uuid.UUID
					if project {
						buildID, _, err = svc.CreateProjectBuildJob(
							ctx, uuid.New(), "api", "demo", "archive.zip", "build.log",
							".", "Dockerfile", nil, "request",
						)
					} else {
						buildID, _, err = svc.CreateBuildJob(
							ctx, "demo", "archive.zip", "build.log", ".", "Dockerfile", nil, "request",
						)
					}
					require.ErrorIs(t, err, scenario.wantErr)
					if scenario.wantErr == nil {
						require.NotEqual(t, uuid.Nil, buildID)
					} else {
						require.Equal(t, uuid.Nil, buildID)
					}
					require.Equal(t, scenario.wantErr == nil || scenario.reserveErr != nil, reserved, "only admitted builds are reserved")
					require.False(t, locked, "capacity lock must be released on every return")
				})
			}
		})
	}
}

func TestBuildServiceReserveBuildArchiveEnforcesStagedQuota(t *testing.T) {
	const archiveKey = "build-archives/copied.zip"
	const archiveSize = int64(5 << 20)
	stagedFull := apperrors.New(apperrors.ErrQuotaExceeded, "staged source quota exceeded")
	statFailure := errors.New("object storage unavailable")
	scenarios := []struct {
		name        string
		quotaDiskMB int64
		statErr     error
		reserveErr  error
		wantErr     error
		wantMessage string
	}{
		{name: "reserves the stat size", quotaDiskMB: 1024},
		{name: "staged quota exceeded", quotaDiskMB: 1024, reserveErr: stagedFull, wantErr: apperrors.ErrQuotaExceeded, wantMessage: "staged source quota exceeded"},
		{name: "disk quota exceeded", quotaDiskMB: 1, wantErr: apperrors.ErrQuotaExceeded, wantMessage: "user disk quota exceeded"},
		{name: "stat fails", statErr: statFailure, wantErr: statFailure},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			owner := uuid.New()
			cfg := coretest.IntegrationConfig()
			cfg.MaxStagedSourceBytesPerUser = 64 << 20
			objects := coremocks.NewBuildObjectStore(t)
			objects.EXPECT().StatObject(mock.Anything, archiveKey).
				Return(objectstorage.ObjectInfo{Size: archiveSize}, scenario.statErr).Once()
			images := coremocks.NewBuildImageRepository(t)
			staged := coremocks.NewStagedObjectRepository(t)
			if scenario.statErr == nil {
				images.EXPECT().GetUserUsedDiskSpace(mock.Anything, owner).Return(int64(0), nil).Once()
				staged.EXPECT().ActiveBytesByOwner(mock.Anything, owner).Return(int64(0), nil).Once()
			}
			if scenario.statErr == nil && scenario.quotaDiskMB > 1 {
				staged.EXPECT().Reserve(mock.Anything, mock.Anything, cfg.MaxStagedSourceBytesPerUser).
					Run(func(_ context.Context, reservation model.StagedObjectReservation, _ int64) {
						require.Equal(t, owner, reservation.OwnerID)
						require.Equal(t, archiveKey, reservation.ObjectKey)
						require.Equal(t, model.StagedObjectKindBuildArchive, reservation.Kind)
						require.Equal(t, archiveSize, reservation.BytesReserved)
					}).Return(scenario.reserveErr).Once()
			}
			svc := newTestBuildService(t, service.BuildServiceDeps{
				Repository:    newBuildRepository(t),
				Images:        images,
				Registry:      coremocks.NewImageRegistryAPI(t),
				Quotas:        testQuotas(t, coretest.StaticConfig{Config: cfg}, map[uuid.UUID]model.UserQuota{owner: {CPUMillicores: 1000, RAMMB: 1024, DiskMB: scenario.quotaDiskMB}}),
				Config:        coretest.StaticConfig{Config: cfg},
				ObjectStore:   objects,
				StagedObjects: staged,
			})
			ctx := accessscope.WithUserScope(context.Background(), owner, "", "")

			err := svc.ReserveBuildArchive(ctx, archiveKey)
			if scenario.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, scenario.wantErr)
			if scenario.wantMessage != "" {
				require.Equal(t, scenario.wantMessage, apperrors.SafeMessage(err))
			}
		})
	}
}
