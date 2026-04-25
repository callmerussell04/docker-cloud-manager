package model

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

type VolumeCreateParams struct {
	ProjectID *uuid.UUID
	Name      string
}

type VolumeMount struct {
	ContainerID uuid.UUID
	VolumeID    uuid.UUID
	MountPath   string
	IsReadOnly  bool
}

type VolumeMountParams struct {
	VolumeID   uuid.UUID
	VolumeName string
	MountPath  string
	IsReadOnly bool
}
