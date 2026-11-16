package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestVolumeServiceCreateValidatesQuotaAndCreatesDockerVolume(t *testing.T) {
	ownerID := uuid.New()
	repo := newVolumeRepoMock(t)
	dockerAPI := coremocks.NewVolumeDockerAPI(t)
	imageRepo := coremocks.NewVolumeImageDiskRepository(t)
	cfg := volumeTestConfig()
	lifecycle := &volumeLifecycleFake{}
	svc := newTestVolumeService(t, service.VolumeServiceDeps{
		Repository: repo,
		Docker:     dockerAPI,
		Config:     cfg,
		Lifecycle:  lifecycle,
		Quotas:     testQuotas(t, cfg, map[uuid.UUID]model.UserQuota{ownerID: {CPUMillicores: 1000, RAMMB: 1024, DiskMB: 1024}}),
		ImageRepo:  imageRepo})
	var saved model.Volume

	imageRepo.EXPECT().GetUserImageBytes(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().GetUserReservedVolumeBytes(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil)

	volumeID, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.VolumeCreateParams{Name: "data"})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, volumeID)
	require.True(t, lifecycle.createQueued)
	saved = lifecycle.volume
	require.Equal(t, ownerID, saved.OwnerID)
	require.Equal(t, "data", saved.Name)
	require.Equal(t, model.VolumeStatusCreating, saved.Status)
	// The Docker name comes from the volume ID alone, so two owners' volumes of
	// one name can never collide on the host.
	require.Equal(t, "vol_"+volumeID.String(), saved.DockerName)
}

func TestVolumeServiceCreateRejectsInvalidInputsAndLimits(t *testing.T) {
	ownerID := uuid.New()
	tests := []struct {
		name   string
		ctx    context.Context
		params model.VolumeCreateParams
		setup  func(*volumeRepoMock, *coremocks.VolumeImageDiskRepository)
		// diskQuotaMB is the owner's custom disk quota.
		diskQuotaMB int64
		wantErr     error
		// wantLimit is the limit a rejection names, if it names one.
		wantLimit apperrors.Limit
	}{
		{
			name:    "missing scope",
			ctx:     context.Background(),
			params:  model.VolumeCreateParams{Name: "data"},
			setup:   func(*volumeRepoMock, *coremocks.VolumeImageDiskRepository) {},
			wantErr: apperrors.ErrUnauthorized,
		},
		{
			name:    "invalid name",
			ctx:     accessscope.WithUserScope(context.Background(), ownerID, "", ""),
			params:  model.VolumeCreateParams{Name: "bad name"},
			setup:   func(*volumeRepoMock, *coremocks.VolumeImageDiskRepository) {},
			wantErr: apperrors.ErrBadRequest,
		},
		{
			name:        "volume count limit",
			ctx:         accessscope.WithUserScope(context.Background(), ownerID, "", ""),
			params:      model.VolumeCreateParams{Name: "data"},
			diskQuotaMB: 1024,
			setup: func(repo *volumeRepoMock, imageRepo *coremocks.VolumeImageDiskRepository) {
				imageRepo.EXPECT().GetUserImageBytes(mock.Anything, ownerID).Return(int64(0), nil)
				repo.EXPECT().GetUserReservedVolumeBytes(mock.Anything, ownerID).Return(int64(0), nil)
				repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(5, nil)
			},
			wantErr:   apperrors.ErrLimitExceeded,
			wantLimit: apperrors.LimitVolumesPerUser,
		},
		{
			name:        "disk quota",
			ctx:         accessscope.WithUserScope(context.Background(), ownerID, "", ""),
			params:      model.VolumeCreateParams{Name: "data"},
			diskQuotaMB: 1,
			setup: func(repo *volumeRepoMock, imageRepo *coremocks.VolumeImageDiskRepository) {
				imageRepo.EXPECT().GetUserImageBytes(mock.Anything, ownerID).Return(int64(0), nil)
				repo.EXPECT().GetUserReservedVolumeBytes(mock.Anything, ownerID).Return(int64(2*1024*1024), nil)
			},
			wantErr:   apperrors.ErrQuotaExceeded,
			wantLimit: apperrors.LimitUserDisk,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newVolumeRepoMock(t)
			imageRepo := coremocks.NewVolumeImageDiskRepository(t)
			tt.setup(repo, imageRepo)
			cfg := volumeTestConfig()
			svc := newTestVolumeService(t, service.VolumeServiceDeps{
				Repository: repo,
				Docker:     coremocks.NewVolumeDockerAPI(t),
				Config:     cfg,
				Lifecycle:  &volumeLifecycleFake{},
				Quotas:     testQuotas(t, cfg, map[uuid.UUID]model.UserQuota{ownerID: {CPUMillicores: 1000, RAMMB: 1024, DiskMB: max(tt.diskQuotaMB, 1)}}),
				ImageRepo:  imageRepo})
			_, err := svc.Create(tt.ctx, tt.params)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.wantLimit, apperrors.LimitOf(err))
		})
	}
}

