package coretest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"
)

// ContainerStoreFake is an in-memory stand-in for Core's container repository:
// containers, their resource operations, the container lifecycle outbox, the
// owner capacity lock and the reservation sums admission reads.
// RunContainerStoreContract holds it to the Postgres repository's behaviour,
// so add a method here only together with the contract test that pins it.
type ContainerStoreFake struct {
	mu         sync.Mutex
	faults     ContainerStoreFaults
	containers map[uuid.UUID]model.Container
	operations map[uuid.UUID]model.ResourceOperation
	// outbox holds lifecycle outbox records in creation order.
	outbox []model.ContainerLifecycleOutbox
	// capacity is the owner capacity lock, held by one caller at a time. Like
	// the repository's owner and host-capacity advisory locks, it serialises
	// every owner.
	capacity chan struct{}
}

var (
	_ ContainerStore                              = (*ContainerStoreFake)(nil)
	_ service.ContainerOutboxRepository           = (*ContainerStoreFake)(nil)
	_ service.BuildContainerReservationRepository = (*ContainerStoreFake)(nil)
)

// NewContainerStoreFake returns an empty store with no faults.
func NewContainerStoreFake() *ContainerStoreFake {
	return &ContainerStoreFake{
		containers: make(map[uuid.UUID]model.Container),
		operations: make(map[uuid.UUID]model.ResourceOperation),
		capacity:   make(chan struct{}, 1),
	}
}

// Fault makes one ContainerStoreFake method fail. The zero Fault never fails.
type Fault struct {
	err error
	// remaining counts the calls still to fail; a negative count never runs out.
	remaining int
}

// FailAlways makes every call fail with err.
func FailAlways(err error) Fault { return Fault{err: err, remaining: -1} }

// FailNext makes the next n calls fail with err; later calls succeed.
func FailNext(n int, err error) Fault { return Fault{err: err, remaining: n} }

// take reports the error for one call and counts it.
func (f *Fault) take() error {
	if f.err == nil || f.remaining == 0 {
		return nil
	}
	if f.remaining > 0 {
		f.remaining--
	}
	return f.err
}

// ContainerStoreFaults holds one Fault per ContainerStoreFake method. A failing
// call returns the fault's error and changes nothing.
type ContainerStoreFaults struct {
	Save                                           Fault
	SaveWithMountsOperationAndOutbox               Fault
	GetByID                                        Fault
	CountByOwnerID                                 Fault
	CheckDomainPrefixExists                        Fault
	GetMountsByContainerID                         Fault
	Delete                                         Fault
	UpdateStatus                                   Fault
	UpdateStatusAndTTLDeadline                     Fault
	MarkStatusError                                Fault
	UpdateStatusByContainerIDAndGeneration         Fault
	UpdateObservedStatusByContainerIDAndGeneration Fault
	UpdateDockerIDAndStatus                        Fault
	UpdateDockerIDRoutingAndGeneration             Fault
	QueueContainerOperation                        Fault
	GetRunning                                     Fault
	HasActiveOperation                             Fault
	GetOperationByID                               Fault
	ClaimPendingOperation                          Fault
	RequeueOperation                               Fault
	CompleteOperation                              Fault
	LeasePendingContainerOutbox                    Fault
	MarkContainerOutboxPublished                   Fault
	MarkContainerOutboxPending                     Fault
	MarkContainerOutboxDiscarded                   Fault
	RecoverInterruptedContainerCreates             Fault
	CancelActiveCreateAndDeleteContainer           Fault
	AcquireOwnerCapacityLock                       Fault
	GetUserReservedMemory                          Fault
	GetUserReservedCPU                             Fault
	GetTotalSystemReservedMemory                   Fault
	GetTotalSystemReservedCPU                      Fault
}

// SetFaults replaces every fault; the zero value clears them.
func (f *ContainerStoreFake) SetFaults(faults ContainerStoreFaults) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults = faults
}

