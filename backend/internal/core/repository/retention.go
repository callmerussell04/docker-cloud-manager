package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RetentionRepository deletes rows that record finished work and that nothing
// reads any more. Every delete takes rows older than a cutoff, at most limit
// per call, in its own statement.
type RetentionRepository struct {
	db *pgxpool.Pool
}

func NewRetentionRepository(db *pgxpool.Pool) *RetentionRepository {
	return &RetentionRepository{db: db}
}

// DeleteFinishedOperations deletes queued operations that ended (done or
// failed) before cutoff. Their container and resource outbox rows are removed
// by cascade. A pending or running operation is never deleted.
func (r *RetentionRepository) DeleteFinishedOperations(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		DELETE FROM resource_operations
		WHERE id IN (
			SELECT id FROM resource_operations
			WHERE status IN ('done', 'failed') AND updated_at < $1
			LIMIT $2
		)`, cutoff, limit)
	return tag.RowsAffected(), wrapOp("retention delete finished operations", err)
}

// DeleteSpentBuildCredentials deletes Registry build credentials last issued
// before cutoff whose build is gone or no longer pending or running. By then
// the build's Registry cleanup has been queued from the credential.
func (r *RetentionRepository) DeleteSpentBuildCredentials(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		DELETE FROM registry_build_credentials
		WHERE build_id IN (
			SELECT c.build_id FROM registry_build_credentials c
			WHERE c.last_issued_at < $1
			  AND NOT EXISTS (
				SELECT 1 FROM builds b
				WHERE b.id = c.build_id AND b.status IN ('pending', 'running')
			  )
			LIMIT $2
		)`, cutoff, limit)
	return tag.RowsAffected(), wrapOp("retention delete spent build credentials", err)
}

// DeleteReleasedStagedObjects deletes staged-object reservations released
// before cutoff. An active reservation is never deleted.
func (r *RetentionRepository) DeleteReleasedStagedObjects(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		DELETE FROM staged_object_reservations
		WHERE id IN (
			SELECT id FROM staged_object_reservations
			WHERE status = 'released' AND released_at < $1
			LIMIT $2
		)`, cutoff, limit)
	return tag.RowsAffected(), wrapOp("retention delete released staged objects", err)
}