// A volume gets the size it asks for, or the default, and is admitted only
// when that size fits what the owner's images and other volumes leave of the
// disk quota.
func TestVolumeServiceCreateReservesItsSize(t *testing.T) {
	const mib = int64(1 << 20)
	for _, tc := range []struct {
		name string
		size int64
		// imagesMB and volumesBytes are already reserved against a 100 MB quota.
		imagesMB, volumesBytes int64
		want                   error
		wantSize               int64
	}{
		{name: "no size gets the default", wantSize: 64 * mib},
		{name: "a given size is kept", size: 10 * mib, wantSize: 10 * mib},
		{name: "the smallest size", size: mib, wantSize: mib},
		{name: "fits exactly beside images and other volumes", size: 10 * mib, imagesMB: 40, volumesBytes: 50 * mib, wantSize: 10 * mib},
		{name: "does not fit beside images and other volumes", size: 11 * mib, imagesMB: 40, volumesBytes: 50 * mib, want: apperrors.ErrQuotaExceeded},
		{name: "the default size counts", volumesBytes: 37 * mib, want: apperrors.ErrQuotaExceeded},
		{name: "other volumes alone fill the quota", size: mib, volumesBytes: 100 * mib, want: apperrors.ErrQuotaExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ownerID := uuid.New()
			repo := newVolumeRepoMock(t)
			imageRepo := coremocks.NewVolumeImageDiskRepository(t)
			lock := coremocks.NewVolumeCapacityLock(t)
			cfg := volumeTestConfig()
			cfg.Config.DefaultVolumeSizeBytes = 64 * mib
			lifecycle := &volumeLifecycleFake{}
			held := false
			lock.EXPECT().AcquireOwnerCapacityLock(mock.Anything, ownerID).RunAndReturn(func(context.Context, uuid.UUID) (func(), error) {
				held = true
				return func() { held = false }, nil
			}).Once()
			lifecycle.onCreate = func() { require.True(t, held, "the volume is queued under the owner capacity lock") }
			imageRepo.EXPECT().GetUserImageBytes(mock.Anything, ownerID).Return(tc.imagesMB<<20, nil)
			repo.EXPECT().GetUserReservedVolumeBytes(mock.Anything, ownerID).Return(tc.volumesBytes, nil)
			repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil).Maybe()
			svc := newTestVolumeService(t, service.VolumeServiceDeps{
				Repository: repo, Docker: coremocks.NewVolumeDockerAPI(t), Config: cfg,
				Lifecycle: lifecycle, ImageRepo: imageRepo, CapacityLock: lock,
				Quotas: testQuotas(t, cfg, map[uuid.UUID]model.UserQuota{ownerID: {CPUMillicores: 1000, RAMMB: 1024, DiskMB: 100}}),
			})

			_, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.VolumeCreateParams{Name: "data", SizeBytes: tc.size})

			require.False(t, held, "the lock is released")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.False(t, lifecycle.createQueued, "a rejected volume is not queued")
				return
			}
			require.NoError(t, err)
			require.True(t, lifecycle.createQueued)
			require.Equal(t, tc.wantSize, lifecycle.volume.SizeBytes)
			require.False(t, lifecycle.volume.SizeEnforced, "sizes are only measured")
		})
	}
}

