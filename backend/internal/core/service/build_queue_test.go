package service_test

import (
	"context"
	"encoding/json"
	"errors"
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
	registry := coremocks.NewImageRegistryAPI(t)
	svc := newTestBuildService(t, service.BuildServiceDeps{
		Repository: repo,
		Images:     imageRepo,
		Registry:   registry,
	})
	ownerID := uuid.New()
	ctx := accessscope.WithUserScope(context.Background(), ownerID, "", "")
	var queuedImage model.Image
	var queuedBuild model.Build
	var queuedOutbox model.BuildQueueOutbox

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
	registry := coremocks.NewImageRegistryAPI(t)
	staged := coremocks.NewStagedObjectRepository(t)
	staged.EXPECT().ActiveBytesByOwner(mock.Anything, mock.Anything).Return(int64(0), nil)
	staged.EXPECT().Reserve(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	svc := newTestBuildService(t, service.BuildServiceDeps{
		Repository:    repo,
		Images:        imageRepo,
		Registry:      registry,
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
		Quotas:      testQuotas(t, nil, nil),
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
}

func TestBuildServiceStartBuildRecordSkipsTerminalBuild(t *testing.T) {
	buildID := uuid.New()
	repo := newBuildRepository(t)
	auth := coremocks.NewBuildRegistryAuthorizer(t)
	svc := newTestBuildService(t, service.BuildServiceDeps{
		Repository:   repo,
		Images:       coremocks.NewBuildImageRepository(t),
		Registry:     coremocks.NewImageRegistryAPI(t),
		Quotas:       testQuotas(t, nil, nil),
		RegistryAuth: auth,
	})

	auth.EXPECT().ClaimBuild(mock.Anything, buildID, mock.Anything, mock.Anything).Return(model.Build{ID: buildID, ImageID: uuid.New(), Status: model.BuildStatusSuccess}, false, nil)

	_, started, err := svc.StartBuildRecord(context.Background(), buildID)
	require.NoError(t, err)
	require.False(t, started)
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
		Quotas:     testQuotas(t, nil, nil),
	})

	repo.EXPECT().GetByID(mock.Anything, buildID).Return(model.Build{ID: buildID, ImageID: imageID, Status: model.BuildStatusFailed}, nil)

	require.NoError(t, svc.CompleteBuildRecord(context.Background(), buildID, imageID, model.BuildStatusSuccess, 0))
}

func TestBuildServiceCompleteBuildRecordRedeliveryQueuesCleanupOnlyForUnsuccessfulBuilds(t *testing.T) {
	for _, tc := range []struct {
		persisted string
		pushedTag model.RegistryTagDisposition
	}{
		{model.BuildStatusCanceled, model.RegistryTagDelete},
		{model.BuildStatusFailedQuotaExceeded, model.RegistryTagDelete},
		// A build the memory pressure worker stopped meets Builder's late
		// completion: the status stays and the manifest is cleaned up.
		{model.BuildStatusFailedResourceExhausted, model.RegistryTagDelete},
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
				Quotas:       testQuotas(t, nil, nil),
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
	registry := coremocks.NewImageRegistryAPI(t)
	auth := coremocks.NewBuildRegistryAuthorizer(t)
	svc := newTestBuildService(t, service.BuildServiceDeps{
		Repository:   repo,
		Images:       imageRepo,
		Registry:     registry,
		ObjectStore:  objects,
		RegistryAuth: auth,
		// The 2 MiB image does not fit a 1 MB disk quota.
		Quotas: testQuotas(t, nil, map[uuid.UUID]model.UserQuota{ownerID: {CPUMillicores: 1000, RAMMB: 1024, DiskMB: 1}}),
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
		Quotas:      testQuotas(t, nil, nil),
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
		Quotas:      testQuotas(t, nil, nil),
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

var errTestDatabase = errors.New("database unavailable")

// The memory pressure worker stops a running build by recording it
// failed_resource_exhausted: status first, then Registry revocation with tag
// deletion and archive cleanup. Builder's status poll then stops Kaniko. The
// stop never cascades a cancellation to the build's Compose deployment.
func TestBuildServiceStopBuildForResourceExhaustion(t *testing.T) {
	type world struct {
		repo     *coremocks.BuildRepository
		images   *coremocks.BuildImageRepository
		auth     *coremocks.BuildRegistryAuthorizer
		objects  *coremocks.BuildObjectStore
		svc      *service.BuildService
		build    model.Build
		recorded []string
	}
	newWorld := func(t *testing.T, status string) *world {
		projectID := uuid.New()
		w := &world{
			repo:    newBuildRepository(t),
			images:  coremocks.NewBuildImageRepository(t),
			auth:    coremocks.NewBuildRegistryAuthorizer(t),
			objects: coremocks.NewBuildObjectStore(t),
			build: model.Build{
				ID: uuid.New(), ImageID: uuid.New(), OwnerID: uuid.New(), ProjectID: &projectID,
				Status: status, ArchiveObjectKey: "build-archives/source.zip",
			},
		}
		w.svc = newTestBuildService(t, service.BuildServiceDeps{
			Repository:   w.repo,
			Images:       w.images,
			Registry:     coremocks.NewImageRegistryAPI(t),
			ObjectStore:  w.objects,
			RegistryAuth: w.auth,
			// No expectations: a platform stop never cancels a deployment.
			Deployments: coremocks.NewBuildDeploymentCanceler(t),
		})
		return w
	}

	t.Run("a running build is recorded, revoked with tag deletion and cleaned up", func(t *testing.T) {
		w := newWorld(t, model.BuildStatusRunning)
		stopped := w.build
		stopped.Status = model.BuildStatusFailedResourceExhausted
		w.repo.EXPECT().GetByID(mock.Anything, w.build.ID).Return(w.build, nil).Once()
		mark := w.images.EXPECT().
			MarkBuildFailedAndDeleteImageTx(mock.Anything, w.build.ID, w.build.ImageID, model.BuildStatusFailedResourceExhausted).
			Run(func(context.Context, uuid.UUID, uuid.UUID, string) { w.recorded = append(w.recorded, "status") }).
			Return(nil).Once()
		w.repo.EXPECT().GetByID(mock.Anything, w.build.ID).Return(stopped, nil).Once()
		w.auth.EXPECT().RevokeBuild(mock.Anything, w.build.ID, model.RegistryTagDelete).
			Run(func(context.Context, uuid.UUID, model.RegistryTagDisposition) {
				w.recorded = append(w.recorded, "revoke")
			}).
			Return(nil).Once().NotBefore(mark)
		w.objects.EXPECT().DeleteObject(mock.Anything, "build-archives/source.zip").
			Run(func(context.Context, string) { w.recorded = append(w.recorded, "archive") }).
			Return(nil).Once()

		ok, err := w.svc.StopBuildForResourceExhaustion(context.Background(), w.build.ID)

		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, []string{"status", "revoke", "archive"}, w.recorded)
	})

	for _, status := range []string{model.BuildStatusPending, model.BuildStatusSuccess, model.BuildStatusCanceled, model.BuildStatusFailedResourceExhausted} {
		t.Run("a "+status+" build is left alone", func(t *testing.T) {
			w := newWorld(t, status)
			w.repo.EXPECT().GetByID(mock.Anything, w.build.ID).Return(w.build, nil).Once()

			ok, err := w.svc.StopBuildForResourceExhaustion(context.Background(), w.build.ID)

			require.NoError(t, err)
			require.False(t, ok)
		})
	}

	t.Run("a build Builder completed first keeps its status and its tag", func(t *testing.T) {
		w := newWorld(t, model.BuildStatusRunning)
		succeeded := w.build
		succeeded.Status = model.BuildStatusSuccess
		w.repo.EXPECT().GetByID(mock.Anything, w.build.ID).Return(w.build, nil).Once()
		// The guarded write finds the build terminal and changes nothing.
		w.images.EXPECT().
			MarkBuildFailedAndDeleteImageTx(mock.Anything, w.build.ID, w.build.ImageID, model.BuildStatusFailedResourceExhausted).
			Return(nil).Once()
		w.repo.EXPECT().GetByID(mock.Anything, w.build.ID).Return(succeeded, nil).Once()

		ok, err := w.svc.StopBuildForResourceExhaustion(context.Background(), w.build.ID)

		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("a failed status write is returned and nothing else happens", func(t *testing.T) {
		w := newWorld(t, model.BuildStatusRunning)
		w.repo.EXPECT().GetByID(mock.Anything, w.build.ID).Return(w.build, nil).Once()
		w.images.EXPECT().
			MarkBuildFailedAndDeleteImageTx(mock.Anything, w.build.ID, w.build.ImageID, model.BuildStatusFailedResourceExhausted).
			Return(errTestDatabase).Once()

		ok, err := w.svc.StopBuildForResourceExhaustion(context.Background(), w.build.ID)

		require.ErrorIs(t, err, errTestDatabase)
		require.False(t, ok)
	})

	t.Run("a failed revocation still counts as stopped", func(t *testing.T) {
		w := newWorld(t, model.BuildStatusRunning)
		stopped := w.build
		stopped.Status = model.BuildStatusFailedResourceExhausted
		w.repo.EXPECT().GetByID(mock.Anything, w.build.ID).Return(w.build, nil).Once()
		w.images.EXPECT().
			MarkBuildFailedAndDeleteImageTx(mock.Anything, w.build.ID, w.build.ImageID, model.BuildStatusFailedResourceExhausted).
			Return(nil).Once()
		w.repo.EXPECT().GetByID(mock.Anything, w.build.ID).Return(stopped, nil).Once()
		// Builder's completion repeats the revocation.
		w.auth.EXPECT().RevokeBuild(mock.Anything, w.build.ID, model.RegistryTagDelete).Return(errTestDatabase).Once()
		w.objects.EXPECT().DeleteObject(mock.Anything, "build-archives/source.zip").Return(nil).Once()

		ok, err := w.svc.StopBuildForResourceExhaustion(context.Background(), w.build.ID)

		require.NoError(t, err)
		require.True(t, ok)
	})
}
