package coretest

import (
	"bytes"
	"context"
	"slices"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// ContainerStore is the part of Core's container repository that the contract
// suite pins. The Postgres repository and ContainerStoreFake both satisfy it.
// It grows with the suite, one method per behaviour a test needs.
type ContainerStore interface {
	Save(ctx context.Context, c model.Container) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Container, error)
	CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error)
	CheckDomainPrefixExists(ctx context.Context, prefix string) (bool, error)
	GetMountsByContainerID(ctx context.Context, containerID uuid.UUID) ([]model.VolumeMountParams, error)
	Delete(ctx context.Context, id uuid.UUID) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	UpdateStatusAndTTLDeadline(ctx context.Context, id uuid.UUID, status string, ttlDeadline *time.Time) error
	MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error
	UpdateObservedStatusByContainerIDAndGeneration(ctx context.Context, containerID uuid.UUID, generation int, status string, exitCode *int, desiredStatus *string, cause error) error
	UpdateDockerIDAndStatus(ctx context.Context, id uuid.UUID, dockerID string, status string) error
	UpdateDockerIDRoutingAndGeneration(ctx context.Context, id uuid.UUID, dockerID string, domainPrefix string, internalPort int, generation int) error
	SaveWithMountsOperationAndOutbox(ctx context.Context, c model.Container, mounts []model.VolumeMount, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox, lockOwner, lockCapacity bool) error
	QueueContainerOperation(ctx context.Context, id uuid.UUID, status string, desiredStatus string, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox) error
	GetRunning(ctx context.Context) ([]model.Container, error)
	ListReservingByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Container, error)
	HasActiveOperation(ctx context.Context, resourceType string, resourceID uuid.UUID) (bool, error)
	GetOperationByID(ctx context.Context, id uuid.UUID) (model.ResourceOperation, error)
	ClaimPendingOperation(ctx context.Context, id uuid.UUID, maxAttempts int) (model.ResourceOperation, bool, error)
	RequeueOperation(ctx context.Context, id uuid.UUID, cause error) error
	CompleteOperation(ctx context.Context, id uuid.UUID, status string, cause error) error
	LeasePendingContainerOutbox(ctx context.Context, limit int) ([]model.ContainerLifecycleOutbox, error)
	MarkContainerOutboxPublished(ctx context.Context, id uuid.UUID) error
	MarkContainerOutboxPending(ctx context.Context, id uuid.UUID, cause error) error
	MarkContainerOutboxDiscarded(ctx context.Context, id uuid.UUID, cause error) error
	RecoverInterruptedContainerCreates(ctx context.Context, cause error, scope model.OperationRecoveryScope) ([]model.RecoveredOperation, error)
	FailInterruptedNonResumableOperations(ctx context.Context, cause error, runningLongerThan time.Duration) ([]model.RecoveredOperation, error)
	RecoverLostContainerDeliveries(ctx context.Context, cause error, olderThan time.Duration) (model.LostDeliveryRecovery, error)
	CancelActiveCreateAndDeleteContainer(ctx context.Context, containerID uuid.UUID, cause error) error
	AcquireOwnerCapacityLock(ctx context.Context, ownerID uuid.UUID) (func(), error)
	GetUserReservedMemory(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetUserReservedCPU(ctx context.Context, ownerID uuid.UUID) (int64, error)
	GetTotalSystemReservedMemory(ctx context.Context) (int64, error)
	GetTotalSystemReservedCPU(ctx context.Context) (int64, error)
	ContainerStoreBackdater
}

// ContainerStoreBackdater ages rows on the store's own clock, so a contract
// test can produce an operation claimed long ago or an outbox record
// published long ago. It is test support: the Postgres runner provides it
// with a statement, not the repository.
type ContainerStoreBackdater interface {
	// BackdateOperation moves the operation's last update age into the past.
	BackdateOperation(ctx context.Context, id uuid.UUID, age time.Duration) error
	// BackdateContainerOutbox moves the last update of the operation's outbox
	// record age into the past.
	BackdateContainerOutbox(ctx context.Context, operationID uuid.UUID, age time.Duration) error
}

// RunContainerStoreContract runs the container store behaviour tests against
// the stores newStore returns. Each subtest gets a fresh, empty store.
func RunContainerStoreContract(t *testing.T, newStore func(t *testing.T) ContainerStore) {
	t.Helper()
	t.Run("unfenced status writes", func(t *testing.T) {
		testUnfencedStatusWrites(t, newStore(t))
	})
	t.Run("delete and count by owner", func(t *testing.T) {
		testDeleteAndCountByOwner(t, newStore(t))
	})
	t.Run("created docker container record", func(t *testing.T) {
		testCreatedDockerContainerRecord(t, newStore(t))
	})
	t.Run("domain prefix lookup", func(t *testing.T) {
		testDomainPrefixLookup(t, newStore(t))
	})
	t.Run("generation-fenced status writes", func(t *testing.T) {
		testGenerationFencedStatusWrites(t, newStore(t))
	})
	t.Run("queued operation allows one active operation per container", func(t *testing.T) {
		testQueuedOperationIsExclusive(t, newStore(t))
	})
	t.Run("operation requeue", func(t *testing.T) {
		testOperationRequeue(t, newStore(t))
	})
	t.Run("container outbox claim and requeue", func(t *testing.T) {
		testContainerOutboxClaimAndRequeue(t, newStore(t))
	})
	t.Run("interrupted-create recovery", func(t *testing.T) {
		testInterruptedCreateRecovery(t, newStore(t))
	})
	t.Run("expired operation recovery", func(t *testing.T) {
		testExpiredOperationRecovery(t, newStore(t))
	})
	t.Run("interrupted non-resumable operations fail", func(t *testing.T) {
		testInterruptedNonResumableOperationsFail(t, newStore(t))
	})
	t.Run("lost delivery recovery", func(t *testing.T) {
		testLostDeliveryRecovery(t, newStore(t))
	})
	t.Run("lost delivery of an operation whose container is gone", func(t *testing.T) {
		testLostDeliveryOfRemovedContainer(t, newStore(t))
	})
	t.Run("cancel and delete of an active create", func(t *testing.T) {
		testCancelActiveCreateAndDelete(t, newStore(t))
	})
	t.Run("owner capacity lock", func(t *testing.T) {
		testOwnerCapacityLock(t, newStore(t))
	})
	t.Run("observation keeps a recorded error for an unchanged status", func(t *testing.T) {
		testObservationKeepsRecordedError(t, newStore(t))
	})
	t.Run("running containers", func(t *testing.T) {
		testRunningContainers(t, newStore(t))
	})
	t.Run("reservation rule", func(t *testing.T) {
		testReservationRule(t, newStore(t))
	})
	t.Run("reserving containers of one owner", func(t *testing.T) {
		testReservingContainersByOwner(t, newStore(t))
	})
}