// A size below 1 MiB is a bad request that does not echo the value, decided
// before any quota is read or lock taken.
func TestVolumeServiceCreateRejectsTooSmallSize(t *testing.T) {
	for _, size := range []int64{1<<20 - 1, 4095, -5} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			lifecycle := &volumeLifecycleFake{}
			svc := newTestVolumeService(t, service.VolumeServiceDeps{
				Repository: newVolumeRepoMock(t), Docker: coremocks.NewVolumeDockerAPI(t), Config: volumeTestConfig(),
				Lifecycle: lifecycle, CapacityLock: coremocks.NewVolumeCapacityLock(t),
			})

			_, err := svc.Create(accessscope.WithUserScope(context.Background(), uuid.New(), "", ""), model.VolumeCreateParams{Name: "data", SizeBytes: size})

			require.ErrorIs(t, err, apperrors.ErrBadRequest)
			require.NotContains(t, err.Error(), fmt.Sprint(size))
			require.False(t, lifecycle.createQueued)
		})
	}
}

// A failed lock rejects the create before anything is read or queued.
func TestVolumeServiceCreateReturnsLockFailure(t *testing.T) {
	ownerID := uuid.New()
	lock := coremocks.NewVolumeCapacityLock(t)
	lockErr := errors.New("lock unavailable")
	lock.EXPECT().AcquireOwnerCapacityLock(mock.Anything, ownerID).Return(nil, lockErr)
	lifecycle := &volumeLifecycleFake{}
	svc := newTestVolumeService(t, service.VolumeServiceDeps{
		Repository: newVolumeRepoMock(t), Docker: coremocks.NewVolumeDockerAPI(t), Config: volumeTestConfig(),
		Lifecycle: lifecycle, CapacityLock: lock,
	})

	_, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.VolumeCreateParams{Name: "data"})

	require.ErrorIs(t, err, lockErr)
	require.False(t, lifecycle.createQueued)
}

func TestVolumeServiceResolveByNameUsesScopeAndRejectsUnavailableVolumes(t *testing.T) {
	ownerID := uuid.New()
	volumeID := uuid.New()
	tests := []struct {
		name    string
		volume  model.Volume
		wantID  uuid.UUID
		wantErr error
	}{
		{
			name:   "available",
			volume: model.Volume{ID: volumeID, OwnerID: ownerID, Name: "data", Status: model.VolumeStatusAvailable},
			wantID: volumeID,
		},
		{
			name:    "missing",
			volume:  model.Volume{ID: volumeID, OwnerID: ownerID, Name: "data", Status: model.VolumeStatusMissing},
			wantErr: apperrors.ErrConflict,
		},
		{
			name:    "deleting",
			volume:  model.Volume{ID: volumeID, OwnerID: ownerID, Name: "data", Status: model.VolumeStatusDeleting},
			wantErr: apperrors.ErrConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newVolumeRepoMock(t)
			repo.EXPECT().GetByName(mock.Anything, ownerID, "data").Return(tt.volume, nil)
			svc := newTestVolumeService(t, service.VolumeServiceDeps{
				Repository: repo,
				Docker:     coremocks.NewVolumeDockerAPI(t),
				Config:     volumeTestConfig(),
				Lifecycle:  &volumeLifecycleFake{},
			})

			gotID, err := svc.ResolveByName(accessscope.WithUserScope(context.Background(), ownerID, "", ""), "data")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantID, gotID)
		})
	}
}

