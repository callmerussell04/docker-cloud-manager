package service_test

import (
	"context"
	"errors"
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
	svc := NewVolumeService(repo, dockerAPI, cfg, VolumeServiceDeps{Users: users, ImageRepo: imageRepo})
	var saved model.Volume
	var runtimeSpec model.VolumeRuntimeSpec

	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaDiskMB: 1024, QuotaRAMMB: 1024}, nil)
	imageRepo.EXPECT().GetUserUsedDiskSpace(mock.Anything, ownerID).Return(int64(0), nil)
	repo.VolumeDiskUsageRepository.EXPECT().GetUserUsedVolumeBytes(mock.Anything, ownerID).Return(int64(0), nil)
	repo.VolumeRepository.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil)
	repo.VolumeRepository.EXPECT().
		Save(mock.Anything, mock.AnythingOfType("model.Volume")).
		Run(func(ctx context.Context, vol model.Volume) {
			saved = vol
		}).
		Return(nil)
	dockerAPI.EXPECT().
		CreateVolume(mock.Anything, mock.AnythingOfType("model.VolumeRuntimeSpec")).
		Run(func(ctx context.Context, spec model.VolumeRuntimeSpec) {
			runtimeSpec = spec
		}).
		Return("vol", nil)
	repo.VolumeStateRepository.EXPECT().UpdateStatus(mock.Anything, mock.AnythingOfType("uuid.UUID"), model.VolumeStatusAvailable).Return(nil)

	volumeID, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.VolumeCreateParams{Name: "data"})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, volumeID)
	require.Equal(t, ownerID, saved.OwnerID)
	require.Equal(t, "vol_"+ownerID.String()[:8]+"_data", runtimeSpec.VolumeName)
	require.Equal(t, volumeID.String(), runtimeSpec.VolumeID)
	require.Equal(t, ownerID.String(), runtimeSpec.OwnerID)
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
				repo.VolumeDiskUsageRepository.EXPECT().GetUserUsedVolumeBytes(mock.Anything, ownerID).Return(int64(0), nil)
				repo.VolumeRepository.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(5, nil)
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
				repo.VolumeDiskUsageRepository.EXPECT().GetUserUsedVolumeBytes(mock.Anything, ownerID).Return(int64(2*1024*1024), nil)
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
			svc := NewVolumeService(repo, coremocks.NewVolumeDockerAPI(t), newVolumeConfigMock(t), VolumeServiceDeps{
				Users:     users,
				ImageRepo: imageRepo,
			})
			_, err := svc.Create(tt.ctx, tt.params)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestVolumeServiceCreateMarksErrorWhenDockerCreateFails(t *testing.T) {
	ownerID := uuid.New()
	repo := newVolumeRepoMock(t)
	dockerAPI := coremocks.NewVolumeDockerAPI(t)
	users := coremocks.NewUserInfoProvider(t)
	imageRepo := coremocks.NewVolumeImageDiskRepository(t)
	dockerErr := errors.New("docker create failed")
	svc := NewVolumeService(repo, dockerAPI, newVolumeConfigMock(t), VolumeServiceDeps{Users: users, ImageRepo: imageRepo})

	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaDiskMB: 1024, QuotaRAMMB: 1024}, nil)
	imageRepo.EXPECT().GetUserUsedDiskSpace(mock.Anything, ownerID).Return(int64(0), nil)
	repo.VolumeDiskUsageRepository.EXPECT().GetUserUsedVolumeBytes(mock.Anything, ownerID).Return(int64(0), nil)
	repo.VolumeRepository.EXPECT().CountByOwnerID(mock.Anything, ownerID).Return(0, nil)
	repo.VolumeRepository.EXPECT().Save(mock.Anything, mock.AnythingOfType("model.Volume")).Return(nil)
	dockerAPI.EXPECT().CreateVolume(mock.Anything, mock.AnythingOfType("model.VolumeRuntimeSpec")).Return("", dockerErr)
	repo.VolumeStateRepository.EXPECT().MarkStatusError(mock.Anything, mock.AnythingOfType("uuid.UUID"), model.VolumeStatusError, dockerErr).Return(nil)

	_, err := svc.Create(accessscope.WithUserScope(context.Background(), ownerID, "", ""), model.VolumeCreateParams{Name: "data"})
	require.ErrorIs(t, err, dockerErr)
}