func (f *ContainerStoreFake) Save(_ context.Context, c model.Container) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.Save.take(); err != nil {
		return err
	}
	return f.insertContainer(c)
}

// SaveWithMountsOperationAndOutbox records the container, and the operation
// and outbox record when they are set, all or nothing. The lock flags are
// ignored: the fake serialises every call. Volume mounts are not modelled.
func (f *ContainerStoreFake) SaveWithMountsOperationAndOutbox(_ context.Context, c model.Container, mounts []model.VolumeMount, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox, _, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.SaveWithMountsOperationAndOutbox.take(); err != nil {
		return err
	}
	if len(mounts) > 0 {
		return errMountsNotModelled
	}
	if _, exists := f.containers[c.ID]; exists {
		return apperrors.ErrAlreadyExists
	}
	withOperation := op.ResourceID != uuid.Nil
	if withOperation {
		if err := f.checkNoActiveOperation(op); err != nil {
			return err
		}
	}
	if err := f.insertContainer(c); err != nil {
		return err
	}
	if withOperation {
		f.insertOperation(op)
	}
	if outbox.OperationID != uuid.Nil {
		f.insertOutbox(outbox)
	}
	return nil
}

func (f *ContainerStoreFake) GetByID(_ context.Context, id uuid.UUID) (model.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.GetByID.take(); err != nil {
		return model.Container{}, err
	}
	c, ok := f.containers[id]
	if !ok {
		return model.Container{}, apperrors.ErrNotFound
	}
	return cloneContainer(c), nil
}

func (f *ContainerStoreFake) CountByOwnerID(_ context.Context, ownerID uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.CountByOwnerID.take(); err != nil {
		return 0, err
	}
	count := 0
	for _, c := range f.containers {
		if c.OwnerID == ownerID {
			count++
		}
	}
	return count, nil
}

// CheckDomainPrefixExists reports whether a container is routed under prefix.
// An empty prefix routes nothing.
func (f *ContainerStoreFake) CheckDomainPrefixExists(_ context.Context, prefix string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.CheckDomainPrefixExists.take(); err != nil {
		return false, err
	}
	if prefix == "" {
		return false, nil
	}
	for _, c := range f.containers {
		if c.DomainPrefix == prefix {
			return true, nil
		}
	}
	return false, nil
}

// GetMountsByContainerID returns no mounts: the fake stores none, because
// SaveWithMountsOperationAndOutbox rejects them.
func (f *ContainerStoreFake) GetMountsByContainerID(_ context.Context, _ uuid.UUID) ([]model.VolumeMountParams, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.GetMountsByContainerID.take(); err != nil {
		return nil, err
	}
	return []model.VolumeMountParams{}, nil
}

// Delete removes the container and its outbox records. Its operations stay.
func (f *ContainerStoreFake) Delete(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.Delete.take(); err != nil {
		return err
	}
	if _, ok := f.containers[id]; !ok {
		return apperrors.ErrNotFound
	}
	f.deleteContainer(id)
	return nil
}

// UpdateStatus writes status whatever generation the container records, and
// clears the last error.
func (f *ContainerStoreFake) UpdateStatus(_ context.Context, id uuid.UUID, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.UpdateStatus.take(); err != nil {
		return err
	}
	return f.updateContainer(id, func(c *model.Container) {
		c.Status = status
		c.LastObservedAt = ptr(time.Now())
		c.LastError = nil
	})
}

// UpdateStatusAndTTLDeadline writes status and the TTL deadline, which a nil
// deadline clears, and clears the last error.
func (f *ContainerStoreFake) UpdateStatusAndTTLDeadline(_ context.Context, id uuid.UUID, status string, ttlDeadline *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.UpdateStatusAndTTLDeadline.take(); err != nil {
		return err
	}
	return f.updateContainer(id, func(c *model.Container) {
		c.Status = status
		c.TTLDeadline = clonePtr(ttlDeadline)
		c.LastObservedAt = ptr(time.Now())
		c.LastError = nil
	})
}

