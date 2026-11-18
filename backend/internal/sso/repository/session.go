package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionRepository persists sessions in the same database as their users.
type SessionRepository struct{ db *pgxpool.Pool }

func NewSessionRepository(db *pgxpool.Pool) *SessionRepository { return &SessionRepository{db: db} }

const userColumns = `id, username, email, password_hash, role, status,
 auth_source, external_provider, external_subject, external_username, last_login_at`

// lastAdminGuardLockKey names the transaction-scoped advisory lock that every
// write able to lower the number of active administrators takes first.
const lastAdminGuardLockKey = "dcm:sso:last-active-admin"

func (r *SessionRepository) WithUser(ctx context.Context, userID uuid.UUID, operation string, fn func(model.User, service.SessionTransaction) error) error {
	return r.withUser(ctx, userID, operation, false, fn)
}

// WithLastAdminGuard is WithUser preceded by the last-active-administrator lock,
// taken before the user row so the transaction's CountActiveAdmins sees every
// committed guarded write.
func (r *SessionRepository) WithLastAdminGuard(ctx context.Context, userID uuid.UUID, operation string, fn func(model.User, service.SessionTransaction) error) error {
	return r.withUser(ctx, userID, operation, true, fn)
}

func (r *SessionRepository) withUser(ctx context.Context, userID uuid.UUID, operation string, guarded bool, fn func(model.User, service.SessionTransaction) error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return sessionStorageError(ctx, "begin session transaction", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after commit; on failure the earlier error is returned
	if guarded {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, lastAdminGuardLockKey); err != nil {
			return sessionStorageError(ctx, "lock last active admin guard", err)
		}
	}
	var user model.User
	err = tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(userScanDest(&user)...)
	if err != nil {
		return sessionStorageError(ctx, "lock session user", err)
	}
	if err := fn(user, &sessionTransaction{tx: tx, userID: userID, guarded: guarded}); err != nil {
		return err
	}
	return sessionStorageError(ctx, "commit "+operation, tx.Commit(ctx))
}

func (r *SessionRepository) FindRefresh(ctx context.Context, digest []byte) (model.RefreshToken, error) {
	var token model.RefreshToken
	err := r.db.QueryRow(ctx, `SELECT t.digest,t.session_id,s.user_id,t.issued_at,t.consumed_at
 FROM refresh_tokens t JOIN sessions s ON s.id=t.session_id WHERE t.digest=$1`, digest).
		Scan(&token.Digest, &token.SessionID, &token.UserID, &token.IssuedAt, &token.ConsumedAt)
	return token, sessionStorageError(ctx, "find refresh token", err)
}

func (r *SessionRepository) VerifySession(ctx context.Context, identity model.AccessIdentity, now time.Time) (model.User, error) {
	var user model.User
	// One snapshot checks the session and current account together; no positive cache.
	err := r.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1 AND status='active'
 AND EXISTS (SELECT 1 FROM sessions WHERE id=$2 AND user_id=users.id AND revoked_at IS NULL
 AND absolute_expires_at>$3 AND idle_expires_at>$3)`, identity.UserID, identity.SessionID, now).Scan(userScanDest(&user)...)
	return user, sessionStorageError(ctx, "verify session", err)
}

func (r *SessionRepository) ProviderSessionUsers(ctx context.Context, provider, providerSessionID string) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT DISTINCT user_id FROM sessions
 WHERE provider=$1 AND provider_session_id=$2 AND revoked_at IS NULL`, provider, providerSessionID)
	if err != nil {
		return nil, sessionStorageError(ctx, "find provider sessions", err)
	}
	userIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	return userIDs, sessionStorageError(ctx, "find provider sessions", err)
}

func (r *SessionRepository) DeleteExpired(ctx context.Context, now time.Time, limit int) error {
	_, err := r.db.Exec(ctx, `DELETE FROM sessions WHERE id IN
 (SELECT id FROM sessions WHERE absolute_expires_at<=$1 ORDER BY absolute_expires_at,id LIMIT $2 FOR UPDATE SKIP LOCKED)`, now, limit)
	return sessionStorageError(ctx, "clean expired sessions", err)
}

type sessionTransaction struct {
	tx      pgx.Tx
	userID  uuid.UUID
	guarded bool
}

func (t *sessionTransaction) CreateSession(ctx context.Context, session model.Session, token model.RefreshToken) error {
	if session.UserID != t.userID {
		return apperrors.ErrForbidden
	}
	_, err := t.tx.Exec(ctx, `INSERT INTO sessions(id,user_id,created_at,absolute_expires_at,idle_expires_at,provider,provider_session_id,id_token)
 VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''))`,
		session.ID, t.userID, session.CreatedAt, session.AbsoluteExpiresAt, session.IdleExpiresAt,
		session.Provider, session.ProviderSessionID, session.IDToken)
	if err != nil {
		return sessionStorageError(ctx, "create session", err)
	}
	return t.insertToken(ctx, token)
}