func TestVolumeServiceDeleteHandlesInUseAndDockerNotFound(t *testing.T) {
	ownerID := uuid.New()
	volumeID := uuid.New()
	tests := []struct {
		name        string
		setup       func(*volumeRepoMock, *coremocks.VolumeDockerAPI)
		wantErr     error
		wantDeleted bool
	}{
		{
			name: "in use",
			setup: func(repo *volumeRepoMock, docker *coremocks.VolumeDockerAPI) {
				repo.VolumeRepository.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{ID: volumeID, OwnerID: ownerID, DockerName: "vol_data"}, nil)
				repo.VolumeRepository.EXPECT().IsVolumeInUse(mock.Anything, volumeID).Return(true, nil)
			},
			wantErr: apperrors.ErrResourceInUse,
		},
		{
			name: "docker missing is idempotent",
			setup: func(repo *volumeRepoMock, docker *coremocks.VolumeDockerAPI) {
				repo.VolumeRepository.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{ID: volumeID, OwnerID: ownerID, DockerName: "vol_data"}, nil)
				repo.VolumeRepository.EXPECT().IsVolumeInUse(mock.Anything, volumeID).Return(false, nil)
				repo.VolumeStateRepository.EXPECT().UpdateStatus(mock.Anything, volumeID, model.VolumeStatusDeleting).Return(nil)
				docker.EXPECT().RemoveVolume(mock.Anything, "vol_data", false).Return(cerrdefs.ErrNotFound)
				repo.VolumeRepository.EXPECT().Delete(mock.Anything, volumeID).Return(nil)
			},
			wantDeleted: true,
		},
		{
			name: "docker error marks volume error",
			setup: func(repo *volumeRepoMock, docker *coremocks.VolumeDockerAPI) {
				removeErr := errors.New("remove failed")
				repo.VolumeRepository.EXPECT().GetByID(mock.Anything, volumeID).Return(model.Volume{ID: volumeID, OwnerID: ownerID, DockerName: "vol_data"}, nil)
				repo.VolumeRepository.EXPECT().IsVolumeInUse(mock.Anything, volumeID).Return(false, nil)
				repo.VolumeStateRepository.EXPECT().UpdateStatus(mock.Anything, volumeID, model.VolumeStatusDeleting).Return(nil)
				docker.EXPECT().RemoveVolume(mock.Anything, "vol_data", false).Return(removeErr)
				repo.VolumeStateRepository.EXPECT().MarkStatusError(mock.Anything, volumeID, model.VolumeStatusError, removeErr).Return(nil)
			},
			wantErr: errors.New("remove failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newVolumeRepoMock(t)
			dockerAPI := coremocks.NewVolumeDockerAPI(t)
			tt.setup(repo, dockerAPI)
			svc := NewVolumeService(repo, dockerAPI, newVolumeConfigMock(t), VolumeServiceDeps{})
			err := svc.Delete(accessscope.WithUserScope(context.Background(), ownerID, "", ""), volumeID)
			if tt.wantErr != nil {
				require.Error(t, err)
				if errors.Is(tt.wantErr, apperrors.ErrResourceInUse) {
					require.ErrorIs(t, err, tt.wantErr)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestVolumeServiceListUsesScopeOwnerFilter(t *testing.T) {
	ownerID := uuid.New()
	repo := newVolumeRepoMock(t)
	svc := NewVolumeService(repo, coremocks.NewVolumeDockerAPI(t), newVolumeConfigMock(t), VolumeServiceDeps{})
	var userOpts model.ListOptions
	var adminOpts model.ListOptions

	repo.VolumeRepository.EXPECT().
		List(mock.Anything, mock.AnythingOfType("model.ListOptions")).
		Run(func(ctx context.Context, opts model.ListOptions) {
			userOpts = opts
		}).
		Return([]model.Volume(nil), 0, nil).
		Once()
	repo.VolumeRepository.EXPECT().
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
	*coremocks.VolumeDiskUsageRepository
	*coremocks.VolumeStateRepository
}

func newVolumeRepoMock(t *testing.T) *volumeRepoMock {
	t.Helper()
	return &volumeRepoMock{
		VolumeRepository:          coremocks.NewVolumeRepository(t),
		VolumeDiskUsageRepository: coremocks.NewVolumeDiskUsageRepository(t),
		VolumeStateRepository:     coremocks.NewVolumeStateRepository(t),
	}
}

func newVolumeConfigMock(t *testing.T) *coremocks.ConfigManager {
	t.Helper()
	cfg := staticConfig{}.Get()
	cfg.MaxVolumesPerUser = 5
	manager := coremocks.NewConfigManager(t)
	manager.EXPECT().Get().Return(cfg).Maybe()
	return manager
}