func testUnfencedStatusWrites(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	c := contractContainer(uuid.New(), model.ContainerStatusStarting)
	c.DesiredStatus = model.ContainerStatusRunning
	c.DockerGeneration = 2
	require.NoError(t, store.Save(ctx, c))

	// An error mark sets status and last error, whatever the generation.
	require.NoError(t, store.MarkStatusError(ctx, c.ID, model.ContainerStatusMissing, errContractCause))
	got := getContainer(t, store, c.ID)
	require.Equal(t, model.ContainerStatusMissing, got.Status)
	require.Equal(t, model.ContainerStatusRunning, got.DesiredStatus)
	require.Equal(t, errContractCause.Error(), deref(got.LastError))
	require.NotNil(t, got.LastObservedAt)

	// A status write clears the last error and keeps the desired status and TTL.
	deadline := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, store.UpdateStatusAndTTLDeadline(ctx, c.ID, model.ContainerStatusRunning, &deadline))
	got = getContainer(t, store, c.ID)
	require.Equal(t, model.ContainerStatusRunning, got.Status)
	require.Nil(t, got.LastError)
	require.NotNil(t, got.TTLDeadline)
	require.WithinDuration(t, deadline, *got.TTLDeadline, time.Second)

	require.NoError(t, store.MarkStatusError(ctx, c.ID, model.ContainerStatusError, errContractCause))
	require.NoError(t, store.UpdateStatus(ctx, c.ID, model.ContainerStatusExited))
	got = getContainer(t, store, c.ID)
	require.Equal(t, model.ContainerStatusExited, got.Status)
	require.Equal(t, model.ContainerStatusRunning, got.DesiredStatus)
	require.Nil(t, got.LastError)
	require.NotNil(t, got.TTLDeadline, "a plain status write keeps the TTL deadline")
	require.Equal(t, 2, got.DockerGeneration)

	// A nil deadline clears the TTL; a nil cause clears the last error.
	require.NoError(t, store.UpdateStatusAndTTLDeadline(ctx, c.ID, model.ContainerStatusRunning, nil))
	require.Nil(t, getContainer(t, store, c.ID).TTLDeadline)
	require.NoError(t, store.MarkStatusError(ctx, c.ID, model.ContainerStatusError, nil))
	got = getContainer(t, store, c.ID)
	require.Equal(t, model.ContainerStatusError, got.Status)
	require.Nil(t, got.LastError)

	// Writes to an unknown container report not found.
	unknown := uuid.New()
	require.ErrorIs(t, store.UpdateStatus(ctx, unknown, model.ContainerStatusExited), apperrors.ErrNotFound)
	require.ErrorIs(t, store.UpdateStatusAndTTLDeadline(ctx, unknown, model.ContainerStatusRunning, nil), apperrors.ErrNotFound)
	require.ErrorIs(t, store.MarkStatusError(ctx, unknown, model.ContainerStatusError, errContractCause), apperrors.ErrNotFound)
}

