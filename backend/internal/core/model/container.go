package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	ContainerStatusPending     = "pending"
	ContainerStatusCreating    = "creating"
	ContainerStatusCreated     = "created"
	ContainerStatusStarting    = "starting"
	ContainerStatusRunning     = "running"
	ContainerStatusStopping    = "stopping"
	ContainerStatusExited      = "exited"
	ContainerStatusExposing    = "exposing"
	ContainerStatusError       = "error"
	ContainerStatusMissing     = "missing"
	ContainerStatusDeleting    = "deleting"
	ContainerStatusReconciling = "reconciling"
)

type Container struct {
	ID                    uuid.UUID
	OwnerID               uuid.UUID
	OwnerUsername         string
	ProjectID             *uuid.UUID
	DockerID              string
	Name                  string
	ImageTag              string
	InternalPort          int
	DomainPrefix          string
	Status                string
	DesiredStatus         string
	TTLDeadline           *time.Time
	EnvVars               []byte
	BaseMemoryReservation int64
	BaseCPUReservation    int64
	LastObservedAt        *time.Time
	LastError             *string
	LastExitCode          *int
	DockerGeneration      int
	NetworkAlias          string
	Command               []string
	Entrypoint            []string
	Restart               string
	Healthcheck           *Healthcheck
	CreatedAt             time.Time
}

// ReservesCapacity is the reservation rule: a container reserves its memory
// and CPU when it is not missing and its status or desired status is running.
// The repository's reservation queries apply the same rule in SQL.
func (c Container) ReservesCapacity() bool {
	if c.Status == ContainerStatusMissing {
		return false
	}
	return c.Status == ContainerStatusRunning || c.DesiredStatus == ContainerStatusRunning
}

type ContainerCreateParams struct {
	ProjectID         *uuid.UUID
	Name              string
	NetworkAlias      string
	ImageTag          string
	InternalPort      int
	EnvVars           map[string]string
	VolumeMounts      []VolumeMountParams
	RequestedMemoryMB int64
	DomainPrefix      string

	Command     []string
	Entrypoint  []string
	Restart     string
	Healthcheck *Healthcheck
}

type ContainerStats struct {
	CPUPercentage    float64
	MemoryUsageBytes int64
	// MemoryLimitBytes is the limit the container runs with now; above
	// MemoryStandardLimitBytes the difference is borrowed.
	MemoryLimitBytes         int64
	MemoryStandardLimitBytes int64
	NetworkRxBytes           int64
	NetworkTxBytes           int64
}

type ContainerRuntimeTarget struct {
	ContainerID      uuid.UUID
	DockerID         string
	Status           string
	OwnerID          uuid.UUID
	DockerGeneration int
}
