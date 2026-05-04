package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	BuildStatusPending             = "pending"
	BuildStatusRunning             = "running"
	BuildStatusSuccess             = "success"
	BuildStatusCanceled            = "canceled"
	BuildStatusFailed              = "failed"
	BuildStatusFailedTimeout       = "failed_timeout"
	BuildStatusFailedQuotaExceeded = "failed_quota_exceeded"
	BuildStatusFailedInternal      = "failed_internal"

	BuildOutboxStatusPending    = "pending"
	BuildOutboxStatusPublishing = "publishing"
	BuildOutboxStatusPublished  = "published"
)

func IsBuildTerminalStatus(status string) bool {
	switch status {
	case BuildStatusSuccess,
		BuildStatusCanceled,
		BuildStatusFailed,
		BuildStatusFailedTimeout,
		BuildStatusFailedQuotaExceeded,
		BuildStatusFailedInternal:
		return true
	default:
		return false
	}
}

func IsBuildFailedStatus(status string) bool {
	switch status {
	case BuildStatusFailed,
		BuildStatusFailedTimeout,
		BuildStatusFailedQuotaExceeded,
		BuildStatusFailedInternal:
		return true
	default:
		return false
	}
}

type Build struct {
	ID               uuid.UUID
	ImageID          uuid.UUID
	OwnerID          uuid.UUID
	OwnerUsername    string
	Status           string
	LogFilePath      string
	ArchiveObjectKey string
	StartedAt        time.Time
	FinishedAt       *time.Time
}

type BuildQueueOutbox struct {
	ID         uuid.UUID
	BuildID    uuid.UUID
	Exchange   string
	RoutingKey string
	Payload    []byte
	Status     string
	Attempts   int
	LastError  *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
