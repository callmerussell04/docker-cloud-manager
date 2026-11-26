package model

import "time"

type Tokens struct {
	AccessToken      string
	RefreshToken     string
	RefreshExpiresAt time.Time
}

// SignedInUser is the user a sign-in authenticated, as SSO named it, so
// Gateway can attribute the sign-in to that user.
type SignedInUser struct {
	ID       string
	Username string
	Role     string
}

// SignIn is a successful local sign-in: the session's tokens and its user.
type SignIn struct {
	Tokens Tokens
	User   SignedInUser
}
