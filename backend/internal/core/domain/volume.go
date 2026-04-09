package domain

import (
	"time"

	"github.com/google/uuid"
)

type Volume struct {
	ID            uuid.UUID
	OwnerID       uuid.UUID
	OwnerUsername string
	ProjectID     *uuid.UUID
	DockerName    string
	Driver        string
	DriverOpts    []byte
	CreatedAt     time.Time
}

type VolumeMount struct {
	ContainerID uuid.UUID
	VolumeID    uuid.UUID
	MountPath   string
	IsReadOnly  bool
}
