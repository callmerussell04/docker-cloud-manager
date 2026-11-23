package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	BuildStatusPending                 = "pending"
	BuildStatusRunning                 = "running"
	BuildStatusSuccess                 = "success"
	BuildStatusCanceled                = "canceled"
	BuildStatusFailed                  = "failed"
	BuildStatusFailedTimeout           = "failed_timeout"
	BuildStatusFailedQuotaExceeded     = "failed_quota_exceeded"
	BuildStatusFailedResourceExhausted = "failed_resource_exhausted"
	BuildStatusFailedInternal          = "failed_internal"

	BuildOutboxStatusPending    = "pending"
	BuildOutboxStatusPublishing = "publishing"
	BuildOutboxStatusPublished  = "published"
	BuildOutboxStatusDiscarded  = "discarded"
)

func IsBuildTerminalStatus(status string) bool {
	switch status {
	case BuildStatusSuccess,
		BuildStatusCanceled,
		BuildStatusFailed,
		BuildStatusFailedTimeout,
		BuildStatusFailedQuotaExceeded,
		BuildStatusFailedResourceExhausted,
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
		BuildStatusFailedResourceExhausted,
		BuildStatusFailedInternal:
		return true
	default:
		return false
	}
}

type Build struct {
	RegistryCredential RegistryCredential
	ID                 uuid.UUID
	// ImageID is uuid.Nil once the build's image record has been deleted.
	ImageID uuid.UUID
	// Tag is the normalized user tag the build was queued for.
	Tag                string
	OwnerID            uuid.UUID
	ProjectID          *uuid.UUID
	ProjectServiceName string
	// Source is where the build's context came from; empty for a Compose
	// project's build.
	Source           BuildSource
	OwnerUsername    string
	Status           string
	LogObjectKey     string
	ArchiveObjectKey string
	CreatedAt        time.Time
	StartedAt        time.Time
	FinishedAt       *time.Time
}

// Build source kinds.
const (
	BuildSourceArchive = "archive"
	BuildSourceGit     = "git"
)

// BuildSource is where a build's context came from: an uploaded archive or a
// Git repository.
type BuildSource struct {
	// Kind is BuildSourceArchive or BuildSourceGit.
	Kind string
	// Location is the archive's file name or the repository URL.
	Location string
	// Ref is the Git ref asked for; empty for an archive or the default
	// branch.
	Ref string
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