func TestVolumeServiceResolveProjectManagedByNameReusesAndAttachesVolumes(t *testing.T) {
	ownerID := uuid.New()
	projectID := uuid.New()
	otherProjectID := uuid.New()
	volumeID := uuid.New()

	tests := []struct {
		name       string
		volume     model.Volume
		setup      func(*volumeRepoMock)
		wantID     uuid.UUID
		wantReused bool
		wantErr    error
	}{
		{
			name:   "same project",
			volume: model.Volume{ID: volumeID, OwnerID: ownerID, ProjectID: &projectID, Name: "project_a_data", Status: model.VolumeStatusAvailable},
			setup: func(repo *volumeRepoMock) {
				repo.EXPECT().LinkProjectVolume(mock.Anything, projectID, volumeID).Return(nil)
			},
			wantID:     volumeID,
			wantReused: true,
		},
		{
			name:   "unassigned preserved volume attaches",
			volume: model.Volume{ID: volumeID, OwnerID: ownerID, Name: "project_a_data", Status: model.VolumeStatusAvailable},
			setup: func(repo *volumeRepoMock) {
				repo.EXPECT().LinkProjectVolume(mock.Anything, projectID, volumeID).Return(nil)
			},
			wantID:     volumeID,
			wantReused: true,
		},
		{
			name:   "other project available volume links",
			volume: model.Volume{ID: volumeID, OwnerID: ownerID, ProjectID: &otherProjectID, Name: "project_a_data", Status: model.VolumeStatusAvailable},
			setup: func(repo *volumeRepoMock) {
				repo.EXPECT().LinkProjectVolume(mock.Anything, projectID, volumeID).Return(nil)
			},
			wantID:     volumeID,
			wantReused: true,
		},
		{
			name:   "unavailable rejected",
			volume: model.Volume{ID: volumeID, OwnerID: ownerID, Name: "project_a_data", Status: model.VolumeStatusCreating},
			setup: func(repo *volumeRepoMock) {
				repo.EXPECT().IsLinkedToProject(mock.Anything, projectID, volumeID).Return(false, nil)
			},
			wantErr: apperrors.ErrConflict,
		},
		{
			name:   "creating linked project waits",
			volume: model.Volume{ID: volumeID, OwnerID: ownerID, ProjectID: &otherProjectID, Name: "project_a_data", Status: model.VolumeStatusCreating},
			setup: func(repo *volumeRepoMock) {
				repo.EXPECT().IsLinkedToProject(mock.Anything, projectID, volumeID).Return(true, nil)
			},
			wantID:     volumeID,
			wantReused: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newVolumeRepoMock(t)
			repo.EXPECT().GetByName(mock.Anything, ownerID, "project_a_data").Return(tt.volume, nil)
			if tt.setup != nil {
				tt.setup(repo)
			}
			svc := newTestVolumeService(t, service.VolumeServiceDeps{
				Repository: repo,
				Docker:     coremocks.NewVolumeDockerAPI(t),
				Config:     volumeTestConfig(),
				Lifecycle:  &volumeLifecycleFake{},
			})

			gotID, reused, err := svc.ResolveProjectManagedByName(accessscope.WithUserScope(context.Background(), ownerID, "", ""), projectID, "project_a_data")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantID, gotID)
			require.Equal(t, tt.wantReused, reused)
		})
	}
}

func TestVolumeServiceCreateRejectsHostDiskFloor(t *testing.T) {
	ownerID := uuid.New()
	repo := newVolumeRepoMock(t)
	dockerAPI := coremocks.NewVolumeDockerAPI(t)
	imageRepo := coremocks.NewVolumeImageDiskRepository(t)
	metrics := coremocks.NewHostDiskMetricsProvider(t)
	cfgData := testSystemConfig()
	cfgData.HostMinFreeDiskBytes = 1024
	cfg := coretest.StaticConfig{Config: cfgData}
	svc := newTestVolumeService(t, service.VolumeServiceDeps{
		Repository:   repo,
		Docker:       dockerAPI,
		Config:       cfg,
		Lifecycle:    &volumeLifecycleFake{},
		Quotas:       testQuotas(t, cfg, map[uuid.UUID]model.UserQuota{ownerID: {CPUMillicores: 1000, RAMMB: 1024, DiskMB: 1024}}),
		ImageRepo:    imageRepo,
		DiskMetrics:  metrics,
		HostDiskPath: "/host"})

	imageRepo.EXPECT().GetUserImageBytes(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().GetUserReservedVolumeBytes(mock.Anything, ownerID).Return(int64(0), nil)
	metrics.EXPECT().GetDiskUsage("/host").Return(model.HostDiskStats{FreeBytes: 512}, nil)

	_, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.VolumeCreateParams{Name: "data"})
	require.ErrorIs(t, err, apperrors.ErrHostExhausted)
}

