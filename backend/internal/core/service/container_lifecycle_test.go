package service_test

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/queue"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	recordedDocker = "docker-recorded"
	exposedDocker  = "docker-exposed"
	createdDocker  = "docker-created"
)

var (
	errStoreWrite     = errors.New("database write failed")
	errStoreRead      = errors.New("database read failed")
	errDockerRejected = errors.New("docker rejected the request")
	errDockerDown     = apperrors.Wrap(apperrors.ErrUnavailable, "docker unavailable", errors.New("connection refused"))
)

// lifecycleFixture is how a container stands when its queued operation
// arrives: the status it had before the request, and the busy and desired
// statuses the request recorded.
type lifecycleFixture struct {
	previous, busy, desired string
}

var lifecycleFixtures = map[string]lifecycleFixture{
	model.OperationCreate: {previous: "", busy: model.ContainerStatusPending, desired: model.ContainerStatusCreated},
	model.OperationStart:  {previous: model.ContainerStatusExited, busy: model.ContainerStatusStarting, desired: model.ContainerStatusRunning},
	model.OperationStop:   {previous: model.ContainerStatusRunning, busy: model.ContainerStatusStopping, desired: model.ContainerStatusExited},
	model.OperationDelete: {previous: model.ContainerStatusExited, busy: model.ContainerStatusDeleting, desired: model.ContainerStatusDeleting},
	model.OperationExpose: {previous: model.ContainerStatusRunning, busy: model.ContainerStatusExposing, desired: model.ContainerStatusRunning},
}

// containerRow is the part of a container row a lifecycle operation changes.
type containerRow struct {
	Status       string
	DockerID     string
	Generation   int
	DomainPrefix string
	InternalPort int
	HasLastError bool
	TTL          ttlState
}

// ttlState is what became of the container's TTL deadline.
type ttlState string

const (
	ttlKept    ttlState = "kept"
	ttlRenewed ttlState = "renewed"
	ttlCleared ttlState = "cleared"
)

type lifecycleCase struct {
	name        string
	operation   string
	ttlDisabled bool
	// recorded gives a queued create a Docker container already recorded, as
	// after a restart that interrupted the create past its record write.
	recorded bool
	// docker scripts the Docker double and returns the containers Docker holds
	// when the operation arrives.
	docker func(d *coremocks.ContainerDockerAPI) []string
	faults coretest.ContainerStoreFaults
	// wantErr lists the errors the queue consumer must receive; none means success.
	wantErr []error
	// wantRow is the container row afterwards; nil means it was deleted.
	wantRow *containerRow
	// wantDesired, when set, is the container's desired status afterwards.
	wantDesired   string
	wantOperation string
	wantDocker    []string
}

// Each queued create, start, stop, delete and expose runs through the
// lifecycle executor: a retryable failure requeues the operation, a terminal
// one fails it and restores the previous status, a missing Docker container
// marks the container missing, and only a Docker container whose generation
// was never recorded is removed.
func TestQueuedContainerLifecycleOutcomes(t *testing.T) {
	var cases []lifecycleCase
	cases = append(cases, createCases()...)
	cases = append(cases, startCases()...)
	cases = append(cases, stopCases()...)
	cases = append(cases, deleteCases()...)
	cases = append(cases, exposeCases()...)
	for _, tc := range cases {
		t.Run(tc.operation+"/"+tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := newContainerStore()
			c, op := queueLifecycleOperation(t, store, tc.operation, tc.recorded)
			store.SetFaults(tc.faults)
			docker := coremocks.NewContainerDockerAPI(t)
			held := tc.docker(docker)
			cfg := lifecycleConfig(24)
			if tc.ttlDisabled {
				cfg = lifecycleConfig(0)
			}
			images := newContainerImageRepository(t)
			images.EXPECT().List(mock.Anything, mock.Anything).Return(nil, 0, nil).Maybe()
			svc := newTestContainerService(t, service.ContainerServiceDeps{
				Repository: store,
				Images:     images,
				Docker:     docker,
				Config:     cfg,
				Logger:     slog.New(slog.DiscardHandler),
			})
			worker := service.NewContainerCreateWorker(svc, cfg, slog.New(slog.DiscardHandler))

			err := worker.HandleMessage(ctx, lifecycleMessage(c, op))

			if len(tc.wantErr) == 0 {
				require.NoError(t, err)
			}
			for _, want := range tc.wantErr {
				require.ErrorIs(t, err, want)
			}
			store.SetFaults(coretest.ContainerStoreFaults{})
			got, getErr := store.GetByID(ctx, c.ID)
			if tc.wantRow == nil {
				require.ErrorIs(t, getErr, apperrors.ErrNotFound, "the container row is deleted")
			} else {
				require.NoError(t, getErr)
				require.Equal(t, *tc.wantRow, rowOf(got))
				if tc.wantDesired != "" {
					require.Equal(t, tc.wantDesired, got.DesiredStatus)
				}
				if tc.wantRow.TTL == ttlRenewed {
					require.WithinDuration(t, time.Now().Add(24*time.Hour), *got.TTLDeadline, time.Minute)
				}
			}
			gotOp, opErr := store.GetOperationByID(ctx, op.ID)
			require.NoError(t, opErr)
			require.Equal(t, tc.wantOperation, gotOp.Status)
			require.ElementsMatch(t, tc.wantDocker, dockerHoldings(docker, held))
		})
	}
}

