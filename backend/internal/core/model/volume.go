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

// VolumeUsageScan is the resumable progress of one volume usage measurement.
// Bytes is the apparent size of files counted so far; it is a complete
// measurement only when Complete is true.
type VolumeUsageScan struct {
	// Cursor is the slash-separated path, relative to the volume root, of the
	// last entry counted in walk order. Empty before the first entry.
	Cursor   string
	Bytes    int64
	Entries  int64
	Complete bool
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
