package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
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
	svc := NewImageService(repo, dockerAPI, registry, containerRepo, staticConfig{})
	lifecycle := newImageLifecycleFake(operationID, imageID, ownerID)
	svc.SetLifecycleRepository(lifecycle)

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

	err := svc.ExecuteQueuedImageOperation(context.Background(), operationID, imageID)
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
	svc := NewImageService(repo, coremocks.NewImageDockerAPI(t), coremocks.NewImageRegistryAPI(t), containerRepo, staticConfig{})

	repo.ImageRepository.EXPECT().GetByID(mock.Anything, imageID).Return(model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo:latest"}, nil)
	containerRepo.EXPECT().IsImageInUse(mock.Anything, ownerID, "demo:latest").Return(true, nil)

	err := svc.Delete(accessscope.WithUserScope(context.Background(), ownerID, "", ""), imageID)
	require.ErrorIs(t, err, apperrors.ErrResourceInUse)
}

func TestImageServiceDeleteContinuesWhenRegistryDigestMissing(t *testing.T) {
	ownerID := uuid.New()
	imageID := uuid.New()
	operationID := uuid.New()
	repo := newImageRepoMock(t)
	registry := coremocks.NewImageRegistryAPI(t)
	containerRepo := coremocks.NewImageContainerRepository(t)
	dockerAPI := coremocks.NewImageDockerAPI(t)
	svc := NewImageService(repo, dockerAPI, registry, containerRepo, staticConfig{})
	lifecycle := newImageLifecycleFake(operationID, imageID, ownerID)
	svc.SetLifecycleRepository(lifecycle)

	repo.ImageRepository.EXPECT().GetByID(mock.Anything, imageID).Return(model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo:latest"}, nil)
	registry.EXPECT().GetImageSizeAndDigest(mock.Anything, ownerID.String()+"_demo", "latest").Return(int64(0), "", apperrors.ErrNotFound)
	dockerAPI.EXPECT().RemoveImage(mock.Anything, mock.MatchedBy(func(tag string) bool {
		return strings.HasSuffix(tag, "demo:latest")
	}), false).Return(nil)
	repo.ImageRepository.EXPECT().Delete(mock.Anything, imageID).Return(nil)

	err := svc.ExecuteQueuedImageOperation(context.Background(), operationID, imageID)
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
	svc := NewImageService(repo, dockerAPI, registry, containerRepo, staticConfig{})
	lifecycle := newImageLifecycleFake(operationID, imageID, ownerID)
	svc.SetLifecycleRepository(lifecycle)

	repo.ImageRepository.EXPECT().GetByID(mock.Anything, imageID).Return(model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo:latest"}, nil)
	registry.EXPECT().GetImageSizeAndDigest(mock.Anything, ownerID.String()+"_demo", "latest").Return(int64(0), "", apperrors.ErrNotFound)
	dockerAPI.EXPECT().RemoveImage(mock.Anything, mock.MatchedBy(func(tag string) bool {
		return strings.HasSuffix(tag, "demo:latest")
	}), false).Return(nil)
	repo.ImageRepository.EXPECT().Delete(mock.Anything, imageID).Return(deleteErr)
	repo.ImageStateRepository.EXPECT().MarkStatusError(mock.Anything, imageID, model.ImageStatusError, deleteErr).Return(nil)

	err := svc.ExecuteQueuedImageOperation(context.Background(), operationID, imageID)
	require.ErrorIs(t, err, deleteErr)
	require.Equal(t, model.OperationStatusFailed, lifecycle.op.Status)
}

func TestImageServiceListUsesScopeOwnerFilter(t *testing.T) {
	ownerID := uuid.New()
	repo := newImageRepoMock(t)
	svc := NewImageService(repo, coremocks.NewImageDockerAPI(t), coremocks.NewImageRegistryAPI(t), coremocks.NewImageContainerRepository(t), staticConfig{})
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

	_, _, err := svc.List(accessscope.WithUserScope(context.Background(), ownerID, "", ""), 50, 10)
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
	*coremocks.ImageStateRepository
}

func newImageRepoMock(t *testing.T) *imageRepoMock {
	t.Helper()
	return &imageRepoMock{
		ImageRepository:      coremocks.NewImageRepository(t),
		ImageStateRepository: coremocks.NewImageStateRepository(t),
	}
}

type imageLifecycleFake struct {
	op model.ResourceOperation
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
	f.op.Status = status
	return nil
}

func (f *imageLifecycleFake) RequeueOperation(ctx context.Context, id uuid.UUID, cause error) error {
	f.op.Status = model.OperationStatusPending
	return nil
}
