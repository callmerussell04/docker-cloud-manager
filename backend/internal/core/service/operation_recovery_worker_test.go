package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/coretest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
	coremocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/core/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// With the default 30-minute handler deadline an operation expires 35 minutes
// after its claim, and a message published 5 minutes ago counts as lost.
const (
	recoveryExpired   = 40 * time.Minute
	recoveryNotExpiry = 30 * time.Minute
	recoveryLost      = 6 * time.Minute
	recoveryNotLost   = 3 * time.Minute
)

// The pass's bounds with the default handler deadline: an operation expires
// 35 minutes after its claim, and a message counts as lost after 5 minutes.
var (
	productionExpiry = model.OperationRecoveryScope{RunningLongerThan: 35 * time.Minute}
	productionLost   = 5 * time.Minute
)

func causeIs(message string) any {
	return mock.MatchedBy(func(err error) bool { return err != nil && err.Error() == message })
}

type recoveryHarness struct {
	store       *coretest.ContainerStoreFake
	resources   *coremocks.ResourceOperationRecoveryStore
	deployments *coremocks.ComposeDeliveryRecoveryStore
	logs        *syncBuffer
	ownerID     uuid.UUID
}

// syncBuffer is a log sink the worker goroutine and the test share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// records decodes every JSON log line written so far.
func (b *syncBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var records []map[string]any
	dec := json.NewDecoder(bytes.NewReader(b.buf.Bytes()))
	for dec.More() {
		var record map[string]any
		require.NoError(t, dec.Decode(&record))
		records = append(records, record)
	}
	return records
}

func newRecoveryHarness(t *testing.T) *recoveryHarness {
	return &recoveryHarness{
		store:       coretest.NewContainerStoreFake(),
		resources:   coremocks.NewResourceOperationRecoveryStore(t),
		deployments: coremocks.NewComposeDeliveryRecoveryStore(t),
		logs:        &syncBuffer{},
		ownerID:     uuid.New(),
	}
}

// expectVolumeAndImagePass expects one pass's volume and image statements,
// with the production bounds and causes, answering expired and expiredErr.
func (h *recoveryHarness) expectVolumeAndImagePass(expired []model.RecoveredOperation, expiredErr error) {
	h.resources.EXPECT().RecoverInterruptedOperations(mock.Anything, causeIs("operation expired and was returned to the queue"), productionExpiry).
		Return(expired, expiredErr).Once()
	h.resources.EXPECT().RecoverLostResourceDeliveries(mock.Anything, causeIs("operation resource no longer exists"), productionLost).
		Return(model.LostDeliveryRecovery{}, nil).Once()
}

// expectDeploymentPass expects one pass's Compose statement, answering lost.
func (h *recoveryHarness) expectDeploymentPass(lost []model.RecoveredDeployment) {
	h.deployments.EXPECT().RepublishLostComposeDeliveries(mock.Anything, productionLost).Return(lost, nil).Once()
}

// expectQuietPasses expects n passes in which volumes, images and
// deployments have nothing to recover.
func (h *recoveryHarness) expectQuietPasses(n int) {
	for range n {
		h.expectVolumeAndImagePass(nil, nil)
		h.expectDeploymentPass(nil)
	}
}

