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
		name            string
		statusErr       error
		completeErr     error
		wantStatusWrite string
	}{
		{name: "status write fails", statusErr: writeErr},
		{name: "completion write fails", completeErr: writeErr, wantStatusWrite: model.ContainerStatusRunning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ownerID, containerID, operationID := uuid.New(), uuid.New(), uuid.New()
			repo := newStartLifecycleRepoMock(t, ownerID, containerID, operationID)
			repo.pendingOp.Operation = model.OperationExpose
			repo.runningOp.Operation = model.OperationExpose
			repo.container.DockerID = "old-docker"
			repo.container.Status = model.ContainerStatusExposing
			repo.container.DockerGeneration = 1
			repo.updateStatusErr = tc.statusErr
			repo.completeErr = tc.completeErr

			dockerAPI := coremocks.NewContainerDockerAPI(t)
			dockerAPI.EXPECT().InspectContainer(mock.Anything, "old-docker").Return(model.ContainerInspection{Name: "/usr_web", Image: "nginx:latest"}, nil)
			dockerAPI.EXPECT().CreateContainer(mock.Anything, mock.Anything).Return("new-docker", nil)
			dockerAPI.EXPECT().StartContainer(mock.Anything, "new-docker").Return(nil)
			dockerAPI.EXPECT().StopContainer(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
			var mu sync.Mutex
			var removed []string
			dockerAPI.EXPECT().RemoveContainer(mock.Anything, mock.Anything, true).RunAndReturn(func(_ context.Context, dockerID string, _ bool) error {
				mu.Lock()
				defer mu.Unlock()
				removed = append(removed, dockerID)
				return nil
			}).Maybe()

			svc := newTestContainerService(t, service.ContainerServiceDeps{
				Repository: repo,
				Docker:     dockerAPI,
				Config:     staticConfig{},
				Logger:     slog.Default(),
			})
			msg := containerLifecycleMessage(operationID, containerID, ownerID, model.OperationExpose)
			msg.PreviousStatus = model.ContainerStatusRunning
			msg.DomainPrefix = "app"
			msg.InternalPort = 8080

			err := svc.ExecuteQueuedContainerOperation(context.Background(), operationID, containerID, msg)

			require.ErrorIs(t, err, writeErr, "the failed write reaches the queue consumer")
			require.Equal(t, "new-docker", repo.recordedDockerID)
			require.Equal(t, 2, repo.recordedGeneration)
			require.Equal(t, []string{"old-docker"}, removed, "only the previous generation is removed")
			require.Empty(t, repo.completedStatus, "the operation stays running until restart recovery")
			require.Equal(t, tc.wantStatusWrite, repo.updatedStatus, "no error status is written over the recorded generation")
		})
	}
}
