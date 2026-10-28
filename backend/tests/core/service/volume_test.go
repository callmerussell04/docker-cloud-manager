package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestVolumeServiceCreateValidatesQuotaAndCreatesDockerVolume(t *testing.T) {
	ownerID := uuid.New()
	repo := newVolumeRepoMock(t)
	dockerAPI := coremocks.NewVolumeDockerAPI(t)
	users := coremocks.NewUserInfoProvider(t)
	imageRepo := coremocks.NewVolumeImageDiskRepository(t)
	cfg := newVolumeConfigMock(t)
	lifecycle := &volumeLifecycleFake{}
	svc := newTestVolumeService(t, VolumeServiceDeps{
		Repository: repo,
		Docker:     dockerAPI,
		Config:     cfg,
		Lifecycle:  lifecycle,
		Users:      users,
		ImageRepo:  imageRepo})
	var saved model.Volume

	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaDiskMB: 1024, QuotaRAMMB: 1024}, nil)
	imageRepo.EXPECT().GetUserUsedDiskSpace(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().GetUserUsedVolumeBytes(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil)

	volumeID, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.VolumeCreateParams{Name: "data"})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, volumeID)
	require.True(t, lifecycle.createQueued)
	saved = lifecycle.volume
	require.Equal(t, ownerID, saved.OwnerID)
	require.Equal(t, "data", saved.Name)
	require.Equal(t, model.VolumeStatusCreating, saved.Status)
	dockerAPI.AssertNotCalled(t, "CreateVolume", mock.Anything, mock.Anything)
}