// runPasses starts the worker, lets n one-minute intervals pass and stops it.
// It must run inside a synctest bubble.
func (h *recoveryHarness) runPasses(t *testing.T, n int) {
	t.Helper()
	worker, err := service.NewOperationRecoveryWorker(service.OperationRecoveryWorkerDeps{
		Containers:  h.store,
		Resources:   h.resources,
		Deployments: h.deployments,
		Config:      coretest.StaticConfig{Config: coretest.SystemConfig()},
		Logger:      slog.New(slog.NewJSONHandler(h.logs, nil)),
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	time.Sleep(time.Duration(n)*time.Minute + time.Second)
	synctest.Wait()
	cancel()
	<-done
}

// queue records a container with a pending operation and its outbox record,
// as the request paths do.
func (h *recoveryHarness) queue(t *testing.T, operation string) model.ResourceOperation {
	t.Helper()
	ctx := context.Background()
	c := model.Container{ID: uuid.New(), OwnerID: h.ownerID, Name: "web", Status: model.ContainerStatusExited, DesiredStatus: model.ContainerStatusExited, DockerID: "docker-id", DockerGeneration: 1}
	op := model.ResourceOperation{ID: uuid.New(), ResourceType: model.ResourceTypeContainer, ResourceID: c.ID, OwnerID: h.ownerID, Operation: operation, Status: model.OperationStatusPending}
	outbox := model.ContainerLifecycleOutbox{ID: uuid.New(), OperationID: op.ID, ContainerID: c.ID, Exchange: "dcm.containers", RoutingKey: "container.lifecycle", Payload: []byte(`{}`), Status: model.ContainerOutboxStatusPending}
	if operation == model.OperationCreate {
		c.Status, c.DockerID = model.ContainerStatusPending, ""
		require.NoError(t, h.store.SaveWithMountsOperationAndOutbox(ctx, c, nil, op, outbox, false, false))
		return op
	}
	require.NoError(t, h.store.Save(ctx, c))
	require.NoError(t, h.store.QueueContainerOperation(ctx, c.ID, transitionalStatus(operation), model.ContainerStatusRunning, op, outbox))
	return op
}

func transitionalStatus(operation string) string {
	switch operation {
	case model.OperationStart:
		return model.ContainerStatusStarting
	case model.OperationStop:
		return model.ContainerStatusStopping
	case model.OperationDelete:
		return model.ContainerStatusDeleting
	default:
		return model.ContainerStatusExposing
	}
}

// publish leases and publishes every pending outbox record.
func (h *recoveryHarness) publish(t *testing.T) {
	t.Helper()
	items, err := h.store.LeasePendingContainerOutbox(context.Background(), 100)
	require.NoError(t, err)
	for _, item := range items {
		require.NoError(t, h.store.MarkContainerOutboxPublished(context.Background(), item.ID))
	}
}

func (h *recoveryHarness) claim(t *testing.T, id uuid.UUID) {
	t.Helper()
	_, claimed, err := h.store.ClaimPendingOperation(context.Background(), id, 3)
	require.NoError(t, err)
	require.True(t, claimed)
}

func (h *recoveryHarness) operation(t *testing.T, id uuid.UUID) model.ResourceOperation {
	t.Helper()
	op, err := h.store.GetOperationByID(context.Background(), id)
	require.NoError(t, err)
	return op
}

func (h *recoveryHarness) container(t *testing.T, id uuid.UUID) model.Container {
	t.Helper()
	c, err := h.store.GetByID(context.Background(), id)
	require.NoError(t, err)
	return c
}

// leasedOperations leases every outbox record the relay would publish now.
func (h *recoveryHarness) leasedOperations(t *testing.T) []uuid.UUID {
	t.Helper()
	items, err := h.store.LeasePendingContainerOutbox(context.Background(), 100)
	require.NoError(t, err)
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.OperationID)
	}
	return ids
}