// MarkStatusError writes status and the cause as last error, which a nil cause
// clears.
func (f *ContainerStoreFake) MarkStatusError(_ context.Context, id uuid.UUID, status string, cause error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.MarkStatusError.take(); err != nil {
		return err
	}
	return f.updateContainer(id, func(c *model.Container) {
		c.Status = status
		c.LastObservedAt = ptr(time.Now())
		c.LastError = errorText(cause)
	})
}

// UpdateStatusByContainerIDAndGeneration writes status only while the
// container records generation, and clears the last error.
func (f *ContainerStoreFake) UpdateStatusByContainerIDAndGeneration(_ context.Context, containerID uuid.UUID, generation int, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.UpdateStatusByContainerIDAndGeneration.take(); err != nil {
		return err
	}
	return f.updateRecordedGeneration(containerID, generation, func(c *model.Container) {
		c.Status = status
		c.LastObservedAt = ptr(time.Now())
		c.LastError = nil
	})
}

// UpdateObservedStatusByContainerIDAndGeneration writes an observation only
// while the container records generation. A nil desiredStatus keeps the
// current one, and an observation without a cause that reports the status
// already recorded keeps the last error.
func (f *ContainerStoreFake) UpdateObservedStatusByContainerIDAndGeneration(_ context.Context, containerID uuid.UUID, generation int, status string, exitCode *int, desiredStatus *string, cause error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.UpdateObservedStatusByContainerIDAndGeneration.take(); err != nil {
		return err
	}
	return f.updateRecordedGeneration(containerID, generation, func(c *model.Container) {
		if cause != nil || c.Status != status {
			c.LastError = errorText(cause)
		}
		c.Status = status
		if desiredStatus != nil {
			c.DesiredStatus = *desiredStatus
		}
		c.LastExitCode = clonePtr(exitCode)
		c.LastObservedAt = ptr(time.Now())
	})
}

// UpdateDockerIDAndStatus records the container's Docker container and sets
// both its status and desired status, keeping its generation.
func (f *ContainerStoreFake) UpdateDockerIDAndStatus(_ context.Context, id uuid.UUID, dockerID string, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.UpdateDockerIDAndStatus.take(); err != nil {
		return err
	}
	return f.updateContainer(id, func(c *model.Container) {
		c.DockerID = dockerID
		c.Status = status
		c.DesiredStatus = status
		c.LastObservedAt = ptr(time.Now())
		c.LastError = nil
	})
}

// UpdateDockerIDRoutingAndGeneration records a new Docker runtime for the
// container, whatever generation it held before.
func (f *ContainerStoreFake) UpdateDockerIDRoutingAndGeneration(_ context.Context, id uuid.UUID, dockerID string, domainPrefix string, internalPort int, generation int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.UpdateDockerIDRoutingAndGeneration.take(); err != nil {
		return err
	}
	return f.updateContainer(id, func(c *model.Container) {
		c.DockerID = dockerID
		c.DomainPrefix = domainPrefix
		c.InternalPort = internalPort
		c.DockerGeneration = generation
		c.LastObservedAt = ptr(time.Now())
		c.LastError = nil
	})
}

// QueueContainerOperation records op and its outbox record and sets the
// container's status and desired status, all or nothing.
func (f *ContainerStoreFake) QueueContainerOperation(_ context.Context, id uuid.UUID, status string, desiredStatus string, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.QueueContainerOperation.take(); err != nil {
		return err
	}
	if err := f.checkNoActiveOperation(op); err != nil {
		return err
	}
	if err := f.updateContainer(id, func(c *model.Container) {
		c.Status = status
		c.DesiredStatus = desiredStatus
		c.LastError = nil
		c.LastObservedAt = ptr(time.Now())
	}); err != nil {
		return err
	}
	f.insertOperation(op)
	f.insertOutbox(outbox)
	return nil
}

