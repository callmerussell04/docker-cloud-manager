package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	ResourceTypeContainer = "container"
	ResourceTypeVolume    = "volume"
	ResourceTypeImage     = "image"
	ResourceTypeProject   = "project"
	ResourceTypeBuild     = "build"

	OperationCreate = "create"
	OperationStart  = "start"
	OperationStop   = "stop"
	OperationDelete = "delete"
	OperationExpose = "expose"

	OperationStatusPending = "pending"
	OperationStatusRunning = "running"
	OperationStatusDone    = "done"
	OperationStatusFailed  = "failed"
)

type ResourceOperation struct {
	ID           uuid.UUID
	ResourceType string
	ResourceID   uuid.UUID
	OwnerID      uuid.UUID
	Operation    string
	Status       string
	Attempts     int
	LastError    *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

const (
	ContainerOutboxStatusPending    = "pending"
	ContainerOutboxStatusPublishing = "publishing"
	ContainerOutboxStatusPublished  = "published"
	ContainerOutboxStatusDiscarded  = "discarded"

	ResourceOutboxStatusPending    = "pending"
	ResourceOutboxStatusPublishing = "publishing"
	ResourceOutboxStatusPublished  = "published"
	ResourceOutboxStatusDiscarded  = "discarded"
)

type ContainerLifecycleOutbox struct {
	ID          uuid.UUID
	OperationID uuid.UUID
	ContainerID uuid.UUID
	Exchange    string
	RoutingKey  string
	Payload     []byte
	Status      string
	Attempts    int
	LastError   *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ResourceLifecycleOutbox struct {
	ID           uuid.UUID
	OperationID  uuid.UUID
	ResourceType string
	ResourceID   uuid.UUID
	Exchange     string
	RoutingKey   string
	Payload      []byte
	Status       string
	Attempts     int
	LastError    *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// OperationRecoveryScope selects the running operations a recovery statement
// settles. Startup recovery and the operation recovery pass share the
// statements and differ only in their scope.
type OperationRecoveryScope struct {
	// RunningLongerThan, when positive, settles only operations claimed longer
	// ago than this on the database clock. Zero settles every running
	// operation, as startup recovery does before any worker runs.
	RunningLongerThan time.Duration
	// ReturnAttempt gives the attempt the interrupted worker spent back to
	// the operation's retry budget.
	ReturnAttempt bool
}

// RecoveredOperation names a queued operation that a recovery statement
// settled.
type RecoveredOperation struct {
	ID           uuid.UUID
	ResourceType string
	ResourceID   uuid.UUID
	Operation    string
}

// LostDeliveryRecovery reports the pending operations whose message was no
// longer in the queue: republished ones, and ones ended because their
// resource no longer exists.
type LostDeliveryRecovery struct {
	Republished []RecoveredOperation
	Ended       []RecoveredOperation
}

// RecoveredDeployment names a queued Compose deployment job whose message was
// republished.
type RecoveredDeployment struct {
	JobID     uuid.UUID
	ProjectID uuid.UUID
}