func testDeleteAndCountByOwner(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	ownerID := uuid.New()
	first := contractContainer(ownerID, model.ContainerStatusExited)
	second := contractContainer(ownerID, model.ContainerStatusRunning)
	other := contractContainer(uuid.New(), model.ContainerStatusRunning)
	for _, c := range []model.Container{first, second, other} {
		require.NoError(t, store.Save(ctx, c))
	}
	count, err := store.CountByOwnerID(ctx, ownerID)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	// Deleting a container takes its outbox records with it and keeps its operations.
	deleteOp := contractOperation(first, model.OperationDelete)
	require.NoError(t, store.QueueContainerOperation(ctx, first.ID, model.ContainerStatusDeleting, model.ContainerStatusDeleting, deleteOp, contractOutbox(deleteOp)))
	require.NoError(t, store.Delete(ctx, first.ID))
	_, err = store.GetByID(ctx, first.ID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	require.Equal(t, model.OperationStatusPending, getOperation(t, store, deleteOp.ID).Status)
	require.Empty(t, leaseOutbox(t, store, 10))
	count, err = store.CountByOwnerID(ctx, ownerID)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// A second delete, or one for an unknown container, reports not found.
	require.ErrorIs(t, store.Delete(ctx, first.ID), apperrors.ErrNotFound)
	require.Equal(t, other.ID, getContainer(t, store, other.ID).ID)
	count, err = store.CountByOwnerID(ctx, uuid.New())
	require.NoError(t, err)
	require.Zero(t, count)
}

func testCreatedDockerContainerRecord(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	c := contractContainer(uuid.New(), model.ContainerStatusCreating)
	c.DockerID = ""
	c.DesiredStatus = model.ContainerStatusRunning
	c.DockerGeneration = 3
	require.NoError(t, store.Save(ctx, c))
	require.NoError(t, store.MarkStatusError(ctx, c.ID, model.ContainerStatusCreating, errContractCause))

	// A container saved without mounts has none.
	mounts, err := store.GetMountsByContainerID(ctx, c.ID)
	require.NoError(t, err)
	require.Empty(t, mounts)

	// Recording the Docker container sets status and desired status, clears
	// the last error and keeps the generation.
	require.NoError(t, store.UpdateDockerIDAndStatus(ctx, c.ID, "docker-created", model.ContainerStatusCreated))
	got := getContainer(t, store, c.ID)
	require.Equal(t, "docker-created", got.DockerID)
	require.Equal(t, model.ContainerStatusCreated, got.Status)
	require.Equal(t, model.ContainerStatusCreated, got.DesiredStatus)
	require.Nil(t, got.LastError)
	require.NotNil(t, got.LastObservedAt)
	require.Equal(t, 3, got.DockerGeneration)

	require.ErrorIs(t, store.UpdateDockerIDAndStatus(ctx, uuid.New(), "docker-other", model.ContainerStatusCreated), apperrors.ErrNotFound)
}

func testDomainPrefixLookup(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	routed := contractContainer(uuid.New(), model.ContainerStatusRunning)
	routed.DomainPrefix = "app-" + routed.ID.String()[:8]
	unrouted := contractContainer(uuid.New(), model.ContainerStatusRunning)
	require.NoError(t, store.Save(ctx, routed))
	require.NoError(t, store.Save(ctx, unrouted))

	exists, err := store.CheckDomainPrefixExists(ctx, routed.DomainPrefix)
	require.NoError(t, err)
	require.True(t, exists)
	exists, err = store.CheckDomainPrefixExists(ctx, "free-"+routed.ID.String()[:8])
	require.NoError(t, err)
	require.False(t, exists)
	exists, err = store.CheckDomainPrefixExists(ctx, "")
	require.NoError(t, err)
	require.False(t, exists, "an empty prefix routes nothing")
}

func testGenerationFencedStatusWrites(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	c := contractContainer(uuid.New(), model.ContainerStatusRunning)
	c.DockerGeneration = 2
	require.NoError(t, store.Save(ctx, c))

	// A write fenced on a generation the record no longer holds changes nothing.
	exitCode := 137
	desired := model.ContainerStatusExited
	require.ErrorIs(t, store.UpdateObservedStatusByContainerIDAndGeneration(ctx, c.ID, 1, model.ContainerStatusError, &exitCode, &desired, errContractCause), apperrors.ErrNotFound)
	got := getContainer(t, store, c.ID)
	require.Equal(t, model.ContainerStatusRunning, got.Status)
	require.Equal(t, model.ContainerStatusRunning, got.DesiredStatus)
	require.Nil(t, got.LastExitCode)

	// Generations start at 1, so generation 0 fences out every write.
	require.ErrorIs(t, store.UpdateObservedStatusByContainerIDAndGeneration(ctx, c.ID, 0, model.ContainerStatusExited, nil, nil, nil), apperrors.ErrNotFound)
	require.Equal(t, model.ContainerStatusRunning, getContainer(t, store, c.ID).Status)

	// The recorded generation's observation is written, keeping desired status when none is given.
	require.NoError(t, store.UpdateObservedStatusByContainerIDAndGeneration(ctx, c.ID, 2, model.ContainerStatusError, &exitCode, nil, errContractCause))
	got = getContainer(t, store, c.ID)
	require.Equal(t, model.ContainerStatusError, got.Status)
	require.Equal(t, model.ContainerStatusRunning, got.DesiredStatus)
	require.Equal(t, &exitCode, got.LastExitCode)
	require.Equal(t, errContractCause.Error(), deref(got.LastError))
	require.NotNil(t, got.LastObservedAt)

	// Recording a new generation fences out writes for the previous one.
	require.NoError(t, store.UpdateDockerIDRoutingAndGeneration(ctx, c.ID, "docker-gen-3", "web-gen3", 8080, 3))
	require.ErrorIs(t, store.UpdateObservedStatusByContainerIDAndGeneration(ctx, c.ID, 2, model.ContainerStatusRunning, nil, nil, nil), apperrors.ErrNotFound)
	require.NoError(t, store.UpdateObservedStatusByContainerIDAndGeneration(ctx, c.ID, 3, model.ContainerStatusRunning, nil, &desired, nil))
	got = getContainer(t, store, c.ID)
	require.Equal(t, 3, got.DockerGeneration)
	require.Equal(t, "docker-gen-3", got.DockerID)
	require.Equal(t, "web-gen3", got.DomainPrefix)
	require.Equal(t, 8080, got.InternalPort)
	require.Equal(t, model.ContainerStatusRunning, got.Status)
	require.Equal(t, model.ContainerStatusExited, got.DesiredStatus)
	require.Nil(t, got.LastExitCode)

	// Fenced writes to an unknown container report not found.
	unknown := uuid.New()
	require.ErrorIs(t, store.UpdateObservedStatusByContainerIDAndGeneration(ctx, unknown, 1, model.ContainerStatusExited, nil, nil, nil), apperrors.ErrNotFound)
	require.ErrorIs(t, store.UpdateDockerIDRoutingAndGeneration(ctx, unknown, "docker-x", "", 0, 2), apperrors.ErrNotFound)
}

func testQueuedOperationIsExclusive(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	c := contractContainer(uuid.New(), model.ContainerStatusExited)
	require.NoError(t, store.Save(ctx, c))

	start := contractOperation(c, model.OperationStart)
	require.NoError(t, store.QueueContainerOperation(ctx, c.ID, model.ContainerStatusStarting, model.ContainerStatusRunning, start, contractOutbox(start)))
	got := getContainer(t, store, c.ID)
	require.Equal(t, model.ContainerStatusStarting, got.Status)
	require.Equal(t, model.ContainerStatusRunning, got.DesiredStatus)
	op := getOperation(t, store, start.ID)
	require.Equal(t, model.OperationStatusPending, op.Status)
	require.Equal(t, model.OperationStart, op.Operation)
	require.Zero(t, op.Attempts)
	active, err := store.HasActiveOperation(ctx, model.ResourceTypeContainer, c.ID)
	require.NoError(t, err)
	require.True(t, active)

	// A second operation conflicts while the first is active and changes nothing.
	stop := contractOperation(c, model.OperationStop)
	require.ErrorIs(t, store.QueueContainerOperation(ctx, c.ID, model.ContainerStatusStopping, model.ContainerStatusExited, stop, contractOutbox(stop)), apperrors.ErrConflict)
	got = getContainer(t, store, c.ID)
	require.Equal(t, model.ContainerStatusStarting, got.Status)
	_, err = store.GetOperationByID(ctx, stop.ID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)

	// Once the first completes, the next operation queues.
	require.NoError(t, store.CompleteOperation(ctx, start.ID, model.OperationStatusDone, nil))
	require.NoError(t, store.QueueContainerOperation(ctx, c.ID, model.ContainerStatusStopping, model.ContainerStatusExited, stop, contractOutbox(stop)))
	require.Equal(t, model.ContainerStatusStopping, getContainer(t, store, c.ID).Status)

	// Queueing for an unknown container leaves no operation behind.
	missing := contractContainer(uuid.New(), model.ContainerStatusExited)
	orphan := contractOperation(missing, model.OperationStart)
	require.ErrorIs(t, store.QueueContainerOperation(ctx, missing.ID, model.ContainerStatusStarting, model.ContainerStatusRunning, orphan, contractOutbox(orphan)), apperrors.ErrNotFound)
	_, err = store.GetOperationByID(ctx, orphan.ID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

func testOperationRequeue(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	c, create := saveQueuedCreate(t, store, uuid.New())

	claimed, ok, err := store.ClaimPendingOperation(ctx, create.ID, 2)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, model.OperationStatusRunning, claimed.Status)
	require.Equal(t, 1, claimed.Attempts)

	// A running operation is not claimed again.
	again, ok, err := store.ClaimPendingOperation(ctx, create.ID, 2)
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, model.OperationStatusRunning, again.Status)
	require.Equal(t, 1, again.Attempts)

	// Requeue returns a running operation to pending, keeping its attempts and cause.
	require.NoError(t, store.RequeueOperation(ctx, create.ID, errContractCause))
	op := getOperation(t, store, create.ID)
	require.Equal(t, model.OperationStatusPending, op.Status)
	require.Equal(t, 1, op.Attempts)
	require.Equal(t, errContractCause.Error(), deref(op.LastError))
	active, err := store.HasActiveOperation(ctx, model.ResourceTypeContainer, c.ID)
	require.NoError(t, err)
	require.True(t, active)

	// Only a running operation can be requeued.
	require.ErrorIs(t, store.RequeueOperation(ctx, create.ID, errContractCause), apperrors.ErrNotFound)

	// The second claim uses the last attempt; afterwards the requeued operation stays pending.
	claimed, ok, err = store.ClaimPendingOperation(ctx, create.ID, 2)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 2, claimed.Attempts)
	require.NoError(t, store.RequeueOperation(ctx, create.ID, errContractCause))
	exhausted, ok, err := store.ClaimPendingOperation(ctx, create.ID, 2)
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, model.OperationStatusPending, exhausted.Status)
	require.Equal(t, 2, exhausted.Attempts)

	// No attempt limit claims it anyway.
	claimed, ok, err = store.ClaimPendingOperation(ctx, create.ID, 0)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 3, claimed.Attempts)

	// A completed operation is terminal.
	require.NoError(t, store.CompleteOperation(ctx, create.ID, model.OperationStatusDone, nil))
	op = getOperation(t, store, create.ID)
	require.Equal(t, model.OperationStatusDone, op.Status)
	require.Nil(t, op.LastError)
	require.ErrorIs(t, store.RequeueOperation(ctx, create.ID, errContractCause), apperrors.ErrNotFound)
	_, ok, err = store.ClaimPendingOperation(ctx, create.ID, 0)
	require.NoError(t, err)
	require.False(t, ok)
	active, err = store.HasActiveOperation(ctx, model.ResourceTypeContainer, c.ID)
	require.NoError(t, err)
	require.False(t, active)

	// Unknown operations report not found.
	unknown := uuid.New()
	_, err = store.GetOperationByID(ctx, unknown)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	_, _, err = store.ClaimPendingOperation(ctx, unknown, 0)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	require.ErrorIs(t, store.RequeueOperation(ctx, unknown, errContractCause), apperrors.ErrNotFound)
	require.ErrorIs(t, store.CompleteOperation(ctx, unknown, model.OperationStatusFailed, errContractCause), apperrors.ErrNotFound)
}