// GetRunning returns the identity, Docker container and reservations of every
// container whose status is running, ordered by ID.
func (f *ContainerStoreFake) GetRunning(context.Context) ([]model.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.GetRunning.take(); err != nil {
		return nil, err
	}
	var running []model.Container
	for _, c := range f.containers {
		if c.Status != model.ContainerStatusRunning {
			continue
		}
		running = append(running, model.Container{
			ID:                    c.ID,
			OwnerID:               c.OwnerID,
			DockerID:              c.DockerID,
			BaseMemoryReservation: c.BaseMemoryReservation,
			BaseCPUReservation:    c.BaseCPUReservation,
		})
	}
	slices.SortFunc(running, func(a, b model.Container) int { return bytes.Compare(a.ID[:], b.ID[:]) })
	return running, nil
}

func (f *ContainerStoreFake) HasActiveOperation(_ context.Context, resourceType string, resourceID uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.HasActiveOperation.take(); err != nil {
		return false, err
	}
	return f.activeOperation(resourceType, resourceID), nil
}

func (f *ContainerStoreFake) GetOperationByID(_ context.Context, id uuid.UUID) (model.ResourceOperation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.GetOperationByID.take(); err != nil {
		return model.ResourceOperation{}, err
	}
	op, ok := f.operations[id]
	if !ok {
		return model.ResourceOperation{}, apperrors.ErrNotFound
	}
	return cloneOperation(op), nil
}

// ClaimPendingOperation moves a pending operation with attempts left to
// running and counts the attempt. Otherwise it returns the operation unclaimed.
// A maxAttempts of zero or less means no limit.
func (f *ContainerStoreFake) ClaimPendingOperation(_ context.Context, id uuid.UUID, maxAttempts int) (model.ResourceOperation, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.ClaimPendingOperation.take(); err != nil {
		return model.ResourceOperation{}, false, err
	}
	op, ok := f.operations[id]
	if !ok {
		return model.ResourceOperation{}, false, apperrors.ErrNotFound
	}
	if op.Status != model.OperationStatusPending || (maxAttempts > 0 && op.Attempts >= maxAttempts) {
		return cloneOperation(op), false, nil
	}
	op.Status = model.OperationStatusRunning
	op.Attempts++
	op.UpdatedAt = time.Now()
	f.operations[id] = op
	return cloneOperation(op), true, nil
}

// RequeueOperation returns a running operation to pending.
func (f *ContainerStoreFake) RequeueOperation(_ context.Context, id uuid.UUID, cause error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.RequeueOperation.take(); err != nil {
		return err
	}
	op, ok := f.operations[id]
	if !ok || op.Status != model.OperationStatusRunning {
		return apperrors.ErrNotFound
	}
	f.setOperationStatus(id, model.OperationStatusPending, cause)
	return nil
}

func (f *ContainerStoreFake) CompleteOperation(_ context.Context, id uuid.UUID, status string, cause error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.CompleteOperation.take(); err != nil {
		return err
	}
	if _, ok := f.operations[id]; !ok {
		return apperrors.ErrNotFound
	}
	f.setOperationStatus(id, status, cause)
	return nil
}

// LeasePendingContainerOutbox leases up to limit records, oldest first: pending
// ones, and publishing ones whose lease has gone stale. A leased record is
// publishing and counts another attempt.
func (f *ContainerStoreFake) LeasePendingContainerOutbox(_ context.Context, limit int) ([]model.ContainerLifecycleOutbox, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.LeasePendingContainerOutbox.take(); err != nil {
		return nil, err
	}
	now := time.Now()
	leased := make([]model.ContainerLifecycleOutbox, 0)
	for i := range f.outbox {
		if len(leased) >= limit {
			break
		}
		item := &f.outbox[i]
		stale := item.Status == model.ContainerOutboxStatusPublishing && item.UpdatedAt.Before(now.Add(-outboxLeaseTimeout))
		if item.Status != model.ContainerOutboxStatusPending && !stale {
			continue
		}
		item.Status = model.ContainerOutboxStatusPublishing
		item.Attempts++
		item.UpdatedAt = now
		leased = append(leased, cloneOutbox(*item))
	}
	return leased, nil
}

