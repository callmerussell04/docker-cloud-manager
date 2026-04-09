package domain

import (
	"time"

	"github.com/google/uuid"
)

const (
	ProjectStatusPending   = "pending"
	ProjectStatusBuilding  = "building"
	ProjectStatusDeploying = "deploying"
	ProjectStatusRunning   = "running"
	ProjectStatusStopped   = "stopped"
	ProjectStatusFailed    = "failed"
)

type Project struct {
	ID           uuid.UUID
	OwnerID      uuid.UUID
	Name         string
	Status       string
	ErrorMessage *string
	CreatedAt    time.Time
}