func TestVolumeServiceCreateMarksErrorWhenDockerCreateFails(t *testing.T) {
	ownerID := uuid.New()
	volumeID := uuid.New()
	operationID := uuid.New()
	repo := newVolumeRepoMock(t)
	dockerAPI := coremocks.NewVolumeDockerAPI(t)
	dockerErr := errors.New("docker create failed")
	lifecycle := newVolumeLifecycleFake(operationID, volumeID, ownerID, model.OperationCreate)
	svc := newTestVolumeService(t, service.VolumeServiceDeps{
		Repository: repo,
		Docker:     dockerAPI,
		Config:     volumeTestConfig(),
		Lifecycle:  lifecycle,
	})

	repo.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{ID: volumeID, OwnerID: ownerID, DockerName: "vol_data"}, nil)
	dockerAPI.EXPECT().CreateVolume(mock.Anything, mock.AnythingOfType("model.VolumeRuntimeSpec")).Return("", dockerErr)
	repo.EXPECT().MarkStatusError(mock.Anything, mock.AnythingOfType("uuid.UUID"), model.VolumeStatusError, dockerErr).Return(nil)

	err := svc.ExecuteQueuedVolumeOperation(context.Background(), operationID, volumeID)
	require.ErrorIs(t, err, dockerErr)
	require.Equal(t, model.OperationStatusFailed, lifecycle.op.Status)
}

func TestVolumeServiceDeleteHandlesInUseAndDockerNotFound(t *testing.T) {
	ownerID := uuid.New()
	volumeID := uuid.New()
	tests := []struct {
		name       string
		setup      func(*volumeRepoMock, *coremocks.VolumeDockerAPI)
		wantErr    error
		wantQueued bool
	}{
		{
			name: "in use",
			setup: func(repo *volumeRepoMock, docker *coremocks.VolumeDockerAPI) {
				repo.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{ID: volumeID, OwnerID: ownerID, DockerName: "vol_data"}, nil)
				repo.EXPECT().IsVolumeInUse(mock.Anything, volumeID).Return(true, nil)
			},
			wantErr: apperrors.ErrResourceInUse,
		},
		{
			name: "delete is queued",
			setup: func(repo *volumeRepoMock, docker *coremocks.VolumeDockerAPI) {
				repo.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{ID: volumeID, OwnerID: ownerID, DockerName: "vol_data"}, nil)
				repo.EXPECT().IsVolumeInUse(mock.Anything, volumeID).Return(false, nil)
			},
			wantQueued: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newVolumeRepoMock(t)
			dockerAPI := coremocks.NewVolumeDockerAPI(t)
			tt.setup(repo, dockerAPI)
			lifecycle := &volumeLifecycleFake{}
			svc := newTestVolumeService(t, service.VolumeServiceDeps{
				Repository: repo,
				Docker:     dockerAPI,
				Config:     volumeTestConfig(),
				Lifecycle:  lifecycle,
			})
			err := svc.Delete(accessscope.WithUserScope(context.Background(), ownerID, "", ""), volumeID)
			if tt.wantErr != nil {
				require.Error(t, err)
				if errors.Is(tt.wantErr, apperrors.ErrResourceInUse) {
					require.ErrorIs(t, err, tt.wantErr)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantQueued, lifecycle.deleteQueued)
			}
		})
	}
}

