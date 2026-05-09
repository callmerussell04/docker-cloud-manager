package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

const (
	StatusActive      = "active"
	StatusDeactivated = "deactivated"
)

const (
	AuthSourceLocal = "local"
	AuthSourceOIDC  = "oidc"
)

type User struct {
	ID               uuid.UUID
	Username         string
	Email            string
	PasswordHash     string
	Role             string
	Status           string
	QuotaCPU         float64
	QuotaRAMMB       int64
	QuotaDiskMB      int64
	AuthSource       string
	ExternalProvider string
	ExternalSubject  string
	ExternalUsername string
	LastLoginAt      *time.Time
}

type ListUsersOptions struct {
	Limit  int
	Offset int
}
