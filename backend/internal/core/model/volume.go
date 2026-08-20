package model

import (
	"time"

	"github.com/google/uuid"
)

type Volume struct {
	ID              uuid.UUID
	OwnerID         uuid.UUID
	OwnerUsername   string
	ProjectID       *uuid.UUID
	Name            string
	DockerName      string
	Status          string
	LastObservedAt  *time.Time
	LastError       *string
	UsedBytes       int64
	UsageObservedAt *time.Time
	CreatedAt       time.Time
}

const (
	VolumeStatusCreating  = "creating"
	VolumeStatusAvailable = "available"
	VolumeStatusDeleting  = "deleting"
	VolumeStatusMissing   = "missing"
	VolumeStatusError     = "error"
)

type VolumeCreateParams struct {
	ProjectID *uuid.UUID
	Name      string
}

type ComposeVolume struct {
	Alias    string
	Name     string
	External bool
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
