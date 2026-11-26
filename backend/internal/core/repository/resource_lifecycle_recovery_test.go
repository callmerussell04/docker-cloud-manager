package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

var errRecoveryCause = errors.New("recovery cause")

// The handler deadline plus headroom, ages on either side of it, and the age
// after which a published message counts as lost.
const (
	recoveryExpiry    = 35 * time.Minute
	recoveryExpired   = 40 * time.Minute
	recoveryNotExpiry = 30 * time.Minute
	recoveryLostAfter = 5 * time.Minute
)

type resourceRecoveryFixture struct {
	db        *pgxpool.Pool
	lifecycle *repository.ResourceLifecycleRepository
	volumes   *repository.VolumeRepository
	images    *repository.ImageRepository
	ownerID   uuid.UUID
}

func newResourceRecoveryFixture(t *testing.T) *resourceRecoveryFixture {
	t.Helper()
	db := dbtest.OpenCorePostgres(t)
	return &resourceRecoveryFixture{
		db:        db,
		lifecycle: repository.NewResourceLifecycleRepository(db),
		volumes:   repository.NewVolumeRepository(db),
		images:    repository.NewImageRepository(db),
		ownerID:   uuid.New(),
	}
}

// queue records a pending volume create, volume delete or image delete with
// its outbox record, as the request paths do, and returns the operation.
func (f *resourceRecoveryFixture) queue(t *testing.T, kind string) model.ResourceOperation {
	t.Helper()
	ctx := context.Background()
	resourceID := uuid.New()
	op := model.ResourceOperation{ID: uuid.New(), ResourceID: resourceID, OwnerID: f.ownerID, Status: model.OperationStatusPending}
	outbox := model.ResourceLifecycleOutbox{ID: uuid.New(), OperationID: op.ID, ResourceID: resourceID, Exchange: "dcm.test", RoutingKey: "resource", Payload: []byte(`{"kind": "` + kind + `"}`), Status: model.ResourceOutboxStatusPending}
	volume := model.Volume{ID: resourceID, OwnerID: f.ownerID, Name: "data-" + resourceID.String()[:8], DockerName: "vol_" + resourceID.String(), SizeBytes: 1 << 20}
	switch kind {
	case "volume create":
		op.ResourceType, op.Operation = model.ResourceTypeVolume, model.OperationCreate
		outbox.ResourceType = model.ResourceTypeVolume
		volume.Status = model.VolumeStatusCreating
		require.NoError(t, f.lifecycle.CreateQueuedVolume(ctx, volume, op, outbox))
	case "volume delete":
		op.ResourceType, op.Operation = model.ResourceTypeVolume, model.OperationDelete
		outbox.ResourceType = model.ResourceTypeVolume
		volume.Status = model.VolumeStatusAvailable
		require.NoError(t, f.volumes.Save(ctx, volume))
		require.NoError(t, f.lifecycle.QueueVolumeDelete(ctx, resourceID, op, outbox))
	case "image delete":
		op.ResourceType, op.Operation = model.ResourceTypeImage, model.OperationDelete
		outbox.ResourceType = model.ResourceTypeImage
		require.NoError(t, f.images.Save(ctx, model.Image{ID: resourceID, OwnerID: f.ownerID, Tag: "demo-" + resourceID.String()[:8] + ":latest", RegistryTag: "latest", Status: model.ImageStatusAvailable}))
		require.NoError(t, f.lifecycle.QueueImageDelete(ctx, resourceID, op, outbox))
	default:
		t.Fatalf("unknown kind %q", kind)
	}
	return op
}

// publish leases and publishes every pending outbox record.
func (f *resourceRecoveryFixture) publish(t *testing.T) {
	t.Helper()
	for _, item := range f.lease(t) {
		require.NoError(t, f.lifecycle.MarkResourceOutboxPublished(context.Background(), item.ID))
	}
}

// discard leases and discards every pending outbox record, as the relay does.
func (f *resourceRecoveryFixture) discard(t *testing.T) {
	t.Helper()
	for _, item := range f.lease(t) {
		require.NoError(t, f.lifecycle.MarkResourceOutboxDiscarded(context.Background(), item.ID, nil))
	}
}

func (f *resourceRecoveryFixture) lease(t *testing.T) []model.ResourceLifecycleOutbox {
	t.Helper()
	items, err := f.lifecycle.LeasePendingResourceOutbox(context.Background(), 100)
	require.NoError(t, err)
	return items
}