func TestVolumeServiceExecuteQueuedDeleteHandlesDockerResults(t *testing.T) {
	ownerID := uuid.New()
	volumeID := uuid.New()
	tests := []struct {
		name    string
		setup   func(*volumeRepoMock, *coremocks.VolumeDockerAPI)
		wantErr error
	}{
		{
			name: "docker missing is idempotent",
			setup: func(repo *volumeRepoMock, docker *coremocks.VolumeDockerAPI) {
				repo.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{ID: volumeID, OwnerID: ownerID, DockerName: "vol_data"}, nil)
				docker.EXPECT().RemoveVolume(mock.Anything, "vol_data", false).Return(cerrdefs.ErrNotFound)
				repo.EXPECT().Delete(mock.Anything, volumeID).Return(nil)
			},
		},
		{
			name: "wrapped missing volume record is idempotent",
			setup: func(repo *volumeRepoMock, docker *coremocks.VolumeDockerAPI) {
				repo.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{}, fmt.Errorf("load volume: %w", apperrors.ErrNotFound))
			},
		},
		{
			name: "wrapped missing volume record on delete is idempotent",
			setup: func(repo *volumeRepoMock, docker *coremocks.VolumeDockerAPI) {
				repo.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{ID: volumeID, OwnerID: ownerID, DockerName: "vol_data"}, nil)
				docker.EXPECT().RemoveVolume(mock.Anything, "vol_data", false).Return(nil)
				repo.EXPECT().Delete(mock.Anything, volumeID).Return(fmt.Errorf("delete volume: %w", apperrors.ErrNotFound))
			},
		},
		{
			name: "docker error marks volume error",
			setup: func(repo *volumeRepoMock, docker *coremocks.VolumeDockerAPI) {
				removeErr := errors.New("remove failed")
				repo.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{ID: volumeID, OwnerID: ownerID, DockerName: "vol_data"}, nil)
				docker.EXPECT().RemoveVolume(mock.Anything, "vol_data", false).Return(removeErr)
				repo.EXPECT().MarkStatusError(mock.Anything, volumeID, model.VolumeStatusError, removeErr).Return(nil)
			},
			wantErr: errors.New("remove failed"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operationID := uuid.New()
			repo := newVolumeRepoMock(t)
			dockerAPI := coremocks.NewVolumeDockerAPI(t)
			tt.setup(repo, dockerAPI)
			lifecycle := newVolumeLifecycleFake(operationID, volumeID, ownerID, model.OperationDelete)
			svc := newTestVolumeService(t, service.VolumeServiceDeps{
				Repository: repo,
				Docker:     dockerAPI,
				Config:     volumeTestConfig(),
				Lifecycle:  lifecycle,
			})
			err := svc.ExecuteQueuedVolumeOperation(context.Background(), operationID, volumeID)
			if tt.wantErr != nil {
				require.Error(t, err)
				require.Equal(t, model.OperationStatusFailed, lifecycle.op.Status)
				return
			}
			require.NoError(t, err)
			require.Equal(t, model.OperationStatusDone, lifecycle.op.Status)
		})
	}
}

func TestVolumeServiceExecuteQueuedReturnsStatusWriteFailure(t *testing.T) {
	ownerID, volumeID := uuid.New(), uuid.New()
	dockerErr := errors.New("docker create failed")
	retryableErr := apperrors.Wrap(apperrors.ErrUnavailable, "docker unavailable", errors.New("connection refused"))
	statusErr := errors.New("update volume status failed")
	markErr := errors.New("mark volume error failed")
	writeErr := apperrors.Wrap(apperrors.ErrUnavailable, "database unavailable", errors.New("connection reset"))
	requeueErr := errors.New("requeue failed")
	tests := []struct {
		name        string
		createErr   error
		statusErr   error
		markErr     error
		completeErr error
		requeueErr  error
		wantErrors  []error
		wantStatus  string
	}{
		{name: "done", completeErr: writeErr, wantErrors: []error{writeErr}, wantStatus: model.OperationStatusRunning},
		{name: "available status", statusErr: statusErr, wantErrors: []error{statusErr}, wantStatus: model.OperationStatusFailed},
		{name: "failed", createErr: dockerErr, completeErr: writeErr, wantErrors: []error{dockerErr, writeErr}, wantStatus: model.OperationStatusRunning},
		{name: "volume error mark", createErr: dockerErr, markErr: markErr, wantErrors: []error{dockerErr, markErr}, wantStatus: model.OperationStatusFailed},
		{name: "requeue", createErr: retryableErr, requeueErr: requeueErr, wantErrors: []error{retryableErr, requeueErr}, wantStatus: model.OperationStatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operationID := uuid.New()
			repo := newVolumeRepoMock(t)
			dockerAPI := coremocks.NewVolumeDockerAPI(t)
			lifecycle := newVolumeLifecycleFake(operationID, volumeID, ownerID, model.OperationCreate)
			lifecycle.requeueErr = tt.requeueErr
			lifecycle.completeErr = tt.completeErr
			svc := newTestVolumeService(t, service.VolumeServiceDeps{
				Repository: repo,
				Docker:     dockerAPI,
				Config:     volumeTestConfig(),
				Lifecycle:  lifecycle,
			})
			repo.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{ID: volumeID, OwnerID: ownerID, DockerName: "vol_data"}, nil)
			dockerAPI.EXPECT().CreateVolume(mock.Anything, mock.AnythingOfType("model.VolumeRuntimeSpec")).Return("", tt.createErr)
			if tt.createErr == nil {
				repo.EXPECT().MarkAvailable(mock.Anything, volumeID, false).Return(tt.statusErr)
			}
			if tt.createErr != nil || tt.statusErr != nil {
				repo.EXPECT().MarkStatusError(mock.Anything, volumeID, model.VolumeStatusError, mock.Anything).Return(tt.markErr)
			}

			err := svc.ExecuteQueuedVolumeOperation(context.Background(), operationID, volumeID)
			for _, want := range tt.wantErrors {
				require.ErrorIs(t, err, want)
			}
			require.Equal(t, tt.wantStatus, lifecycle.op.Status)
		})
	}
}

