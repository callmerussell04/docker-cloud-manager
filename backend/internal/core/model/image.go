package model

import (
	"time"

	"github.com/google/uuid"
)

type Image struct {
	ID            uuid.UUID
	OwnerID       uuid.UUID
	OwnerUsername string
	Tag           string
	// RegistryTag is the tag this record's image is pushed and pulled under
	// in its owner's Registry repository. A build's record uses its own ID,
	// so a rebuild never overwrites the image available under Tag.
	RegistryTag string
	// Digest is the manifest digest recorded when a build makes the image
	// available; nil for images made available before it was recorded.
	Digest *string
	// SizeBytes is the image's size as the Registry reports it.
	SizeBytes      int64
	Status         string
	LastObservedAt *time.Time
	LastError      *string
	CreatedAt      time.Time
}

const (
	ImageStatusBuilding  = "building"
	ImageStatusAvailable = "available"
	ImageStatusDeleting  = "deleting"
	ImageStatusMissing   = "missing"
	ImageStatusError     = "error"
)
