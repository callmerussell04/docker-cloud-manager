package repository

import (
	"context"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RegistryGCRepository keeps the singleton registry_gc_state row that gates
// Registry garbage collection against build claims. Every deadline is measured
// with the database clock, the same clock ClaimBuild compares the lease with.
type RegistryGCRepository struct {
	db *pgxpool.Pool
}

func NewRegistryGCRepository(db *pgxpool.Pool) *RegistryGCRepository {
	return &RegistryGCRepository{db: db}
}

// TryAcquireLease takes the collection lease for lease when no build runs and
// no earlier lease is live. It holds the build claim lock, so no claim can
// start a build between the running check and the lease write. While builds
// run and the last completed collection is older than drainAfter, it sets
// draining instead, so new claims are refused until the running builds finish.
func (r *RegistryGCRepository) TryAcquireLease(ctx context.Context, lease, drainAfter time.Duration) (model.RegistryGCAttempt, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return model.RegistryGCAttempt{}, wrapOp("registry gc acquire lease", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, buildClaimLockKey); err != nil {
		return model.RegistryGCAttempt{}, wrapOp("registry gc acquire lease", err)
	}
	var running, leaseLive, draining, overdue bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM builds WHERE status='running'),
 COALESCE(lease_until>clock_timestamp(),false), draining_since IS NOT NULL,
 last_completed_at<clock_timestamp()-$1*interval '1 second'
 FROM registry_gc_state FOR UPDATE`, drainAfter.Seconds()).Scan(&running, &leaseLive, &draining, &overdue)
	if err != nil {
		return model.RegistryGCAttempt{}, wrapOp("registry gc acquire lease", err)
	}
	attempt := model.RegistryGCAttempt{Draining: draining}
	switch {
	case !running && !leaseLive:
		if _, err := tx.Exec(ctx, setLeaseSQL, lease.Seconds()); err != nil {
			return model.RegistryGCAttempt{}, wrapOp("registry gc acquire lease", err)
		}
		attempt.Acquired = true
	case running && !draining && overdue:
		if _, err := tx.Exec(ctx, `UPDATE registry_gc_state SET draining_since=clock_timestamp()`); err != nil {
			return model.RegistryGCAttempt{}, wrapOp("registry gc start draining", err)
		}
		attempt.Draining = true
	default:
		return attempt, nil
	}
	if err := commitTx(ctx, tx, "registry gc acquire lease"); err != nil {
		return model.RegistryGCAttempt{}, err
	}
	return attempt, nil
}

// setLeaseSQL sets the lease deadline to $1 seconds from now.
const setLeaseSQL = `UPDATE registry_gc_state SET lease_until=clock_timestamp()+$1*interval '1 second'`

// RenewLease moves the deadline of a live lease to lease from now, for a
// collection that is still running. A lease that has already lapsed is not
// renewed, because a claim may have started a build since; renewed is false.
func (r *RegistryGCRepository) RenewLease(ctx context.Context, lease time.Duration) (bool, error) {
	tag, err := r.db.Exec(ctx, setLeaseSQL+` WHERE lease_until>clock_timestamp()`, lease.Seconds())
	if err != nil {
		return false, wrapOp("registry gc renew lease", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ReleaseLease ends a collection: it clears the lease and draining. completed
// records that the collection process was observed finished, which resets the
// drain clock; a collection that could not run leaves it as it was.
func (r *RegistryGCRepository) ReleaseLease(ctx context.Context, completed bool) error {
	_, err := r.db.Exec(ctx, `UPDATE registry_gc_state SET lease_until=NULL, draining_since=NULL,
 last_completed_at=CASE WHEN $1 THEN clock_timestamp() ELSE last_completed_at END`, completed)
	return wrapOp("registry gc release lease", err)
}
