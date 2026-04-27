package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type BuildRepository struct {
	db *sql.DB
}

func NewBuildRepository(db *sql.DB) *BuildRepository {
	return &BuildRepository{db: db}
}

func (r *BuildRepository) Save(ctx context.Context, b model.Build) error {
	query := `
		INSERT INTO builds (id, image_id, owner_id, status, log_file_path, archive_object_key, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	var finishedAt sql.NullTime
	if b.FinishedAt != nil {
		finishedAt.Time = *b.FinishedAt
		finishedAt.Valid = true
	}

	_, err := r.db.ExecContext(ctx, query, b.ID, b.ImageID, b.OwnerID, b.Status, b.LogFilePath, b.ArchiveObjectKey, b.StartedAt, finishedAt)
	return err
}

func (r *BuildRepository) CreateQueuedBuild(ctx context.Context, img model.Image, build model.Build, outbox model.BuildQueueOutbox) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	imgStatus := img.Status
	if imgStatus == "" {
		imgStatus = model.ImageStatusBuilding
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO images (id, owner_id, tag, size_mb, is_custom, metadata, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, img.ID, img.OwnerID, img.Tag, img.SizeMB, img.IsCustom, img.Metadata, imgStatus); err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrAlreadyExists
		}
		return err
	}

	var finishedAt sql.NullTime
	if build.FinishedAt != nil {
		finishedAt.Time = *build.FinishedAt
		finishedAt.Valid = true
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO builds (id, image_id, owner_id, status, log_file_path, archive_object_key, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, build.ID, build.ImageID, build.OwnerID, build.Status, build.LogFilePath, build.ArchiveObjectKey, build.StartedAt, finishedAt); err != nil {
		return err
	}

	outboxStatus := outbox.Status
	if outboxStatus == "" {
		outboxStatus = model.BuildOutboxStatusPending
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO build_queue_outbox (id, build_id, exchange, routing_key, payload, status, attempts, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, outbox.ID, outbox.BuildID, outbox.Exchange, outbox.RoutingKey, string(outbox.Payload), outboxStatus, outbox.Attempts, nullableString(outbox.LastError)); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *BuildRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	query := `
		UPDATE builds 
		SET status = $1, finished_at = $2 
		WHERE id = $3
	`

	var finishedAt sql.NullTime
	if model.IsBuildTerminalStatus(status) {
		finishedAt.Time = time.Now()
		finishedAt.Valid = true
	}

	res, err := r.db.ExecContext(ctx, query, status, finishedAt, id)
	if err != nil {
		return err
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}

	return nil
}

func (r *BuildRepository) GetByImageID(ctx context.Context, imageID uuid.UUID) ([]model.Build, error) {
	query := `
		SELECT id, image_id, owner_id, status, log_file_path, archive_object_key, started_at, finished_at 
		FROM builds 
		WHERE image_id = $1 
		ORDER BY started_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, imageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var builds []model.Build

	for rows.Next() {
		var b model.Build
		var finishedAt sql.NullTime

		if err := rows.Scan(&b.ID, &b.ImageID, &b.OwnerID, &b.Status, &b.LogFilePath, &b.ArchiveObjectKey, &b.StartedAt, &finishedAt); err != nil {
			return nil, err
		}

		if finishedAt.Valid {
			b.FinishedAt = &finishedAt.Time
		}
		builds = append(builds, b)
	}
	return builds, rows.Err()
}

