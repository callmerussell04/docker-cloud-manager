package ssotest

import (
	"context"
	"crypto/sha256"
	"sync"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	"github.com/stretchr/testify/require"
)

// RunLoginAttemptContract checks a login-attempt store against the behaviour
// AuthService relies on. Run it against every implementation: the in-memory
// LoginAttempts and the PostgreSQL repository. newStore returns an empty store.
func RunLoginAttemptContract(t *testing.T, newStore func(t *testing.T) service.LoginAttemptRepository) {
	t.Helper()
	// PostgreSQL keeps microseconds.
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	t.Run("an attempt is consumed once with its state, provider and binding", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)
		attempt := loginAttempt("state", "binding", base)
		require.NoError(t, store.SaveLoginAttempt(ctx, attempt))

		got, err := store.ConsumeLoginAttempt(ctx, digest("state"), "keycloak", digest("binding"), base.Add(time.Minute))
		require.NoError(t, err)
		requireSameAttempt(t, attempt, got)

		_, err = store.ConsumeLoginAttempt(ctx, digest("state"), "keycloak", digest("binding"), base.Add(time.Minute))
		require.ErrorIs(t, err, apperrors.ErrNotFound, "an attempt is usable once")
	})

	t.Run("a wrong binding, provider or state consumes nothing", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)
		attempt := loginAttempt("state", "binding", base)
		require.NoError(t, store.SaveLoginAttempt(ctx, attempt))
		now := base.Add(time.Minute)

		_, err := store.ConsumeLoginAttempt(ctx, digest("state"), "keycloak", digest("other-binding"), now)
		require.ErrorIs(t, err, apperrors.ErrNotFound)
		_, err = store.ConsumeLoginAttempt(ctx, digest("state"), "other-provider", digest("binding"), now)
		require.ErrorIs(t, err, apperrors.ErrNotFound)
		_, err = store.ConsumeLoginAttempt(ctx, digest("other-state"), "keycloak", digest("binding"), now)
		require.ErrorIs(t, err, apperrors.ErrNotFound)

		got, err := store.ConsumeLoginAttempt(ctx, digest("state"), "keycloak", digest("binding"), now)
		require.NoError(t, err, "the refused callbacks left the attempt in place")
		requireSameAttempt(t, attempt, got)
	})

	t.Run("an expired attempt is refused before cleanup has run", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)
		attempt := loginAttempt("state", "binding", base)
		require.NoError(t, store.SaveLoginAttempt(ctx, attempt))

		_, err := store.ConsumeLoginAttempt(ctx, digest("state"), "keycloak", digest("binding"), attempt.ExpiresAt)
		require.ErrorIs(t, err, apperrors.ErrNotFound, "an attempt is expired at its expiry")
		_, err = store.ConsumeLoginAttempt(ctx, digest("state"), "keycloak", digest("binding"), attempt.ExpiresAt.Add(-time.Microsecond))
		require.NoError(t, err, "an attempt is valid until its expiry")
	})

	t.Run("cleanup deletes expired attempts in bounded batches", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)
		expired := []string{"expired-1", "expired-2", "expired-3"}
		for _, state := range expired {
			require.NoError(t, store.SaveLoginAttempt(ctx, loginAttempt(state, "binding", base)))
		}
		live := loginAttempt("live", "binding", base.Add(time.Hour))
		require.NoError(t, store.SaveLoginAttempt(ctx, live))
		afterExpiry := base.Add(service.OIDCLoginAttemptTTL)

		require.NoError(t, store.DeleteExpiredLoginAttempts(ctx, afterExpiry, 2))
		// Consuming at a time before expiry shows which rows are still stored.
		remaining := 0
		for _, state := range expired {
			if _, err := store.ConsumeLoginAttempt(ctx, digest(state), "keycloak", digest("binding"), base); err == nil {
				remaining++
				require.NoError(t, store.SaveLoginAttempt(ctx, loginAttempt(state, "binding", base)))
			}
		}
		require.Equal(t, 1, remaining, "one pass deletes at most its limit")

		require.NoError(t, store.DeleteExpiredLoginAttempts(ctx, afterExpiry, 2))
		for _, state := range expired {
			_, err := store.ConsumeLoginAttempt(ctx, digest(state), "keycloak", digest("binding"), base)
			require.ErrorIs(t, err, apperrors.ErrNotFound)
		}
		_, err := store.ConsumeLoginAttempt(ctx, digest("live"), "keycloak", digest("binding"), afterExpiry)
		require.NoError(t, err, "cleanup keeps an unexpired attempt")
	})

	t.Run("of concurrent consumptions exactly one succeeds", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)
		require.NoError(t, store.SaveLoginAttempt(ctx, loginAttempt("state", "binding", base)))

		const callers = 8
		var wg sync.WaitGroup
		results := make(chan error, callers)
		start := make(chan struct{})
		for range callers {
			wg.Go(func() {
				<-start
				_, err := store.ConsumeLoginAttempt(ctx, digest("state"), "keycloak", digest("binding"), base.Add(time.Minute))
				results <- err
			})
		}
		close(start)
		wg.Wait()
		close(results)
		succeeded := 0
		for err := range results {
			if err == nil {
				succeeded++
				continue
			}
			require.ErrorIs(t, err, apperrors.ErrNotFound)
		}
		require.Equal(t, 1, succeeded)
	})
}

func loginAttempt(state, binding string, createdAt time.Time) model.LoginAttempt {
	return model.LoginAttempt{
		StateDigest:   digest(state),
		Provider:      "keycloak",
		Nonce:         "nonce-" + state,
		PKCEVerifier:  "verifier-" + state,
		BindingDigest: digest(binding),
		RedirectAfter: "http://frontend.example/auth/callback",
		CreatedAt:     createdAt,
		ExpiresAt:     createdAt.Add(service.OIDCLoginAttemptTTL),
	}
}

func digest(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func requireSameAttempt(t *testing.T, want, got model.LoginAttempt) {
	t.Helper()
	require.True(t, want.CreatedAt.Equal(got.CreatedAt), "created at")
	require.True(t, want.ExpiresAt.Equal(got.ExpiresAt), "expires at")
	want.CreatedAt, want.ExpiresAt = time.Time{}, time.Time{}
	got.CreatedAt, got.ExpiresAt = time.Time{}, time.Time{}
	require.Equal(t, want, got)
}
