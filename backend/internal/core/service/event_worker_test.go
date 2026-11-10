package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestEventWorkerMarksMissingContainerOnSync(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		containerID := uuid.New()
		repo := coremocks.NewEventContainerRepo(t)
		dockerAPI := coremocks.NewEventDockerAPI(t)
		repo.EXPECT().GetNonExited(mock.Anything).Return([]model.Container{{
			ID:       containerID,
			DockerID: "missing-docker",
			Status:   model.ContainerStatusRunning,
		}}, nil).Once()
		dockerAPI.EXPECT().InspectContainer(mock.Anything, "missing-docker").Return(model.ContainerInspection{}, cerrdefs.ErrNotFound).Once()
		repo.EXPECT().MarkStatusError(mock.Anything, containerID, model.ContainerStatusMissing, mock.Anything).Return(nil).Once()
		dockerAPI.EXPECT().ListenEvents(mock.Anything).RunAndReturn(closedEventStreams).Maybe()

		runUntilSettled(t, service.NewEventWorker(repo, nil, dockerAPI, nil, testConfig(), slog.Default()).Run)
	})
}

func TestEventWorkerSkipsCreatingContainerWithActiveOperation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		containerID := uuid.New()
		repo := coremocks.NewEventContainerRepo(t)
		dockerAPI := coremocks.NewEventDockerAPI(t)
		repo.EXPECT().GetNonExited(mock.Anything).Return([]model.Container{{
			ID:     containerID,
			Status: model.ContainerStatusCreating,
		}}, nil).Once()
		repo.EXPECT().HasActiveOperation(mock.Anything, model.ResourceTypeContainer, containerID).Return(true, nil)
		dockerAPI.EXPECT().ListenEvents(mock.Anything).RunAndReturn(closedEventStreams).Maybe()

		// The strict mocks fail the test if the sync inspects or marks the container.
		runUntilSettled(t, service.NewEventWorker(repo, nil, dockerAPI, nil, testConfig(), slog.Default()).Run)
	})
}

func TestEventWorkerMarksCreatingContainerWithoutActiveOperationMissing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		containerID := uuid.New()
		repo := coremocks.NewEventContainerRepo(t)
		dockerAPI := coremocks.NewEventDockerAPI(t)
		repo.EXPECT().GetNonExited(mock.Anything).Return([]model.Container{{
			ID:     containerID,
			Status: model.ContainerStatusCreating,
		}}, nil).Once()
		repo.EXPECT().HasActiveOperation(mock.Anything, model.ResourceTypeContainer, containerID).Return(false, nil)
		repo.EXPECT().MarkStatusError(mock.Anything, containerID, model.ContainerStatusMissing, mock.Anything).Return(nil).Once()
		dockerAPI.EXPECT().ListenEvents(mock.Anything).RunAndReturn(closedEventStreams).Maybe()

		runUntilSettled(t, service.NewEventWorker(repo, nil, dockerAPI, nil, testConfig(), slog.Default()).Run)
	})
}

func TestEventWorkerMarksMissingVolumeOnSync(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		volumeID := uuid.New()
		containerRepo := coremocks.NewEventContainerRepo(t)
		volumeRepo := coremocks.NewEventVolumeRepo(t)
		dockerAPI := coremocks.NewEventDockerAPI(t)
		containerRepo.EXPECT().GetNonExited(mock.Anything).Return([]model.Container(nil), nil).Once()
		volumeRepo.EXPECT().GetReconcileCandidates(mock.Anything).Return([]model.Volume{{
			ID:         volumeID,
			DockerName: "missing-volume",
			Status:     model.VolumeStatusAvailable,
		}}, nil).Once()
		dockerAPI.EXPECT().InspectVolume(mock.Anything, "missing-volume").Return(model.VolumeInspection{}, cerrdefs.ErrNotFound).Once()
		volumeRepo.EXPECT().MarkStatusError(mock.Anything, volumeID, model.VolumeStatusMissing, mock.Anything).Return(nil).Once()
		dockerAPI.EXPECT().ListenEvents(mock.Anything).RunAndReturn(closedEventStreams).Maybe()

		runUntilSettled(t, service.NewEventWorker(containerRepo, volumeRepo, dockerAPI, nil, testConfig(), slog.Default()).Run)
	})
}

