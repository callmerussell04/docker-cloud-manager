package model

import (
	"github.com/google/uuid"
	"time"
)

// AccessIdentity binds an access JWT to its server-side session and user.
type AccessIdentity struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

// Tokens are returned only after the session transaction commits.
type Tokens struct {
	AccessToken      string
	RefreshToken     string
	RefreshExpiresAt time.Time
}

// Session is the durable authorization lifetime shared by a refresh-token family.
type Session struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	CreatedAt         time.Time
	AbsoluteExpiresAt time.Time
	IdleExpiresAt     time.Time
	RevokedAt         *time.Time
}

// RefreshToken contains only a digest, including for consumed predecessors.
type RefreshToken struct {
	Digest     []byte
	SessionID  uuid.UUID
	UserID     uuid.UUID
	IssuedAt   time.Time
	ConsumedAt *time.Time
}
