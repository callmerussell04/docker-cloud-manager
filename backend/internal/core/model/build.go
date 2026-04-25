package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	BuildStatusPending = "pending"
	BuildStatusRunning = "running"
	BuildStatusSuccess = "success"
	BuildStatusFailed  = "failed"
)

type Build struct {
	ID            uuid.UUID
	ImageID       uuid.UUID
	OwnerID       uuid.UUID
	OwnerUsername string
	Status        string
	LogFilePath   string
	StartedAt     time.Time
	FinishedAt    *time.Time
}
