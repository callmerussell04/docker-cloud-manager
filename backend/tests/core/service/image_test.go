package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/tests/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestImageServiceDeleteRemovesRegistryDockerAndRecord(t *testing.T) {
	ownerID := uuid.New()
	imageID := uuid.New()
	operationID := uuid.New()
	repo := newImageRepoMock(t)
	registry := coremocks.NewImageRegistryAPI(t)
	dockerAPI := coremocks.NewImageDockerAPI(t)
	containerRepo := coremocks.NewImageContainerRepository(t)
	lifecycle := newImageLifecycleFake(operationID, imageID, ownerID)
	svc, err := NewImageService(ImageServiceDeps{
		Repository:   repo,
		Docker:       dockerAPI,
		Registry:     registry,
		Containers:   containerRepo,
		Config:       staticConfig{},
		Lifecycle:    lifecycle,
		RegistryAuth: unlockedRepositoryFence(t),
	})
	require.NoError(t, err)

	repo.ImageRepository.EXPECT().GetByID(mock.Anything, imageID).Return(model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo:1.2.3"}, nil)
	var registryRepo, registryTag, deletedDigest string
	registry.EXPECT().
		GetImageSizeAndDigest(mock.Anything, ownerID.String()+"_demo", "1.2.3").
		Run(func(ctx context.Context, repoName, tag string) {
			registryRepo = repoName
			registryTag = tag
		}).
		Return(int64(0), "sha256:abc", nil)
	registry.EXPECT().
		DeleteManifest(mock.Anything, ownerID.String()+"_demo", "sha256:abc").
		Run(func(ctx context.Context, repoName, digest string) {
			deletedDigest = digest
		}).
		Return(nil)
	dockerAPI.EXPECT().RemoveImage(mock.Anything, mock.MatchedBy(func(tag string) bool {
		return strings.HasSuffix(tag, "demo:1.2.3")
	}), false).Return(nil)
	repo.ImageRepository.EXPECT().Delete(mock.Anything, imageID).Return(nil)

	err = svc.ExecuteQueuedImageOperation(context.Background(), operationID, imageID)
	require.NoError(t, err)
	require.Equal(t, ownerID.String()+"_demo", registryRepo)
	require.Equal(t, "1.2.3", registryTag)
	require.Equal(t, "sha256:abc", deletedDigest)
	require.Equal(t, model.OperationStatusDone, lifecycle.op.Status)
}

func TestImageServiceDeleteBlocksImageInUse(t *testing.T) {
	ownerID := uuid.New()
	imageID := uuid.New()
	repo := newImageRepoMock(t)
	containerRepo := coremocks.NewImageContainerRepository(t)
	svc, err := NewImageService(ImageServiceDeps{
		Repository:   repo,
		Docker:       coremocks.NewImageDockerAPI(t),
		Registry:     coremocks.NewImageRegistryAPI(t),
		Containers:   containerRepo,
		Config:       staticConfig{},
		Lifecycle:    &imageLifecycleFake{},
		RegistryAuth: unlockedRepositoryFence(t),
	})
	require.NoError(t, err)

	repo.ImageRepository.EXPECT().GetByID(mock.Anything, imageID).Return(model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo:latest"}, nil)
	containerRepo.EXPECT().IsImageInUse(mock.Anything, ownerID, "demo:latest").Return(true, nil)

	err = svc.Delete(accessscope.WithUserScope(context.Background(), ownerID, "", ""), imageID)
	require.ErrorIs(t, err, apperrors.ErrResourceInUse)
}

func TestImageServiceDeleteRetainsStateAfterRegistryLookupFailure(t *testing.T) {
	ownerID, imageID, operationID := uuid.New(), uuid.New(), uuid.New()
	repo := newImageRepoMock(t)
	registry := coremocks.NewImageRegistryAPI(t)
	lifecycle := newImageLifecycleFake(operationID, imageID, ownerID)
	svc, err := NewImageService(ImageServiceDeps{
		Repository:   repo,
		Docker:       coremocks.NewImageDockerAPI(t),
		Registry:     registry,
		Containers:   coremocks.NewImageContainerRepository(t),
		Config:       staticConfig{},
		Lifecycle:    lifecycle,
		RegistryAuth: unlockedRepositoryFence(t),
	})
	require.NoError(t, err)
	cause := apperrors.Wrap(apperrors.ErrUnavailable, "registry lookup failed", errors.New("connection refused"))
	repo.ImageRepository.EXPECT().GetByID(mock.Anything, imageID).Return(model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo:latest", Status: model.ImageStatusDeleting}, nil)
	registry.EXPECT().GetImageSizeAndDigest(mock.Anything, ownerID.String()+"_demo", "latest").Return(int64(0), "", cause)

	err = svc.ExecuteQueuedImageOperation(context.Background(), operationID, imageID)
	require.ErrorIs(t, err, cause)
	require.Equal(t, model.OperationStatusPending, lifecycle.op.Status)
}

func TestImageServiceDeleteRetriesAfterRegistryTimeout(t *testing.T) {
	for _, phase := range []string{"lookup", "delete"} {
		t.Run(phase, func(t *testing.T) {
			ownerID, imageID, operationID := uuid.New(), uuid.New(), uuid.New()
			repo := newImageRepoMock(t)
			registry := coremocks.NewImageRegistryAPI(t)
			dockerAPI := coremocks.NewImageDockerAPI(t)
			lifecycle := newImageLifecycleFake(operationID, imageID, ownerID)
			svc, err := NewImageService(ImageServiceDeps{
				Repository:   repo,
				Docker:       dockerAPI,
				Registry:     registry,
				Containers:   coremocks.NewImageContainerRepository(t),
				Config:       staticConfig{},
				Lifecycle:    lifecycle,
				RegistryAuth: unlockedRepositoryFence(t),
			})
			require.NoError(t, err)
			cause := apperrors.Wrap(apperrors.ErrTimeout, "registry attempt timed out", context.DeadlineExceeded)
			repo.ImageRepository.EXPECT().GetByID(mock.Anything, imageID).Return(model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo:latest", Status: model.ImageStatusDeleting}, nil)
			repoName := ownerID.String() + "_demo"
			if phase == "lookup" {
				registry.EXPECT().GetImageSizeAndDigest(mock.Anything, repoName, "latest").Return(int64(0), "", cause).Once()
			} else {
				registry.EXPECT().GetImageSizeAndDigest(mock.Anything, repoName, "latest").Return(int64(0), "sha256:digest", nil).Once()
				registry.EXPECT().DeleteManifest(mock.Anything, repoName, "sha256:digest").Return(cause).Once()
			}

			err = svc.ExecuteQueuedImageOperation(context.Background(), operationID, imageID)
			require.ErrorIs(t, err, apperrors.ErrTimeout)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, model.OperationStatusPending, lifecycle.op.Status)
			repo.AssertNotCalled(t, "Delete", mock.Anything, imageID)

			registry.EXPECT().GetImageSizeAndDigest(mock.Anything, repoName, "latest").Return(int64(0), "sha256:digest", nil).Once()
			registry.EXPECT().DeleteManifest(mock.Anything, repoName, "sha256:digest").Return(nil).Once()
			dockerAPI.EXPECT().RemoveImage(mock.Anything, mock.Anything, false).Return(nil).Once()
			repo.ImageRepository.EXPECT().Delete(mock.Anything, imageID).Return(nil).Once()
			require.NoError(t, svc.ExecuteQueuedImageOperation(context.Background(), operationID, imageID))
			require.Equal(t, model.OperationStatusDone, lifecycle.op.Status)
		})
	}
}

// The lock pool has one connection shared with all admission, so a stalled
// Registry call may hold the repository lock only for the cleanup job budget.
func TestImageServiceDeleteBoundsRegistryWorkUnderRepositoryLock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ownerID, imageID, operationID := uuid.New(), uuid.New(), uuid.New()
		repo := newImageRepoMock(t)
		registry := coremocks.NewImageRegistryAPI(t)
		lifecycle := newImageLifecycleFake(operationID, imageID, ownerID)
		fence := coremocks.NewImageRepositoryFencer(t)
		started := time.Now()
		var heldFor time.Duration
		fence.EXPECT().FenceAndLockRepository(mock.Anything, ownerID.String()+"_demo").Return(func() { heldFor = time.Since(started) }, nil)
		cfg := staticConfig{}.Get()
		cfg.RegistryCleanupJobTimeoutSeconds = 20
		configManager := coremocks.NewConfigManager(t)
		configManager.EXPECT().Get().Return(cfg)
		svc, err := NewImageService(ImageServiceDeps{
			Repository:   repo,
			Docker:       coremocks.NewImageDockerAPI(t),
			Registry:     registry,
			Containers:   coremocks.NewImageContainerRepository(t),
			Config:       configManager,
			Lifecycle:    lifecycle,
			RegistryAuth: fence,
		})
		require.NoError(t, err)
		repo.ImageRepository.EXPECT().GetByID(mock.Anything, imageID).Return(model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo:latest", Status: model.ImageStatusDeleting}, nil)
		registry.EXPECT().GetImageSizeAndDigest(mock.Anything, ownerID.String()+"_demo", "latest").
			RunAndReturn(func(ctx context.Context, _, _ string) (int64, string, error) {
				<-ctx.Done()
				return 0, "", apperrors.Wrap(apperrors.ErrTimeout, "registry attempt timed out", ctx.Err())
			})

		err = svc.ExecuteQueuedImageOperation(context.Background(), operationID, imageID)
		require.ErrorIs(t, err, apperrors.ErrTimeout)
		require.Equal(t, 20*time.Second, heldFor)
		require.Equal(t, model.OperationStatusPending, lifecycle.op.Status)
	})
}

func TestImageServiceDeleteContinuesWhenRegistryDigestMissing(t *testing.T) {
	ownerID := uuid.New()
	imageID := uuid.New()
	operationID := uuid.New()
	repo := newImageRepoMock(t)
	registry := coremocks.NewImageRegistryAPI(t)
	containerRepo := coremocks.NewImageContainerRepository(t)
	dockerAPI := coremocks.NewImageDockerAPI(t)
	lifecycle := newImageLifecycleFake(operationID, imageID, ownerID)
	svc, err := NewImageService(ImageServiceDeps{
		Repository:   repo,
		Docker:       dockerAPI,
		Registry:     registry,
		Containers:   containerRepo,
		Config:       staticConfig{},
		Lifecycle:    lifecycle,
		RegistryAuth: unlockedRepositoryFence(t),
	})
	require.NoError(t, err)

	repo.ImageRepository.EXPECT().GetByID(mock.Anything, imageID).Return(model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo:latest"}, nil)
	registry.EXPECT().GetImageSizeAndDigest(mock.Anything, ownerID.String()+"_demo", "latest").Return(int64(0), "", apperrors.ErrNotFound)
	dockerAPI.EXPECT().RemoveImage(mock.Anything, mock.MatchedBy(func(tag string) bool {
		return strings.HasSuffix(tag, "demo:latest")
	}), false).Return(nil)
	repo.ImageRepository.EXPECT().Delete(mock.Anything, imageID).Return(nil)

	err = svc.ExecuteQueuedImageOperation(context.Background(), operationID, imageID)
	require.NoError(t, err)
	require.Equal(t, model.OperationStatusDone, lifecycle.op.Status)
}

func TestImageServiceDeleteMarksImageErrorOnRepositoryDeleteFailure(t *testing.T) {
	ownerID := uuid.New()
	imageID := uuid.New()
	operationID := uuid.New()
	deleteErr := errors.New("delete failed")
	repo := newImageRepoMock(t)
	registry := coremocks.NewImageRegistryAPI(t)
	containerRepo := coremocks.NewImageContainerRepository(t)
	dockerAPI := coremocks.NewImageDockerAPI(t)
	lifecycle := newImageLifecycleFake(operationID, imageID, ownerID)
	svc, err := NewImageService(ImageServiceDeps{
		Repository:   repo,
		Docker:       dockerAPI,
		Registry:     registry,
		Containers:   containerRepo,
		Config:       staticConfig{},
		Lifecycle:    lifecycle,
		RegistryAuth: unlockedRepositoryFence(t),
	})
	require.NoError(t, err)

	repo.ImageRepository.EXPECT().GetByID(mock.Anything, imageID).Return(model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo:latest"}, nil)
	registry.EXPECT().GetImageSizeAndDigest(mock.Anything, ownerID.String()+"_demo", "latest").Return(int64(0), "", apperrors.ErrNotFound)
	dockerAPI.EXPECT().RemoveImage(mock.Anything, mock.MatchedBy(func(tag string) bool {
		return strings.HasSuffix(tag, "demo:latest")
	}), false).Return(nil)
	repo.ImageRepository.EXPECT().Delete(mock.Anything, imageID).Return(deleteErr)
	repo.ImageRepository.EXPECT().MarkStatusError(mock.Anything, imageID, model.ImageStatusError, deleteErr).Return(nil)

	err = svc.ExecuteQueuedImageOperation(context.Background(), operationID, imageID)
	require.ErrorIs(t, err, deleteErr)
	require.Equal(t, model.OperationStatusFailed, lifecycle.op.Status)
}

func TestImageServiceDeleteReturnsTerminalStatusWriteFailure(t *testing.T) {
	deleteErr := errors.New("delete failed")
	markErr := errors.New("mark image error failed")
	writeErr := apperrors.Wrap(apperrors.ErrUnavailable, "database unavailable", errors.New("connection reset"))
	tests := []struct {
		name       string
		deleteErr  error
		markErr    error
		wantErrors []error
	}{
		{name: "done", wantErrors: []error{writeErr}},
		{name: "failed", deleteErr: deleteErr, wantErrors: []error{deleteErr, writeErr}},
		{name: "image error mark", deleteErr: deleteErr, markErr: markErr, wantErrors: []error{deleteErr, markErr, writeErr}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ownerID, imageID, operationID := uuid.New(), uuid.New(), uuid.New()
			repo := newImageRepoMock(t)
			registry := coremocks.NewImageRegistryAPI(t)
			dockerAPI := coremocks.NewImageDockerAPI(t)
			lifecycle := newImageLifecycleFake(operationID, imageID, ownerID)
			lifecycle.completeErr = writeErr
			svc, err := NewImageService(ImageServiceDeps{
				Repository:   repo,
				Docker:       dockerAPI,
				Registry:     registry,
				Containers:   coremocks.NewImageContainerRepository(t),
				Config:       staticConfig{},
				Lifecycle:    lifecycle,
				RegistryAuth: unlockedRepositoryFence(t),
			})
			require.NoError(t, err)
			repo.ImageRepository.EXPECT().GetByID(mock.Anything, imageID).Return(model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo:latest"}, nil)
			registry.EXPECT().GetImageSizeAndDigest(mock.Anything, ownerID.String()+"_demo", "latest").Return(int64(0), "", apperrors.ErrNotFound)
			dockerAPI.EXPECT().RemoveImage(mock.Anything, mock.Anything, false).Return(nil)
			repo.ImageRepository.EXPECT().Delete(mock.Anything, imageID).Return(tt.deleteErr)
			if tt.deleteErr != nil {
				repo.ImageRepository.EXPECT().MarkStatusError(mock.Anything, imageID, model.ImageStatusError, tt.deleteErr).Return(tt.markErr)
			}

			err = svc.ExecuteQueuedImageOperation(context.Background(), operationID, imageID)
			for _, want := range tt.wantErrors {
				require.ErrorIs(t, err, want)
			}
			require.Equal(t, model.OperationStatusRunning, lifecycle.op.Status)
		})
	}
}

func TestImageServiceListUsesScopeOwnerFilter(t *testing.T) {
	ownerID := uuid.New()
	repo := newImageRepoMock(t)
	svc, err := NewImageService(ImageServiceDeps{
		Repository:   repo,
		Docker:       coremocks.NewImageDockerAPI(t),
		Registry:     coremocks.NewImageRegistryAPI(t),
		Containers:   coremocks.NewImageContainerRepository(t),
		Config:       staticConfig{},
		Lifecycle:    &imageLifecycleFake{},
		RegistryAuth: unlockedRepositoryFence(t),
	})
	require.NoError(t, err)
	var userOpts model.ListOptions
	var adminOpts model.ListOptions

	repo.ImageRepository.EXPECT().
		List(mock.Anything, mock.AnythingOfType("model.ListOptions")).
		Run(func(ctx context.Context, opts model.ListOptions) {
			userOpts = opts
		}).
		Return([]model.Image(nil), 0, nil).
		Once()
	repo.ImageRepository.EXPECT().
		List(mock.Anything, mock.AnythingOfType("model.ListOptions")).
		Run(func(ctx context.Context, opts model.ListOptions) {
			adminOpts = opts
		}).
		Return([]model.Image(nil), 0, nil).
		Once()

	_, _, err = svc.List(accessscope.WithUserScope(context.Background(), ownerID, "", ""), 50, 10)
	require.NoError(t, err)
	require.NotNil(t, userOpts.OwnerID)
	require.Equal(t, ownerID, *userOpts.OwnerID)
	require.Equal(t, 50, userOpts.Limit)
	require.Equal(t, 10, userOpts.Offset)

	_, _, err = svc.List(accessscope.WithAdminScope(context.Background(), uuid.New(), "", ""), 20, 0)
	require.NoError(t, err)
	require.Nil(t, adminOpts.OwnerID)
}

type imageRepoMock struct {
	*coremocks.ImageRepository
}

func newImageRepoMock(t *testing.T) *imageRepoMock {
	t.Helper()
	return &imageRepoMock{
		ImageRepository: coremocks.NewImageRepository(t),
	}
}

type imageLifecycleFake struct {
	op          model.ResourceOperation
	completeErr error
}

func newImageLifecycleFake(operationID, imageID, ownerID uuid.UUID) *imageLifecycleFake {
	return &imageLifecycleFake{
		op: model.ResourceOperation{
			ID:           operationID,
			ResourceType: model.ResourceTypeImage,
			ResourceID:   imageID,
			OwnerID:      ownerID,
			Operation:    model.OperationDelete,
			Status:       model.OperationStatusPending,
		},
	}
}

func (f *imageLifecycleFake) QueueImageDelete(ctx context.Context, id uuid.UUID, op model.ResourceOperation, outbox model.ResourceLifecycleOutbox) error {
	f.op = op
	return nil
}

func (f *imageLifecycleFake) HasActiveOperation(ctx context.Context, resourceType string, resourceID uuid.UUID) (bool, error) {
	return false, nil
}

func (f *imageLifecycleFake) ClaimPendingOperation(ctx context.Context, id uuid.UUID, maxAttempts int) (model.ResourceOperation, bool, error) {
	if f.op.Status != model.OperationStatusPending {
		return f.op, false, nil
	}
	f.op.Status = model.OperationStatusRunning
	f.op.Attempts++
	return f.op, true, nil
}

func (f *imageLifecycleFake) CompleteOperation(ctx context.Context, id uuid.UUID, status string, cause error) error {
	if f.completeErr != nil {
		return f.completeErr
	}
	f.op.Status = status
	return nil
}

func (f *imageLifecycleFake) RequeueOperation(ctx context.Context, id uuid.UUID, cause error) error {
	f.op.Status = model.OperationStatusPending
	return nil
}
