package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	BuildStatusPending             = "pending"
	BuildStatusRunning             = "running"
	BuildStatusSuccess             = "success"
	BuildStatusFailed              = "failed"
	BuildStatusFailedTimeout       = "failed_timeout"
	BuildStatusFailedQuotaExceeded = "failed_quota_exceeded"
	BuildStatusFailedInternal      = "failed_internal"
)

func IsBuildTerminalStatus(status string) bool {
	switch status {
	case BuildStatusSuccess,
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
	ID            uuid.UUID
	ImageID       uuid.UUID
	OwnerID       uuid.UUID
	OwnerUsername string
	Status        string
	LogFilePath   string
	StartedAt     time.Time
	FinishedAt    *time.Time
}