func createCases() []lifecycleCase {
	// create scripts a create of the public nginx image: pullErr, when set,
	// makes the image absent and its pull fail; createErr fails the container
	// creation. A pulled image must be removed again when the create fails.
	create := func(pull bool, pullErr, createErr error) func(d *coremocks.ContainerDockerAPI) []string {
		return func(d *coremocks.ContainerDockerAPI) []string {
			d.EXPECT().EnsureUserNetwork(mock.Anything, mock.Anything).Return("network", nil).Once()
			d.EXPECT().ImageExists(mock.Anything, "nginx:latest").Return(!pull, nil).Once()
			if pull {
				d.EXPECT().PullImage(mock.Anything, "nginx:latest").Return(pullErr).Once()
			}
			if pullErr != nil {
				return nil
			}
			d.EXPECT().CreateContainer(mock.Anything, mock.MatchedBy(func(spec model.ContainerRuntimeSpec) bool {
				return spec.ImageName == "nginx:latest" && spec.Generation == 1
			})).Return(createdDocker, createErr).Once()
			d.EXPECT().RemoveContainer(mock.Anything, createdDocker, true).Return(nil).Maybe()
			if pull && createErr != nil {
				d.EXPECT().RemoveImage(mock.Anything, "nginx:latest", false).Return(nil).Once()
			}
			return nil
		}
	}
	// createRecordFails scripts a create whose record write fails: the
	// created container and the image pulled for it are removed.
	createRecordFails := func(d *coremocks.ContainerDockerAPI) []string {
		held := create(true, nil, nil)(d)
		d.EXPECT().RemoveImage(mock.Anything, "nginx:latest", false).Return(nil).Once()
		return held
	}
	created := create(false, nil, nil)
	queued := containerRow{Status: model.ContainerStatusPending, Generation: 1, TTL: ttlKept}
	busy := queued
	busy.Status = model.ContainerStatusCreating
	recorded := containerRow{Status: model.ContainerStatusCreated, DockerID: createdDocker, Generation: 1, TTL: ttlKept}
	failed := busy
	failed.Status = model.ContainerStatusError
	failed.HasLastError = true
	alreadyRecorded := recorded
	alreadyRecorded.DockerID = recordedDocker
	op := model.OperationCreate
	return []lifecycleCase{
		{name: "success records the container", operation: op, docker: created, wantRow: &recorded, wantOperation: model.OperationStatusDone, wantDocker: held(createdDocker)},
		{name: "success after pulling the image", operation: op, docker: create(true, nil, nil), wantRow: &recorded, wantOperation: model.OperationStatusDone, wantDocker: held(createdDocker)},
		{name: "recorded container completes without docker work", operation: op, recorded: true, docker: noDockerCalls, wantRow: &alreadyRecorded, wantDesired: model.ContainerStatusCreated, wantOperation: model.OperationStatusDone, wantDocker: held(recordedDocker)},
		{name: "image not found", operation: op, docker: create(true, cerrdefs.ErrNotFound, nil), wantErr: []error{apperrors.ErrBadRequest}, wantRow: &failed, wantOperation: model.OperationStatusFailed},
		{name: "retryable docker error", operation: op, docker: create(false, nil, errDockerDown), wantErr: []error{apperrors.ErrUnavailable}, wantRow: &busy, wantOperation: model.OperationStatusPending},
		{name: "terminal docker error removes the pulled image", operation: op, docker: create(true, nil, errDockerRejected), wantErr: []error{errDockerRejected}, wantRow: &failed, wantOperation: model.OperationStatusFailed},
		{name: "claim write fails", operation: op, docker: noCreateDockerCalls, faults: coretest.ContainerStoreFaults{ClaimPendingOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &queued, wantOperation: model.OperationStatusPending},
		{name: "creating status write fails", operation: op, docker: noCreateDockerCalls, faults: coretest.ContainerStoreFaults{UpdateStatus: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &queued, wantOperation: model.OperationStatusPending},
		{name: "mounts read fails", operation: op, docker: noCreateDockerCalls, faults: coretest.ContainerStoreFaults{GetMountsByContainerID: coretest.FailAlways(errStoreRead)}, wantErr: []error{errStoreRead}, wantRow: &failed, wantOperation: model.OperationStatusFailed},
		{name: "record write fails", operation: op, docker: createRecordFails, faults: coretest.ContainerStoreFaults{UpdateDockerIDAndStatus: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &failed, wantOperation: model.OperationStatusFailed},
		{name: "completion write fails", operation: op, docker: created, faults: coretest.ContainerStoreFaults{CompleteOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &recorded, wantOperation: model.OperationStatusRunning, wantDocker: held(createdDocker)},
		{name: "requeue write fails", operation: op, docker: create(false, nil, errDockerDown), faults: coretest.ContainerStoreFaults{RequeueOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{apperrors.ErrUnavailable, errStoreWrite}, wantRow: &failed, wantOperation: model.OperationStatusFailed},
		{name: "error mark write fails", operation: op, docker: create(false, nil, errDockerRejected), faults: coretest.ContainerStoreFaults{MarkStatusError: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errDockerRejected, errStoreWrite}, wantRow: &busy, wantOperation: model.OperationStatusFailed},
		{name: "failed-operation write fails", operation: op, docker: create(false, nil, errDockerRejected), faults: coretest.ContainerStoreFaults{CompleteOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errDockerRejected, errStoreWrite}, wantRow: &failed, wantOperation: model.OperationStatusRunning},
	}
}

func startCases() []lifecycleCase {
	start := func(err error) func(d *coremocks.ContainerDockerAPI) []string {
		return func(d *coremocks.ContainerDockerAPI) []string {
			d.EXPECT().StartContainer(mock.Anything, recordedDocker).Return(err).Once()
			return holdingUnlessMissing(err)
		}
	}
	busy := containerRow{Status: model.ContainerStatusStarting, DockerID: recordedDocker, Generation: 1, TTL: ttlKept}
	running := busy
	running.Status = model.ContainerStatusRunning
	restored := busy
	restored.Status = model.ContainerStatusExited
	restored.HasLastError = true
	missing := busy
	missing.Status = model.ContainerStatusMissing
	missing.HasLastError = true
	running.TTL = ttlRenewed
	noTTL := running
	noTTL.TTL = ttlCleared
	op := model.OperationStart
	return []lifecycleCase{
		{name: "success sets the TTL deadline", operation: op, docker: start(nil), wantRow: &running, wantOperation: model.OperationStatusDone, wantDocker: held(recordedDocker)},
		{name: "success with TTL disabled clears the deadline", operation: op, ttlDisabled: true, docker: start(nil), wantRow: &noTTL, wantOperation: model.OperationStatusDone, wantDocker: held(recordedDocker)},
		{name: "docker not found", operation: op, docker: start(cerrdefs.ErrNotFound), wantErr: []error{apperrors.ErrConflict}, wantRow: &missing, wantOperation: model.OperationStatusFailed},
		{name: "retryable docker error", operation: op, docker: start(errDockerDown), wantErr: []error{apperrors.ErrUnavailable}, wantRow: &busy, wantOperation: model.OperationStatusPending, wantDocker: held(recordedDocker)},
		{name: "terminal docker error", operation: op, docker: start(errDockerRejected), wantErr: []error{errDockerRejected}, wantRow: &restored, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "claim write fails", operation: op, docker: noDockerCalls, faults: coretest.ContainerStoreFaults{ClaimPendingOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &busy, wantOperation: model.OperationStatusPending, wantDocker: held(recordedDocker)},
		{name: "status write fails", operation: op, docker: start(nil), faults: coretest.ContainerStoreFaults{UpdateStatusAndTTLDeadline: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &restored, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "completion write fails", operation: op, docker: start(nil), faults: coretest.ContainerStoreFaults{CompleteOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &running, wantOperation: model.OperationStatusRunning, wantDocker: held(recordedDocker)},
		{name: "requeue write fails", operation: op, docker: start(errDockerDown), faults: coretest.ContainerStoreFaults{RequeueOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{apperrors.ErrUnavailable, errStoreWrite}, wantRow: &restored, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "error mark write fails", operation: op, docker: start(errDockerRejected), faults: coretest.ContainerStoreFaults{MarkStatusError: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errDockerRejected, errStoreWrite}, wantRow: &busy, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "failed-operation write fails", operation: op, docker: start(errDockerRejected), faults: coretest.ContainerStoreFaults{CompleteOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errDockerRejected, errStoreWrite}, wantRow: &restored, wantOperation: model.OperationStatusRunning, wantDocker: held(recordedDocker)},
	}
}

func stopCases() []lifecycleCase {
	stop := func(err error) func(d *coremocks.ContainerDockerAPI) []string {
		return func(d *coremocks.ContainerDockerAPI) []string {
			d.EXPECT().StopContainer(mock.Anything, recordedDocker, 1).Return(err).Once()
			return holdingUnlessMissing(err)
		}
	}
	busy := containerRow{Status: model.ContainerStatusStopping, DockerID: recordedDocker, Generation: 1, TTL: ttlKept}
	exited := busy
	exited.Status = model.ContainerStatusExited
	restored := busy
	restored.Status = model.ContainerStatusRunning
	restored.HasLastError = true
	missing := busy
	missing.Status = model.ContainerStatusMissing
	missing.HasLastError = true
	op := model.OperationStop
	return []lifecycleCase{
		{name: "success", operation: op, docker: stop(nil), wantRow: &exited, wantOperation: model.OperationStatusDone, wantDocker: held(recordedDocker)},
		{name: "docker not found", operation: op, docker: stop(cerrdefs.ErrNotFound), wantErr: []error{apperrors.ErrConflict}, wantRow: &missing, wantOperation: model.OperationStatusFailed},
		{name: "retryable docker error", operation: op, docker: stop(errDockerDown), wantErr: []error{apperrors.ErrUnavailable}, wantRow: &busy, wantOperation: model.OperationStatusPending, wantDocker: held(recordedDocker)},
		{name: "terminal docker error", operation: op, docker: stop(errDockerRejected), wantErr: []error{errDockerRejected}, wantRow: &restored, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "claim write fails", operation: op, docker: noDockerCalls, faults: coretest.ContainerStoreFaults{ClaimPendingOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &busy, wantOperation: model.OperationStatusPending, wantDocker: held(recordedDocker)},
		{name: "status write fails", operation: op, docker: stop(nil), faults: coretest.ContainerStoreFaults{UpdateStatus: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &restored, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "completion write fails", operation: op, docker: stop(nil), faults: coretest.ContainerStoreFaults{CompleteOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &exited, wantOperation: model.OperationStatusRunning, wantDocker: held(recordedDocker)},
		{name: "requeue write fails", operation: op, docker: stop(errDockerDown), faults: coretest.ContainerStoreFaults{RequeueOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{apperrors.ErrUnavailable, errStoreWrite}, wantRow: &restored, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "error mark write fails", operation: op, docker: stop(errDockerRejected), faults: coretest.ContainerStoreFaults{MarkStatusError: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errDockerRejected, errStoreWrite}, wantRow: &busy, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "failed-operation write fails", operation: op, docker: stop(errDockerRejected), faults: coretest.ContainerStoreFaults{CompleteOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errDockerRejected, errStoreWrite}, wantRow: &restored, wantOperation: model.OperationStatusRunning, wantDocker: held(recordedDocker)},
	}
}

func deleteCases() []lifecycleCase {
	remove := func(err error) func(d *coremocks.ContainerDockerAPI) []string {
		return func(d *coremocks.ContainerDockerAPI) []string {
			d.EXPECT().RemoveContainer(mock.Anything, recordedDocker, true).Return(err).Once()
			d.EXPECT().RemoveNetwork(mock.Anything, mock.Anything).Return(nil).Maybe()
			return holdingUnlessMissing(err)
		}
	}
	busy := containerRow{Status: model.ContainerStatusDeleting, DockerID: recordedDocker, Generation: 1, TTL: ttlKept}
	restored := busy
	restored.Status = model.ContainerStatusExited
	restored.HasLastError = true
	op := model.OperationDelete
	return []lifecycleCase{
		{name: "success", operation: op, docker: remove(nil), wantOperation: model.OperationStatusDone},
		{name: "docker not found is success", operation: op, docker: remove(cerrdefs.ErrNotFound), wantOperation: model.OperationStatusDone},
		{name: "retryable docker error", operation: op, docker: remove(errDockerDown), wantErr: []error{apperrors.ErrUnavailable}, wantRow: &busy, wantOperation: model.OperationStatusPending, wantDocker: held(recordedDocker)},
		{name: "terminal docker error", operation: op, docker: remove(errDockerRejected), wantErr: []error{errDockerRejected}, wantRow: &restored, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "claim write fails", operation: op, docker: noDockerCalls, faults: coretest.ContainerStoreFaults{ClaimPendingOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &busy, wantOperation: model.OperationStatusPending, wantDocker: held(recordedDocker)},
		{name: "row delete fails", operation: op, docker: remove(nil), faults: coretest.ContainerStoreFaults{Delete: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &restored, wantOperation: model.OperationStatusFailed},
		{name: "completion write fails", operation: op, docker: remove(nil), faults: coretest.ContainerStoreFaults{CompleteOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantOperation: model.OperationStatusRunning},
		{name: "requeue write fails", operation: op, docker: remove(errDockerDown), faults: coretest.ContainerStoreFaults{RequeueOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{apperrors.ErrUnavailable, errStoreWrite}, wantRow: &restored, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "error mark write fails", operation: op, docker: remove(errDockerRejected), faults: coretest.ContainerStoreFaults{MarkStatusError: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errDockerRejected, errStoreWrite}, wantRow: &busy, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "failed-operation write fails", operation: op, docker: remove(errDockerRejected), faults: coretest.ContainerStoreFaults{CompleteOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errDockerRejected, errStoreWrite}, wantRow: &restored, wantOperation: model.OperationStatusRunning, wantDocker: held(recordedDocker)},
	}
}

func exposeCases() []lifecycleCase {
	inspect := func(err error) func(d *coremocks.ContainerDockerAPI) []string {
		return func(d *coremocks.ContainerDockerAPI) []string {
			d.EXPECT().InspectContainer(mock.Anything, recordedDocker).Return(model.ContainerInspection{Name: "/usr_web", Image: "nginx:latest"}, err).Once()
			return holdingUnlessMissing(err)
		}
	}
	// replace scripts an expose that creates and starts the new generation;
	// startErr fails the start. Removals report success.
	replace := func(startErr error) func(d *coremocks.ContainerDockerAPI) []string {
		return func(d *coremocks.ContainerDockerAPI) []string {
			held := inspect(nil)(d)
			d.EXPECT().CreateContainer(mock.Anything, mock.MatchedBy(func(spec model.ContainerRuntimeSpec) bool {
				return spec.Generation == 2 && spec.Domain == "app.example.test" && spec.InternalPort == 8080
			})).Return(exposedDocker, nil).Once()
			d.EXPECT().StartContainer(mock.Anything, exposedDocker).Return(startErr).Once()
			d.EXPECT().RemoveContainer(mock.Anything, mock.Anything, true).Return(nil).Maybe()
			return held
		}
	}
	busy := containerRow{Status: model.ContainerStatusExposing, DockerID: recordedDocker, Generation: 1, TTL: ttlKept}
	restored := busy
	restored.Status = model.ContainerStatusRunning
	restored.HasLastError = true
	missing := busy
	missing.Status = model.ContainerStatusMissing
	missing.HasLastError = true
	recorded := containerRow{Status: model.ContainerStatusExposing, DockerID: exposedDocker, Generation: 2, DomainPrefix: "app", InternalPort: 8080, TTL: ttlKept}
	exposed := recorded
	exposed.Status = model.ContainerStatusRunning
	op := model.OperationExpose
	return []lifecycleCase{
		{name: "success replaces the generation", operation: op, docker: replace(nil), wantRow: &exposed, wantOperation: model.OperationStatusDone, wantDocker: held(exposedDocker)},
		{name: "docker not found", operation: op, docker: inspect(cerrdefs.ErrNotFound), wantErr: []error{apperrors.ErrConflict}, wantRow: &missing, wantOperation: model.OperationStatusFailed},
		{name: "retryable docker error", operation: op, docker: inspect(errDockerDown), wantErr: []error{apperrors.ErrUnavailable}, wantRow: &busy, wantOperation: model.OperationStatusPending, wantDocker: held(recordedDocker)},
		{name: "retryable error removes the unrecorded generation", operation: op, docker: replace(errDockerDown), wantErr: []error{apperrors.ErrUnavailable}, wantRow: &busy, wantOperation: model.OperationStatusPending, wantDocker: held(recordedDocker)},
		{name: "terminal docker error", operation: op, docker: inspect(errDockerRejected), wantErr: []error{errDockerRejected}, wantRow: &restored, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "claim write fails", operation: op, docker: noDockerCalls, faults: coretest.ContainerStoreFaults{ClaimPendingOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &busy, wantOperation: model.OperationStatusPending, wantDocker: held(recordedDocker)},
		{name: "generation write fails", operation: op, docker: replace(nil), faults: coretest.ContainerStoreFaults{UpdateDockerIDRoutingAndGeneration: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &restored, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "status write fails", operation: op, docker: replace(nil), faults: coretest.ContainerStoreFaults{UpdateStatus: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &recorded, wantOperation: model.OperationStatusRunning, wantDocker: held(exposedDocker)},
		{name: "completion write fails", operation: op, docker: replace(nil), faults: coretest.ContainerStoreFaults{CompleteOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errStoreWrite}, wantRow: &exposed, wantOperation: model.OperationStatusRunning, wantDocker: held(exposedDocker)},
		{name: "requeue write fails", operation: op, docker: inspect(errDockerDown), faults: coretest.ContainerStoreFaults{RequeueOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{apperrors.ErrUnavailable, errStoreWrite}, wantRow: &restored, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "error mark write fails", operation: op, docker: inspect(errDockerRejected), faults: coretest.ContainerStoreFaults{MarkStatusError: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errDockerRejected, errStoreWrite}, wantRow: &busy, wantOperation: model.OperationStatusFailed, wantDocker: held(recordedDocker)},
		{name: "failed-operation write fails", operation: op, docker: inspect(errDockerRejected), faults: coretest.ContainerStoreFaults{CompleteOperation: coretest.FailAlways(errStoreWrite)}, wantErr: []error{errDockerRejected, errStoreWrite}, wantRow: &restored, wantOperation: model.OperationStatusRunning, wantDocker: held(recordedDocker)},
	}
}

// containerStore puts the container store fake behind ContainerService. The
// paths its tests drive reach only methods the fake models; any other
// repository method resolves to the nil embedded interface and panics, so a
// path that starts using one fails loudly.
type containerStore struct {
	*coretest.ContainerStoreFake
	unmodelledContainerRepository
}

type unmodelledContainerRepository struct{ service.ContainerRepository }

func newContainerStore() *containerStore {
	return &containerStore{ContainerStoreFake: coretest.NewContainerStoreFake()}
}

// queuedRequest is the one operation a request queued, read back from the
// store with its outbox record and the container as the request left it.
type queuedRequest struct {
	op        model.ResourceOperation
	outbox    model.ContainerLifecycleOutbox
	container model.Container
}

// requireQueued returns the one operation queued in store.
func requireQueued(t *testing.T, store *containerStore) queuedRequest {
	t.Helper()
	ctx := context.Background()
	outbox, err := store.LeasePendingContainerOutbox(ctx, 10)
	require.NoError(t, err)
	require.Len(t, outbox, 1, "exactly one operation is queued")
	op, err := store.GetOperationByID(ctx, outbox[0].OperationID)
	require.NoError(t, err)
	c, err := store.GetByID(ctx, outbox[0].ContainerID)
	require.NoError(t, err)
	return queuedRequest{op: op, outbox: outbox[0], container: c}
}

// requireNothingQueued asserts that no operation is queued in store.
func requireNothingQueued(t *testing.T, store *containerStore) {
	t.Helper()
	outbox, err := store.LeasePendingContainerOutbox(context.Background(), 10)
	require.NoError(t, err)
	require.Empty(t, outbox, "no operation is queued")
}

// saveContainer records c in store.
func saveContainer(t *testing.T, store *containerStore, c model.Container) {
	t.Helper()
	require.NoError(t, store.Save(context.Background(), c))
}

// lifecycleTTL is the TTL deadline every container starts with.
var lifecycleTTL = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

// queueLifecycleOperation records a container and queues operation for it, as
// the request path does. A create's container has no Docker container unless
// recorded is set; a recorded one is already created, as restart recovery
// leaves it.
func queueLifecycleOperation(t *testing.T, store *containerStore, operation string, recorded bool) (model.Container, model.ResourceOperation) {
	t.Helper()
	ctx := context.Background()
	fixture := lifecycleFixtures[operation]
	ttl := lifecycleTTL
	c := model.Container{
		ID:               uuid.New(),
		OwnerID:          uuid.New(),
		Name:             "web",
		ImageTag:         "nginx:latest",
		DockerID:         recordedDocker,
		DockerGeneration: 1,
		Status:           fixture.previous,
		DesiredStatus:    fixture.previous,
		TTLDeadline:      &ttl,
	}
	op := model.ResourceOperation{
		ID:           uuid.New(),
		ResourceType: model.ResourceTypeContainer,
		ResourceID:   c.ID,
		OwnerID:      c.OwnerID,
		Operation:    operation,
	}
	outbox := model.ContainerLifecycleOutbox{ID: uuid.New(), OperationID: op.ID, ContainerID: c.ID}
	if operation == model.OperationCreate {
		c.Status, c.DesiredStatus = fixture.busy, fixture.desired
		if recorded {
			c.Status = model.ContainerStatusCreated
		} else {
			c.DockerID = ""
		}
		op.Status = model.OperationStatusPending
		require.NoError(t, store.SaveWithMountsOperationAndOutbox(ctx, c, nil, op, outbox, false, false))
		return c, op
	}
	require.NoError(t, store.Save(ctx, c))
	require.NoError(t, store.QueueContainerOperation(ctx, c.ID, fixture.busy, fixture.desired, op, outbox))
	return c, op
}

func lifecycleMessage(c model.Container, op model.ResourceOperation) queue.ContainerLifecycleMessage {
	msg := queue.ContainerLifecycleMessage{
		OperationID:    op.ID.String(),
		ContainerID:    c.ID.String(),
		OwnerID:        c.OwnerID.String(),
		Operation:      op.Operation,
		PreviousStatus: lifecycleFixtures[op.Operation].previous,
		CreatedAt:      time.Now().Unix(),
	}
	if op.Operation == model.OperationExpose {
		msg.DomainPrefix = "app"
		msg.InternalPort = 8080
	}
	return msg
}

func rowOf(c model.Container) containerRow {
	return containerRow{
		Status:       c.Status,
		DockerID:     c.DockerID,
		Generation:   c.DockerGeneration,
		DomainPrefix: c.DomainPrefix,
		InternalPort: c.InternalPort,
		HasLastError: c.LastError != nil,
		TTL:          ttlOf(c.TTLDeadline),
	}
}

func ttlOf(deadline *time.Time) ttlState {
	switch {
	case deadline == nil:
		return ttlCleared
	case deadline.Equal(lifecycleTTL):
		return ttlKept
	default:
		return ttlRenewed
	}
}

// dockerHoldings replays the Docker double's calls over the containers it held
// at the start: a created container is added, and a removed one, or one Docker
// reports not found, is gone.
func dockerHoldings(d *coremocks.ContainerDockerAPI, initial []string) []string {
	holdings := slices.Clone(initial)
	for _, call := range d.Calls {
		switch call.Method {
		case "CreateContainer":
			if call.ReturnArguments.Error(1) == nil {
				holdings = append(holdings, call.ReturnArguments.String(0))
			}
		case "RemoveContainer":
			if err := call.ReturnArguments.Error(0); err == nil || cerrdefs.IsNotFound(err) {
				removed := call.Arguments.String(1)
				holdings = slices.DeleteFunc(holdings, func(id string) bool { return id == removed })
			}
		}
	}
	return holdings
}

// holdingUnlessMissing is what Docker holds when its first call answers err.
func holdingUnlessMissing(err error) []string {
	if cerrdefs.IsNotFound(err) {
		return nil
	}
	return held(recordedDocker)
}

func noDockerCalls(*coremocks.ContainerDockerAPI) []string { return held(recordedDocker) }

// noCreateDockerCalls is a create that fails before any Docker call.
func noCreateDockerCalls(*coremocks.ContainerDockerAPI) []string { return nil }

func held(ids ...string) []string { return ids }

// lifecycleConfig is testConfig with the given container TTL.
func lifecycleConfig(ttlHours int64) coretest.StaticConfig {
	cfg := testConfig()
	cfg.Config.ContainerTTLHours = ttlHours
	return cfg
}

// A delete that cancels a create while it runs owns the outcome: the create
// stops before creating a Docker container and writes nothing over the
// canceled operation.
func TestQueuedCreateCanceledByDeleteKeepsTheCancellation(t *testing.T) {
	ctx := context.Background()
	store := newContainerStore()
	c, op := queueLifecycleOperation(t, store, model.OperationCreate, false)
	cancelCause := apperrors.New(apperrors.ErrConflict, "container create canceled by delete")
	docker := coremocks.NewContainerDockerAPI(t)
	docker.EXPECT().EnsureUserNetwork(mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, name string) (string, error) {
		require.NoError(t, store.CancelActiveCreateAndDeleteContainer(ctx, c.ID, cancelCause))
		return name, nil
	}).Once()
	docker.EXPECT().RemoveNetwork(mock.Anything, mock.Anything).Return(nil).Once()
	images := newContainerImageRepository(t)
	images.EXPECT().List(mock.Anything, mock.Anything).Return(nil, 0, nil).Maybe()
	cfg := lifecycleConfig(24)
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository: store,
		Images:     images,
		Docker:     docker,
		Config:     cfg,
		Logger:     slog.New(slog.DiscardHandler),
	})
	worker := service.NewContainerCreateWorker(svc, cfg, slog.New(slog.DiscardHandler))

	err := worker.HandleMessage(ctx, lifecycleMessage(c, op))

	require.ErrorIs(t, err, apperrors.ErrConflict)
	_, getErr := store.GetByID(ctx, c.ID)
	require.ErrorIs(t, getErr, apperrors.ErrNotFound, "the delete removed the container")
	gotOp, opErr := store.GetOperationByID(ctx, op.ID)
	require.NoError(t, opErr)
	require.Equal(t, model.OperationStatusFailed, gotOp.Status)
	require.Equal(t, cancelCause.Error(), *gotOp.LastError, "the cancellation's cause stands")
}

// A create that cannot read whether its operation is still running stops
// without settling it: the operation stays running for restart recovery
// instead of being failed on a read error.
func TestQueuedCreateLeavesOperationRunningWhenItsFenceCannotBeRead(t *testing.T) {
	ctx := context.Background()
	store := newContainerStore()
	c, op := queueLifecycleOperation(t, store, model.OperationCreate, false)
	docker := coremocks.NewContainerDockerAPI(t)
	docker.EXPECT().EnsureUserNetwork(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, name string) (string, error) {
		store.SetFaults(coretest.ContainerStoreFaults{GetOperationByID: coretest.FailNext(1, errStoreRead)})
		return name, nil
	}).Once()
	images := newContainerImageRepository(t)
	images.EXPECT().List(mock.Anything, mock.Anything).Return(nil, 0, nil).Maybe()
	cfg := lifecycleConfig(24)
	svc := newTestContainerService(t, service.ContainerServiceDeps{
		Repository: store,
		Images:     images,
		Docker:     docker,
		Config:     cfg,
		Logger:     slog.New(slog.DiscardHandler),
	})
	worker := service.NewContainerCreateWorker(svc, cfg, slog.New(slog.DiscardHandler))

	err := worker.HandleMessage(ctx, lifecycleMessage(c, op))

	require.ErrorIs(t, err, errStoreRead)
	got, getErr := store.GetByID(ctx, c.ID)
	require.NoError(t, getErr)
	require.Equal(t, model.ContainerStatusCreating, got.Status)
	require.Nil(t, got.LastError, "no error status is written")
	gotOp, opErr := store.GetOperationByID(ctx, op.ID)
	require.NoError(t, opErr)
	require.Equal(t, model.OperationStatusRunning, gotOp.Status)
}
