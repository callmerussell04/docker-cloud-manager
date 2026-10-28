package resource_flow_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/infrastructure/registry"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/resourcequeue"
	coremocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/core/service"
	"github.com/callmerussell04/docker-cloud-manager/tests/testutil/coretest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestQueuedImageDeletionRetainsOwnershipUntilRegistryConfirmsRemoval(t *testing.T) {
	for _, tc := range []struct {
		name          string
		lookupStatus  int
		deleteStatus  int
		missingDigest bool
		invalidBody   bool
		retry         bool
	}{
		{name: "lookup failure then retry", lookupStatus: http.StatusServiceUnavailable, retry: true},
		{name: "delete failure then retry", deleteStatus: http.StatusInternalServerError, retry: true},
		{name: "missing digest then retry", missingDigest: true, retry: true},
		{name: "invalid response then retry", invalidBody: true, retry: true},
		{name: "lookup not found", lookupStatus: http.StatusNotFound},
		{name: "delete not found", deleteStatus: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repos := coretest.OpenCoreRepositories(t)
			ownerID, imageID := uuid.New(), uuid.New()
			require.NoError(t, repos.Images.Save(ctx, model.Image{ID: imageID, OwnerID: ownerID, Tag: "demo:latest"}))
			var failedAttempt atomic.Bool
			failedAttempt.Store(tc.retry)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					if tc.lookupStatus != 0 && (failedAttempt.Load() || !tc.retry) {
						w.WriteHeader(tc.lookupStatus)
						return
					}
					if !tc.missingDigest || !failedAttempt.Load() {
						w.Header().Set("Docker-Content-Digest", "sha256:abc")
					}
					if tc.invalidBody && failedAttempt.Load() {
						_, _ = w.Write([]byte("private malformed response"))
						return
					}
					_, _ = w.Write([]byte(`{"config":{"size":100},"layers":[]}`))
				case http.MethodDelete:
					if tc.deleteStatus != 0 && (failedAttempt.Load() || !tc.retry) {
						w.WriteHeader(tc.deleteStatus)
						return
					}
					w.WriteHeader(http.StatusAccepted)
				default:
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			}))
			t.Cleanup(server.Close)
			adapter, err := registry.NewSecureAdapter(server.URL, server.Client(), func(context.Context, string, model.RegistryTrustedAccess) (string, error) {
				return "test-token", nil
			})
			require.NoError(t, err)
			dockerAPI := coremocks.NewImageDockerAPI(t)
			dockerAPI.EXPECT().RemoveImage(mock.Anything, mock.Anything, false).Return(nil).Once()
			cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
			lifecycle := repository.NewResourceLifecycleRepository(repos.DB)
			images, err := service.NewImageService(service.ImageServiceDeps{
				Repository:   repos.Images,
				Docker:       dockerAPI,
				Registry:     adapter,
				Containers:   repos.Containers,
				Config:       cfg,
				Lifecycle:    lifecycle,
				RegistryAuth: repos.RegistryAuthorization(),
			})
			require.NoError(t, err)
			require.ErrorIs(t, images.Delete(coretest.UserContext(uuid.New()), imageID), apperrors.ErrNotFound)
			require.NoError(t, images.Delete(coretest.UserContext(ownerID), imageID))
			outbox, err := lifecycle.LeasePendingResourceOutbox(ctx, 10)
			require.NoError(t, err)
			require.Len(t, outbox, 1)
			var message resourcequeue.LifecycleMessage
			require.NoError(t, json.Unmarshal(outbox[0].Payload, &message))
			require.Equal(t, ownerID.String(), message.OwnerID)
			require.NoError(t, lifecycle.MarkResourceOutboxPublished(ctx, outbox[0].ID))
			operationID := uuid.MustParse(message.OperationID)
			worker := service.NewResourceLifecycleWorker(nil, images, coretest.DiscardLogger())
			if tc.retry {
				err := worker.HandleMessage(ctx, message)
				require.ErrorIs(t, err, apperrors.ErrUnavailable)
				image, err := repos.Images.GetByID(ctx, imageID)
				require.NoError(t, err)
				require.Equal(t, ownerID, image.OwnerID)
				require.Equal(t, model.ImageStatusDeleting, image.Status)
				op, err := lifecycle.GetOperationByID(ctx, operationID)
				require.NoError(t, err)
				require.Equal(t, model.OperationStatusPending, op.Status)
				require.Equal(t, 1, op.Attempts)
				require.NotNil(t, op.LastError)
				require.NotEmpty(t, *op.LastError)
				require.NotContains(t, *op.LastError, "private malformed response")
				dockerAPI.AssertNotCalled(t, "RemoveImage", mock.Anything, mock.Anything, mock.Anything)
				// Reconstruct the service to prove retry uses durable state.
				images, err = service.NewImageService(service.ImageServiceDeps{
					Repository:   repos.Images,
					Docker:       dockerAPI,
					Registry:     adapter,
					Containers:   repos.Containers,
					Config:       cfg,
					Lifecycle:    lifecycle,
					RegistryAuth: repos.RegistryAuthorization(),
				})
				require.NoError(t, err)
				worker = service.NewResourceLifecycleWorker(nil, images, coretest.DiscardLogger())
				failedAttempt.Store(false)
			}
			require.NoError(t, worker.HandleMessage(ctx, message))
			_, err = repos.Images.GetByID(ctx, imageID)
			require.ErrorIs(t, err, apperrors.ErrNotFound)
			op, err := lifecycle.GetOperationByID(ctx, operationID)
			require.NoError(t, err)
			require.Equal(t, model.OperationStatusDone, op.Status)
			wantAttempts := 1
			if tc.retry {
				wantAttempts = 2
			}
			require.Equal(t, wantAttempts, op.Attempts)
			require.NoError(t, worker.HandleMessage(ctx, message))
			require.Equal(t, 1, countTable(t, repos.DB, "resource_lifecycle_queue_outbox"))
		})
	}
}

