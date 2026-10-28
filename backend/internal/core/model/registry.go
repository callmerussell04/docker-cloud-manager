package model

import (
	"github.com/google/uuid"
	"time"
)

// RegistryCredential is returned only to the worker that wins the durable claim.
// It must never be copied to resource DTOs, queue messages or logs.
type RegistryCredential struct {
	Username    string
	Password    string
	Destination string
}

type RegistryPrincipal struct {
	ReadOnly         bool
	BuildID          uuid.UUID
	OwnerID          uuid.UUID
	Repository       string
	PullRepositories []string
}

type RegistryAccess struct {
	Repository string
	Actions    []string
}

type RegistryTokenGrant struct {
	Subject   string
	Service   string
	IssuedAt  time.Time
	ExpiresAt time.Time
	Access    []RegistryAccess
}

type RegistryCleanup struct {
	ID         uuid.UUID
	Repository string
	Tag        string
}

// RegistryCleanupFacts are read in one snapshot under the repository lock.
type RegistryCleanupFacts struct {
	// PublisherActive: a pending or running build targets the repository.
	PublisherActive bool
	// TokenMayWrite: a build token issued within the fence may still push.
	TokenMayWrite bool
	// TagAvailable: an available image owns the job's tag.
	TagAvailable bool
}

type RegistryCleanupState string

const (
	RegistryCleanupPending  RegistryCleanupState = "pending"
	RegistryCleanupObsolete RegistryCleanupState = "obsolete"
	RegistryCleanupReady    RegistryCleanupState = "ready"
)

// RegistryTrustedAccess selects the actions a Core-internal Registry token
// grants on one durable repository.
type RegistryTrustedAccess string

const (
	RegistryTrustedPull    RegistryTrustedAccess = "pull"
	RegistryTrustedCleanup RegistryTrustedAccess = "cleanup"
)

// RegistryTagDisposition says what becomes of the tag a revoked build may have
// pushed: a successful build keeps it, any other outcome schedules its deletion.
type RegistryTagDisposition string

const (
	RegistryTagKeep   RegistryTagDisposition = "keep"
	RegistryTagDelete RegistryTagDisposition = "delete"
)

// PushedTagDisposition returns the disposition of the tag pushed by a build
// that ended with status.
func PushedTagDisposition(status string) RegistryTagDisposition {
	if status == BuildStatusSuccess {
		return RegistryTagKeep
	}
	return RegistryTagDelete
}