func testContainerOutboxClaimAndRequeue(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	_, first := saveQueuedCreate(t, store, uuid.New())
	_, second := saveQueuedCreate(t, store, uuid.New())

	// Leases go oldest first, up to the limit, and mark the record publishing.
	leased := leaseOutbox(t, store, 1)
	require.Len(t, leased, 1)
	firstItem := leased[0]
	require.Equal(t, first.ID, firstItem.OperationID)
	require.Equal(t, first.ResourceID, firstItem.ContainerID)
	require.Equal(t, model.ContainerOutboxStatusPublishing, firstItem.Status)
	require.Equal(t, 1, firstItem.Attempts)
	require.Equal(t, "dcm.contract", firstItem.Exchange)
	require.Equal(t, "container.create", firstItem.RoutingKey)
	require.JSONEq(t, `{"operation_id":"`+first.ID.String()+`"}`, string(firstItem.Payload))

	// A fresh lease is not handed out again.
	leased = leaseOutbox(t, store, 10)
	require.Len(t, leased, 1)
	secondItem := leased[0]
	require.Equal(t, second.ID, secondItem.OperationID)
	require.Empty(t, leaseOutbox(t, store, 10))

	// A record marked pending again is leased again, counting another attempt.
	require.NoError(t, store.MarkContainerOutboxPending(ctx, firstItem.ID, errContractCause))
	leased = leaseOutbox(t, store, 10)
	require.Len(t, leased, 1)
	require.Equal(t, firstItem.ID, leased[0].ID)
	require.Equal(t, 2, leased[0].Attempts)
	require.Equal(t, errContractCause.Error(), deref(leased[0].LastError))

	// Published and discarded records are never leased.
	require.NoError(t, store.MarkContainerOutboxPublished(ctx, firstItem.ID))
	require.NoError(t, store.MarkContainerOutboxDiscarded(ctx, secondItem.ID, errContractCause))
	require.Empty(t, leaseOutbox(t, store, 10))

	unknown := uuid.New()
	require.ErrorIs(t, store.MarkContainerOutboxPublished(ctx, unknown), apperrors.ErrNotFound)
	require.ErrorIs(t, store.MarkContainerOutboxPending(ctx, unknown, errContractCause), apperrors.ErrNotFound)
	require.ErrorIs(t, store.MarkContainerOutboxDiscarded(ctx, unknown, errContractCause), apperrors.ErrNotFound)
}

func testInterruptedCreateRecovery(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	ownerID := uuid.New()
	// The interrupted create: claimed and its message already published.
	_, interrupted := saveQueuedCreate(t, store, ownerID)
	publishOutbox(t, store)
	claimOperation(t, store, interrupted.ID)
	// A create still waiting for its message, also published.
	_, waiting := saveQueuedCreate(t, store, ownerID)
	publishOutbox(t, store)
	// A finished create and a running start, neither of which recovery touches.
	_, finished := saveQueuedCreate(t, store, ownerID)
	publishOutbox(t, store)
	claimOperation(t, store, finished.ID)
	require.NoError(t, store.CompleteOperation(ctx, finished.ID, model.OperationStatusDone, nil))
	started := contractContainer(ownerID, model.ContainerStatusExited)
	require.NoError(t, store.Save(ctx, started))
	start := contractOperation(started, model.OperationStart)
	require.NoError(t, store.QueueContainerOperation(ctx, started.ID, model.ContainerStatusStarting, model.ContainerStatusRunning, start, contractOutbox(start)))
	publishOutbox(t, store)
	claimOperation(t, store, start.ID)

	recovered, err := store.RecoverInterruptedContainerCreates(ctx, errContractCause, model.OperationRecoveryScope{})
	require.NoError(t, err)
	require.Equal(t, []model.RecoveredOperation{recoveredOf(interrupted)}, recovered)

	// The interrupted create is pending again with its attempt kept and the cause recorded.
	op := getOperation(t, store, interrupted.ID)
	require.Equal(t, model.OperationStatusPending, op.Status)
	require.Equal(t, 1, op.Attempts)
	require.Equal(t, errContractCause.Error(), deref(op.LastError))
	require.Nil(t, getOperation(t, store, waiting.ID).LastError)
	require.Equal(t, model.OperationStatusDone, getOperation(t, store, finished.ID).Status)
	require.Equal(t, model.OperationStatusRunning, getOperation(t, store, start.ID).Status)

	// Every pending create's message is published again; nothing else is.
	leased := leaseOutbox(t, store, 10)
	require.Len(t, leased, 2)
	require.Equal(t, interrupted.ID, leased[0].OperationID)
	require.Equal(t, waiting.ID, leased[1].OperationID)

	// The recovered create can be claimed again.
	claimed := claimOperation(t, store, interrupted.ID)
	require.Equal(t, 2, claimed.Attempts)

	recovered, err = store.RecoverInterruptedContainerCreates(ctx, errContractCause, model.OperationRecoveryScope{})
	require.NoError(t, err)
	require.Len(t, recovered, 1)
}

