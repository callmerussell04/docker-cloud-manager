package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	ContainerStatusCreating = "creating"
	ContainerStatusCreated  = "created"
	ContainerStatusRunning  = "running"
	ContainerStatusExited   = "exited"
	ContainerStatusError    = "error"
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
	TTLDeadline           *time.Time
	EnvVars               []byte
	BaseMemoryReservation int64
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
