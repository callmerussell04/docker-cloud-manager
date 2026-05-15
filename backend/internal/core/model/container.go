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
	MemoryLimitBytes int64
	NetworkRxBytes   int64
	NetworkTxBytes   int64
}

type ContainerRuntimeTarget struct {
	ContainerID      uuid.UUID
	DockerID         string
	Status           string
	OwnerID          uuid.UUID
	DockerGeneration int
}