// The handler deadline plus headroom, and ages on either side of it.
const (
	contractExpiry    = 35 * time.Minute
	contractExpired   = 40 * time.Minute
	contractNotExpiry = 30 * time.Minute
	contractLostAfter = 5 * time.Minute
)

func testExpiredOperationRecovery(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	ownerID := uuid.New()
	// An expired create: claimed long ago and its message already published.
	_, expired := saveQueuedCreate(t, store, ownerID)
	publishOutbox(t, store)
	claimOperation(t, store, expired.ID)
	backdateOperation(t, store, expired.ID, contractExpired)
	// A create a worker may still be acting on.
	_, recent := saveQueuedCreate(t, store, ownerID)
	publishOutbox(t, store)
	claimOperation(t, store, recent.ID)
	backdateOperation(t, store, recent.ID, contractNotExpiry)
	// A create waiting for its published message, which a pass must not
	// publish again, unlike startup recovery.
	_, waiting := saveQueuedCreate(t, store, ownerID)
	publishOutbox(t, store)

	recovered, err := store.RecoverInterruptedContainerCreates(ctx, errContractCause, model.OperationRecoveryScope{RunningLongerThan: contractExpiry})
	require.NoError(t, err)
	require.Equal(t, []model.RecoveredOperation{recoveredOf(expired)}, recovered)

	// The expired create is pending again without returning its attempt.
	op := getOperation(t, store, expired.ID)
	require.Equal(t, model.OperationStatusPending, op.Status)
	require.Equal(t, 1, op.Attempts)
	require.Equal(t, errContractCause.Error(), deref(op.LastError))
	require.Equal(t, model.OperationStatusRunning, getOperation(t, store, recent.ID).Status)
	require.Equal(t, model.OperationStatusPending, getOperation(t, store, waiting.ID).Status)

	// Only the expired create's message is published again, with its identity.
	leased := leaseOutbox(t, store, 10)
	require.Len(t, leased, 1)
	require.Equal(t, expired.ID, leased[0].OperationID)
	require.JSONEq(t, string(contractOutbox(expired).Payload), string(leased[0].Payload))

	// Startup recovery returns the interrupted attempt instead.
	claimOperation(t, store, expired.ID)
	_, err = store.RecoverInterruptedContainerCreates(ctx, errContractCause, model.OperationRecoveryScope{ReturnAttempt: true})
	require.NoError(t, err)
	require.Equal(t, 1, getOperation(t, store, expired.ID).Attempts)
}

func testInterruptedNonResumableOperationsFail(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	ownerID := uuid.New()
	type queued struct {
		container model.Container
		op        model.ResourceOperation
	}
	queue := func(operation, status, desired string, age time.Duration) queued {
		c := contractContainer(ownerID, model.ContainerStatusRunning)
		require.NoError(t, store.Save(ctx, c))
		op := contractOperation(c, operation)
		require.NoError(t, store.QueueContainerOperation(ctx, c.ID, status, desired, op, contractOutbox(op)))
		publishOutbox(t, store)
		claimOperation(t, store, op.ID)
		backdateOperation(t, store, op.ID, age)
		return queued{container: getContainer(t, store, c.ID), op: op}
	}
	expired := []queued{
		queue(model.OperationStart, model.ContainerStatusStarting, model.ContainerStatusRunning, contractExpired),
		queue(model.OperationStop, model.ContainerStatusStopping, model.ContainerStatusExited, contractExpired),
		queue(model.OperationDelete, model.ContainerStatusDeleting, model.ContainerStatusDeleting, contractExpired),
		queue(model.OperationExpose, model.ContainerStatusExposing, model.ContainerStatusRunning, contractExpired),
	}
	recent := queue(model.OperationStart, model.ContainerStatusStarting, model.ContainerStatusRunning, contractNotExpiry)
	_, create := saveQueuedCreate(t, store, ownerID)
	claimOperation(t, store, create.ID)
	backdateOperation(t, store, create.ID, contractExpired)

	failed, err := store.FailInterruptedNonResumableOperations(ctx, errContractCause, contractExpiry)
	require.NoError(t, err)

	want := make([]model.RecoveredOperation, 0, len(expired))
	for _, q := range expired {
		want = append(want, recoveredOf(q.op))
		op := getOperation(t, store, q.op.ID)
		require.Equal(t, model.OperationStatusFailed, op.Status)
		require.Equal(t, errContractCause.Error(), deref(op.LastError))
		// The container row is untouched; state sync records what Docker reports.
		got := getContainer(t, store, q.container.ID)
		require.Equal(t, q.container.Status, got.Status)
		require.Equal(t, q.container.DesiredStatus, got.DesiredStatus)
	}
	require.ElementsMatch(t, want, failed)
	require.Equal(t, model.OperationStatusRunning, getOperation(t, store, recent.op.ID).Status)
	require.Equal(t, model.OperationStatusRunning, getOperation(t, store, create.ID).Status, "a create is resumable")

	// Without an age bound, as at startup, every non-resumable operation fails.
	failed, err = store.FailInterruptedNonResumableOperations(ctx, errContractCause, 0)
	require.NoError(t, err)
	require.Equal(t, []model.RecoveredOperation{recoveredOf(recent.op)}, failed)
}

