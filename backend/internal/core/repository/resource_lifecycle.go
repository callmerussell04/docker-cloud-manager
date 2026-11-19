package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ResourceLifecycleRepository struct {
	db *pgxpool.Pool
}

func NewResourceLifecycleRepository(db *pgxpool.Pool) *ResourceLifecycleRepository {
	return &ResourceLifecycleRepository{db: db}
}

// resumableResourceOperation matches, on resource_operations aliased op, the
// volume and image operations recovery returns to the queue: volume create
// and delete, and image delete. Its parameters are numbered from first.
func resumableResourceOperation(first int) string {
	return fmt.Sprintf(`((op.resource_type = $%[1]d AND op.operation IN ($%[2]d, $%[3]d)) OR (op.resource_type = $%[4]d AND op.operation = $%[3]d))`,
		first, first+1, first+2, first+3)
}

func resumableResourceOperationArgs() []any {
	return []any{model.ResourceTypeVolume, model.OperationCreate, model.OperationDelete, model.ResourceTypeImage}
}

// RecoverInterruptedOperations returns running volume create and delete and
// image delete operations in scope to pending, returning their attempt when
// the scope says so, and marks outbox records pending with their original
// message identity. Docker names and Registry references are stable, so
// workers can repeat side effects. Without an age bound, as at startup before
// publishers and consumers run, the records of every pending such operation
// are marked too, because a published message may have been acknowledged
// before shutdown; with one, as in the operation recovery pass, only the
// recovered operations' records.
func (r *ResourceLifecycleRepository) RecoverInterruptedOperations(ctx context.Context, cause error, scope model.OperationRecoveryScope) ([]model.RecoveredOperation, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin resource operation recovery: %w", err)
	}
	defer tx.Rollback(ctx)

	args := append([]any{model.OperationStatusPending, nullableError(cause), model.OperationStatusRunning, scope.ReturnAttempt, ageSeconds(scope.RunningLongerThan)},
		resumableResourceOperationArgs()...)
	rows, err := tx.Query(ctx, `
		UPDATE resource_operations op
		SET status = $1,
			attempts = CASE WHEN $4 THEN GREATEST(op.attempts - 1, 0) ELSE op.attempts END,
			last_error = $2, updated_at = NOW()
		WHERE op.status = $3
			AND `+claimedBefore("op.updated_at", 5)+`
			AND `+resumableResourceOperation(6)+`
		RETURNING op.id, op.resource_type, op.resource_id, op.operation
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("requeue interrupted resource operations: %w", err)
	}
	recovered, err := collectRecoveredOperations(rows)
	if err != nil {
		return nil, fmt.Errorf("requeue interrupted resource operations: %w", err)
	}
	args = append([]any{model.ResourceOutboxStatusPending, nullableError(cause), model.OperationStatusPending, ageSeconds(scope.RunningLongerThan), recoveredIDs(recovered)},
		resumableResourceOperationArgs()...)
	if _, err := tx.Exec(ctx, `
		UPDATE resource_lifecycle_queue_outbox o
		SET status = $1, published_at = NULL, last_error = $2, updated_at = NOW()
		FROM resource_operations op
		WHERE o.operation_id = op.id AND op.status = $3
			AND ($4::double precision = 0 OR op.id = ANY($5))
			AND `+resumableResourceOperation(6)+`
	`, args...); err != nil {
		return nil, fmt.Errorf("requeue resource lifecycle outbox: %w", err)
	}
	if err := commitTx(ctx, tx, "resource operation recovery"); err != nil {
		return nil, err
	}
	return recovered, nil
}

// RecoverLostResourceDeliveries finds pending volume and image operations
// whose message is no longer in the queue, judged on the database clock. One
// whose outbox record was published or discarded longer ago than olderThan,
// and whose resource row exists, has the record returned to pending with its
// payload, for the relay to publish again. One whose resource row is gone,
// that was last updated longer ago than olderThan and whose record the relay
// no longer owns is ended: done for a delete, failed with cause otherwise.
// Without that, the relay would discard it again on every pass.
func (r *ResourceLifecycleRepository) RecoverLostResourceDeliveries(ctx context.Context, cause error, olderThan time.Duration) (model.LostDeliveryRecovery, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return model.LostDeliveryRecovery{}, wrapOp("resource recover lost deliveries", err)
	}
	defer tx.Rollback(ctx)

	const resourceExists = `CASE op.resource_type
			WHEN $1 THEN EXISTS (SELECT 1 FROM volumes v WHERE v.id = op.resource_id)
			WHEN $2 THEN EXISTS (SELECT 1 FROM images i WHERE i.id = op.resource_id)
			ELSE TRUE
		END`
	rows, err := tx.Query(ctx, `
		UPDATE resource_lifecycle_queue_outbox o
		SET status = $3, published_at = NULL, updated_at = NOW()
		FROM resource_operations op
		WHERE o.operation_id = op.id
			AND op.status = $4
			AND o.status IN ($5, $6)
			AND `+updatedBefore("o.updated_at", 7)+`
			AND `+resourceExists+`
		RETURNING op.id, op.resource_type, op.resource_id, op.operation
	`, model.ResourceTypeVolume, model.ResourceTypeImage, model.ResourceOutboxStatusPending, model.OperationStatusPending,
		model.ResourceOutboxStatusPublished, model.ResourceOutboxStatusDiscarded, ageSeconds(olderThan))
	if err != nil {
		return model.LostDeliveryRecovery{}, wrapOp("resource recover lost deliveries", err)
	}
	republished, err := collectRecoveredOperations(rows)
	if err != nil {
		return model.LostDeliveryRecovery{}, wrapOp("resource recover lost deliveries", err)
	}
	rows, err = tx.Query(ctx, `
		UPDATE resource_operations op
		SET status = CASE WHEN op.operation = $3 THEN $4 ELSE $5 END,
			last_error = CASE WHEN op.operation = $3 THEN NULL ELSE $6 END,
			updated_at = NOW()
		WHERE op.resource_type IN ($1, $2)
			AND op.status = $7
			AND `+updatedBefore("op.updated_at", 8)+`
			AND NOT `+resourceExists+`
			AND NOT EXISTS (
				SELECT 1 FROM resource_lifecycle_queue_outbox o
				WHERE o.operation_id = op.id
					AND (o.status IN ($9, $10) OR NOT `+updatedBefore("o.updated_at", 8)+`)
			)
		RETURNING op.id, op.resource_type, op.resource_id, op.operation
	`, model.ResourceTypeVolume, model.ResourceTypeImage, model.OperationDelete, model.OperationStatusDone, model.OperationStatusFailed,
		nullableError(cause), model.OperationStatusPending, ageSeconds(olderThan),
		model.ResourceOutboxStatusPending, model.ResourceOutboxStatusPublishing)
	if err != nil {
		return model.LostDeliveryRecovery{}, wrapOp("resource recover lost deliveries", err)
	}
	ended, err := collectRecoveredOperations(rows)
	if err != nil {
		return model.LostDeliveryRecovery{}, wrapOp("resource recover lost deliveries", err)
	}
	if err := commitTx(ctx, tx, "recover lost resource deliveries"); err != nil {
		return model.LostDeliveryRecovery{}, err
	}
	return model.LostDeliveryRecovery{Republished: republished, Ended: ended}, nil
}

func (r *ResourceLifecycleRepository) CreateQueuedVolume(ctx context.Context, vol model.Volume, op model.ResourceOperation, outbox model.ResourceLifecycleOutbox) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return wrapOp("resource create queued volume", err)
	}
	defer tx.Rollback(ctx)

	if err := insertVolumeTx(ctx, tx, vol); err != nil {
		return wrapOp("resource create queued volume", err)
	}
	if err := insertResourceOperationTx(ctx, tx, op); err != nil {
		return wrapOp("resource create queued volume", err)
	}
	if err := insertResourceLifecycleOutboxTx(ctx, tx, outbox); err != nil {
		return wrapOp("resource create queued volume", err)
	}
	return commitTx(ctx, tx, "create queued volume")
}

func (r *ResourceLifecycleRepository) QueueVolumeDelete(ctx context.Context, id uuid.UUID, op model.ResourceOperation, outbox model.ResourceLifecycleOutbox) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return wrapOp("resource queue volume delete", err)
	}
	defer tx.Rollback(ctx)

	if err := insertResourceOperationTx(ctx, tx, op); err != nil {
		return wrapOp("resource queue volume delete", err)
	}
	res, err := tx.Exec(ctx, `
		UPDATE volumes
		SET status = $1, last_observed_at = NOW(), last_error = NULL
		WHERE id = $2
	`, model.VolumeStatusDeleting, id)
	if err != nil {
		return wrapOp("resource queue volume delete", err)
	}
	if err := rowsAffectedOrNotFound(res); err != nil {
		return wrapOp("resource queue volume delete", err)
	}
	if err := insertResourceLifecycleOutboxTx(ctx, tx, outbox); err != nil {
		return wrapOp("resource queue volume delete", err)
	}
	return commitTx(ctx, tx, "queue volume delete")
}

func (r *ResourceLifecycleRepository) QueueImageDelete(ctx context.Context, id uuid.UUID, op model.ResourceOperation, outbox model.ResourceLifecycleOutbox) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return wrapOp("resource queue image delete", err)
	}
	defer tx.Rollback(ctx)

	if err := insertResourceOperationTx(ctx, tx, op); err != nil {
		return wrapOp("resource queue image delete", err)
	}
	res, err := tx.Exec(ctx, `
		UPDATE images
		SET status = $1, last_observed_at = NOW(), last_error = NULL
		WHERE id = $2
	`, model.ImageStatusDeleting, id)
	if err != nil {
		return wrapOp("resource queue image delete", err)
	}
	if err := rowsAffectedOrNotFound(res); err != nil {
		return wrapOp("resource queue image delete", err)
	}
	if err := insertResourceLifecycleOutboxTx(ctx, tx, outbox); err != nil {
		return wrapOp("resource queue image delete", err)
	}
	return commitTx(ctx, tx, "queue image delete")
}

func (r *ResourceLifecycleRepository) HasActiveOperation(ctx context.Context, resourceType string, resourceID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM resource_operations
			WHERE resource_type = $1
				AND resource_id = $2
				AND status IN ($3, $4)
		)
	`, resourceType, resourceID, model.OperationStatusPending, model.OperationStatusRunning).Scan(&exists)
	return exists, err
}