func TestEventWorkerLogsFailedStatusWrites(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		containerID, volumeID := uuid.New(), uuid.New()
		writeErr := errors.New("database unavailable")
		repo := coremocks.NewEventContainerRepo(t)
		volumeRepo := coremocks.NewEventVolumeRepo(t)
		dockerAPI := coremocks.NewEventDockerAPI(t)
		settings := testSystemConfig()
		settings.EventSyncIntervalSeconds = 3600
		repo.EXPECT().GetNonExited(mock.Anything).Return([]model.Container{{ID: containerID, DockerID: "missing-docker", Status: model.ContainerStatusRunning}}, nil).Once()
		dockerAPI.EXPECT().InspectContainer(mock.Anything, "missing-docker").Return(model.ContainerInspection{}, cerrdefs.ErrNotFound)
		repo.EXPECT().MarkStatusError(mock.Anything, containerID, model.ContainerStatusMissing, mock.Anything).Return(writeErr)
		volumeRepo.EXPECT().GetReconcileCandidates(mock.Anything).Return([]model.Volume{{ID: volumeID, DockerName: "missing-volume", Status: model.VolumeStatusAvailable}}, nil).Once()
		dockerAPI.EXPECT().InspectVolume(mock.Anything, "missing-volume").Return(model.VolumeInspection{}, cerrdefs.ErrNotFound)
		volumeRepo.EXPECT().MarkStatusError(mock.Anything, volumeID, model.VolumeStatusMissing, mock.Anything).Return(writeErr)
		events := make(chan model.ContainerEvent)
		dockerAPI.EXPECT().ListenEvents(mock.Anything).RunAndReturn(func(ctx context.Context) (<-chan model.ContainerEvent, <-chan error) {
			return events, make(chan error)
		}).Once()
		repo.EXPECT().UpdateObservedStatusByDockerID(mock.Anything, "docker-id", model.ContainerStatusRunning, mock.Anything, mock.Anything, mock.Anything).Return(writeErr)
		var logs bytes.Buffer
		worker := service.NewEventWorker(repo, volumeRepo, dockerAPI, nil, coretest.StaticConfig{Config: settings}, slog.New(slog.NewJSONHandler(&logs, nil)))
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			worker.Run(ctx)
		}()
		synctest.Wait()
		events <- model.ContainerEvent{Type: "container", Action: "start", DockerID: "docker-id"}
		synctest.Wait()
		cancel()
		<-done

		errorsByMessage := map[string]string{}
		for line := range bytes.Lines(logs.Bytes()) {
			var entry struct {
				Msg   string `json:"msg"`
				Error string `json:"error"`
			}
			require.NoError(t, json.Unmarshal(line, &entry))
			errorsByMessage[entry.Msg] = entry.Error
		}
		for _, msg := range []string{"failed to record container status", "failed to record volume status", "failed to record container event status"} {
			require.Equal(t, writeErr.Error(), errorsByMessage[msg], msg)
		}
		require.NotContains(t, errorsByMessage, "docker container missing during state sync")
		require.NotContains(t, errorsByMessage, "docker volume missing during state sync")
	})
}

// runUntilSettled runs a worker loop inside the caller's synctest bubble until
// every goroutine blocks, then stops it and waits for it to return. Time does
// not advance, so only work due at startup runs.
func runUntilSettled(t *testing.T, run func(context.Context)) {
	t.Helper()
	stop := startInBubble(t, run)
	synctest.Wait()
	stop()
}

// startInBubble starts a worker loop and returns a function that cancels it
// and waits for it to return.
func startInBubble(t *testing.T, run func(context.Context)) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}