func testLostDeliveryRecovery(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	ownerID := uuid.New()
	queue := func(operation string) model.ResourceOperation {
		c := contractContainer(ownerID, model.ContainerStatusExited)
		require.NoError(t, store.Save(ctx, c))
		op := contractOperation(c, operation)
		require.NoError(t, store.QueueContainerOperation(ctx, c.ID, model.ContainerStatusStarting, model.ContainerStatusRunning, op, contractOutbox(op)))
		return op
	}
	// Published long ago and still pending: its message is gone.
	published := queue(model.OperationStart)
	publishOutbox(t, store)
	backdateOutbox(t, store, published.ID, contractExpired)
	// Discarded by the relay long ago and still pending.
	discarded := queue(model.OperationStop)
	discardOutbox(t, store)
	backdateOutbox(t, store, discarded.ID, contractExpired)
	// Published recently: the message may still be queued.
	recent := queue(model.OperationExpose)
	publishOutbox(t, store)
	backdateOutbox(t, store, recent.ID, contractLostAfter-time.Minute)
	// A running operation with an old published record belongs to its worker.
	running := queue(model.OperationDelete)
	publishOutbox(t, store)
	claimOperation(t, store, running.ID)
	backdateOutbox(t, store, running.ID, contractExpired)
	// Records the relay still owns: pending, and leased for publishing.
	publishing := queue(model.OperationStart)
	leaseOutbox(t, store, 10)
	backdateOutbox(t, store, publishing.ID, contractExpired)
	unpublished := queue(model.OperationStop)
	backdateOutbox(t, store, unpublished.ID, contractExpired)

	got, err := store.RecoverLostContainerDeliveries(ctx, errContractCause, contractLostAfter)
	require.NoError(t, err)
	require.ElementsMatch(t, []model.RecoveredOperation{recoveredOf(published), recoveredOf(discarded)}, got.Republished)
	require.Empty(t, got.Ended)

	// The republished records are leased again with their message identity.
	// The pending record and the stale lease are leased by the relay's own
	// rules, as they always were.
	leased := leaseOutbox(t, store, 10)
	byOperation := make(map[uuid.UUID]model.ContainerLifecycleOutbox, len(leased))
	for _, item := range leased {
		byOperation[item.OperationID] = item
	}
	require.Len(t, byOperation, 4)
	require.JSONEq(t, string(contractOutbox(published).Payload), string(byOperation[published.ID].Payload))
	require.JSONEq(t, string(contractOutbox(discarded).Payload), string(byOperation[discarded.ID].Payload))
	require.Contains(t, byOperation, unpublished.ID)
	require.Contains(t, byOperation, publishing.ID)
	for _, id := range []uuid.UUID{published.ID, discarded.ID, recent.ID, publishing.ID, unpublished.ID} {
		require.Equal(t, model.OperationStatusPending, getOperation(t, store, id).Status)
	}
	require.Equal(t, model.OperationStatusRunning, getOperation(t, store, running.ID).Status)
}

func testLostDeliveryOfRemovedContainer(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	ownerID := uuid.New()
	queueAndRemove := func(operation string, age time.Duration) model.ResourceOperation {
		c := contractContainer(ownerID, model.ContainerStatusExited)
		require.NoError(t, store.Save(ctx, c))
		op := contractOperation(c, operation)
		require.NoError(t, store.QueueContainerOperation(ctx, c.ID, model.ContainerStatusDeleting, model.ContainerStatusDeleting, op, contractOutbox(op)))
		publishOutbox(t, store)
		// Removing the row removes its outbox record with it.
		require.NoError(t, store.Delete(ctx, c.ID))
		backdateOperation(t, store, op.ID, age)
		return op
	}
	deleted := queueAndRemove(model.OperationDelete, contractExpired)
	started := queueAndRemove(model.OperationStart, contractExpired)
	recent := queueAndRemove(model.OperationStop, contractLostAfter-time.Minute)

	got, err := store.RecoverLostContainerDeliveries(ctx, errContractCause, contractLostAfter)
	require.NoError(t, err)
	require.Empty(t, got.Republished)
	require.ElementsMatch(t, []model.RecoveredOperation{recoveredOf(deleted), recoveredOf(started)}, got.Ended)

	// A delete whose container is gone is done; anything else failed.
	require.Equal(t, model.OperationStatusDone, getOperation(t, store, deleted.ID).Status)
	op := getOperation(t, store, started.ID)
	require.Equal(t, model.OperationStatusFailed, op.Status)
	require.Equal(t, errContractCause.Error(), deref(op.LastError))
	require.Equal(t, model.OperationStatusPending, getOperation(t, store, recent.ID).Status)
	require.False(t, hasActiveOperation(t, store, deleted.ResourceID))
}

func recoveredOf(op model.ResourceOperation) model.RecoveredOperation {
	return model.RecoveredOperation{ID: op.ID, ResourceType: op.ResourceType, ResourceID: op.ResourceID, Operation: op.Operation}
}

func backdateOperation(t *testing.T, store ContainerStore, id uuid.UUID, age time.Duration) {
	t.Helper()
	require.NoError(t, store.BackdateOperation(context.Background(), id, age))
}

func backdateOutbox(t *testing.T, store ContainerStore, operationID uuid.UUID, age time.Duration) {
	t.Helper()
	require.NoError(t, store.BackdateContainerOutbox(context.Background(), operationID, age))
}

// discardOutbox leases and discards every pending outbox record, as the relay
// does for a message it decides not to publish.
func discardOutbox(t *testing.T, store ContainerStore) {
	t.Helper()
	for _, item := range leaseOutbox(t, store, 100) {
		require.NoError(t, store.MarkContainerOutboxDiscarded(context.Background(), item.ID, nil))
	}
}

func hasActiveOperation(t *testing.T, store ContainerStore, containerID uuid.UUID) bool {
	t.Helper()
	active, err := store.HasActiveOperation(context.Background(), model.ResourceTypeContainer, containerID)
	require.NoError(t, err)
	return active
}

