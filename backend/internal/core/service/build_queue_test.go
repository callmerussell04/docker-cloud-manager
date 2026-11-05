package service_test

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/buildqueue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBuildServiceCreateBuildJobCreatesOutboxPayload(t *testing.T) {
	repo := newBuildRepository(t)
	imageRepo := coremocks.NewBuildImageRepository(t)
	users := coremocks.NewUserInfoProvider(t)
	registry := coremocks.NewImageRegistryAPI(t)
	svc := newTestBuildService(t, service.BuildServiceDeps{
		Repository: repo,
		Images:     imageRepo,
		Registry:   registry,
		Users:      users,
	})
	ownerID := uuid.New()
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	var queuedImage model.Image
	var queuedBuild model.Build
	var queuedOutbox model.BuildQueueOutbox

	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaDiskMB: 1024}, nil)
	imageRepo.EXPECT().GetUserUsedDiskSpace(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().
		CreateQueuedBuild(mock.Anything, mock.AnythingOfType("model.Image"), mock.AnythingOfType("model.Build"), mock.AnythingOfType("model.BuildQueueOutbox")).
		Run(func(ctx context.Context, img model.Image, build model.Build, outbox model.BuildQueueOutbox) {
			queuedImage = img
			queuedBuild = build
			queuedOutbox = outbox
		}).
		Return(nil)

	buildID, imageID, err := svc.CreateBuildJob(ctx, "demo-app", "build-archives/source.zip", "build-logs/source.log", ".", "Dockerfile", map[string]string{"VERSION": "1"}, "request-id")
	require.NoError(t, err)
	require.Equal(t, buildID, queuedBuild.ID)
	require.Equal(t, imageID, queuedImage.ID)
	require.Equal(t, "build-logs/source.log", queuedBuild.LogFilePath)
	require.Equal(t, "build-archives/source.zip", queuedBuild.ArchiveObjectKey)

	var msg buildqueue.ImageBuildMessage
	require.NoError(t, json.Unmarshal(queuedOutbox.Payload, &msg))
	require.Equal(t, buildID.String(), msg.BuildID)
	require.Equal(t, imageID.String(), msg.ImageID)
	require.Equal(t, ownerID.String(), msg.OwnerID)
	require.Equal(t, "demo-app:latest", msg.Tag)
	require.Equal(t, buildqueue.ExchangeName, queuedOutbox.Exchange)
	require.Equal(t, buildqueue.RoutingKey, queuedOutbox.RoutingKey)
}

func TestBuildServiceCreateBuildFromArchiveUploadsAndCreatesJob(t *testing.T) {
	repo := newBuildRepository(t)
	imageRepo := coremocks.NewBuildImageRepository(t)
	objects := coremocks.NewBuildObjectStore(t)
	users := coremocks.NewUserInfoProvider(t)
	registry := coremocks.NewImageRegistryAPI(t)
	staged := coremocks.NewStagedObjectRepository(t)
	staged.EXPECT().ActiveBytesByOwner(mock.Anything, mock.Anything).Return(int64(0), nil)
	staged.EXPECT().Reserve(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	svc := newTestBuildService(t, service.BuildServiceDeps{
		Repository:    repo,
		Images:        imageRepo,
		Registry:      registry,
		Users:         users,
		ObjectStore:   objects,
		StagedObjects: staged,
	})
	ownerID := uuid.New()
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	var uploaded []string
	var queuedBuild model.Build

	objects.EXPECT().
		UploadStream(mock.Anything, mock.MatchedBy(func(objectKey string) bool {
			return strings.HasPrefix(objectKey, "build-archives/") && strings.HasSuffix(objectKey, ".zip")
		}), mock.Anything, int64(-1), "application/octet-stream").
		Run(func(ctx context.Context, objectKey string, reader io.Reader, size int64, contentType string) {
			uploaded = append(uploaded, objectKey)
			_, err := io.Copy(io.Discard, reader)
			require.NoError(t, err)
		}).
		Return(nil)
	objects.EXPECT().
		StatObject(mock.Anything, mock.MatchedBy(func(objectKey string) bool {
			return len(uploaded) == 1 && objectKey == uploaded[0]
		})).
		Return(objectstorage.ObjectInfo{Size: int64(len("archive"))}, nil).Once()
	staged.EXPECT().
		UpdateBytes(mock.Anything, mock.AnythingOfType("string"), int64(len("archive"))).
		Return(nil).Once()
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaDiskMB: 1024}, nil)
	imageRepo.EXPECT().GetUserUsedDiskSpace(mock.Anything, ownerID).Return(int64(0), nil)
	repo.EXPECT().
		CreateQueuedBuild(mock.Anything, mock.AnythingOfType("model.Image"), mock.AnythingOfType("model.Build"), mock.AnythingOfType("model.BuildQueueOutbox")).
		Run(func(ctx context.Context, img model.Image, build model.Build, outbox model.BuildQueueOutbox) {
			queuedBuild = build
		}).
		Return(nil)

	result, err := svc.CreateBuildFromArchive(ctx, service.BuildArchiveInput{
		Tag:         "demo-app",
		ContextDir:  ".",
		Dockerfile:  "Dockerfile",
		BuildArgs:   map[string]string{"VERSION": "1"},
		ArchiveName: "source.zip",
		Archive:     strings.NewReader("archive"),
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, result.BuildID)
	require.Equal(t, result.BuildID, queuedBuild.ID)
	require.Len(t, uploaded, 1)
	require.Equal(t, uploaded[0], queuedBuild.ArchiveObjectKey)
	require.NotEmpty(t, queuedBuild.LogFilePath)
}