func (f *resourceRecoveryFixture) claim(t *testing.T, id uuid.UUID) {
	t.Helper()
	_, claimed, err := f.lifecycle.ClaimPendingOperation(context.Background(), id, 3)
	require.NoError(t, err)
	require.True(t, claimed)
}

func (f *resourceRecoveryFixture) operation(t *testing.T, id uuid.UUID) model.ResourceOperation {
	t.Helper()
	op, err := f.lifecycle.GetOperationByID(context.Background(), id)
	require.NoError(t, err)
	return op
}

func (f *resourceRecoveryFixture) backdateOperation(t *testing.T, id uuid.UUID, age time.Duration) {
	t.Helper()
	require.NoError(t, backdate(context.Background(), f.db, `UPDATE resource_operations SET updated_at = updated_at - make_interval(secs => $2) WHERE id = $1`, id, age))
}

func (f *resourceRecoveryFixture) backdateOutbox(t *testing.T, operationID uuid.UUID, age time.Duration) {
	t.Helper()
	require.NoError(t, backdate(context.Background(), f.db, `UPDATE resource_lifecycle_queue_outbox SET updated_at = updated_at - make_interval(secs => $2) WHERE operation_id = $1`, operationID, age))
}

func (f *resourceRecoveryFixture) removeResource(t *testing.T, op model.ResourceOperation) {
	t.Helper()
	if op.ResourceType == model.ResourceTypeImage {
		require.NoError(t, f.images.Delete(context.Background(), op.ResourceID))
		return
	}
	require.NoError(t, f.volumes.Delete(context.Background(), op.ResourceID))
}

func recovered(op model.ResourceOperation) model.RecoveredOperation {
	return model.RecoveredOperation{ID: op.ID, ResourceType: op.ResourceType, ResourceID: op.ResourceID, Operation: op.Operation}
}

func operationsOf(items []model.ResourceLifecycleOutbox) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.OperationID)
	}
	return ids
}

var resourceKinds = []string{"volume create", "volume delete", "image delete"}

func TestResourceLifecycleRecoveryRequeuesExpiredOperationsWithoutReturningTheAttempt(t *testing.T) {
	for _, kind := range resourceKinds {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			f := newResourceRecoveryFixture(t)
			expired := f.queue(t, kind)
			f.publish(t)
			f.claim(t, expired.ID)
			f.backdateOperation(t, expired.ID, recoveryExpired)
			recent := f.queue(t, kind)
			f.publish(t)
			f.claim(t, recent.ID)
			f.backdateOperation(t, recent.ID, recoveryNotExpiry)
			// Waiting for its published message: a pass does not publish it twice.
			waiting := f.queue(t, kind)
			f.publish(t)

			got, err := f.lifecycle.RecoverInterruptedOperations(ctx, errRecoveryCause, model.OperationRecoveryScope{RunningLongerThan: recoveryExpiry})

			require.NoError(t, err)
			require.Equal(t, []model.RecoveredOperation{recovered(expired)}, got)
			op := f.operation(t, expired.ID)
			require.Equal(t, model.OperationStatusPending, op.Status)
			require.Equal(t, 1, op.Attempts, "an expired operation spent its attempt")
			require.Equal(t, errRecoveryCause.Error(), *op.LastError)
			require.Equal(t, model.OperationStatusRunning, f.operation(t, recent.ID).Status)
			require.Equal(t, model.OperationStatusPending, f.operation(t, waiting.ID).Status)
			require.Equal(t, []uuid.UUID{expired.ID}, operationsOf(f.lease(t)))
		})
	}
}

