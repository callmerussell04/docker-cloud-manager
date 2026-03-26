package domain

import (
	"time"

	"github.com/google/uuid"
)

type Image struct {
	ID        uuid.UUID
	OwnerID   uuid.UUID
	Tag       string
	SizeMB    int
	IsCustom  bool
	Metadata  []byte
	CreatedAt time.Time
}