func (f *ContainerStoreFake) MarkContainerOutboxPublished(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.MarkContainerOutboxPublished.take(); err != nil {
		return err
	}
	return f.setOutboxStatus(id, model.ContainerOutboxStatusPublished, nil)
}

func (f *ContainerStoreFake) MarkContainerOutboxPending(_ context.Context, id uuid.UUID, cause error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.MarkContainerOutboxPending.take(); err != nil {
		return err
	}
	return f.setOutboxStatus(id, model.ContainerOutboxStatusPending, cause)
}

func (f *ContainerStoreFake) MarkContainerOutboxDiscarded(_ context.Context, id uuid.UUID, cause error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.MarkContainerOutboxDiscarded.take(); err != nil {
		return err
	}
	return f.setOutboxStatus(id, model.ContainerOutboxStatusDiscarded, cause)
}

// RecoverInterruptedContainerCreates returns running container creates to
// pending, then marks the outbox record of every pending container create
// pending so its message is published again. It reports how many creates were
// running.
func (f *ContainerStoreFake) RecoverInterruptedContainerCreates(_ context.Context, cause error) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.RecoverInterruptedContainerCreates.take(); err != nil {
		return 0, err
	}
	var recovered int64
	for id, op := range f.operations {
		if isContainerCreate(op) && op.Status == model.OperationStatusRunning {
			f.setOperationStatus(id, model.OperationStatusPending, cause)
			recovered++
		}
	}
	now := time.Now()
	for i := range f.outbox {
		op := f.operations[f.outbox[i].OperationID]
		if isContainerCreate(op) && op.Status == model.OperationStatusPending {
			f.outbox[i].Status = model.ContainerOutboxStatusPending
			f.outbox[i].LastError = errorText(cause)
			f.outbox[i].UpdatedAt = now
		}
	}
	return recovered, nil
}

// CancelActiveCreateAndDeleteContainer fails the container's active create
// operations and deletes the container with its outbox records, all or nothing.
func (f *ContainerStoreFake) CancelActiveCreateAndDeleteContainer(_ context.Context, containerID uuid.UUID, cause error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.faults.CancelActiveCreateAndDeleteContainer.take(); err != nil {
		return err
	}
	if _, ok := f.containers[containerID]; !ok {
		return apperrors.ErrNotFound
	}
	for id, op := range f.operations {
		if isContainerCreate(op) && op.ResourceID == containerID && isActiveOperation(op.Status) {
			f.setOperationStatus(id, model.OperationStatusFailed, cause)
		}
	}
	f.deleteContainer(containerID)
	return nil
}

// AcquireOwnerCapacityLock waits for the owner capacity lock until ctx ends. The
// lock is released by the returned function, which is idempotent, or when ctx
// ends.
func (f *ContainerStoreFake) AcquireOwnerCapacityLock(ctx context.Context, _ uuid.UUID) (func(), error) {
	f.mu.Lock()
	err := f.faults.AcquireOwnerCapacityLock.take()
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case f.capacity <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("acquire admission lock: %w", ctx.Err())
	}
	var once sync.Once
	release := func() { once.Do(func() { <-f.capacity }) }
	stop := context.AfterFunc(ctx, release)
	return func() {
		stop()
		release()
	}, nil
}

// GetUserReservedMemory sums the memory reserved by ownerID's containers.
func (f *ContainerStoreFake) GetUserReservedMemory(_ context.Context, ownerID uuid.UUID) (int64, error) {
	return f.sumReserved(&f.faults.GetUserReservedMemory, &ownerID, func(c model.Container) int64 { return c.BaseMemoryReservation })
}

