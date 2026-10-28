package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

// SessionRepository persists sessions in the same database as their users.
type SessionRepository struct{ db *sql.DB }

func NewSessionRepository(db *sql.DB) *SessionRepository { return &SessionRepository{db: db} }

const userColumns = `id, username, email, password_hash, role, status, quota_cpu, quota_ram_mb, quota_disk_mb,
 auth_source, external_provider, external_subject, external_username, last_login_at`

func (r *SessionRepository) WithUser(ctx context.Context, userID uuid.UUID, operation string, fn func(model.User, service.SessionTransaction) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return sessionStorageError(ctx, "begin session transaction", err)
	}
	defer func() { _ = tx.Rollback() }()
	var user model.User
	err = tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(userScanDest(&user)...)
	if err != nil {
		return sessionStorageError(ctx, "lock session user", err)
	}
	if err := fn(user, &sessionTransaction{tx: tx, userID: userID}); err != nil {
		return err
	}
	return sessionStorageError(ctx, "commit "+operation, tx.Commit())
}

func (r *SessionRepository) FindRefresh(ctx context.Context, digest []byte) (model.RefreshToken, error) {
	var token model.RefreshToken
	err := r.db.QueryRowContext(ctx, `SELECT t.digest,t.session_id,s.user_id,t.issued_at,t.consumed_at
 FROM refresh_tokens t JOIN sessions s ON s.id=t.session_id WHERE t.digest=$1`, digest).
		Scan(&token.Digest, &token.SessionID, &token.UserID, &token.IssuedAt, &token.ConsumedAt)
	return token, sessionStorageError(ctx, "find refresh token", err)
}

func (r *SessionRepository) VerifySession(ctx context.Context, identity model.AccessIdentity, now time.Time) (model.User, error) {
	var user model.User
	// One snapshot checks the session and current account together; no positive cache.
	err := r.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1 AND status='active'
 AND EXISTS (SELECT 1 FROM sessions WHERE id=$2 AND user_id=users.id AND revoked_at IS NULL
 AND absolute_expires_at>$3 AND idle_expires_at>$3)`, identity.UserID, identity.SessionID, now).Scan(userScanDest(&user)...)
	return user, sessionStorageError(ctx, "verify session", err)
}

func (r *SessionRepository) DeleteExpired(ctx context.Context, now time.Time, limit int) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE id IN
 (SELECT id FROM sessions WHERE absolute_expires_at<=$1 ORDER BY absolute_expires_at,id LIMIT $2 FOR UPDATE SKIP LOCKED)`, now, limit)
	return sessionStorageError(ctx, "clean expired sessions", err)
}

type sessionTransaction struct {
	tx     *sql.Tx
	userID uuid.UUID
}

func (t *sessionTransaction) CreateSession(ctx context.Context, session model.Session, token model.RefreshToken) error {
	if session.UserID != t.userID {
		return apperrors.ErrForbidden
	}
	_, err := t.tx.ExecContext(ctx, `INSERT INTO sessions(id,user_id,created_at,absolute_expires_at,idle_expires_at) VALUES($1,$2,$3,$4,$5)`,
		session.ID, t.userID, session.CreatedAt, session.AbsoluteExpiresAt, session.IdleExpiresAt)
	if err != nil {
		return sessionStorageError(ctx, "create session", err)
	}
	return t.insertToken(ctx, token)
}

func (t *sessionTransaction) insertToken(ctx context.Context, token model.RefreshToken) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO refresh_tokens(digest,session_id,issued_at) VALUES($1,$2,$3)`, token.Digest, token.SessionID, token.IssuedAt)
	return sessionStorageError(ctx, "store refresh token", err)
}

func (t *sessionTransaction) GetRefresh(ctx context.Context, digest []byte) (model.Session, model.RefreshToken, error) {
	var session model.Session
	var token model.RefreshToken
	err := t.tx.QueryRowContext(ctx, `SELECT s.id,s.user_id,s.created_at,s.absolute_expires_at,s.idle_expires_at,s.revoked_at,
 t.digest,t.issued_at,t.consumed_at FROM sessions s JOIN refresh_tokens t ON t.session_id=s.id
 WHERE t.digest=$1 AND s.user_id=$2 FOR UPDATE OF s,t`, digest, t.userID).
		Scan(&session.ID, &session.UserID, &session.CreatedAt, &session.AbsoluteExpiresAt, &session.IdleExpiresAt, &session.RevokedAt,
			&token.Digest, &token.IssuedAt, &token.ConsumedAt)
	token.SessionID, token.UserID = session.ID, session.UserID
	return session, token, sessionStorageError(ctx, "lock refresh session", err)
}

func (t *sessionTransaction) Rotate(ctx context.Context, session model.Session, previous, next model.RefreshToken) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE refresh_tokens SET consumed_at=$2 WHERE digest=$1`, previous.Digest, next.IssuedAt)
	if err != nil {
		return sessionStorageError(ctx, "consume refresh token", err)
	}
	if err := t.insertToken(ctx, next); err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `UPDATE sessions SET idle_expires_at=$2 WHERE id=$1 AND user_id=$3`, session.ID, session.IdleExpiresAt, t.userID)
	return sessionStorageError(ctx, "advance session expiry", err)
}

func (t *sessionTransaction) Revoke(ctx context.Context, id uuid.UUID, now time.Time, reason string) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=$2,revoked_reason=$3 WHERE id=$1 AND user_id=$4 AND revoked_at IS NULL`, id, now, reason, t.userID)
	return sessionStorageError(ctx, "revoke session", err)
}

func (t *sessionTransaction) RevokeAll(ctx context.Context, userID uuid.UUID, now time.Time) error {
	if userID != t.userID {
		return apperrors.ErrForbidden
	}
	_, err := t.tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=$2,revoked_reason='user_security_change' WHERE user_id=$1 AND revoked_at IS NULL`, userID, now)
	return sessionStorageError(ctx, "revoke user sessions", err)
}

func (t *sessionTransaction) UpdateUser(ctx context.Context, user model.User) error {
	if user.ID != t.userID {
		return apperrors.ErrForbidden
	}
	return sessionStorageError(ctx, "update session user", updateUser(ctx, t.tx, user))
}

// Driver errors may include bind values (digests or password hashes). Keep them
// out of returned/logged errors; preserve a safe operation and availability kind.
func sessionStorageError(ctx context.Context, operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, apperrors.ErrNotFound) {
		return apperrors.ErrNotFound
	}
	if errors.Is(err, apperrors.ErrAlreadyExists) {
		return apperrors.ErrAlreadyExists
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", operation, apperrors.ErrTimeout)
	}
	return fmt.Errorf("%s: %w", operation, apperrors.ErrUnavailable)
}