func (r *BuildRepository) GetUserBuilds(ctx context.Context, ownerID uuid.UUID) ([]model.Build, error) {
	query := `
		SELECT b.id, b.image_id, b.owner_id, b.status, b.log_file_path, b.archive_object_key, b.started_at, b.finished_at 
		FROM builds b
		WHERE b.owner_id = $1 
		ORDER BY b.started_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var builds []model.Build
	for rows.Next() {
		var b model.Build
		var finishedAt sql.NullTime

		if err := rows.Scan(&b.ID, &b.ImageID, &b.OwnerID, &b.Status, &b.LogFilePath, &b.ArchiveObjectKey, &b.StartedAt, &finishedAt); err != nil {
			return nil, err
		}

		if finishedAt.Valid {
			b.FinishedAt = &finishedAt.Time
		}
		builds = append(builds, b)
	}
	return builds, rows.Err()
}

func (r *BuildRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Build, error) {
	query := `
		SELECT id, image_id, owner_id, status, log_file_path, archive_object_key, started_at, finished_at 
		FROM builds 
		WHERE id = $1
	`
	var b model.Build
	var finishedAt sql.NullTime

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&b.ID, &b.ImageID, &b.OwnerID, &b.Status, &b.LogFilePath, &b.ArchiveObjectKey, &b.StartedAt, &finishedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Build{}, apperrors.ErrNotFound
		}
		return model.Build{}, err
	}

	if finishedAt.Valid {
		b.FinishedAt = &finishedAt.Time
	}
	return b, nil
}

func (r *BuildRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM builds WHERE id = $1`
	res, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *BuildRepository) GetStaleBuilds(ctx context.Context, threshold time.Time) ([]model.Build, error) {
	query := `
		SELECT id, image_id, owner_id, status, log_file_path, archive_object_key, started_at 
		FROM builds 
		WHERE status IN ($1, $2) AND started_at < $3
	`
	rows, err := r.db.QueryContext(ctx, query, model.BuildStatusPending, model.BuildStatusRunning, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var builds []model.Build
	for rows.Next() {
		var b model.Build
		if err := rows.Scan(&b.ID, &b.ImageID, &b.OwnerID, &b.Status, &b.LogFilePath, &b.ArchiveObjectKey, &b.StartedAt); err != nil {
			return nil, err
		}
		builds = append(builds, b)
	}
	return builds, rows.Err()
}

func (r *BuildRepository) GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Build, int, error) {
	var total int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM builds`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	query := `
		SELECT b.id, b.image_id, b.owner_id, b.status, b.log_file_path, b.archive_object_key, b.started_at, b.finished_at
		FROM builds b
		ORDER BY b.started_at DESC LIMIT $1 OFFSET $2
	`
	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var builds []model.Build
	for rows.Next() {
		var b model.Build
		var finishedAt sql.NullTime
		if err := rows.Scan(&b.ID, &b.ImageID, &b.OwnerID, &b.Status, &b.LogFilePath, &b.ArchiveObjectKey, &b.StartedAt, &finishedAt); err != nil {
			return nil, 0, err
		}
		if finishedAt.Valid {
			b.FinishedAt = &finishedAt.Time
		}
		builds = append(builds, b)
	}
	return builds, total, rows.Err()
}

func (r *BuildRepository) LeasePendingBuildOutbox(ctx context.Context, limit int) ([]model.BuildQueueOutbox, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		WITH next AS (
			SELECT id
			FROM build_queue_outbox
			WHERE status = $1
			   OR (status = $2 AND updated_at < NOW() - INTERVAL '1 minute')
			ORDER BY created_at
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		UPDATE build_queue_outbox o
		SET status = $2, attempts = attempts + 1, updated_at = NOW()
		FROM next
		WHERE o.id = next.id
		RETURNING o.id, o.build_id, o.exchange, o.routing_key, o.payload, o.status, o.attempts, o.last_error, o.created_at, o.updated_at
	`, model.BuildOutboxStatusPending, model.BuildOutboxStatusPublishing, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.BuildQueueOutbox
	for rows.Next() {
		var item model.BuildQueueOutbox
		var lastError sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.BuildID,
			&item.Exchange,
			&item.RoutingKey,
			&item.Payload,
			&item.Status,
			&item.Attempts,
			&lastError,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if lastError.Valid {
			item.LastError = &lastError.String
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

func (r *BuildRepository) MarkBuildOutboxPublished(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE build_queue_outbox
		SET status = $1, published_at = NOW(), updated_at = NOW(), last_error = NULL
		WHERE id = $2
	`, model.BuildOutboxStatusPublished, id)
	if err != nil {
		return err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *BuildRepository) MarkBuildOutboxPending(ctx context.Context, id uuid.UUID, cause error) error {
	var lastError sql.NullString
	if cause != nil {
		lastError.String = cause.Error()
		lastError.Valid = true
	}

	res, err := r.db.ExecContext(ctx, `
		UPDATE build_queue_outbox
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE id = $3
	`, model.BuildOutboxStatusPending, lastError, id)
	if err != nil {
		return err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func nullableString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}