func TestQueuedImageDeletionKeepsAdmissionAvailableWhileFencing(t *testing.T) {
	ctx := context.Background()
	f := newFencedImageDeletion(t)
	sibling := f.claimSibling(t, "demo:v2")
	message := f.queueDelete(t)
	result := make(chan error, 1)
	go func() { result <- f.worker.HandleMessage(ctx, message) }()
	require.Eventually(t, func() bool {
		op, err := f.lifecycle.GetOperationByID(ctx, uuid.MustParse(message.OperationID))
		return err == nil && op.Status == model.OperationStatusRunning
	}, 5*time.Second, 10*time.Millisecond)

	// The deletion waits for the sibling build for as long as it runs; admission
	// must keep its single lock connection throughout that wait.
	for range 3 {
		time.Sleep(200 * time.Millisecond)
		admitCtx, cancel := context.WithTimeout(ctx, time.Second)
		release, err := f.repos.Containers.AcquireOwnerCapacityLock(admitCtx, f.ownerID)
		require.NoError(t, err, "container admission must not wait behind a fencing deletion")
		release()
		unlock, err := f.repos.Builds.AcquireCapacityLock(admitCtx)
		require.NoError(t, err, "build admission must not wait behind a fencing deletion")
		unlock()
		cancel()
	}
	select {
	case err := <-result:
		t.Fatalf("deletion finished while a sibling build was still running: %v", err)
	default:
	}

	// Docker cache cleanup runs after the Registry step and must not hold the lock.
	var cleanupAdmission error
	f.docker.EXPECT().RemoveImage(mock.Anything, mock.Anything, false).Run(func(context.Context, string, bool) {
		admitCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		release, err := f.repos.Containers.AcquireOwnerCapacityLock(admitCtx, f.ownerID)
		if err == nil {
			release()
		}
		cleanupAdmission = err
	}).Return(nil).Once()
	f.finishBuild(t, sibling)
	require.NoError(t, waitResult(t, result))
	require.NoError(t, cleanupAdmission, "the repository lock must be released before Docker cleanup")
	f.requireDeleted(t, message, 1)
}

