package model

import "time"

// LoginAttempt is an OIDC sign-in in flight between start and callback. It is
// stored under the SHA-256 digest of its state and bound to the browser that
// started it through the digest of a binding secret held in that browser's
// cookie; neither secret is stored. The nonce and PKCE verifier are kept as
// they are, because the code exchange needs them.
type LoginAttempt struct {
	StateDigest   []byte
	Provider      string
	Nonce         string
	PKCEVerifier  string
	BindingDigest []byte
	RedirectAfter string
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

// OIDCLoginStart is what a browser needs to go to the provider: the
// authorization URL, the attempt's state, the binding secret for its cookie,
// and when the attempt expires.
type OIDCLoginStart struct {
	AuthURL      string
	State        string
	StateBinding string
	ExpiresAt    time.Time
}

// OIDCCallbackOutcome is how an OIDC callback ended when it ended as expected.
// Provider or storage unavailability and timeouts are errors, not outcomes.
type OIDCCallbackOutcome string

const (
	OIDCCallbackSuccess            OIDCCallbackOutcome = "success"
	OIDCCallbackInvalidAttempt     OIDCCallbackOutcome = "invalid_attempt"
	OIDCCallbackIdentityRejected   OIDCCallbackOutcome = "identity_rejected"
	OIDCCallbackAccountDeactivated OIDCCallbackOutcome = "account_deactivated"
	OIDCCallbackAccountConflict    OIDCCallbackOutcome = "account_conflict"
)

// OIDCCallbackResult carries tokens and the signed-in user only on success.
// RedirectAfter is the attempt's target once the attempt has been consumed,
// and empty before.
type OIDCCallbackResult struct {
	Outcome       OIDCCallbackOutcome
	Tokens        Tokens
	User          SignedInUser
	RedirectAfter string
}