// Startup recovery keeps its outcomes on the shared statements: every running
// resumable operation returns to pending with its attempt back, and every
// pending one's message is published again.
func TestResourceLifecycleStartupRecoveryOutcomesAreUnchanged(t *testing.T) {
	ctx := context.Background()
	f := newResourceRecoveryFixture(t)
	var running, pending []model.ResourceOperation
	for _, kind := range resourceKinds {
		op := f.queue(t, kind)
		f.publish(t)
		f.claim(t, op.ID)
		running = append(running, op)
		waiting := f.queue(t, kind)
		f.publish(t)
		pending = append(pending, waiting)
	}

	got, err := f.lifecycle.RecoverInterruptedOperations(ctx, errRecoveryCause, model.OperationRecoveryScope{ReturnAttempt: true})

	require.NoError(t, err)
	want := make([]model.RecoveredOperation, 0, len(running))
	leasable := make([]uuid.UUID, 0, len(running)+len(pending))
	for _, op := range running {
		want = append(want, recovered(op))
		got := f.operation(t, op.ID)
		require.Equal(t, model.OperationStatusPending, got.Status)
		require.Zero(t, got.Attempts)
		leasable = append(leasable, op.ID)
	}
	for _, op := range pending {
		leasable = append(leasable, op.ID)
	}
	require.ElementsMatch(t, want, got)
	require.ElementsMatch(t, leasable, operationsOf(f.lease(t)))
}

func TestResourceLifecycleRecoveryFailsExpiredNonResumableOperations(t *testing.T) {
	ctx := context.Background()
	f := newResourceRecoveryFixture(t)
	containers := repository.NewContainerRepository(f.db, f.db)
	expired := f.queue(t, "volume create")
	f.claim(t, expired.ID)
	f.backdateOperation(t, expired.ID, recoveryExpired)

	failed, err := containers.FailInterruptedNonResumableOperations(ctx, errRecoveryCause, recoveryExpiry)

	require.NoError(t, err)
	require.Empty(t, failed, "volume and image operations are resumable")
	require.Equal(t, model.OperationStatusRunning, f.operation(t, expired.ID).Status)
}

func TestResourceLifecycleRecoveryRepublishesLostDeliveries(t *testing.T) {
	for _, kind := range resourceKinds {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			f := newResourceRecoveryFixture(t)
			published := f.queue(t, kind)
			f.publish(t)
			f.backdateOutbox(t, published.ID, recoveryExpired)
			discarded := f.queue(t, kind)
			f.discard(t)
			f.backdateOutbox(t, discarded.ID, recoveryExpired)
			recent := f.queue(t, kind)
			f.publish(t)
			f.backdateOutbox(t, recent.ID, recoveryLostAfter-time.Minute)
			running := f.queue(t, kind)
			f.publish(t)
			f.claim(t, running.ID)
			f.backdateOutbox(t, running.ID, recoveryExpired)
			unpublished := f.queue(t, kind)
			f.backdateOutbox(t, unpublished.ID, recoveryExpired)

			got, err := f.lifecycle.RecoverLostResourceDeliveries(ctx, errRecoveryCause, recoveryLostAfter)

			require.NoError(t, err)
			require.ElementsMatch(t, []model.RecoveredOperation{recovered(published), recovered(discarded)}, got.Republished)
			require.Empty(t, got.Ended)
			leased := f.lease(t)
			require.ElementsMatch(t, []uuid.UUID{published.ID, discarded.ID, unpublished.ID}, operationsOf(leased))
			for _, item := range leased {
				require.JSONEq(t, `{"kind": "`+kind+`"}`, string(item.Payload))
			}
		})
	}
}

func TestResourceLifecycleRecoveryEndsLostOperationsWhoseResourceIsGone(t *testing.T) {
	for _, kind := range resourceKinds {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			f := newResourceRecoveryFixture(t)
			gone := f.queue(t, kind)
			f.discard(t)
			f.removeResource(t, gone)
			f.backdateOutbox(t, gone.ID, recoveryExpired)
			f.backdateOperation(t, gone.ID, recoveryExpired)
			// The relay still owns a record that is pending.
			owned := f.queue(t, kind)
			f.removeResource(t, owned)
			f.backdateOutbox(t, owned.ID, recoveryExpired)
			f.backdateOperation(t, owned.ID, recoveryExpired)

			got, err := f.lifecycle.RecoverLostResourceDeliveries(ctx, errRecoveryCause, recoveryLostAfter)

			require.NoError(t, err)
			require.Empty(t, got.Republished)
			require.Equal(t, []model.RecoveredOperation{recovered(gone)}, got.Ended)
			op := f.operation(t, gone.ID)
			if gone.Operation == model.OperationDelete {
				require.Equal(t, model.OperationStatusDone, op.Status)
			} else {
				require.Equal(t, model.OperationStatusFailed, op.Status)
				require.Equal(t, errRecoveryCause.Error(), *op.LastError)
			}
			require.Equal(t, model.OperationStatusPending, f.operation(t, owned.ID).Status)
		})
	}
}
