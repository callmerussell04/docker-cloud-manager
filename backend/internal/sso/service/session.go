package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	"github.com/google/uuid"
)

// SessionRepository serializes mutations with user changes across SSO instances.
type SessionRepository interface {
	// WithUser commits fn's writes; operation names the commit in returned errors.
	WithUser(ctx context.Context, userID uuid.UUID, operation string, fn func(model.User, SessionTransaction) error) error
	FindRefresh(ctx context.Context, digest []byte) (model.RefreshToken, error)
	VerifySession(ctx context.Context, identity model.AccessIdentity, now time.Time) (model.User, error)
	DeleteExpired(ctx context.Context, now time.Time, limit int) error
}

// SessionTransaction is valid only inside WithUser, after locking the user.
// Callbacks return errors to roll back. Replay revocation must return nil to commit.
type SessionTransaction interface {
	CreateSession(ctx context.Context, session model.Session, token model.RefreshToken) error
	GetRefresh(ctx context.Context, digest []byte) (model.Session, model.RefreshToken, error)
	Rotate(ctx context.Context, session model.Session, previous, next model.RefreshToken) error
	Revoke(ctx context.Context, sessionID uuid.UUID, now time.Time, reason string) error
	RevokeAll(ctx context.Context, userID uuid.UUID, now time.Time) error
	UpdateUser(ctx context.Context, user model.User) error
}

func (s *AuthService) createSession(ctx context.Context, authenticated model.User, local bool) (model.Tokens, error) {
	var result model.Tokens
	err := s.sessions.WithUser(ctx, authenticated.ID, "create session", func(user model.User, tx SessionTransaction) error {
		if user.Status != model.StatusActive || (local && (user.PasswordHash != authenticated.PasswordHash || user.AuthSource == model.AuthSourceOIDC)) {
			return apperrors.ErrInvalidCredentials
		}
		now := s.now()
		session := model.Session{ID: uuid.New(), UserID: user.ID, CreatedAt: now, AbsoluteExpiresAt: now.Add(s.cfg.SessionTTL)}
		session.IdleExpiresAt = earlier(now.Add(s.cfg.SessionIdleTTL), session.AbsoluteExpiresAt)
		var token model.RefreshToken
		var err error
		result, token, err = s.issueTokens(user, session, now)
		if err != nil {
			return err
		}
		return tx.CreateSession(ctx, session, token)
	})
	if err != nil {
		return model.Tokens{}, err
	}
	return result, nil
}

func (s *AuthService) issueTokens(user model.User, session model.Session, now time.Time) (model.Tokens, model.RefreshToken, error) {
	var secret [32]byte
	if _, err := io.ReadFull(s.random, secret[:]); err != nil {
		return model.Tokens{}, model.RefreshToken{}, apperrors.Wrap(apperrors.ErrInternal, apperrors.ErrInternal.Error(), fmt.Errorf("generate refresh token: %w", err))
	}
	value := base64.RawURLEncoding.EncodeToString(secret[:])
	access, err := s.tokenProvider.GenerateAccessToken(user, session.ID, earlier(now.Add(s.cfg.AccessTTL), session.AbsoluteExpiresAt))
	if err != nil {
		return model.Tokens{}, model.RefreshToken{}, apperrors.Wrap(apperrors.ErrInternal, apperrors.ErrInternal.Error(), fmt.Errorf("generate access token: %w", err))
	}
	digest := sha256.Sum256([]byte(value))
	return model.Tokens{AccessToken: access, RefreshToken: value, RefreshExpiresAt: earlier(session.IdleExpiresAt, session.AbsoluteExpiresAt)},
		model.RefreshToken{Digest: digest[:], SessionID: session.ID, UserID: user.ID, IssuedAt: now}, nil
}

func refreshDigest(value string) ([]byte, bool) {
	if len(value) != 43 {
		return nil, false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return nil, false
	}
	digest := sha256.Sum256([]byte(value))
	return digest[:], true
}

func (s *AuthService) Refresh(ctx context.Context, value string) (model.Tokens, error) {
	digest, valid := refreshDigest(value)
	if !valid {
		return model.Tokens{}, apperrors.ErrInvalidToken
	}
	found, err := s.sessions.FindRefresh(ctx, digest)
	if errors.Is(err, apperrors.ErrNotFound) {
		return model.Tokens{}, apperrors.ErrInvalidToken
	}
	if err != nil {
		return model.Tokens{}, err
	}
	var result model.Tokens
	var rejected bool
	err = s.sessions.WithUser(ctx, found.UserID, "refresh session", func(user model.User, tx SessionTransaction) error {
		session, token, err := tx.GetRefresh(ctx, digest)
		if errors.Is(err, apperrors.ErrNotFound) {
			rejected = true
			return nil
		}
		if err != nil {
			return err
		}
		now := s.now()
		if user.Status != model.StatusActive || session.UserID != user.ID || session.RevokedAt != nil || !now.Before(session.AbsoluteExpiresAt) || !now.Before(session.IdleExpiresAt) {
			rejected = true
			return nil
		}
		if token.ConsumedAt != nil {
			rejected = true
			return tx.Revoke(ctx, session.ID, now, "replay")
		}
		session.IdleExpiresAt = earlier(now.Add(s.cfg.SessionIdleTTL), session.AbsoluteExpiresAt)
		var next model.RefreshToken
		result, next, err = s.issueTokens(user, session, now)
		if err != nil {
			return err
		}
		return tx.Rotate(ctx, session, token, next)
	})
	if errors.Is(err, apperrors.ErrNotFound) || (err == nil && rejected) {
		return model.Tokens{}, apperrors.ErrInvalidToken
	}
	if err != nil {
		return model.Tokens{}, err
	}
	return result, nil
}

// Logout revokes the family identified by any of its retained secrets.
func (s *AuthService) Logout(ctx context.Context, value string) error {
	digest, valid := refreshDigest(value)
	if !valid {
		return nil
	}
	found, err := s.sessions.FindRefresh(ctx, digest)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	err = s.sessions.WithUser(ctx, found.UserID, "logout session", func(_ model.User, tx SessionTransaction) error {
		session, _, err := tx.GetRefresh(ctx, digest)
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return tx.Revoke(ctx, session.ID, s.now(), "logout")
	})
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil
	}
	return err
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// updateUser serializes credential/status mutations with session creation/refresh.
func (s *AuthService) updateUser(ctx context.Context, user model.User, passwordChanged, statusChanged bool) error {
	return s.sessions.WithUser(ctx, user.ID, "update session user", func(current model.User, tx SessionTransaction) error {
		if !passwordChanged {
			user.PasswordHash = current.PasswordHash
		}
		if !statusChanged {
			user.Status = current.Status
		}
		if err := tx.UpdateUser(ctx, user); err != nil {
			return err
		}
		if passwordChanged || user.Status == model.StatusDeactivated {
			return tx.RevokeAll(ctx, user.ID, s.now())
		}
		return nil
	})
}

// CleanupSessions bounds each pass; authorization enforces expiry independently.
func (s *AuthService) CleanupSessions(ctx context.Context) error {
	return s.sessions.DeleteExpired(ctx, s.now(), 100)
}