func TestQueuedImageDeletionRequeuesWhenAClaimSlipsInDuringTheFence(t *testing.T) {
	ctx := context.Background()
	f := newFencedImageDeletion(t)
	previous := f.claimSibling(t, "demo:v2")
	f.finishBuild(t, previous)
	// Leave three seconds of the token fence open.
	_, err := f.repos.DB.ExecContext(ctx, `UPDATE registry_build_credentials SET last_issued_at=clock_timestamp()-interval '178 seconds' WHERE build_id=$1`, previous)
	require.NoError(t, err)
	message := f.queueDelete(t)
	result := make(chan error, 1)
	go func() { result <- f.worker.HandleMessage(ctx, message) }()
	require.Eventually(t, func() bool {
		var revoked bool
		err := f.repos.DB.QueryRowContext(ctx, `SELECT revoked_at IS NOT NULL FROM registry_build_credentials WHERE build_id=$1`, previous).Scan(&revoked)
		return err == nil && revoked
	}, 5*time.Second, 10*time.Millisecond)
	admitCtx, cancelAdmit := context.WithTimeout(ctx, time.Second)
	release, err := f.repos.Containers.AcquireOwnerCapacityLock(admitCtx, f.ownerID)
	cancelAdmit()
	require.NoError(t, err, "admission must not wait behind the token fence")
	release()

	// A new publisher can claim while the deletion waits out the fence unlocked.
	late := f.claimSibling(t, "demo:v3")
	require.ErrorIs(t, waitResult(t, result), apperrors.ErrUnavailable)
	operationID := uuid.MustParse(message.OperationID)
	op, err := f.lifecycle.GetOperationByID(ctx, operationID)
	require.NoError(t, err)
	require.Equal(t, model.OperationStatusPending, op.Status)
	require.Equal(t, 1, op.Attempts)
	image, err := f.repos.Images.GetByID(ctx, f.imageID)
	require.NoError(t, err)
	require.Equal(t, model.ImageStatusDeleting, image.Status)
	require.Zero(t, f.deletes.Load(), "Registry deletion must not race the late publisher")

	f.finishBuild(t, late)
	f.docker.EXPECT().RemoveImage(mock.Anything, mock.Anything, false).Return(nil).Once()
	blocked := f.queueSibling(t, "demo:v4")
	claimed := make(chan error, 1)
	hook := func(*http.Request) {
		hash := sha256.Sum256([]byte("blocked-" + blocked.String()))
		_, started, err := f.grants.ClaimBuild(ctx, blocked, hash[:], time.Minute)
		if err == nil && started {
			err = errors.New("claim started during Registry deletion")
		}
		claimed <- err
	}
	f.onDelete.Store(&hook)
	require.NoError(t, f.worker.HandleMessage(ctx, message))
	require.ErrorIs(t, <-claimed, apperrors.ErrUnavailable, "claims stay excluded during Registry deletion")
	f.requireDeleted(t, message, 2)
}

func TestQueuedImageDeletionReleasesTheRepositoryLockWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := newFencedImageDeletion(t)
	message := f.queueDelete(t)
	reached := make(chan struct{})
	hook := func(r *http.Request) {
		close(reached)
		<-r.Context().Done()
	}
	f.onDelete.Store(&hook)
	result := make(chan error, 1)
	go func() { result <- f.worker.HandleMessage(ctx, message) }()
	<-reached
	sibling := f.queueSibling(t, "demo:v2")
	hash := sha256.Sum256([]byte("sibling-secret"))
	_, _, err := f.grants.ClaimBuild(context.Background(), sibling, hash[:], time.Minute)
	require.ErrorIs(t, err, apperrors.ErrUnavailable, "claims stay excluded during Registry deletion")

	cancel()
	require.Error(t, waitResult(t, result))
	admitCtx, cancelAdmit := context.WithTimeout(context.Background(), time.Second)
	defer cancelAdmit()
	release, err := f.repos.Containers.AcquireOwnerCapacityLock(admitCtx, f.ownerID)
	require.NoError(t, err, "cancellation must release the lock connection")
	release()
	_, started, err := f.grants.ClaimBuild(context.Background(), sibling, hash[:], time.Minute)
	require.NoError(t, err)
	require.True(t, started, "cancellation must release the repository lock")
	op, err := f.lifecycle.GetOperationByID(context.Background(), uuid.MustParse(message.OperationID))
	require.NoError(t, err)
	require.Equal(t, model.OperationStatusPending, op.Status, "a canceled deletion stays retryable")
}

type fencedImageDeletion struct {
	repos     coretest.CoreRepositories
	grants    *repository.RegistryGrantRepository
	lifecycle *repository.ResourceLifecycleRepository
	images    *service.ImageService
	docker    *coremocks.ImageDockerAPI
	worker    *service.ResourceLifecycleWorker
	ownerID   uuid.UUID
	imageID   uuid.UUID
	deletes   *atomic.Int32
	onDelete  *atomic.Pointer[func(*http.Request)]
}

