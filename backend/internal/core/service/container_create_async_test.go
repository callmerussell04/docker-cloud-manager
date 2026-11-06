package service_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Once Expose records the new generation, the database points at the new
// Docker container. A later write failure must leave that container in place,
// still remove the previous generation, and reach the queue consumer with the
// operation left running for restart recovery.
func TestContainerServiceQueuedExposeKeepsRecordedGenerationWhenLaterWriteFails(t *testing.T) {
	writeErr := apperrors.Wrap(apperrors.ErrUnavailable, "database unavailable", errors.New("connection reset"))
	for _, tc := range []struct {
		name       string
		faults     coretest.ContainerStoreFaults
		wantStatus string
	}{
		{name: "status write fails", faults: coretest.ContainerStoreFaults{UpdateStatus: coretest.FailAlways(writeErr)}, wantStatus: model.ContainerStatusExposing},
		{name: "completion write fails", faults: coretest.ContainerStoreFaults{CompleteOperation: coretest.FailAlways(writeErr)}, wantStatus: model.ContainerStatusRunning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			ownerID, containerID, operationID := uuid.New(), uuid.New(), uuid.New()
			store := newContainerStore()
			saveContainer(t, store, model.Container{
				ID:               containerID,
				OwnerID:          ownerID,
				Name:             "web",
				DockerID:         "old-docker",
				DockerGeneration: 1,
				Status:           model.ContainerStatusRunning,
				DesiredStatus:    model.ContainerStatusRunning,
			})
			op := model.ResourceOperation{ID: operationID, ResourceType: model.ResourceTypeContainer, ResourceID: containerID, OwnerID: ownerID, Operation: model.OperationExpose}
			outbox := model.ContainerLifecycleOutbox{ID: uuid.New(), OperationID: operationID, ContainerID: containerID}
			require.NoError(t, store.QueueContainerOperation(ctx, containerID, model.ContainerStatusExposing, model.ContainerStatusRunning, op, outbox))
			store.SetFaults(tc.faults)

			dockerAPI := coremocks.NewContainerDockerAPI(t)
			dockerAPI.EXPECT().InspectContainer(mock.Anything, "old-docker").Return(model.ContainerInspection{Name: "/usr_web", Image: "nginx:latest"}, nil)
			dockerAPI.EXPECT().CreateContainer(mock.Anything, mock.Anything).Return("new-docker", nil)
			dockerAPI.EXPECT().StartContainer(mock.Anything, "new-docker").Return(nil)
			var mu sync.Mutex
			var removed []string
			dockerAPI.EXPECT().RemoveContainer(mock.Anything, mock.Anything, true).RunAndReturn(func(_ context.Context, dockerID string, _ bool) error {
				mu.Lock()
				defer mu.Unlock()
				removed = append(removed, dockerID)
				return nil
			}).Maybe()

			svc := newTestContainerService(t, service.ContainerServiceDeps{
				Repository: store,
				Docker:     dockerAPI,
				Config:     testConfig(),
				Logger:     slog.Default(),
			})
			msg := containerLifecycleMessage(operationID, containerID, ownerID, model.OperationExpose)
			msg.PreviousStatus = model.ContainerStatusRunning
			msg.DomainPrefix = "app"
			msg.InternalPort = 8080
			worker := service.NewContainerCreateWorker(svc, testConfig(), slog.New(slog.DiscardHandler))

			err := worker.HandleMessage(ctx, msg)

			require.ErrorIs(t, err, writeErr, "the failed write reaches the queue consumer")
			store.SetFaults(coretest.ContainerStoreFaults{})
			got, getErr := store.GetByID(ctx, containerID)
			require.NoError(t, getErr)
			require.Equal(t, "new-docker", got.DockerID)
			require.Equal(t, 2, got.DockerGeneration)
			require.Equal(t, []string{"old-docker"}, removed, "only the previous generation is removed")
			gotOp, opErr := store.GetOperationByID(ctx, operationID)
			require.NoError(t, opErr)
			require.Equal(t, model.OperationStatusRunning, gotOp.Status, "the operation stays running until restart recovery")
			require.Equal(t, tc.wantStatus, got.Status, "no error status is written over the recorded generation")
			require.Nil(t, got.LastError)
		})
	}
}
