package service_test

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Queued delete removes the removed runtime's image from the Docker cache
// after the container, best effort and without force. Docker refusing because
// another container or tag still uses the image is expected: the delete
// succeeds and nothing is logged as an error.
func TestQueuedDeleteRemovesTheContainersImage(t *testing.T) {
	conflict := apperrors.Wrap(apperrors.ErrConflict, "image is in use", cerrdefs.ErrConflict)
	cases := []struct {
		name       string
		removeErr  error
		wantLogged bool
	}{
		{name: "unused image is removed"},
		{name: "image in use is kept quietly", removeErr: cerrdefs.ErrConflict},
		{name: "wrapped refusal is kept quietly", removeErr: conflict},
		{name: "image already gone", removeErr: cerrdefs.ErrNotFound},
		{name: "other failure is logged", removeErr: errDockerRejected, wantLogged: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := newContainerStore()
			c, op := queueLifecycleOperation(t, store, model.OperationDelete, false)
			docker := coremocks.NewContainerDockerAPI(t)
			// The adapter creates every container from the resolved image ID,
			// so the inspected image is that ID: Docker refuses its removal
			// while a container uses it or several tags name it.
			docker.EXPECT().InspectContainer(mock.Anything, recordedDocker).Return(model.ContainerInspection{Image: "sha256:0ab1"}, nil).Once()
			docker.EXPECT().RemoveContainer(mock.Anything, recordedDocker, true).Return(nil).Once()
			docker.EXPECT().RemoveImage(mock.Anything, "sha256:0ab1", false).Return(tc.removeErr).Once()
			docker.EXPECT().RemoveNetwork(mock.Anything, mock.Anything).Return(nil).Maybe()
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

			require.NoError(t, runLifecycle(t, store, docker, logger, lifecycleMessage(c, op)))

			_, err := store.GetByID(ctx, c.ID)
			require.ErrorIs(t, err, apperrors.ErrNotFound)
			gotOp, err := store.GetOperationByID(ctx, op.ID)
			require.NoError(t, err)
			require.Equal(t, model.OperationStatusDone, gotOp.Status)
			removeContainer := slices.Index(dockerCalls(docker), dockerCall{"RemoveContainer", recordedDocker})
			removeImage := slices.Index(dockerCalls(docker), dockerCall{"RemoveImage", "sha256:0ab1"})
			require.Less(t, removeContainer, removeImage, "the image is removed after the container")
			if tc.wantLogged {
				require.Contains(t, logs.String(), "level\":\"WARN")
			} else {
				require.Empty(t, logs.String())
			}
		})
	}
}

// A delete of a container that has no Docker runtime makes no image call: the
// strict Docker double fails on any inspection or image removal.
func TestQueuedDeleteWithoutDockerRuntimeMakesNoImageCall(t *testing.T) {
	ctx := context.Background()
	store := newContainerStore()
	c := model.Container{
		ID: uuid.New(), OwnerID: uuid.New(), Name: "web", ImageTag: "nginx:latest",
		Status: model.ContainerStatusMissing, DesiredStatus: model.ContainerStatusExited,
		BaseMemoryReservation: 128 << 20, BaseCPUReservation: 500,
	}
	saveContainer(t, store, c)
	op := model.ResourceOperation{ID: uuid.New(), ResourceType: model.ResourceTypeContainer, ResourceID: c.ID, OwnerID: c.OwnerID, Operation: model.OperationDelete}
	require.NoError(t, store.QueueContainerOperation(ctx, c.ID, model.ContainerStatusDeleting, model.ContainerStatusDeleting, op,
		model.ContainerLifecycleOutbox{ID: uuid.New(), OperationID: op.ID, ContainerID: c.ID}))
	docker := coremocks.NewContainerDockerAPI(t)
	docker.EXPECT().RemoveNetwork(mock.Anything, mock.Anything).Return(nil).Maybe()
	msg := lifecycleMessage(c, op)
	msg.PreviousStatus = model.ContainerStatusMissing

	require.NoError(t, runLifecycle(t, store, docker, slog.New(slog.DiscardHandler), msg))

	_, err := store.GetByID(ctx, c.ID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}