func TestBuildServiceOpenBuildLogsChecksScopedAccessBeforeObjectStore(t *testing.T) {
	ownerID := uuid.New()
	buildID := uuid.New()
	repo := newBuildRepository(t)
	imageRepo := coremocks.NewBuildImageRepository(t)
	objects := coremocks.NewBuildObjectStore(t)
	svc := newTestBuildService(t, service.BuildServiceDeps{
		Repository:  repo,
		Images:      imageRepo,
		Registry:    coremocks.NewImageRegistryAPI(t),
		Users:       coremocks.NewUserInfoProvider(t),
		ObjectStore: objects,
	})
	ctx := accessscope.WithUserScope(context.Background(), uuid.New(), "", "")

	repo.EXPECT().GetByID(mock.Anything, buildID).Return(model.Build{
		ID:          buildID,
		ImageID:     uuid.New(),
		OwnerID:     ownerID,
		LogFilePath: "build-logs/source.log",
	}, nil)

	_, err := svc.OpenBuildLogs(ctx, buildID)
	require.Error(t, err)
	objects.AssertNotCalled(t, "OpenObject", mock.Anything, mock.Anything)
}

func TestBuildServiceStartBuildRecordSkipsTerminalBuild(t *testing.T) {
	buildID := uuid.New()
	repo := newBuildRepository(t)
	auth := coremocks.NewBuildRegistryAuthorizer(t)
	svc := newTestBuildService(t, service.BuildServiceDeps{
		Repository:   repo,
		Images:       coremocks.NewBuildImageRepository(t),
		Registry:     coremocks.NewImageRegistryAPI(t),
		Users:        coremocks.NewUserInfoProvider(t),
		RegistryAuth: auth,
	})

	auth.EXPECT().ClaimBuild(mock.Anything, buildID, mock.Anything).Return(model.Build{ID: buildID, ImageID: uuid.New(), Status: model.BuildStatusSuccess}, false, nil)

	_, started, err := svc.StartBuildRecord(context.Background(), buildID)
	require.NoError(t, err)
	require.False(t, started)
	repo.AssertNotCalled(t, "UpdateStatus", mock.Anything, mock.Anything, mock.Anything)
}

