package ssotest

import (
	"bytes"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
)

// LoginAttempts is the in-memory login-attempt store for SSO tests. It meets
// the contract RunLoginAttemptContract checks, like the PostgreSQL repository.
type LoginAttempts struct {
	mu       sync.Mutex
	attempts map[string]model.LoginAttempt
}

var _ service.LoginAttemptRepository = (*LoginAttempts)(nil)

// NewLoginAttempts returns an empty store.
func NewLoginAttempts() *LoginAttempts {
	return &LoginAttempts{attempts: map[string]model.LoginAttempt{}}
}

func (s *LoginAttempts) SaveLoginAttempt(_ context.Context, attempt model.LoginAttempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := string(attempt.StateDigest)
	attempt.StateDigest = slices.Clone(attempt.StateDigest)
	attempt.BindingDigest = slices.Clone(attempt.BindingDigest)
	s.attempts[key] = attempt
	return nil
}

func (s *LoginAttempts) ConsumeLoginAttempt(_ context.Context, stateDigest []byte, provider string, bindingDigest []byte, now time.Time) (model.LoginAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := string(stateDigest)
	attempt, ok := s.attempts[key]
	if !ok || attempt.Provider != provider || !bytes.Equal(attempt.BindingDigest, bindingDigest) || !attempt.ExpiresAt.After(now) {
		return model.LoginAttempt{}, apperrors.ErrNotFound
	}
	delete(s.attempts, key)
	return attempt, nil
}

func (s *LoginAttempts) DeleteExpiredLoginAttempts(_ context.Context, now time.Time, limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, attempt := range s.attempts {
		if limit <= 0 {
			break
		}
		if !attempt.ExpiresAt.After(now) {
			delete(s.attempts, key)
			limit--
		}
	}
	return nil
}