func (t *sessionTransaction) insertToken(ctx context.Context, token model.RefreshToken) error {
	_, err := t.tx.Exec(ctx, `INSERT INTO refresh_tokens(digest,session_id,issued_at) VALUES($1,$2,$3)`, token.Digest, token.SessionID, token.IssuedAt)
	return sessionStorageError(ctx, "store refresh token", err)
}

func (t *sessionTransaction) GetRefresh(ctx context.Context, digest []byte) (model.Session, model.RefreshToken, error) {
	var session model.Session
	var token model.RefreshToken
	err := t.tx.QueryRow(ctx, `SELECT s.id,s.user_id,s.created_at,s.absolute_expires_at,s.idle_expires_at,s.revoked_at,
 COALESCE(s.provider,''),COALESCE(s.provider_session_id,''),COALESCE(s.id_token,''),
 t.digest,t.issued_at,t.consumed_at FROM sessions s JOIN refresh_tokens t ON t.session_id=s.id
 WHERE t.digest=$1 AND s.user_id=$2 FOR UPDATE OF s,t`, digest, t.userID).
		Scan(&session.ID, &session.UserID, &session.CreatedAt, &session.AbsoluteExpiresAt, &session.IdleExpiresAt, &session.RevokedAt,
			&session.Provider, &session.ProviderSessionID, &session.IDToken,
			&token.Digest, &token.IssuedAt, &token.ConsumedAt)
	token.SessionID, token.UserID = session.ID, session.UserID
	return session, token, sessionStorageError(ctx, "lock refresh session", err)
}

func (t *sessionTransaction) Rotate(ctx context.Context, session model.Session, previous, next model.RefreshToken) error {
	_, err := t.tx.Exec(ctx, `UPDATE refresh_tokens SET consumed_at=$2 WHERE digest=$1`, previous.Digest, next.IssuedAt)
	if err != nil {
		return sessionStorageError(ctx, "consume refresh token", err)
	}
	if err := t.insertToken(ctx, next); err != nil {
		return err
	}
	_, err = t.tx.Exec(ctx, `UPDATE sessions SET idle_expires_at=$2 WHERE id=$1 AND user_id=$3`, session.ID, session.IdleExpiresAt, t.userID)
	return sessionStorageError(ctx, "advance session expiry", err)
}

func (t *sessionTransaction) Revoke(ctx context.Context, id uuid.UUID, now time.Time, reason string) error {
	_, err := t.tx.Exec(ctx, `UPDATE sessions SET revoked_at=$2,revoked_reason=$3,id_token=NULL WHERE id=$1 AND user_id=$4 AND revoked_at IS NULL`, id, now, reason, t.userID)
	return sessionStorageError(ctx, "revoke session", err)
}

func (t *sessionTransaction) RevokeAll(ctx context.Context, userID uuid.UUID, now time.Time) error {
	if userID != t.userID {
		return apperrors.ErrForbidden
	}
	_, err := t.tx.Exec(ctx, `UPDATE sessions SET revoked_at=$2,revoked_reason='user_security_change',id_token=NULL WHERE user_id=$1 AND revoked_at IS NULL`, userID, now)
	return sessionStorageError(ctx, "revoke user sessions", err)
}

func (t *sessionTransaction) RevokeProviderSessions(ctx context.Context, provider, providerSessionID string, now time.Time) (int, error) {
	tag, err := t.tx.Exec(ctx, `UPDATE sessions SET revoked_at=$3,revoked_reason='provider_logout',id_token=NULL
 WHERE user_id=$1 AND provider=$2 AND revoked_at IS NULL AND ($4='' OR provider_session_id=$4)`,
		t.userID, provider, now, providerSessionID)
	if err != nil {
		return 0, sessionStorageError(ctx, "revoke provider sessions", err)
	}
	return int(tag.RowsAffected()), nil
}

func (t *sessionTransaction) UpdateUser(ctx context.Context, user model.User) error {
	if user.ID != t.userID {
		return apperrors.ErrForbidden
	}
	return sessionStorageError(ctx, "update session user", updateUser(ctx, t.tx, user))
}

func (t *sessionTransaction) CountActiveAdmins(ctx context.Context) (int, error) {
	if !t.guarded {
		// Without the guard lock a concurrent removal could commit after this count.
		return 0, apperrors.Wrap(apperrors.ErrInternal, apperrors.ErrInternal.Error(), errors.New("count active admins outside the last-admin guard"))
	}
	var total int
	err := t.tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role=$1 AND status=$2`, model.RoleAdmin, model.StatusActive).Scan(&total)
	return total, sessionStorageError(ctx, "count active admins", err)
}

// Driver errors may include bind values (digests or password hashes). Keep them
// out of returned/logged errors; preserve a safe operation and availability kind.
func sessionStorageError(ctx context.Context, operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, apperrors.ErrNotFound) {
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