func TestVolumeServiceListUsesScopeOwnerFilter(t *testing.T) {
	ownerID := uuid.New()
	repo := newVolumeRepoMock(t)
	svc := newTestVolumeService(t, service.VolumeServiceDeps{
		Repository: repo,
		Docker:     coremocks.NewVolumeDockerAPI(t),
		Config:     volumeTestConfig(),
		Lifecycle:  &volumeLifecycleFake{},
	})
	var userOpts model.ListOptions
	var adminOpts model.ListOptions

	repo.EXPECT().
		List(mock.Anything, mock.AnythingOfType("model.ListOptions")).
		Run(func(ctx context.Context, opts model.ListOptions) {
			userOpts = opts
		}).
		Return([]model.Volume(nil), 0, nil).
		Once()
	repo.EXPECT().
		List(mock.Anything, mock.AnythingOfType("model.ListOptions")).
		Run(func(ctx context.Context, opts model.ListOptions) {
			adminOpts = opts
		}).
		Return([]model.Volume(nil), 0, nil).
		Once()

	_, _, err := svc.List(accessscope.WithUserScope(context.Background(), ownerID, "", ""), 25, 5)
	require.NoError(t, err)
	require.NotNil(t, userOpts.OwnerID)
	require.Equal(t, ownerID, *userOpts.OwnerID)
	require.Equal(t, 25, userOpts.Limit)
	require.Equal(t, 5, userOpts.Offset)

	_, _, err = svc.List(accessscope.WithAdminScope(context.Background(), uuid.New(), "", ""), 10, 0)
	require.NoError(t, err)
	require.Nil(t, adminOpts.OwnerID)
}

type volumeRepoMock struct {
	*coremocks.VolumeRepository
}

func newVolumeRepoMock(t *testing.T) *volumeRepoMock {
	t.Helper()
	return &volumeRepoMock{
		VolumeRepository: coremocks.NewVolumeRepository(t),
	}
}

type volumeLifecycleFake struct {
	// onCreate, when set, runs as the volume is queued.
	onCreate     func()
	op           model.ResourceOperation
	volume       model.Volume
	createQueued bool
	deleteQueued bool
	active       bool
	completeErr  error
	requeueErr   error
}

func newVolumeLifecycleFake(operationID, volumeID, ownerID uuid.UUID, operation string) *volumeLifecycleFake {
	return &volumeLifecycleFake{
		op: model.ResourceOperation{
			ID:           operationID,
			ResourceType: model.ResourceTypeVolume,
			ResourceID:   volumeID,
			OwnerID:      ownerID,
			Operation:    operation,
			Status:       model.OperationStatusPending,
		},
	}
}

func (f *volumeLifecycleFake) CreateQueuedVolume(ctx context.Context, vol model.Volume, op model.ResourceOperation, outbox model.ResourceLifecycleOutbox) error {
	if f.onCreate != nil {
		f.onCreate()
	}
	f.volume = vol
	f.op = op
	f.createQueued = true
	return nil
}

func (f *volumeLifecycleFake) QueueVolumeDelete(ctx context.Context, id uuid.UUID, op model.ResourceOperation, outbox model.ResourceLifecycleOutbox) error {
	f.op = op
	f.deleteQueued = true
	return nil
}

func (f *volumeLifecycleFake) HasActiveOperation(ctx context.Context, resourceType string, resourceID uuid.UUID) (bool, error) {
	return f.active, nil
}

func (f *volumeLifecycleFake) GetOperationByID(ctx context.Context, id uuid.UUID) (model.ResourceOperation, error) {
	return f.op, nil
}

