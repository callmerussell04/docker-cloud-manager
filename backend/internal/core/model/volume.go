package model

import (
	"time"

	"github.com/google/uuid"
)

type Volume struct {
	ID            uuid.UUID
	OwnerID       uuid.UUID
	OwnerUsername string
	// ProjectID is the volume's oldest project link, read from the link
	// table. On create it names the project the volume is created in.
	ProjectID       *uuid.UUID
	Name            string
	DockerName      string
	Status          string
	LastObservedAt  *time.Time
	LastError       *string
	UsedBytes       int64
	UsageObservedAt *time.Time
	// SizeBytes is reserved against the owner's disk quota from creation and
	// never changes.
	SizeBytes int64
	// SizeEnforced reports whether Docker was given SizeBytes at creation, so
	// the kernel refuses writes past it. Otherwise the volume usage worker
	// checks the size periodically.
	SizeEnforced bool
	CreatedAt    time.Time
	// UsedBy names the containers that mount the volume, by name. Only a
	// volume list fills it; Core refuses to delete a volume while it is
	// non-empty.
	UsedBy []VolumeUser
}

// VolumeUser is a container that mounts a volume.
type VolumeUser struct {
	ContainerID uuid.UUID
	Name        string
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
	// SizeBytes is the requested size; zero means the configured default.
	SizeBytes int64
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
