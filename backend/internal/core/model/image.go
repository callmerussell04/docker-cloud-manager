package model

import (
	"time"

	"github.com/google/uuid"
)

type Image struct {
	ID             uuid.UUID
	OwnerID        uuid.UUID
	OwnerUsername  string
	Tag            string
	SizeMB         int
	Metadata       []byte
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
