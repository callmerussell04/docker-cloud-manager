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

type RegistryCleanupState string

const (
	RegistryCleanupPending  RegistryCleanupState = "pending"
	RegistryCleanupObsolete RegistryCleanupState = "obsolete"
	RegistryCleanupReady    RegistryCleanupState = "ready"
)
