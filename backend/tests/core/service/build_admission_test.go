package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/tests/mocks/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/tests/testutil/coretest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBuildServiceAdmission(t *testing.T) {
	failure := errors.New("admission storage unavailable")
	scenarios := []struct {
		name                                              string
		queued, active                                    int
		memory, cpu                                       int64
		lockErr, countErr, activeErr, reserveErr, wantErr error
		queueDisabled                                     bool
	}{
		{name: "reservation succeeds"},
		{name: "reservation fails", reserveErr: failure, wantErr: failure},
		{name: "lock fails", lockErr: failure, wantErr: failure},
		{name: "queue full", queued: 2, wantErr: apperrors.ErrLimitExceeded},
		{name: "queue counter fails", countErr: failure, wantErr: failure},
		{name: "active counter fails", activeErr: failure, wantErr: failure},
		{name: "active builds reserve memory", active: 1, memory: 3 << 30, wantErr: apperrors.ErrHostExhausted},
		{name: "active builds reserve cpu", active: 1, cpu: 1000, wantErr: apperrors.ErrHostExhausted},
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
					cfg.BuildMemoryBytes = 1 << 30
					cfg.BuildCPUQuota, cfg.BuildCPUPeriod = 100000, 100000
					cfg.OvercommitFactor, cfg.CPUOvercommitFactor = 1, 1
					cfg.ReservedSystemMemory, cfg.ReservedSystemCPU = 0, 0
					deps.Config = coretest.StaticConfig{Config: cfg}
					owner := uuid.New()
					users := coremocks.NewUserInfoProvider(t)
					users.EXPECT().GetUser(mock.Anything, owner).Return(model.UserInfo{ID: owner, QuotaDiskMB: 1024}, nil)
					deps.Users = users
					repo := coremocks.NewBuildRepository(t)
					deps.Repository = repo
					locked := false
					repo.EXPECT().AcquireCapacityLock(mock.Anything).Return(func() { locked = false }, scenario.lockErr).
						Run(func(context.Context) { locked = scenario.lockErr == nil }).Once()
					if scenario.lockErr == nil && !scenario.queueDisabled {
						repo.EXPECT().CountActiveByOwner(mock.Anything, owner).Return(scenario.queued, scenario.countErr).Once()
					}
					queueAdmitted := scenario.queueDisabled || (scenario.queued < 2 && scenario.countErr == nil)
					if scenario.lockErr == nil && queueAdmitted {
						containers := coremocks.NewBuildContainerReservationRepository(t)
						containers.EXPECT().GetTotalSystemReservedMemory(mock.Anything).Return(scenario.memory, nil).Once()
						containers.EXPECT().GetTotalSystemReservedCPU(mock.Anything).Return(scenario.cpu, nil).Once()
						deps.Containers = containers
						repo.EXPECT().CountActive(mock.Anything).Return(scenario.active, scenario.activeErr).Once()
						metrics := coremocks.NewHostMetricsProvider(t)
						deps.HostMetrics = metrics
						if scenario.activeErr == nil {
							metrics.EXPECT().GetTotalMemory().Return(int64(4<<30), nil).Once()
							if scenario.memory == 0 {
								metrics.EXPECT().GetLogicalCPUs().Return(int64(2), nil).Once()
							}
						}
					}
					if scenario.wantErr == nil || scenario.reserveErr != nil {
						images := coremocks.NewBuildImageRepository(t)
						images.EXPECT().GetUserUsedDiskSpace(mock.Anything, owner).Return(int64(0), nil).Once()
						deps.Images = images
						repo.EXPECT().CreateQueuedBuild(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
							Run(func(_ context.Context, _ model.Image, build model.Build, _ model.BuildQueueOutbox) {
								require.True(t, locked, "reservation must be durable before releasing capacity")
								require.Equal(t, owner, build.OwnerID)
								require.Equal(t, project, build.ProjectID != nil)
								require.Equal(t, model.BuildStatusPending, build.Status)
							}).Return(scenario.reserveErr).Once()
					}
					svc, err := NewBuildService(deps)
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
			users := coremocks.NewUserInfoProvider(t)
			images := coremocks.NewBuildImageRepository(t)
			staged := coremocks.NewStagedObjectRepository(t)
			if scenario.statErr == nil {
				users.EXPECT().GetUser(mock.Anything, owner).Return(model.UserInfo{ID: owner, QuotaDiskMB: scenario.quotaDiskMB}, nil).Once()
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
			svc := newTestBuildService(t, BuildServiceDeps{
				Repository:    newBuildRepository(t),
				Images:        images,
				Registry:      coremocks.NewImageRegistryAPI(t),
				Users:         users,
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