func (r *ResourceLifecycleRepository) GetOperationByID(ctx context.Context, id uuid.UUID) (model.ResourceOperation, error) {
	op, err := scanResourceOperation(r.db.QueryRow(ctx, `
		SELECT id, resource_type, resource_id, owner_id, operation, status, attempts, last_error, created_at, updated_at
		FROM resource_operations
		WHERE id = $1
	`, id))
	if err != nil {
		return model.ResourceOperation{}, err
	}
	return op, nil
}

func (r *ResourceLifecycleRepository) ClaimPendingOperation(ctx context.Context, id uuid.UUID, maxAttempts int) (model.ResourceOperation, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return model.ResourceOperation{}, false, wrapOp("resource claim pending operation", err)
	}
	defer tx.Rollback(ctx)

	op, err := scanResourceOperation(tx.QueryRow(ctx, `
		SELECT id, resource_type, resource_id, owner_id, operation, status, attempts, last_error, created_at, updated_at
		FROM resource_operations
		WHERE id = $1
		FOR UPDATE
	`, id))
	if err != nil {
		return model.ResourceOperation{}, false, wrapOp("resource claim pending operation", err)
	}
	if op.Status != model.OperationStatusPending || (maxAttempts > 0 && op.Attempts >= maxAttempts) {
		if err := commitTx(ctx, tx, "skip unclaimable resource operation"); err != nil {
			return model.ResourceOperation{}, false, err
		}
		return op, false, nil
	}
	op, err = scanResourceOperation(tx.QueryRow(ctx, `
		UPDATE resource_operations
		SET status = $1, attempts = attempts + 1, updated_at = NOW()
		WHERE id = $2
		RETURNING id, resource_type, resource_id, owner_id, operation, status, attempts, last_error, created_at, updated_at
	`, model.OperationStatusRunning, id))
	if err != nil {
		return model.ResourceOperation{}, false, wrapOp("resource claim pending operation", err)
	}
	if err := commitTx(ctx, tx, "claim resource operation"); err != nil {
		return model.ResourceOperation{}, false, err
	}
	return op, true, nil
}

