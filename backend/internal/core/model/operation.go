package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	ResourceTypeContainer = "container"
	ResourceTypeVolume    = "volume"
	ResourceTypeImage     = "image"
	ResourceTypeProject   = "project"
	ResourceTypeBuild     = "build"

	OperationCreate = "create"
	OperationStart  = "start"
	OperationStop   = "stop"
	OperationDelete = "delete"
	OperationExpose = "expose"

	OperationStatusPending = "pending"
	OperationStatusRunning = "running"
	OperationStatusDone    = "done"
	OperationStatusFailed  = "failed"
)

type ResourceOperation struct {
	ID           uuid.UUID
	ResourceType string
	ResourceID   uuid.UUID
	OwnerID      uuid.UUID
	Operation    string
	Status       string
	Attempts     int
	LastError    *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