func testCancelActiveCreateAndDelete(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	ownerID := uuid.New()
	pending, pendingCreate := saveQueuedCreate(t, store, ownerID)
	running, runningCreate := saveQueuedCreate(t, store, ownerID)
	claimOperation(t, store, runningCreate.ID)
	plain := contractContainer(ownerID, model.ContainerStatusExited)
	require.NoError(t, store.Save(ctx, plain))

	for _, cancel := range []struct {
		container model.Container
		create    model.ResourceOperation
	}{
		{pending, pendingCreate},
		{running, runningCreate},
	} {
		require.NoError(t, store.CancelActiveCreateAndDeleteContainer(ctx, cancel.container.ID, errContractCause))
		_, err := store.GetByID(ctx, cancel.container.ID)
		require.ErrorIs(t, err, apperrors.ErrNotFound)
		op := getOperation(t, store, cancel.create.ID)
		require.Equal(t, model.OperationStatusFailed, op.Status)
		require.Equal(t, errContractCause.Error(), deref(op.LastError))
		active, err := store.HasActiveOperation(ctx, model.ResourceTypeContainer, cancel.container.ID)
		require.NoError(t, err)
		require.False(t, active)
	}
	// Canceled creates leave no message to publish.
	require.Empty(t, leaseOutbox(t, store, 10))

	// A container without an active create is still deleted.
	require.NoError(t, store.CancelActiveCreateAndDeleteContainer(ctx, plain.ID, errContractCause))
	_, err := store.GetByID(ctx, plain.ID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)

	require.ErrorIs(t, store.CancelActiveCreateAndDeleteContainer(ctx, uuid.New(), errContractCause), apperrors.ErrNotFound)
}

func testOwnerCapacityLock(t *testing.T, store ContainerStore) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	owner, other := uuid.New(), uuid.New()

	release, err := store.AcquireOwnerCapacityLock(ctx, owner)
	require.NoError(t, err)

	// The lock covers host capacity too, so it excludes the same owner and every other owner.
	for _, waiter := range []uuid.UUID{owner, other} {
		waitCtx, cancelWait := context.WithTimeout(ctx, contractWait)
		_, err = store.AcquireOwnerCapacityLock(waitCtx, waiter)
		cancelWait()
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}

	// Release is idempotent and lets the next holder in.
	release()
	release()
	release, err = store.AcquireOwnerCapacityLock(ctx, other)
	require.NoError(t, err)
	release()

	// Ending the acquiring context releases the lock without calling release.
	holdCtx, endHold := context.WithCancel(ctx)
	_, err = store.AcquireOwnerCapacityLock(holdCtx, owner)
	require.NoError(t, err)
	endHold()
	release, err = store.AcquireOwnerCapacityLock(ctx, other)
	require.NoError(t, err)
	release()
}

// containerStatuses is every status a container row can hold.
var containerStatuses = []string{
	model.ContainerStatusPending,
	model.ContainerStatusCreating,
	model.ContainerStatusCreated,
	model.ContainerStatusStarting,
	model.ContainerStatusRunning,
	model.ContainerStatusStopping,
	model.ContainerStatusExited,
	model.ContainerStatusExposing,
	model.ContainerStatusError,
	model.ContainerStatusMissing,
	model.ContainerStatusDeleting,
	model.ContainerStatusReconciling,
}

// reservingDesiredStatuses pins the reservation rule: for each status, the
// desired statuses under which the container counts as reserved. A row counts
// when it is not missing and its status or desired status is running.
var reservingDesiredStatuses = map[string][]string{
	model.ContainerStatusPending:     {model.ContainerStatusRunning},
	model.ContainerStatusCreating:    {model.ContainerStatusRunning},
	model.ContainerStatusCreated:     {model.ContainerStatusRunning},
	model.ContainerStatusStarting:    {model.ContainerStatusRunning},
	model.ContainerStatusRunning:     containerStatuses,
	model.ContainerStatusStopping:    {model.ContainerStatusRunning},
	model.ContainerStatusExited:      {model.ContainerStatusRunning},
	model.ContainerStatusExposing:    {model.ContainerStatusRunning},
	model.ContainerStatusError:       {model.ContainerStatusRunning},
	model.ContainerStatusMissing:     nil,
	model.ContainerStatusDeleting:    {model.ContainerStatusRunning},
	model.ContainerStatusReconciling: {model.ContainerStatusRunning},
}

// A stop records why the platform stopped a container; the Docker events that
// follow report the exit the record already holds and must not erase it.
func testObservationKeepsRecordedError(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	c := contractContainer(uuid.New(), model.ContainerStatusStopping)
	require.NoError(t, store.Save(ctx, c))
	require.NoError(t, store.MarkStatusError(ctx, c.ID, model.ContainerStatusExited, errContractCause))

	// The same status without a cause keeps the recorded error and still
	// records the exit code.
	exitCode := 143
	require.NoError(t, store.UpdateObservedStatusByContainerIDAndGeneration(ctx, c.ID, 1, model.ContainerStatusExited, &exitCode, nil, nil))
	got := getContainer(t, store, c.ID)
	require.Equal(t, errContractCause.Error(), deref(got.LastError))
	require.Equal(t, &exitCode, got.LastExitCode)

	// A cause replaces it, whatever the status.
	other := contractError("another cause")
	require.NoError(t, store.UpdateObservedStatusByContainerIDAndGeneration(ctx, c.ID, 1, model.ContainerStatusExited, nil, nil, other))
	require.Equal(t, other.Error(), deref(getContainer(t, store, c.ID).LastError))

	// A new status without a cause clears it.
	require.NoError(t, store.UpdateObservedStatusByContainerIDAndGeneration(ctx, c.ID, 1, model.ContainerStatusRunning, nil, nil, nil))
	require.Nil(t, getContainer(t, store, c.ID).LastError)
}

