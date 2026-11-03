package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Admission transactions use their own bounded pool so pinned lock holders and
// waiters cannot consume the query connections needed to finish reservations.
// Ending ctx rolls the transaction back, releasing all advisory locks.
func acquireAdmissionLock(ctx context.Context, db *pgxpool.Pool, keys []string) (func(), error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin admission lock: %w", err)
	}
	release := lockRelease(tx)
	for _, key := range keys {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, key); err != nil {
			release()
			if ctx.Err() != nil {
				return nil, fmt.Errorf("acquire admission lock: %w", ctx.Err())
			}
			return nil, fmt.Errorf("acquire admission lock: %w", err)
		}
	}
	return releaseOnDone(ctx, release), nil
}