// GetUserReservedCPU sums the CPU millicores reserved by ownerID's containers.
func (f *ContainerStoreFake) GetUserReservedCPU(_ context.Context, ownerID uuid.UUID) (int64, error) {
	return f.sumReserved(&f.faults.GetUserReservedCPU, &ownerID, func(c model.Container) int64 { return c.BaseCPUReservation })
}

// GetTotalSystemReservedMemory sums the memory reserved by every container.
func (f *ContainerStoreFake) GetTotalSystemReservedMemory(_ context.Context) (int64, error) {
	return f.sumReserved(&f.faults.GetTotalSystemReservedMemory, nil, func(c model.Container) int64 { return c.BaseMemoryReservation })
}

// GetTotalSystemReservedCPU sums the CPU millicores reserved by every container.
func (f *ContainerStoreFake) GetTotalSystemReservedCPU(_ context.Context) (int64, error) {
	return f.sumReserved(&f.faults.GetTotalSystemReservedCPU, nil, func(c model.Container) int64 { return c.BaseCPUReservation })
}

// sumReserved sums amount over the reserved containers, of ownerID when set.
// A container is reserved when it is not missing and its status or desired
// status is running, as in the repository's reservation queries.
func (f *ContainerStoreFake) sumReserved(fault *Fault, ownerID *uuid.UUID, amount func(model.Container) int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := fault.take(); err != nil {
		return 0, err
	}
	var total int64
	for _, c := range f.containers {
		if ownerID != nil && c.OwnerID != *ownerID {
			continue
		}
		if c.Status == model.ContainerStatusMissing {
			continue
		}
		if c.Status == model.ContainerStatusRunning || c.DesiredStatus == model.ContainerStatusRunning {
			total += amount(c)
		}
	}
	return total, nil
}

// outboxLeaseTimeout is how long a publishing lease holds before the record
// can be leased again, as in the repository's lease query.
const outboxLeaseTimeout = time.Minute

// errMountsNotModelled rejects volume mounts, which the fake does not store.
var errMountsNotModelled = errors.New("container store fake: volume mounts are not modelled")

// errOperationInProgress matches the repository's mapping of the unique index
// that allows one pending or running operation per resource.
var errOperationInProgress = apperrors.New(apperrors.ErrConflict, "resource operation is already in progress")

// insertContainer applies the column defaults of the containers table.
func (f *ContainerStoreFake) insertContainer(c model.Container) error {
	if _, exists := f.containers[c.ID]; exists {
		return apperrors.ErrAlreadyExists
	}
	if c.DesiredStatus == "" {
		c.DesiredStatus = c.Status
	}
	if c.DesiredStatus == "" {
		c.DesiredStatus = model.ContainerStatusCreated
	}
	if c.DockerGeneration <= 0 {
		c.DockerGeneration = 1
	}
	c.CreatedAt = time.Now()
	f.containers[c.ID] = cloneContainer(c)
	return nil
}

// updateContainer applies update to the container with id, or reports not
// found.
func (f *ContainerStoreFake) updateContainer(id uuid.UUID, update func(*model.Container)) error {
	c, ok := f.containers[id]
	if !ok {
		return apperrors.ErrNotFound
	}
	update(&c)
	f.containers[id] = c
	return nil
}

// updateRecordedGeneration applies update only while the container records
// generation, and reports not found otherwise.
func (f *ContainerStoreFake) updateRecordedGeneration(id uuid.UUID, generation int, update func(*model.Container)) error {
	if c, ok := f.containers[id]; !ok || c.DockerGeneration != generation {
		return apperrors.ErrNotFound
	}
	return f.updateContainer(id, update)
}

// deleteContainer removes the container and, as the outbox foreign key
// cascades, its outbox records. Its operations stay.
func (f *ContainerStoreFake) deleteContainer(id uuid.UUID) {
	delete(f.containers, id)
	f.outbox = slices.DeleteFunc(f.outbox, func(item model.ContainerLifecycleOutbox) bool {
		return item.ContainerID == id
	})
}