func TestOperationRecoveryPassSettlesExpiredContainerOperations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newRecoveryHarness(t)
		create := h.queue(t, model.OperationCreate)
		recentCreate := h.queue(t, model.OperationCreate)
		nonResumable := map[string]model.ResourceOperation{}
		for _, operation := range []string{model.OperationStart, model.OperationStop, model.OperationDelete, model.OperationExpose} {
			nonResumable[operation] = h.queue(t, operation)
		}
		recentStart := h.queue(t, model.OperationStart)
		h.publish(t)
		before := map[uuid.UUID]model.Container{}
		for _, op := range nonResumable {
			h.claim(t, op.ID)
			require.NoError(t, h.store.BackdateOperation(ctx, op.ID, recoveryExpired))
			before[op.ResourceID] = h.container(t, op.ResourceID)
		}
		h.claim(t, create.ID)
		require.NoError(t, h.store.BackdateOperation(ctx, create.ID, recoveryExpired))
		for _, op := range []model.ResourceOperation{recentCreate, recentStart} {
			h.claim(t, op.ID)
			require.NoError(t, h.store.BackdateOperation(ctx, op.ID, recoveryNotExpiry))
		}

		h.expectQuietPasses(1)
		h.runPasses(t, 1)

		// The expired create is delivered again, its attempt kept.
		op := h.operation(t, create.ID)
		require.Equal(t, model.OperationStatusPending, op.Status)
		require.Equal(t, 1, op.Attempts)
		require.Equal(t, "operation expired and was returned to the queue", *op.LastError)
		require.Equal(t, []uuid.UUID{create.ID}, h.leasedOperations(t))
		// Expired start, stop, delete and expose fail; the row waits for state sync.
		for operation, queued := range nonResumable {
			op := h.operation(t, queued.ID)
			require.Equal(t, model.OperationStatusFailed, op.Status, operation)
			require.Equal(t, "operation expired before it finished", *op.LastError)
			got := h.container(t, queued.ResourceID)
			require.Equal(t, before[queued.ResourceID].Status, got.Status, operation)
			require.Equal(t, before[queued.ResourceID].DesiredStatus, got.DesiredStatus, operation)
		}
		// Operations a worker may still be acting on are untouched.
		require.Equal(t, model.OperationStatusRunning, h.operation(t, recentCreate.ID).Status)
		require.Equal(t, model.OperationStatusRunning, h.operation(t, recentStart.ID).Status)
	})
}

func TestOperationRecoveryPassRepublishesLostContainerDeliveries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newRecoveryHarness(t)
		lost := map[string]model.ResourceOperation{}
		for _, operation := range []string{model.OperationCreate, model.OperationStart, model.OperationStop, model.OperationDelete, model.OperationExpose} {
			lost[operation] = h.queue(t, operation)
		}
		recent := h.queue(t, model.OperationStart)
		h.publish(t)
		for _, op := range lost {
			require.NoError(t, h.store.BackdateContainerOutbox(ctx, op.ID, recoveryLost))
		}
		require.NoError(t, h.store.BackdateContainerOutbox(ctx, recent.ID, recoveryNotLost))
		// Still waiting for the relay.
		unpublished := h.queue(t, model.OperationStop)

		h.expectQuietPasses(1)
		h.runPasses(t, 1)

		want := []uuid.UUID{unpublished.ID}
		for _, op := range lost {
			want = append(want, op.ID)
			require.Equal(t, model.OperationStatusPending, h.operation(t, op.ID).Status)
		}
		require.ElementsMatch(t, want, h.leasedOperations(t))
	})
}

func TestOperationRecoveryPassEndsLostOperationsWhoseContainerIsGone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newRecoveryHarness(t)
		deleted := h.queue(t, model.OperationDelete)
		started := h.queue(t, model.OperationStart)
		h.publish(t)
		for _, op := range []model.ResourceOperation{deleted, started} {
			require.NoError(t, h.store.Delete(ctx, op.ResourceID))
			require.NoError(t, h.store.BackdateOperation(ctx, op.ID, recoveryLost))
		}

		h.expectQuietPasses(1)
		h.runPasses(t, 1)

		require.Equal(t, model.OperationStatusDone, h.operation(t, deleted.ID).Status)
		require.Equal(t, model.OperationStatusFailed, h.operation(t, started.ID).Status)
	})
}