func TestVolumeServiceCreateRejectsInvalidInputsAndLimits(t *testing.T) {
	ownerID := uuid.New()
	tests := []struct {
		name    string
		ctx     context.Context
		params  model.VolumeCreateParams
		setup   func(*volumeRepoMock, *coremocks.UserInfoProvider, *coremocks.VolumeImageDiskRepository)
		wantErr error
	}{
		{
			name:    "missing scope",
			ctx:     context.Background(),
			params:  model.VolumeCreateParams{Name: "data"},
			setup:   func(*volumeRepoMock, *coremocks.UserInfoProvider, *coremocks.VolumeImageDiskRepository) {},
			wantErr: apperrors.ErrUnauthorized,
		},
		{
			name:    "invalid name",
			ctx:     accessscope.WithUserScope(context.Background(), ownerID, "", ""),
			params:  model.VolumeCreateParams{Name: "bad name"},
			setup:   func(*volumeRepoMock, *coremocks.UserInfoProvider, *coremocks.VolumeImageDiskRepository) {},
			wantErr: apperrors.ErrBadRequest,
		},
		{
			name:   "volume count limit",
			ctx:    accessscope.WithUserScope(context.Background(), ownerID, "", ""),
			params: model.VolumeCreateParams{Name: "data"},
			setup: func(repo *volumeRepoMock, users *coremocks.UserInfoProvider, imageRepo *coremocks.VolumeImageDiskRepository) {
				users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaDiskMB: 1024, QuotaRAMMB: 1024}, nil)
				imageRepo.EXPECT().GetUserUsedDiskSpace(mock.Anything, ownerID).Return(int64(0), nil)
				repo.EXPECT().GetUserUsedVolumeBytes(mock.Anything, ownerID).Return(int64(0), nil)
				repo.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(5, nil)
			},
			wantErr: apperrors.ErrLimitExceeded,
		},
		{
			name:   "disk quota",
			ctx:    accessscope.WithUserScope(context.Background(), ownerID, "", ""),
			params: model.VolumeCreateParams{Name: "data"},
			setup: func(repo *volumeRepoMock, users *coremocks.UserInfoProvider, imageRepo *coremocks.VolumeImageDiskRepository) {
				users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaDiskMB: 1, QuotaRAMMB: 1024}, nil)
				imageRepo.EXPECT().GetUserUsedDiskSpace(mock.Anything, ownerID).Return(int64(0), nil)
				repo.EXPECT().GetUserUsedVolumeBytes(mock.Anything, ownerID).Return(int64(2*1024*1024), nil)
			},
			wantErr: apperrors.ErrQuotaExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newVolumeRepoMock(t)
			users := coremocks.NewUserInfoProvider(t)
			imageRepo := coremocks.NewVolumeImageDiskRepository(t)
			tt.setup(repo, users, imageRepo)
			svc := newTestVolumeService(t, VolumeServiceDeps{
				Repository: repo,
				Docker:     coremocks.NewVolumeDockerAPI(t),
				Config:     newVolumeConfigMock(t),
				Lifecycle:  &volumeLifecycleFake{},
				Users:      users,
				ImageRepo:  imageRepo})
			_, err := svc.Create(tt.ctx, tt.params)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
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
			svc := newTestVolumeService(t, VolumeServiceDeps{
				Repository: repo,
				Docker:     coremocks.NewVolumeDockerAPI(t),
				Config:     newVolumeConfigMock(t),
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
			svc := newTestVolumeService(t, VolumeServiceDeps{
				Repository: repo,
				Docker:     coremocks.NewVolumeDockerAPI(t),
				Config:     newVolumeConfigMock(t),
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
	users := coremocks.NewUserInfoProvider(t)
	imageRepo := coremocks.NewVolumeImageDiskRepository(t)
	metrics := coremocks.NewHostDiskMetricsProvider(t)
	cfgData := staticConfig{}.Get()
	cfgData.HostMinFreeDiskBytes = 1024
	cfg := coremocks.NewConfigManager(t)
	cfg.EXPECT().Get().Return(cfgData).Maybe()
	svc := newTestVolumeService(t, VolumeServiceDeps{
		Repository:   repo,
		Docker:       dockerAPI,
		Config:       cfg,
		Lifecycle:    &volumeLifecycleFake{},
		Users:        users,
		ImageRepo:    imageRepo,
		DiskMetrics:  metrics,
		HostDiskPath: "/host"})

	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaDiskMB: 1024, QuotaRAMMB: 1024}, nil)
	imageRepo.EXPECT().GetUserUsedDiskSpace(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().GetUserUsedVolumeBytes(mock.Anything, ownerID).Return(int64(0), nil)
	metrics.EXPECT().GetDiskUsage("/host").Return(model.HostDiskStats{FreeBytes: 512}, nil)

	_, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.VolumeCreateParams{Name: "data"})
	require.ErrorIs(t, err, apperrors.ErrHostExhausted)
	repo.AssertNotCalled(t, "Save", mock.Anything, mock.Anything)
	dockerAPI.AssertNotCalled(t, "CreateVolume", mock.Anything, mock.Anything)
}

func TestVolumeServiceCreateMarksErrorWhenDockerCreateFails(t *testing.T) {
	ownerID := uuid.New()
	volumeID := uuid.New()
	operationID := uuid.New()
	repo := newVolumeRepoMock(t)
	dockerAPI := coremocks.NewVolumeDockerAPI(t)
	dockerErr := errors.New("docker create failed")
	lifecycle := newVolumeLifecycleFake(operationID, volumeID, ownerID, model.OperationCreate)
	svc := newTestVolumeService(t, VolumeServiceDeps{
		Repository: repo,
		Docker:     dockerAPI,
		Config:     newVolumeConfigMock(t),
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
			svc := newTestVolumeService(t, VolumeServiceDeps{
				Repository: repo,
				Docker:     dockerAPI,
				Config:     newVolumeConfigMock(t),
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
			svc := newTestVolumeService(t, VolumeServiceDeps{
				Repository: repo,
				Docker:     dockerAPI,
				Config:     newVolumeConfigMock(t),
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
			svc := newTestVolumeService(t, VolumeServiceDeps{
				Repository: repo,
				Docker:     dockerAPI,
				Config:     newVolumeConfigMock(t),
				Lifecycle:  lifecycle,
			})
			repo.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{ID: volumeID, OwnerID: ownerID, DockerName: "vol_data"}, nil)
			dockerAPI.EXPECT().CreateVolume(mock.Anything, mock.AnythingOfType("model.VolumeRuntimeSpec")).Return("", tt.createErr)
			if tt.createErr == nil {
				repo.EXPECT().UpdateStatus(mock.Anything, volumeID, model.VolumeStatusAvailable).Return(tt.statusErr)
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
	svc := newTestVolumeService(t, VolumeServiceDeps{
		Repository: repo,
		Docker:     coremocks.NewVolumeDockerAPI(t),
		Config:     newVolumeConfigMock(t),
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

func newVolumeConfigMock(t *testing.T) *coremocks.ConfigManager {
	t.Helper()
	cfg := staticConfig{}.Get()
	cfg.MaxVolumesPerUser = 5
	manager := coremocks.NewConfigManager(t)
	manager.EXPECT().Get().Return(cfg).Maybe()
	return manager
}