func newFencedImageDeletion(t *testing.T) *fencedImageDeletion {
	t.Helper()
	ctx := context.Background()
	f := &fencedImageDeletion{repos: coretest.OpenCoreRepositories(t), ownerID: uuid.New(), imageID: uuid.New(), deletes: &atomic.Int32{}, onDelete: &atomic.Pointer[func(*http.Request)]{}}
	f.grants = f.repos.Grants
	f.lifecycle = repository.NewResourceLifecycleRepository(f.repos.DB)
	require.NoError(t, f.repos.Images.Save(ctx, model.Image{ID: f.imageID, OwnerID: f.ownerID, Tag: "demo:latest"}))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Docker-Content-Digest", "sha256:abc")
			_, _ = w.Write([]byte(`{"config":{"size":100},"layers":[]}`))
		case http.MethodDelete:
			if hook := f.onDelete.Load(); hook != nil {
				(*hook)(r)
			}
			f.deletes.Add(1)
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)
	adapter, err := registry.NewSecureAdapter(server.URL, server.Client(), func(context.Context, string, model.RegistryTrustedAccess) (string, error) {
		return "test-token", nil
	})
	require.NoError(t, err)
	f.docker = coremocks.NewImageDockerAPI(t)
	f.images, err = service.NewImageService(service.ImageServiceDeps{
		Repository:   f.repos.Images,
		Docker:       f.docker,
		Registry:     adapter,
		Containers:   f.repos.Containers,
		Config:       coretest.StaticConfig{Config: coretest.IntegrationConfig()},
		Lifecycle:    f.lifecycle,
		RegistryAuth: f.repos.RegistryAuthorization(),
	})
	require.NoError(t, err)
	f.worker = service.NewResourceLifecycleWorker(nil, f.images, coretest.DiscardLogger())
	return f
}

func (f *fencedImageDeletion) queueSibling(t *testing.T, tag string) uuid.UUID {
	t.Helper()
	imageID, buildID := uuid.New(), uuid.New()
	require.NoError(t, f.repos.Builds.CreateQueuedBuild(context.Background(),
		model.Image{ID: imageID, OwnerID: f.ownerID, Tag: tag},
		model.Build{ID: buildID, ImageID: imageID, OwnerID: f.ownerID, Status: model.BuildStatusPending, StartedAt: time.Now()},
		model.BuildQueueOutbox{ID: uuid.New(), BuildID: buildID, Exchange: "test", RoutingKey: "test", Payload: []byte(`{}`)}))
	return buildID
}

func (f *fencedImageDeletion) claimSibling(t *testing.T, tag string) uuid.UUID {
	t.Helper()
	buildID := f.queueSibling(t, tag)
	hash := sha256.Sum256([]byte("secret-" + buildID.String()))
	_, started, err := f.grants.ClaimBuild(context.Background(), buildID, hash[:], time.Minute)
	require.NoError(t, err)
	require.True(t, started)
	return buildID
}

// finishBuild ends a sibling build whose credential is outside the token fence.
func (f *fencedImageDeletion) finishBuild(t *testing.T, buildID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	_, err := f.repos.DB.ExecContext(ctx, `UPDATE builds SET status=$2, finished_at=clock_timestamp() WHERE id=$1`, buildID, model.BuildStatusSuccess)
	require.NoError(t, err)
	_, err = f.repos.DB.ExecContext(ctx, `UPDATE registry_build_credentials SET last_issued_at=clock_timestamp()-interval '5 minutes' WHERE build_id=$1`, buildID)
	require.NoError(t, err)
}

func (f *fencedImageDeletion) queueDelete(t *testing.T) resourcequeue.LifecycleMessage {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, f.images.Delete(coretest.UserContext(f.ownerID), f.imageID))
	outbox, err := f.lifecycle.LeasePendingResourceOutbox(ctx, 10)
	require.NoError(t, err)
	require.Len(t, outbox, 1)
	var message resourcequeue.LifecycleMessage
	require.NoError(t, json.Unmarshal(outbox[0].Payload, &message))
	require.NoError(t, f.lifecycle.MarkResourceOutboxPublished(ctx, outbox[0].ID))
	return message
}

func (f *fencedImageDeletion) requireDeleted(t *testing.T, message resourcequeue.LifecycleMessage, attempts int) {
	t.Helper()
	_, err := f.repos.Images.GetByID(context.Background(), f.imageID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	op, err := f.lifecycle.GetOperationByID(context.Background(), uuid.MustParse(message.OperationID))
	require.NoError(t, err)
	require.Equal(t, model.OperationStatusDone, op.Status)
	require.Equal(t, attempts, op.Attempts)
	require.Equal(t, int32(1), f.deletes.Load())
}

func waitResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("image deletion did not finish")
		return nil
	}
}
