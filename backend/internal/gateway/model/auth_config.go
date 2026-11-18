package model

import "time"

type AuthConfig struct {
	LocalLoginEnabled    bool
	LocalRegisterEnabled bool
	OIDCProviders        []OIDCProvider
}

// OIDCProvider is an enabled external provider; Available is false while SSO has not reached it.
type OIDCProvider struct {
	Name      string
	Available bool
}

// OIDCLoginStartResult is a started sign-in attempt: where to send the
// browser, the state naming the attempt, the browser-binding secret for its
// cookie, and when the attempt expires.
type OIDCLoginStartResult struct {
	AuthURL      string
	State        string
	StateBinding string
	ExpiresAt    time.Time
}

// OIDCCallbackOutcome is how SSO ended a callback that ended as expected;
// unavailability and timeouts are errors instead.
type OIDCCallbackOutcome string

const (
	OIDCCallbackSuccess            OIDCCallbackOutcome = "success"
	OIDCCallbackInvalidAttempt     OIDCCallbackOutcome = "invalid_attempt"
	OIDCCallbackIdentityRejected   OIDCCallbackOutcome = "identity_rejected"
	OIDCCallbackAccountDeactivated OIDCCallbackOutcome = "account_deactivated"
	OIDCCallbackAccountConflict    OIDCCallbackOutcome = "account_conflict"
)

// OIDCCallbackResult carries tokens only on success. RedirectAfter is the
// attempt's target once SSO consumed the attempt, and empty before.
type OIDCCallbackResult struct {
	Outcome       OIDCCallbackOutcome
	Tokens        Tokens
	RedirectAfter string
}