func closedEventStreams(ctx context.Context) (<-chan model.ContainerEvent, <-chan error) {
	eventCh := make(chan model.ContainerEvent)
	errCh := make(chan error)
	close(eventCh)
	close(errCh)
	return eventCh, errCh
}

func TestEventWorkerReleasesSubscriptions(t *testing.T) {
	for _, termination := range []string{"error_with_messages_open", "errors_closed", "messages_closed", "shutdown_during_backoff"} {
		t.Run(termination, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				repo := coremocks.NewEventContainerRepo(t)
				dockerAPI := coremocks.NewEventDockerAPI(t)
				settings := testSystemConfig()
				settings.EventSyncIntervalSeconds = 1
				settings.EventReconnectDelaySeconds = 2
				var syncs int
				repo.EXPECT().GetNonExited(mock.Anything).RunAndReturn(func(ctx context.Context) ([]model.Container, error) {
					assert.NoError(t, ctx.Err(), "reconciliation must retain the application context")
					syncs++
					return nil, nil
				})

				type subscription struct {
					ctx      context.Context
					messages chan model.ContainerEvent
					errors   chan error
					stopped  chan struct{}
				}
				var subscriptions []subscription
				dockerAPI.EXPECT().ListenEvents(mock.Anything).RunAndReturn(func(ctx context.Context) (<-chan model.ContainerEvent, <-chan error) {
					if len(subscriptions) > 0 {
						previous := subscriptions[len(subscriptions)-1]
						assert.ErrorIs(t, previous.ctx.Err(), context.Canceled, "cancel before replacing the subscription")
						select {
						case <-previous.stopped:
						default:
							t.Error("previous subscription is still running at reconnect")
						}
					}
					sub := subscription{ctx, make(chan model.ContainerEvent), make(chan error, 1), make(chan struct{})}
					subscriptions = append(subscriptions, sub)
					go func() {
						<-ctx.Done()
						close(sub.stopped)
					}()
					return sub.messages, sub.errors
				})
				worker := service.NewEventWorker(repo, nil, dockerAPI, nil, coretest.StaticConfig{Config: settings}, slog.Default())
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan struct{})
				go func() {
					defer close(done)
					worker.Run(ctx)
				}()
				synctest.Wait()
				require.Len(t, subscriptions, 1)

				for cycle := 0; cycle < 5; cycle++ {
					sub := subscriptions[cycle]
					switch termination {
					case "error_with_messages_open", "shutdown_during_backoff":
						sub.errors <- errors.New("daemon disconnected")
					case "errors_closed":
						close(sub.errors)
					case "messages_closed":
						close(sub.messages)
					}
					synctest.Wait()
					require.ErrorIs(t, sub.ctx.Err(), context.Canceled)
					select {
					case <-sub.stopped:
					default:
						t.Fatal("abandoned subscription did not stop during backoff")
					}
					time.Sleep(time.Second)
					synctest.Wait()
					require.Len(t, subscriptions, cycle+1, "respect reconnect delay")
					require.NoError(t, ctx.Err())
					time.Sleep(time.Second)
					synctest.Wait()
					require.Len(t, subscriptions, cycle+2)
					require.NoError(t, subscriptions[cycle+1].ctx.Err())
				}
				require.GreaterOrEqual(t, syncs, 11, "periodic reconciliation continues through reconnects")
				if termination == "shutdown_during_backoff" {
					subscriptions[len(subscriptions)-1].errors <- errors.New("daemon disconnected")
					synctest.Wait()
				}
				cancel()
				synctest.Wait()
				select {
				case <-done:
				default:
					t.Fatal("event worker did not stop")
				}
				time.Sleep(3 * time.Second)
				synctest.Wait()
				require.Len(t, subscriptions, 6, "shutdown must not reconnect")
				for _, sub := range subscriptions {
					select {
					case <-sub.stopped:
					default:
						t.Fatal("subscription survived application shutdown")
					}
				}
			})
		})
	}
}