func testRunningContainers(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	var want []model.Container
	for _, status := range containerStatuses {
		c := contractContainer(uuid.New(), status)
		c.DesiredStatus = model.ContainerStatusRunning
		c.BaseMemoryReservation, c.BaseCPUReservation = 64<<20, 125
		require.NoError(t, store.Save(ctx, c))
		if status == model.ContainerStatusRunning {
			want = append(want, model.Container{
				ID: c.ID, OwnerID: c.OwnerID, DockerID: c.DockerID,
				BaseMemoryReservation: c.BaseMemoryReservation, BaseCPUReservation: c.BaseCPUReservation,
			})
		}
	}
	second := contractContainer(uuid.New(), model.ContainerStatusRunning)
	require.NoError(t, store.Save(ctx, second))
	want = append(want, model.Container{
		ID: second.ID, OwnerID: second.OwnerID, DockerID: second.DockerID,
		BaseMemoryReservation: second.BaseMemoryReservation, BaseCPUReservation: second.BaseCPUReservation,
	})
	slices.SortFunc(want, func(a, b model.Container) int { return bytes.Compare(a.ID[:], b.ID[:]) })

	// Only rows whose status is running are returned, whatever they desire,
	// ordered by ID and carrying the fields a resource decision needs.
	got, err := store.GetRunning(ctx)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// testReservationRule tables every status against every desired status, one
// owner per row. Each row's model.Container.ReservesCapacity must agree with
// reservingDesiredStatuses and with what the reservation sums count, so the Go
// rule and the store's rule cannot drift apart.
func testReservationRule(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	const memory, cpu = int64(64 << 20), int64(125)
	reservedRows := 0
	for _, status := range containerStatuses {
		for _, desired := range containerStatuses {
			c := contractContainer(uuid.New(), status)
			c.DesiredStatus = desired
			c.BaseMemoryReservation, c.BaseCPUReservation = memory, cpu
			require.NoError(t, store.Save(ctx, c))

			reserves := c.ReservesCapacity()
			require.Equal(t, slices.Contains(reservingDesiredStatuses[status], desired), reserves,
				"ReservesCapacity for status %q desired %q", status, desired)
			wantMemory, wantCPU := int64(0), int64(0)
			if reserves {
				wantMemory, wantCPU = memory, cpu
				reservedRows++
			}
			gotMemory, err := store.GetUserReservedMemory(ctx, c.OwnerID)
			require.NoError(t, err)
			require.Equal(t, wantMemory, gotMemory, "memory reserved by status %q desired %q", status, desired)
			gotCPU, err := store.GetUserReservedCPU(ctx, c.OwnerID)
			require.NoError(t, err)
			require.Equal(t, wantCPU, gotCPU, "cpu reserved by status %q desired %q", status, desired)
		}
	}
	require.Equal(t, 22, reservedRows, "running rows plus non-missing rows desired running")

	totalMemory, err := store.GetTotalSystemReservedMemory(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(reservedRows)*memory, totalMemory)
	totalCPU, err := store.GetTotalSystemReservedCPU(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(reservedRows)*cpu, totalCPU)

	// An owner with no rows reserves nothing.
	none, err := store.GetUserReservedMemory(ctx, uuid.New())
	require.NoError(t, err)
	require.Zero(t, none)
}

func testReservingContainersByOwner(t *testing.T, store ContainerStore) {
	ctx := context.Background()
	ownerID := uuid.New()
	var want []model.Container
	for _, status := range containerStatuses {
		for _, desired := range containerStatuses {
			c := contractContainer(ownerID, status)
			c.DesiredStatus = desired
			require.NoError(t, store.Save(ctx, c))
			if c.ReservesCapacity() {
				want = append(want, model.Container{
					ID: c.ID, OwnerID: c.OwnerID, DockerID: c.DockerID, Status: c.Status, DesiredStatus: c.DesiredStatus,
				})
			}
		}
	}
	other := contractContainer(uuid.New(), model.ContainerStatusRunning)
	require.NoError(t, store.Save(ctx, other))
	slices.SortFunc(want, func(a, b model.Container) int { return bytes.Compare(a.ID[:], b.ID[:]) })

	// The reservation rule decides the rows, ordered by ID, carrying what a
	// stop needs; another owner's running container is not among them.
	got, err := store.ListReservingByOwner(ctx, ownerID)
	require.NoError(t, err)
	require.Len(t, got, 22)
	require.Equal(t, want, got)

	none, err := store.ListReservingByOwner(ctx, uuid.New())
	require.NoError(t, err)
	require.Empty(t, none)
}

// publishOutbox leases and publishes every pending outbox record.
func publishOutbox(t *testing.T, store ContainerStore) {
	t.Helper()
	for _, item := range leaseOutbox(t, store, 100) {
		require.NoError(t, store.MarkContainerOutboxPublished(context.Background(), item.ID))
	}
}

func claimOperation(t *testing.T, store ContainerStore, id uuid.UUID) model.ResourceOperation {
	t.Helper()
	op, ok, err := store.ClaimPendingOperation(context.Background(), id, 0)
	require.NoError(t, err)
	require.True(t, ok)
	return op
}

func leaseOutbox(t *testing.T, store ContainerStore, limit int) []model.ContainerLifecycleOutbox {
	t.Helper()
	items, err := store.LeasePendingContainerOutbox(context.Background(), limit)
	require.NoError(t, err)
	return items
}

// saveQueuedCreate records a pending container with its pending create
// operation and outbox record, as container create requests do.
func saveQueuedCreate(t *testing.T, store ContainerStore, ownerID uuid.UUID) (model.Container, model.ResourceOperation) {
	t.Helper()
	c := contractContainer(ownerID, model.ContainerStatusPending)
	c.DockerID = ""
	c.DesiredStatus = model.ContainerStatusCreated
	op := contractOperation(c, model.OperationCreate)
	require.NoError(t, store.SaveWithMountsOperationAndOutbox(context.Background(), c, nil, op, contractOutbox(op), false, false))
	return c, op
}

func contractOperation(c model.Container, operation string) model.ResourceOperation {
	return model.ResourceOperation{
		ID:           uuid.New(),
		ResourceType: model.ResourceTypeContainer,
		ResourceID:   c.ID,
		OwnerID:      c.OwnerID,
		Operation:    operation,
		Status:       model.OperationStatusPending,
	}
}

func contractOutbox(op model.ResourceOperation) model.ContainerLifecycleOutbox {
	return model.ContainerLifecycleOutbox{
		ID:          uuid.New(),
		OperationID: op.ID,
		ContainerID: op.ResourceID,
		Exchange:    "dcm.contract",
		RoutingKey:  "container." + op.Operation,
		Payload:     []byte(`{"operation_id":"` + op.ID.String() + `"}`),
		Status:      model.ContainerOutboxStatusPending,
	}
}

func getOperation(t *testing.T, store ContainerStore, id uuid.UUID) model.ResourceOperation {
	t.Helper()
	op, err := store.GetOperationByID(context.Background(), id)
	require.NoError(t, err)
	return op
}

type contractError string

func (e contractError) Error() string { return string(e) }

const errContractCause = contractError("contract cause")

func contractContainer(ownerID uuid.UUID, status string) model.Container {
	id := uuid.New()
	return model.Container{
		ID:                    id,
		OwnerID:               ownerID,
		DockerID:              "docker-" + id.String()[:12],
		Name:                  "web-" + id.String()[:8],
		ImageTag:              "nginx:latest",
		Status:                status,
		DesiredStatus:         status,
		EnvVars:               []byte(`{}`),
		BaseMemoryReservation: 256 << 20,
		BaseCPUReservation:    250,
		DockerGeneration:      1,
	}
}

func getContainer(t *testing.T, store ContainerStore, id uuid.UUID) model.Container {
	t.Helper()
	c, err := store.GetByID(context.Background(), id)
	require.NoError(t, err)
	return c
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// contractWait bounds how long a contract test waits for a store to block.
const contractWait = 200 * time.Millisecond
