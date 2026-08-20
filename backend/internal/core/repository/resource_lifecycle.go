package repository

import (
	"context"
	"database/sql"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

type ResourceLifecycleRepository struct {
	db *sql.DB
}

func NewResourceLifecycleRepository(db *sql.DB) *ResourceLifecycleRepository {
	return &ResourceLifecycleRepository{db: db}
}

func (r *ResourceLifecycleRepository) CreateQueuedVolume(ctx context.Context, vol model.Volume, op model.ResourceOperation, outbox model.ResourceLifecycleOutbox) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := insertVolumeTx(ctx, tx, vol); err != nil {
		return err
	}
	if vol.ProjectID != nil {
		if err := insertProjectVolumeLinkTx(ctx, tx, *vol.ProjectID, vol.ID); err != nil {
			return err
		}
	}
	if err := insertResourceOperationTx(ctx, tx, op); err != nil {
		return err
	}
	if err := insertResourceLifecycleOutboxTx(ctx, tx, outbox); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ResourceLifecycleRepository) QueueVolumeDelete(ctx context.Context, id uuid.UUID, op model.ResourceOperation, outbox model.ResourceLifecycleOutbox) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := insertResourceOperationTx(ctx, tx, op); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE volumes
		SET status = $1, last_observed_at = NOW(), last_error = NULL
		WHERE id = $2
	`, model.VolumeStatusDeleting, id)
	if err != nil {
		return err
	}
	if err := rowsAffectedOrNotFound(res); err != nil {
		return err
	}
	if err := insertResourceLifecycleOutboxTx(ctx, tx, outbox); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ResourceLifecycleRepository) QueueImageDelete(ctx context.Context, id uuid.UUID, op model.ResourceOperation, outbox model.ResourceLifecycleOutbox) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := insertResourceOperationTx(ctx, tx, op); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE images
		SET status = $1, last_observed_at = NOW(), last_error = NULL
		WHERE id = $2
	`, model.ImageStatusDeleting, id)
	if err != nil {
		return err
	}
	if err := rowsAffectedOrNotFound(res); err != nil {
		return err
	}
	if err := insertResourceLifecycleOutboxTx(ctx, tx, outbox); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ResourceLifecycleRepository) HasActiveOperation(ctx context.Context, resourceType string, resourceID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
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
	op, err := scanResourceOperation(r.db.QueryRowContext(ctx, `
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
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.ResourceOperation{}, false, err
	}
	defer tx.Rollback()

	op, err := scanResourceOperation(tx.QueryRowContext(ctx, `
		SELECT id, resource_type, resource_id, owner_id, operation, status, attempts, last_error, created_at, updated_at
		FROM resource_operations
		WHERE id = $1
		FOR UPDATE
	`, id))
	if err != nil {
		return model.ResourceOperation{}, false, err
	}
	if op.Status != model.OperationStatusPending || (maxAttempts > 0 && op.Attempts >= maxAttempts) {
		if err := tx.Commit(); err != nil {
			return model.ResourceOperation{}, false, err
		}
		return op, false, nil
	}
	op, err = scanResourceOperation(tx.QueryRowContext(ctx, `
		UPDATE resource_operations
		SET status = $1, attempts = attempts + 1, updated_at = NOW()
		WHERE id = $2
		RETURNING id, resource_type, resource_id, owner_id, operation, status, attempts, last_error, created_at, updated_at
	`, model.OperationStatusRunning, id))
	if err != nil {
		return model.ResourceOperation{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return model.ResourceOperation{}, false, err
	}
	return op, true, nil
}

func (r *ResourceLifecycleRepository) CompleteOperation(ctx context.Context, id uuid.UUID, status string, cause error) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE resource_operations
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE id = $3
	`, status, nullableError(cause), id)
	if err != nil {
		return err
	}
	return rowsAffectedOrNotFound(res)
}

func (r *ResourceLifecycleRepository) RequeueOperation(ctx context.Context, id uuid.UUID, cause error) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE resource_operations
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE id = $3 AND status = $4
	`, model.OperationStatusPending, nullableError(cause), id, model.OperationStatusRunning)
	if err != nil {
		return err
	}
	return rowsAffectedOrNotFound(res)
}

func (r *ResourceLifecycleRepository) LeasePendingResourceOutbox(ctx context.Context, limit int) ([]model.ResourceLifecycleOutbox, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
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
		return nil, err
	}
	defer rows.Close()

	items := make([]model.ResourceLifecycleOutbox, 0)
	for rows.Next() {
		item, err := scanResourceLifecycleOutbox(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
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
	res, err := r.db.ExecContext(ctx, query, status, nullableError(cause), id)
	if err != nil {
		return err
	}
	return rowsAffectedOrNotFound(res)
}

func insertResourceLifecycleOutboxTx(ctx context.Context, tx *sql.Tx, outbox model.ResourceLifecycleOutbox) error {
	_, err := tx.ExecContext(ctx, `
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
		if err == sql.ErrNoRows {
			return model.ResourceLifecycleOutbox{}, apperrors.ErrNotFound
		}
		return model.ResourceLifecycleOutbox{}, err
	}
	return item, nil
}

func rowsAffectedOrNotFound(res sql.Result) error {
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}
