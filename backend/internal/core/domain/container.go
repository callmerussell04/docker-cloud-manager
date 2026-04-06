package domain

import (
	"time"

	"github.com/google/uuid"
)

const (
	ContainerStatusCreated = "created"
	ContainerStatusRunning = "running"
	ContainerStatusExited  = "exited"
	ContainerStatusError   = "error"
)

type Container struct {
	ID                    uuid.UUID
	OwnerID               uuid.UUID
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
