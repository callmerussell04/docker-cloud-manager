package service_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// runLifecycle runs one queued operation through the lifecycle executor over
// store and docker, logging to logger.
func runLifecycle(t *testing.T, store *containerStore, docker *coremocks.ContainerDockerAPI, logger *slog.Logger, msg queue.ContainerLifecycleMessage) error {
	t.Helper()
	cfg := lifecycleConfig(24)
	images := newContainerImageRepository(t)
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository: store,
		Images:     images,
		Docker:     docker,
		Config:     cfg,
		Logger:     logger,
	})
	return service.NewContainerCreateWorker(svc, cfg, logger).HandleMessage(context.Background(), msg)
}

// dockerCall is one call the Docker double received: its method and the
// Docker container or image it named.
type dockerCall struct{ method, target string }

func dockerCalls(d *coremocks.ContainerDockerAPI) []dockerCall {
	calls := make([]dockerCall, 0, len(d.Calls))
	for _, call := range d.Calls {
		target := ""
		if call.Method != "CreateContainer" {
			target = call.Arguments.String(1)
		}
		calls = append(calls, dockerCall{method: call.Method, target: target})
	}
	return calls
}

// Exposing a running container never runs two generations on the same
// volumes: Docker creates the new generation, stops the old one with the
// configured timeout and only then starts the new one, and the old one is
// removed only once the new one is recorded.
func TestQueuedExposeStopsTheOldGenerationBeforeStartingTheNew(t *testing.T) {
	store := newContainerStore()
	c, op := queueLifecycleOperation(t, store, model.OperationExpose, false)
	docker := coremocks.NewContainerDockerAPI(t)
	docker.EXPECT().InspectContainer(mock.Anything, recordedDocker).Return(model.ContainerInspection{Name: "/usr_web", Image: "nginx:latest"}, nil).Once()
	docker.EXPECT().CreateContainer(mock.Anything, mock.Anything).Return(exposedDocker, nil).Once()
	docker.EXPECT().StopContainer(mock.Anything, recordedDocker, 1).Return(nil).Once()
	docker.EXPECT().StartContainer(mock.Anything, exposedDocker).Return(nil).Once()
	docker.EXPECT().RemoveContainer(mock.Anything, recordedDocker, true).RunAndReturn(func(ctx context.Context, _ string, _ bool) error {
		got, err := store.GetByID(ctx, c.ID)
		require.NoError(t, err)
		require.Equal(t, exposedDocker, got.DockerID, "the old generation is removed only after the new one is recorded")
		return nil
	}).Once()
	docker.EXPECT().InspectContainer(mock.Anything, exposedDocker).Return(model.ContainerInspection{State: model.ContainerState{Running: true, Status: "running"}}, nil).Once()

	require.NoError(t, runLifecycle(t, store, docker, slog.New(slog.DiscardHandler), lifecycleMessage(c, op)))

	require.GreaterOrEqual(t, len(docker.Calls), 4)
	require.Equal(t, []dockerCall{
		{"InspectContainer", recordedDocker},
		{"CreateContainer", ""},
		{"StopContainer", recordedDocker},
		{"StartContainer", exposedDocker},
	}, dockerCalls(docker)[:4])
}