func (r *ResourceLifecycleRepository) CompleteOperation(ctx context.Context, id uuid.UUID, status string, cause error) error {
	res, err := r.db.Exec(ctx, `
		UPDATE resource_operations
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE id = $3
	`, status, nullableError(cause), id)
	if err != nil {
		return wrapOp("resource complete operation", err)
	}
	return rowsAffectedOrNotFound(res)
}

func (r *ResourceLifecycleRepository) RequeueOperation(ctx context.Context, id uuid.UUID, cause error) error {
	res, err := r.db.Exec(ctx, `
		UPDATE resource_operations
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE id = $3 AND status = $4
	`, model.OperationStatusPending, nullableError(cause), id, model.OperationStatusRunning)
	if err != nil {
		return wrapOp("resource requeue operation", err)
	}
	return rowsAffectedOrNotFound(res)
}

func (r *ResourceLifecycleRepository) LeasePendingResourceOutbox(ctx context.Context, limit int) ([]model.ResourceLifecycleOutbox, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, wrapOp("resource lease pending resource outbox", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		WITH next AS (
			SELECT id
			FROM resource_lifecycle_queue_outbox
			WHERE status = $1
			   OR (status = $2 AND updated_at < NOW() - INTERVAL '1 minute')
			ORDER BY created_at
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		UPDATE resource_lifecycle_queue_outbox o
		SET status = $2, attempts = attempts + 1, updated_at = NOW()
		FROM next
		WHERE o.id = next.id
		RETURNING o.id, o.operation_id, o.resource_type, o.resource_id, o.exchange, o.routing_key, o.payload, o.status, o.attempts, o.last_error, o.created_at, o.updated_at
	`, model.ResourceOutboxStatusPending, model.ResourceOutboxStatusPublishing, limit)
	if err != nil {
		return nil, wrapOp("resource lease pending resource outbox", err)
	}
	defer rows.Close()

	items := make([]model.ResourceLifecycleOutbox, 0)
	for rows.Next() {
		item, err := scanResourceLifecycleOutbox(rows)
		if err != nil {
			return nil, wrapOp("resource lease pending resource outbox", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapOp("resource lease pending resource outbox", err)
	}
	if err := commitTx(ctx, tx, "lease pending resource outbox"); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ResourceLifecycleRepository) MarkResourceOutboxPublished(ctx context.Context, id uuid.UUID) error {
	return r.updateResourceOutboxStatus(ctx, id, model.ResourceOutboxStatusPublished, nil)
}

func (r *ResourceLifecycleRepository) MarkResourceOutboxPending(ctx context.Context, id uuid.UUID, cause error) error {
	return r.updateResourceOutboxStatus(ctx, id, model.ResourceOutboxStatusPending, cause)
}

func (r *ResourceLifecycleRepository) MarkResourceOutboxDiscarded(ctx context.Context, id uuid.UUID, cause error) error {
	return r.updateResourceOutboxStatus(ctx, id, model.ResourceOutboxStatusDiscarded, cause)
}

func (r *ResourceLifecycleRepository) updateResourceOutboxStatus(ctx context.Context, id uuid.UUID, status string, cause error) error {
	query := `
		UPDATE resource_lifecycle_queue_outbox
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE id = $3
	`
	if status == model.ResourceOutboxStatusPublished {
		query = `
			UPDATE resource_lifecycle_queue_outbox
			SET status = $1, published_at = NOW(), last_error = $2, updated_at = NOW()
			WHERE id = $3
		`
	}
	res, err := r.db.Exec(ctx, query, status, nullableError(cause), id)
	if err != nil {
		return err
	}
	return rowsAffectedOrNotFound(res)
}

func insertResourceLifecycleOutboxTx(ctx context.Context, tx pgx.Tx, outbox model.ResourceLifecycleOutbox) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO resource_lifecycle_queue_outbox (id, operation_id, resource_type, resource_id, exchange, routing_key, payload, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, outbox.ID, outbox.OperationID, outbox.ResourceType, outbox.ResourceID, outbox.Exchange, outbox.RoutingKey, outbox.Payload, outbox.Status)
	return err
}

func scanResourceLifecycleOutbox(s scanner) (model.ResourceLifecycleOutbox, error) {
	var item model.ResourceLifecycleOutbox
	err := s.Scan(
		&item.ID,
		&item.OperationID,
		&item.ResourceType,
		&item.ResourceID,
		&item.Exchange,
		&item.RoutingKey,
		&item.Payload,
		&item.Status,
		&item.Attempts,
		&item.LastError,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ResourceLifecycleOutbox{}, apperrors.ErrNotFound
		}
		return model.ResourceLifecycleOutbox{}, err
	}
	return item, nil
}

func rowsAffectedOrNotFound(res pgconn.CommandTag) error {
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}
