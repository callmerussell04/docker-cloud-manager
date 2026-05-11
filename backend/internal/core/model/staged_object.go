package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	StagedObjectKindBuildArchive  = "build_archive"
	StagedObjectKindComposeSource = "compose_source"

	StagedObjectStatusActive   = "active"
	StagedObjectStatusReleased = "released"
)

type StagedObjectReservation struct {
	ID            uuid.UUID
	OwnerID       uuid.UUID
	ObjectKey     string
	Kind          string
	BytesReserved int64
	Status        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ReleasedAt    *time.Time
}
