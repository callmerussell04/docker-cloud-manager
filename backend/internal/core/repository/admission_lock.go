package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// Admission transactions use their own bounded pool so pinned lock holders and
// waiters cannot consume the query connections needed to finish reservations.
// BeginTx also arranges rollback on cancellation, releasing all advisory locks.
func acquireAdmissionLock(ctx context.Context, db *sql.DB, keys []string) (func(), error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin admission lock: %w", err)
	}
	release := lockRelease(tx)
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, key); err != nil {
			release()
			if ctx.Err() != nil {
				return nil, fmt.Errorf("acquire admission lock: %w", ctx.Err())
			}
			return nil, fmt.Errorf("acquire admission lock: %w", err)
		}
	}
	return release, nil
}
