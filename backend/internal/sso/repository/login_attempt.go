package repository

import (
	"context"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LoginAttemptRepository stores OIDC login attempts in the SSO database, so
// an attempt survives an SSO restart and is shared by every SSO instance.
type LoginAttemptRepository struct{ db *pgxpool.Pool }

func NewLoginAttemptRepository(db *pgxpool.Pool) *LoginAttemptRepository {
	return &LoginAttemptRepository{db: db}
}

func (r *LoginAttemptRepository) SaveLoginAttempt(ctx context.Context, attempt model.LoginAttempt) error {
	_, err := r.db.Exec(ctx, `INSERT INTO oidc_login_attempts
 (state_digest,provider,nonce,pkce_verifier,binding_digest,redirect_after,created_at,expires_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		attempt.StateDigest, attempt.Provider, attempt.Nonce, attempt.PKCEVerifier,
		attempt.BindingDigest, attempt.RedirectAfter, attempt.CreatedAt, attempt.ExpiresAt)
	return storageError(ctx, "store login attempt", err)
}

// ConsumeLoginAttempt is one statement, so of concurrent callbacks for one
// attempt only the first delete returns the row; a wrong binding or provider
// matches no row and deletes nothing.
func (r *LoginAttemptRepository) ConsumeLoginAttempt(ctx context.Context, stateDigest []byte, provider string, bindingDigest []byte, now time.Time) (model.LoginAttempt, error) {
	var attempt model.LoginAttempt
	err := r.db.QueryRow(ctx, `DELETE FROM oidc_login_attempts
 WHERE state_digest=$1 AND provider=$2 AND binding_digest=$3 AND expires_at>$4
 RETURNING state_digest,provider,nonce,pkce_verifier,binding_digest,redirect_after,created_at,expires_at`,
		stateDigest, provider, bindingDigest, now).
		Scan(&attempt.StateDigest, &attempt.Provider, &attempt.Nonce, &attempt.PKCEVerifier,
			&attempt.BindingDigest, &attempt.RedirectAfter, &attempt.CreatedAt, &attempt.ExpiresAt)
	return attempt, storageError(ctx, "consume login attempt", err)
}

func (r *LoginAttemptRepository) DeleteExpiredLoginAttempts(ctx context.Context, now time.Time, limit int) error {
	_, err := r.db.Exec(ctx, `DELETE FROM oidc_login_attempts WHERE state_digest IN
 (SELECT state_digest FROM oidc_login_attempts WHERE expires_at<=$1 ORDER BY expires_at,state_digest LIMIT $2 FOR UPDATE SKIP LOCKED)`, now, limit)
	return storageError(ctx, "clean expired login attempts", err)
}
