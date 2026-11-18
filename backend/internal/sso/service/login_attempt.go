package service

import (
	"context"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
)

// OIDCLoginAttemptTTL is how long a user has between starting an OIDC sign-in
// and returning from the provider. It is long enough for a first-time
// registration or OTP setup at the provider. Gateway sets the binding cookie's
// lifetime from the expiry SSO returns and keeps no constant of its own.
const OIDCLoginAttemptTTL = 30 * time.Minute

// loginAttemptCleanupBatch bounds each cleanup pass over expired attempts.
const loginAttemptCleanupBatch = 100

// LoginAttemptRepository stores OIDC login attempts durably, so an attempt
// survives an SSO restart and is shared by every SSO instance.
type LoginAttemptRepository interface {
	SaveLoginAttempt(ctx context.Context, attempt model.LoginAttempt) error
	// ConsumeLoginAttempt removes and returns the attempt with this state
	// digest, provider and binding digest that is unexpired at now (its expiry
	// is after now), whether or not cleanup has run. It answers
	// apperrors.ErrNotFound otherwise and then consumes nothing. Of concurrent
	// consumptions of one attempt exactly one succeeds.
	ConsumeLoginAttempt(ctx context.Context, stateDigest []byte, provider string, bindingDigest []byte, now time.Time) (model.LoginAttempt, error)
	// DeleteExpiredLoginAttempts removes at most limit attempts expired at now.
	DeleteExpiredLoginAttempts(ctx context.Context, now time.Time, limit int) error
}
