package resource_flow_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

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
			adapter, err := registry.NewSecureAdapter(server.URL, server.Client(), func(context.Context, string, bool) (string, error) {
				return "test-token", nil
			})
			require.NoError(t, err)
			dockerAPI := coremocks.NewImageDockerAPI(t)
			dockerAPI.EXPECT().RemoveImage(mock.Anything, mock.Anything, false).Return(nil).Once()
			cfg := coretest.StaticConfig{Config: coretest.IntegrationConfig()}
			images := service.NewImageService(repos.Images, dockerAPI, adapter, repos.Containers, cfg)
			lifecycle := repository.NewResourceLifecycleRepository(repos.DB)
			images.SetLifecycleRepository(lifecycle)
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
				images = service.NewImageService(repos.Images, dockerAPI, adapter, repos.Containers, cfg)
				images.SetLifecycleRepository(lifecycle)
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
