package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/jackc/pgx/v5"
)

// storageError is the error a session or login-attempt query returns. Driver
// errors may include bind values (digests, password hashes or PKCE verifiers),
// so they stay out of returned and logged errors; only a safe operation name
// and the availability kind remain.
func storageError(ctx context.Context, operation string, err error) error {
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
