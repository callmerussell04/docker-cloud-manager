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

// SignedInUser names the user a session was created for, as the session's
// transaction read it, so the caller can attribute the sign-in.
type SignedInUser struct {
	ID       uuid.UUID
	Username string
	Role     string
}

// SignIn is a new session's tokens and its user.
type SignIn struct {
	Tokens Tokens
	User   SignedInUser
}

// Session is the durable authorization lifetime shared by a refresh-token family.
type Session struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	CreatedAt         time.Time
	AbsoluteExpiresAt time.Time
	IdleExpiresAt     time.Time
	RevokedAt         *time.Time
	// Provider, ProviderSessionID and IDToken are set only for a session
	// created by an OIDC callback. The ID token is cleared when the session is
	// revoked and leaves SSO only inside the provider sign-out URL.
	Provider          string
	ProviderSessionID string
	IDToken           string
}

// RefreshToken contains only a digest, including for consumed predecessors.
type RefreshToken struct {
	Digest     []byte
	SessionID  uuid.UUID
	UserID     uuid.UUID
	IssuedAt   time.Time
	ConsumedAt *time.Time
}