func (f *volumeLifecycleFake) ClaimPendingOperation(ctx context.Context, id uuid.UUID, maxAttempts int) (model.ResourceOperation, bool, error) {
	if f.op.Status != model.OperationStatusPending {
		return f.op, false, nil
	}
	f.op.Status = model.OperationStatusRunning
	f.op.Attempts++
	return f.op, true, nil
}

func (f *volumeLifecycleFake) CompleteOperation(ctx context.Context, id uuid.UUID, status string, cause error) error {
	if f.completeErr != nil {
		return f.completeErr
	}
	f.op.Status = status
	return nil
}

func (f *volumeLifecycleFake) RequeueOperation(ctx context.Context, id uuid.UUID, cause error) error {
	if f.requeueErr != nil {
		return f.requeueErr
	}
	f.op.Status = model.OperationStatusPending
	return nil
}

func volumeTestConfig() coretest.StaticConfig {
	cfg := testConfig()
	cfg.Config.MaxVolumesPerUser = 5
	return cfg
}

// The queued create is where a volume's mode is decided, once: only when
// enforcement is on and the host supports it does Docker get the size, and
// only then is the volume recorded as kernel-enforced. Docker refusing the
// size fails the create like any other failure; there is no silent fallback.
func TestVolumeServiceExecuteQueuedCreateDecidesTheSizeMode(t *testing.T) {
	const sizeBytes = 256 << 20
	refused := fmt.Errorf("%w: quota size requested but no quota support", cerrdefs.ErrInvalidArgument)
	tests := []struct {
		name         string
		mode         model.VolumeSizeMode
		dockerErr    error
		wantSize     int64
		wantEnforced bool
		wantStatus   string
	}{
		{name: "measured", mode: model.VolumeSizeMeasured, wantSize: 0, wantEnforced: false, wantStatus: model.OperationStatusDone},
		{name: "enforced", mode: model.VolumeSizeEnforced, wantSize: sizeBytes, wantEnforced: true, wantStatus: model.OperationStatusDone},
		{name: "unavailable", mode: model.VolumeSizeUnavailable, wantSize: 0, wantEnforced: false, wantStatus: model.OperationStatusDone},
		{name: "enforced but Docker refuses the size", mode: model.VolumeSizeEnforced, dockerErr: refused, wantSize: sizeBytes, wantStatus: model.OperationStatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ownerID, volumeID, operationID := uuid.New(), uuid.New(), uuid.New()
			repo := newVolumeRepoMock(t)
			dockerAPI := coremocks.NewVolumeDockerAPI(t)
			lifecycle := newVolumeLifecycleFake(operationID, volumeID, ownerID, model.OperationCreate)
			sizeMode := coremocks.NewVolumeSizeModeReader(t)
			sizeMode.EXPECT().VolumeSizeMode().Return(tt.mode)
			svc := newTestVolumeService(t, service.VolumeServiceDeps{
				Repository: repo,
				Docker:     dockerAPI,
				Config:     volumeTestConfig(),
				Lifecycle:  lifecycle,
				SizeMode:   sizeMode,
			})
			repo.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{
				ID: volumeID, OwnerID: ownerID, DockerName: "vol_data", SizeBytes: sizeBytes, Status: model.VolumeStatusCreating,
			}, nil)
			var sent model.VolumeRuntimeSpec
			dockerAPI.EXPECT().CreateVolume(mock.Anything, mock.AnythingOfType("model.VolumeRuntimeSpec")).
				Run(func(_ context.Context, spec model.VolumeRuntimeSpec) { sent = spec }).
				Return("vol_data", tt.dockerErr).Once()
			if tt.dockerErr == nil {
				repo.EXPECT().MarkAvailable(mock.Anything, volumeID, tt.wantEnforced).Return(nil).Once()
			} else {
				repo.EXPECT().MarkStatusError(mock.Anything, volumeID, model.VolumeStatusError, mock.Anything).Return(nil).Once()
			}

			err := svc.ExecuteQueuedVolumeOperation(context.Background(), operationID, volumeID)

			if tt.dockerErr != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, "vol_data", sent.VolumeName)
			require.Equal(t, tt.wantSize, sent.SizeBytes)
			require.Equal(t, tt.wantStatus, lifecycle.op.Status)
		})
	}
}