func TestBuildServiceCompleteBuildRecordSkipsTerminalBuild(t *testing.T) {
	buildID := uuid.New()
	imageID := uuid.New()
	repo := newBuildRepository(t)
	imageRepo := coremocks.NewBuildImageRepository(t)
	svc := newTestBuildService(t, service.BuildServiceDeps{
		Repository: repo,
		Images:     imageRepo,
		Registry:   coremocks.NewImageRegistryAPI(t),
		Users:      coremocks.NewUserInfoProvider(t),
	})

	repo.EXPECT().GetByID(mock.Anything, buildID).Return(model.Build{ID: buildID, ImageID: imageID, Status: model.BuildStatusFailed}, nil)

	require.NoError(t, svc.CompleteBuildRecord(context.Background(), buildID, imageID, model.BuildStatusSuccess, 0))
	imageRepo.AssertNotCalled(t, "UpdateBuildAndImageSizeTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	imageRepo.AssertNotCalled(t, "MarkBuildFailedAndDeleteImageTx", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestBuildServiceCompleteBuildRecordRedeliveryQueuesCleanupOnlyForUnsuccessfulBuilds(t *testing.T) {
	for _, tc := range []struct {
		persisted string
		pushedTag model.RegistryTagDisposition
	}{
		{model.BuildStatusCanceled, model.RegistryTagDelete},
		{model.BuildStatusFailedQuotaExceeded, model.RegistryTagDelete},
		{model.BuildStatusFailedInternal, model.RegistryTagDelete},
		{model.BuildStatusSuccess, model.RegistryTagKeep},
	} {
		t.Run(tc.persisted, func(t *testing.T) {
			buildID := uuid.New()
			imageID := uuid.New()
			repo := newBuildRepository(t)
			auth := coremocks.NewBuildRegistryAuthorizer(t)
			svc := newTestBuildService(t, service.BuildServiceDeps{
				Repository:   repo,
				Images:       coremocks.NewBuildImageRepository(t),
				Registry:     coremocks.NewImageRegistryAPI(t),
				Users:        coremocks.NewUserInfoProvider(t),
				RegistryAuth: auth,
			})
			repo.EXPECT().GetByID(mock.Anything, buildID).Return(model.Build{ID: buildID, ImageID: imageID, Status: tc.persisted}, nil)
			auth.EXPECT().RevokeBuild(mock.Anything, buildID, tc.pushedTag).Return(nil).Once()

			require.NoError(t, svc.CompleteBuildRecord(context.Background(), buildID, imageID, model.BuildStatusSuccess, 0))
		})
	}
}

func TestBuildServiceCompleteBuildRecordQuotaFailureDoesNotReturnRetryableError(t *testing.T) {
	buildID := uuid.New()
	imageID := uuid.New()
	ownerID := uuid.New()
	repo := newBuildRepository(t)
	imageRepo := coremocks.NewBuildImageRepository(t)
	objects := coremocks.NewBuildObjectStore(t)
	users := coremocks.NewUserInfoProvider(t)
	registry := coremocks.NewImageRegistryAPI(t)
	auth := coremocks.NewBuildRegistryAuthorizer(t)
	svc := newTestBuildService(t, service.BuildServiceDeps{
		Repository:   repo,
		Images:       imageRepo,
		Registry:     registry,
		Users:        users,
		ObjectStore:  objects,
		RegistryAuth: auth,
	})
	var failedStatus string
	var deleted []string

	repo.EXPECT().GetByID(mock.Anything, buildID).Return(model.Build{
		ID:               buildID,
		ImageID:          imageID,
		OwnerID:          ownerID,
		Status:           model.BuildStatusRunning,
		ArchiveObjectKey: "build-archives/source.zip",
	}, nil)
	imageRepo.EXPECT().GetByID(mock.Anything, imageID).Return(model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo-app:latest"}, nil)
	registry.EXPECT().GetImageSizeAndDigest(mock.Anything, ownerID.String()+"_demo-app", "latest").Return(int64(2*1024*1024), "digest", nil)
	users.EXPECT().GetUser(mock.Anything, ownerID).Return(model.UserInfo{ID: ownerID, QuotaDiskMB: 1}, nil)
	imageRepo.EXPECT().GetUserUsedDiskSpace(mock.Anything, ownerID).Return(int64(0), nil)
	imageRepo.EXPECT().GetReplacementImageSizeMB(mock.Anything, ownerID, "demo-app:latest", imageID).Return(int64(0), nil)
	imageRepo.EXPECT().
		MarkBuildFailedAndDeleteImageTx(mock.Anything, buildID, imageID, model.BuildStatusFailedQuotaExceeded).
		Run(func(ctx context.Context, gotBuildID, gotImageID uuid.UUID, status string) {
			failedStatus = status
		}).
		Return(nil)
	revoke := auth.EXPECT().RevokeBuild(mock.Anything, buildID, model.RegistryTagKeep).Return(nil).Once()
	// The rejected manifest is removed by the fenced Registry cleanup job.
	auth.EXPECT().RevokeBuild(mock.Anything, buildID, model.RegistryTagDelete).Return(nil).Once().NotBefore(revoke)
	objects.EXPECT().DeleteObject(mock.Anything, "build-archives/source.zip").Run(func(ctx context.Context, objectKey string) {
		deleted = append(deleted, objectKey)
	}).Return(nil)

	require.NoError(t, svc.CompleteBuildRecord(context.Background(), buildID, imageID, model.BuildStatusSuccess, 0))
	require.Equal(t, model.BuildStatusFailedQuotaExceeded, failedStatus)
	require.Equal(t, []string{"build-archives/source.zip"}, deleted)
}

func TestBuildServiceCancelStandaloneBuildDoesNotCancelDeployment(t *testing.T) {
	buildID := uuid.New()
	imageID := uuid.New()
	ownerID := uuid.New()
	repo := newBuildRepository(t)
	imageRepo := coremocks.NewBuildImageRepository(t)
	objects := coremocks.NewBuildObjectStore(t)
	deployments := coremocks.NewBuildDeploymentCanceler(t)
	svc := newTestBuildService(t, service.BuildServiceDeps{
		Repository:  repo,
		Images:      imageRepo,
		Registry:    coremocks.NewImageRegistryAPI(t),
		Users:       coremocks.NewUserInfoProvider(t),
		ObjectStore: objects,
		Deployments: deployments,
	})
	var failedStatus string
	var deleted []string

	repo.EXPECT().GetByID(mock.Anything, buildID).Return(model.Build{
		ID:               buildID,
		ImageID:          imageID,
		OwnerID:          ownerID,
		Status:           model.BuildStatusPending,
		ArchiveObjectKey: "build-archives/source.zip",
	}, nil)
	imageRepo.EXPECT().
		MarkBuildFailedAndDeleteImageTx(mock.Anything, buildID, imageID, model.BuildStatusCanceled).
		Run(func(ctx context.Context, gotBuildID, gotImageID uuid.UUID, status string) {
			failedStatus = status
		}).
		Return(nil)
	objects.EXPECT().DeleteObject(mock.Anything, "build-archives/source.zip").Run(func(ctx context.Context, objectKey string) {
		deleted = append(deleted, objectKey)
	}).Return(nil)

	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	require.NoError(t, svc.CancelBuildRecord(ctx, buildID))
	require.Equal(t, model.BuildStatusCanceled, failedStatus)
	require.Equal(t, []string{"build-archives/source.zip"}, deleted)
	deployments.AssertNotCalled(t, "CancelDeploymentForBuild", mock.Anything, mock.Anything, mock.Anything)
}

func TestBuildServiceCancelComposeBuildCancelsDeploymentOnce(t *testing.T) {
	buildID := uuid.New()
	imageID := uuid.New()
	ownerID := uuid.New()
	projectID := uuid.New()
	repo := newBuildRepository(t)
	imageRepo := coremocks.NewBuildImageRepository(t)
	deployments := coremocks.NewBuildDeploymentCanceler(t)
	svc := newTestBuildService(t, service.BuildServiceDeps{
		Repository:  repo,
		Images:      imageRepo,
		Registry:    coremocks.NewImageRegistryAPI(t),
		Users:       coremocks.NewUserInfoProvider(t),
		Deployments: deployments,
	})
	pendingBuild := model.Build{ID: buildID, ImageID: imageID, OwnerID: ownerID, ProjectID: &projectID, Status: model.BuildStatusPending}
	canceledBuild := pendingBuild
	canceledBuild.Status = model.BuildStatusCanceled
	var cancelCalls int

	repo.EXPECT().GetByID(mock.Anything, buildID).Return(pendingBuild, nil).Once()
	imageRepo.EXPECT().MarkBuildFailedAndDeleteImageTx(mock.Anything, buildID, imageID, model.BuildStatusCanceled).Return(nil).Once()
	deployments.EXPECT().
		CancelDeploymentForBuild(mock.Anything, projectID, buildID).
		Run(func(ctx context.Context, gotProjectID, gotBuildID uuid.UUID) {
			cancelCalls++
		}).
		Return(nil).
		Once()
	repo.EXPECT().GetByID(mock.Anything, buildID).Return(canceledBuild, nil).Once()

	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	require.NoError(t, svc.CancelBuildRecord(ctx, buildID))
	require.NoError(t, svc.CancelBuildRecord(ctx, buildID))
	require.Equal(t, 1, cancelCalls)
}
