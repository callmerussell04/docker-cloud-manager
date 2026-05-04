package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	ProjectStatusPending   = "pending"
	ProjectStatusBuilding  = "building"
	ProjectStatusDeploying = "deploying"
	ProjectStatusStarting  = "starting"
	ProjectStatusRunning   = "running"
	ProjectStatusStopping  = "stopping"
	ProjectStatusStopped   = "stopped"
	ProjectStatusFailed    = "failed"
	ProjectStatusDeleting  = "deleting"
)

type Project struct {
	ID            uuid.UUID
	OwnerID       uuid.UUID
	OwnerUsername string
	Name          string
	Status        string
	ErrorMessage  *string
	CreatedAt     time.Time
}

type ProjectServiceNode struct {
	ProjectID    uuid.UUID
	ContainerID  uuid.UUID
	ServiceName  string
	StartOrder   int
	Dependencies []ProjectServiceDependency
}

type ProjectServiceDependency struct {
	ProjectID            uuid.UUID
	ContainerID          uuid.UUID
	DependsOnContainerID uuid.UUID
	DependsOnServiceName string
	Condition            string
	Optional             bool
}
