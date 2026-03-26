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
	ID              uuid.UUID
	OwnerID         uuid.UUID
	DockerID        string
	Name            string
	ImageTag        string
	InternalPort    int
	Status          string
	TTLDeadline     *time.Time
	EnvVars         []byte
	ResourcesConfig []byte
	CreatedAt       time.Time
}
