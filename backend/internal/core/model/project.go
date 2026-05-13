package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	ProjectStatusPending   = "pending"
	ProjectStatusBuilding  = "building"
	ProjectStatusDeploying = "deploying"
	ProjectStatusCanceling = "canceling"
	ProjectStatusCanceled  = "canceled"
	ProjectStatusStarting  = "starting"
	ProjectStatusRunning   = "running"
	ProjectStatusStopping  = "stopping"
	ProjectStatusStopped   = "stopped"
	ProjectStatusFailed    = "failed"
	ProjectStatusDeleting  = "deleting"
)

const (
	ComposeSourceTypeUpload = "upload"
	ComposeSourceTypeGit    = "git"

	ComposeDeploymentStatusQueued    = "queued"
	ComposeDeploymentStatusRunning   = "running"
	ComposeDeploymentStatusCanceling = "canceling"
	ComposeDeploymentStatusCanceled  = "canceled"
	ComposeDeploymentStatusSucceeded = "succeeded"
	ComposeDeploymentStatusFailed    = "failed"

	ComposeOutboxStatusPending    = "pending"
	ComposeOutboxStatusPublishing = "publishing"
	ComposeOutboxStatusPublished  = "published"
	ComposeOutboxStatusDiscarded  = "discarded"

	ComposeDeploymentStagePlanning = "planning"
	ComposeDeploymentStageBuilding = "building"
	ComposeDeploymentStageCreating = "creating"
	ComposeDeploymentStageStarting = "starting"
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

type ComposeDeploymentJob struct {
	ID              uuid.UUID
	ProjectID       uuid.UUID
	OwnerID         uuid.UUID
	SourceType      string
	SourceObjectKey string
	ComposeFile     string
	Status          string
	Attempts        int
	CancelRequested bool
	ErrorMessage    *string
	RequestID       string
	Stage           string
	PlanJSON        []byte
	ResourceMapJSON []byte
	CreatedAt       time.Time
	UpdatedAt       time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
}

type ComposeDeploymentOutbox struct {
	ID         uuid.UUID
	JobID      uuid.UUID
	Exchange   string
	RoutingKey string
	Payload    json.RawMessage
	Status     string
	Attempts   int
	LastError  *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func IsComposeDeploymentTerminalStatus(status string) bool {
	switch status {
	case ComposeDeploymentStatusCanceled, ComposeDeploymentStatusSucceeded, ComposeDeploymentStatusFailed:
		return true
	default:
		return false
	}
}