// The status recorded after expose is what Docker reports for the new
// generation, with its exit code and cause, not an assumed running.
func TestQueuedExposeRecordsTheNewGenerationsObservedStatus(t *testing.T) {
	exitCode := 1
	cases := []struct {
		name         string
		state        model.ContainerState
		inspectErr   error
		wantStatus   string
		wantExitCode *int
		wantLastErr  bool
	}{
		{name: "running", state: model.ContainerState{Running: true, Status: "running"}, wantStatus: model.ContainerStatusRunning},
		{name: "exited with an error", state: model.ContainerState{Status: "exited", ExitCode: 1}, wantStatus: model.ContainerStatusError, wantExitCode: &exitCode, wantLastErr: true},
		{name: "killed for memory", state: model.ContainerState{Status: "exited", ExitCode: 1, OOMKilled: true}, wantStatus: model.ContainerStatusError, wantExitCode: &exitCode, wantLastErr: true},
		// State sync corrects the status once Docker answers again.
		{name: "unreadable new generation keeps the previous status", inspectErr: errDockerDown, wantStatus: model.ContainerStatusRunning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newContainerStore()
			c, op := queueLifecycleOperation(t, store, model.OperationExpose, false)
			docker := coremocks.NewContainerDockerAPI(t)
			docker.EXPECT().InspectContainer(mock.Anything, recordedDocker).Return(model.ContainerInspection{Name: "/usr_web", Image: "nginx:latest"}, nil).Once()
			docker.EXPECT().CreateContainer(mock.Anything, mock.Anything).Return(exposedDocker, nil).Once()
			docker.EXPECT().StopContainer(mock.Anything, recordedDocker, 1).Return(nil).Once()
			docker.EXPECT().StartContainer(mock.Anything, exposedDocker).Return(nil).Once()
			docker.EXPECT().RemoveContainer(mock.Anything, recordedDocker, true).Return(nil).Once()
			docker.EXPECT().InspectContainer(mock.Anything, exposedDocker).Return(model.ContainerInspection{State: tc.state}, tc.inspectErr).Once()

			require.NoError(t, runLifecycle(t, store, docker, slog.New(slog.DiscardHandler), lifecycleMessage(c, op)))

			got, err := store.GetByID(context.Background(), c.ID)
			require.NoError(t, err)
			require.Equal(t, exposedDocker, got.DockerID)
			require.Equal(t, tc.wantStatus, got.Status)
			require.Equal(t, tc.wantExitCode, got.LastExitCode)
			require.Equal(t, tc.wantLastErr, got.LastError != nil)
		})
	}
}

// A container that should not run is exposed without stopping or starting
// anything, and keeps its status.
func TestQueuedExposeOfAStoppedContainerNeitherStopsNorStarts(t *testing.T) {
	ctx := context.Background()
	store := newContainerStore()
	c := model.Container{
		ID: uuid.New(), OwnerID: uuid.New(), Name: "web", ImageTag: "nginx:latest",
		DockerID: recordedDocker, DockerGeneration: 1,
		Status: model.ContainerStatusExited, DesiredStatus: model.ContainerStatusExited,
		BaseMemoryReservation: 128 << 20, BaseCPUReservation: 500,
	}
	saveContainer(t, store, c)
	op := model.ResourceOperation{ID: uuid.New(), ResourceType: model.ResourceTypeContainer, ResourceID: c.ID, OwnerID: c.OwnerID, Operation: model.OperationExpose}
	require.NoError(t, store.QueueContainerOperation(ctx, c.ID, model.ContainerStatusExposing, model.ContainerStatusExited, op,
		model.ContainerLifecycleOutbox{ID: uuid.New(), OperationID: op.ID, ContainerID: c.ID}))
	docker := coremocks.NewContainerDockerAPI(t)
	docker.EXPECT().InspectContainer(mock.Anything, recordedDocker).Return(model.ContainerInspection{Name: "/usr_web", Image: "nginx:latest"}, nil).Once()
	docker.EXPECT().CreateContainer(mock.Anything, mock.Anything).Return(exposedDocker, nil).Once()
	docker.EXPECT().RemoveContainer(mock.Anything, recordedDocker, true).Return(nil).Once()
	msg := lifecycleMessage(c, op)
	msg.PreviousStatus = model.ContainerStatusExited

	require.NoError(t, runLifecycle(t, store, docker, slog.New(slog.DiscardHandler), msg))

	got, err := store.GetByID(ctx, c.ID)
	require.NoError(t, err)
	require.Equal(t, exposedDocker, got.DockerID)
	require.Equal(t, model.ContainerStatusExited, got.Status)
}