func (f *ContainerStoreFake) activeOperation(resourceType string, resourceID uuid.UUID) bool {
	for _, op := range f.operations {
		if op.ResourceType == resourceType && op.ResourceID == resourceID && isActiveOperation(op.Status) {
			return true
		}
	}
	return false
}

func (f *ContainerStoreFake) checkNoActiveOperation(op model.ResourceOperation) error {
	status := op.Status
	if status == "" {
		status = model.OperationStatusPending
	}
	if isActiveOperation(status) && f.activeOperation(op.ResourceType, op.ResourceID) {
		return errOperationInProgress
	}
	return nil
}

// insertOperation applies the column defaults of resource_operations. Callers
// check checkNoActiveOperation first.
func (f *ContainerStoreFake) insertOperation(op model.ResourceOperation) {
	if op.ID == uuid.Nil {
		op.ID = uuid.New()
	}
	if op.Status == "" {
		op.Status = model.OperationStatusPending
	}
	now := time.Now()
	op.CreatedAt = now
	op.UpdatedAt = now
	f.operations[op.ID] = cloneOperation(op)
}

func (f *ContainerStoreFake) setOperationStatus(id uuid.UUID, status string, cause error) {
	op := f.operations[id]
	op.Status = status
	op.LastError = errorText(cause)
	op.UpdatedAt = time.Now()
	f.operations[id] = op
}

// insertOutbox applies the column defaults of container_lifecycle_queue_outbox.
func (f *ContainerStoreFake) insertOutbox(item model.ContainerLifecycleOutbox) {
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	if item.Status == "" {
		item.Status = model.ContainerOutboxStatusPending
	}
	now := time.Now()
	item.CreatedAt = now
	item.UpdatedAt = now
	f.outbox = append(f.outbox, cloneOutbox(item))
}

func (f *ContainerStoreFake) setOutboxStatus(id uuid.UUID, status string, cause error) error {
	for i := range f.outbox {
		if f.outbox[i].ID == id {
			f.outbox[i].Status = status
			f.outbox[i].LastError = errorText(cause)
			f.outbox[i].UpdatedAt = time.Now()
			return nil
		}
	}
	return apperrors.ErrNotFound
}

func isContainerCreate(op model.ResourceOperation) bool {
	return op.ResourceType == model.ResourceTypeContainer && op.Operation == model.OperationCreate
}

func isActiveOperation(status string) bool {
	return status == model.OperationStatusPending || status == model.OperationStatusRunning
}

func cloneContainer(c model.Container) model.Container {
	c.ProjectID = clonePtr(c.ProjectID)
	c.TTLDeadline = clonePtr(c.TTLDeadline)
	c.EnvVars = slices.Clone(c.EnvVars)
	c.LastObservedAt = clonePtr(c.LastObservedAt)
	c.LastError = clonePtr(c.LastError)
	c.LastExitCode = clonePtr(c.LastExitCode)
	c.Command = slices.Clone(c.Command)
	c.Entrypoint = slices.Clone(c.Entrypoint)
	if c.Healthcheck != nil {
		healthcheck := *c.Healthcheck
		healthcheck.Test = slices.Clone(healthcheck.Test)
		c.Healthcheck = &healthcheck
	}
	return c
}

func cloneOperation(op model.ResourceOperation) model.ResourceOperation {
	op.LastError = clonePtr(op.LastError)
	return op
}

func cloneOutbox(item model.ContainerLifecycleOutbox) model.ContainerLifecycleOutbox {
	item.Payload = slices.Clone(item.Payload)
	item.LastError = clonePtr(item.LastError)
	return item
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func ptr[T any](v T) *T { return &v }

// errorText stores a cause the way the repository stores last_error.
func errorText(cause error) *string {
	if cause == nil {
		return nil
	}
	return ptr(cause.Error())
}