func TestOperationRecoveryPassLogsEachRecoveryAndOneSummary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newRecoveryHarness(t)
		start := h.queue(t, model.OperationStart)
		h.publish(t)
		require.NoError(t, h.store.BackdateContainerOutbox(ctx, start.ID, recoveryLost))
		volume := model.RecoveredOperation{ID: uuid.New(), ResourceType: model.ResourceTypeVolume, ResourceID: uuid.New(), Operation: model.OperationCreate}
		h.expectVolumeAndImagePass([]model.RecoveredOperation{volume}, nil)
		deployment := model.RecoveredDeployment{JobID: uuid.New(), ProjectID: uuid.New()}
		h.expectDeploymentPass([]model.RecoveredDeployment{deployment})
		h.expectQuietPasses(1)

		h.runPasses(t, 2)

		records := h.logs.records(t)
		require.Len(t, records, 4, "three recoveries and one summary; the quiet second pass logs nothing")
		byOperation := map[string]map[string]any{}
		for _, record := range records[:3] {
			require.Equal(t, "WARN", record["level"])
			if id, ok := record["operation_id"].(string); ok {
				byOperation[id] = record
			} else {
				byOperation[record["job_id"].(string)] = record
			}
		}
		require.Equal(t, map[string]any{
			"operation_id": start.ID.String(), "resource_type": "container", "resource_id": start.ResourceID.String(),
			"operation": "start", "outcome": "republished",
		}, recoveryFields(byOperation[start.ID.String()]))
		require.Equal(t, map[string]any{
			"operation_id": volume.ID.String(), "resource_type": "volume", "resource_id": volume.ResourceID.String(),
			"operation": "create", "outcome": "requeued",
		}, recoveryFields(byOperation[volume.ID.String()]))
		require.Equal(t, deployment.ProjectID.String(), byOperation[deployment.JobID.String()]["project_id"])
		summary := records[3]
		require.Equal(t, "operation recovery pass settled operations", summary["msg"])
		require.EqualValues(t, 1, summary["requeued"])
		require.EqualValues(t, 0, summary["failed"])
		require.EqualValues(t, 1, summary["republished"])
		require.EqualValues(t, 0, summary["ended"])
		require.EqualValues(t, 1, summary["republished_deployments"])
	})
}

func TestOperationRecoveryPassLogsAFailedStatementAndRunsTheRest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := newRecoveryHarness(t)
		start := h.queue(t, model.OperationStart)
		h.publish(t)
		require.NoError(t, h.store.BackdateContainerOutbox(ctx, start.ID, recoveryLost))
		h.expectVolumeAndImagePass(nil, errors.New("database unreachable"))
		h.expectDeploymentPass(nil)

		h.runPasses(t, 1)

		require.Contains(t, h.leasedOperations(t), start.ID)
		var failures int
		for _, record := range h.logs.records(t) {
			if record["msg"] == "operation recovery statement failed" {
				failures++
				require.Equal(t, "ERROR", record["level"])
			}
		}
		require.Equal(t, 1, failures)
	})
}

func TestNewOperationRecoveryWorkerRejectsMissingDependencies(t *testing.T) {
	depstest.RequireRejectsMissing(t, func(t *testing.T) service.OperationRecoveryWorkerDeps {
		return service.OperationRecoveryWorkerDeps{
			Containers:  coretest.NewContainerStoreFake(),
			Resources:   coremocks.NewResourceOperationRecoveryStore(t),
			Deployments: coremocks.NewComposeDeliveryRecoveryStore(t),
			Config:      coretest.StaticConfig{Config: coretest.SystemConfig()},
			Logger:      coretest.DiscardLogger(),
		}
	}, service.NewOperationRecoveryWorker, map[string]string{
		"Containers":  "containers",
		"Resources":   "resources",
		"Deployments": "deployments",
		"Config":      "config",
	})
}

func recoveryFields(record map[string]any) map[string]any {
	fields := map[string]any{}
	for _, key := range []string{"operation_id", "resource_type", "resource_id", "operation", "outcome"} {
		fields[key] = record[key]
	}
	return fields
}
