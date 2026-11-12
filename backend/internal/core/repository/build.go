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

type BuildRepository struct {
	db     *pgxpool.Pool
	lockDB *pgxpool.Pool
}

func NewBuildRepository(db, lockDB *pgxpool.Pool) *BuildRepository {
	return &BuildRepository{db: db, lockDB: lockDB}
}

func (r *BuildRepository) Save(ctx context.Context, b model.Build) error {
	query := `
		INSERT INTO builds (id, image_id, owner_id, project_id, project_service_name, status, log_file_path, archive_object_key, created_at, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE($9, clock_timestamp()), $10, $11)
	`
	finishedAt := b.FinishedAt

	_, err := r.db.Exec(
		ctx,
		query,
		b.ID,
		b.ImageID,
		b.OwnerID,
		nullableUUID(b.ProjectID),
		nullIfZero(b.ProjectServiceName),
		b.Status,
		b.LogFilePath,
		b.ArchiveObjectKey,
		nullTimeIfZero(b.CreatedAt),
		nullTimeIfZero(b.StartedAt),
		finishedAt,
	)
	return err
}

func (r *BuildRepository) CreateQueuedBuild(ctx context.Context, img model.Image, build model.Build, outbox model.BuildQueueOutbox) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return wrapOp("build create queued build", err)
	}
	defer tx.Rollback(ctx)

	imgStatus := img.Status
	if imgStatus == "" {
		imgStatus = model.ImageStatusBuilding
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO images (id, owner_id, tag, size_mb, metadata, status)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, img.ID, img.OwnerID, img.Tag, img.SizeMB, img.Metadata, imgStatus); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrAlreadyExists
		}
		return wrapOp("build create queued build", err)
	}

	finishedAt := build.FinishedAt
	if _, err := tx.Exec(ctx, `
			INSERT INTO builds (id, image_id, owner_id, project_id, project_service_name, status, log_file_path, archive_object_key, created_at, started_at, finished_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE($9, clock_timestamp()), $10, $11)
		`,
		build.ID,
		build.ImageID,
		build.OwnerID,
		nullableUUID(build.ProjectID),
		nullIfZero(build.ProjectServiceName),
		build.Status,
		build.LogFilePath,
		build.ArchiveObjectKey,
		nullTimeIfZero(build.CreatedAt),
		nullTimeIfZero(build.StartedAt),
		finishedAt,
	); err != nil {
		return wrapOp("build create queued build", err)
	}

	outboxStatus := outbox.Status
	if outboxStatus == "" {
		outboxStatus = model.BuildOutboxStatusPending
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO build_queue_outbox (id, build_id, exchange, routing_key, payload, status, attempts, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, outbox.ID, outbox.BuildID, outbox.Exchange, outbox.RoutingKey, string(outbox.Payload), outboxStatus, outbox.Attempts, outbox.LastError); err != nil {
		return wrapOp("build create queued build", err)
	}

	return commitTx(ctx, tx, "create queued build")
}

func (r *BuildRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	query := `
		UPDATE builds 
		SET status = $1, finished_at = $2 
		WHERE id = $3
	`

	var finishedAt *time.Time
	if model.IsBuildTerminalStatus(status) {
		finishedAt = ptr(time.Now())
	}

	res, err := r.db.Exec(ctx, query, status, finishedAt, id)
	if err != nil {
		return err
	}

	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}

	return nil
}

func (r *BuildRepository) GetByImageID(ctx context.Context, imageID uuid.UUID) ([]model.Build, error) {
	query := `
		SELECT id, image_id, owner_id, project_id, project_service_name, status, log_file_path, archive_object_key, created_at, started_at, finished_at
		FROM builds
		WHERE image_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Query(ctx, query, imageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var builds []model.Build

	for rows.Next() {
		var b model.Build
		var finishedAt *time.Time
		var startedAt *time.Time
		var projectID *string
		var projectServiceName *string

		if err := rows.Scan(&b.ID, &b.ImageID, &b.OwnerID, &projectID, &projectServiceName, &b.Status, &b.LogFilePath, &b.ArchiveObjectKey, &b.CreatedAt, &startedAt, &finishedAt); err != nil {
			return nil, err
		}
		b.StartedAt = valueOrZero(startedAt)
		applyBuildProjectFields(&b, projectID, projectServiceName)

		b.FinishedAt = finishedAt
		builds = append(builds, b)
	}
	return builds, rows.Err()
}

func (r *BuildRepository) GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Build, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, image_id, owner_id, project_id, project_service_name, status, log_file_path, archive_object_key, created_at, started_at, finished_at
		FROM builds
		WHERE project_id = $1
		ORDER BY created_at DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	builds := make([]model.Build, 0)
	for rows.Next() {
		build, err := scanBuild(rows)
		if err != nil {
			return nil, err
		}
		builds = append(builds, build)
	}
	return builds, rows.Err()
}

func (r *BuildRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Build, error) {
	query := `
		SELECT id, image_id, owner_id, project_id, project_service_name, status, log_file_path, archive_object_key, created_at, started_at, finished_at
		FROM builds
		WHERE id = $1
	`
	var b model.Build
	var finishedAt *time.Time
	var startedAt *time.Time
	var projectID *string
	var projectServiceName *string

	err := r.db.QueryRow(ctx, query, id).Scan(
		&b.ID, &b.ImageID, &b.OwnerID, &projectID, &projectServiceName, &b.Status, &b.LogFilePath, &b.ArchiveObjectKey, &b.CreatedAt, &startedAt, &finishedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Build{}, apperrors.ErrNotFound
		}
		return model.Build{}, err
	}

	b.FinishedAt = finishedAt
	b.StartedAt = valueOrZero(startedAt)
	applyBuildProjectFields(&b, projectID, projectServiceName)
	return b, nil
}

func (r *BuildRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM builds WHERE id = $1`
	res, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *BuildRepository) GetStaleBuilds(ctx context.Context, threshold time.Time) ([]model.Build, error) {
	query := `
		SELECT id, image_id, owner_id, project_id, project_service_name, status, log_file_path, archive_object_key, created_at, started_at
		FROM builds
		WHERE status = $1 AND started_at < $2
	`
	rows, err := r.db.Query(ctx, query, model.BuildStatusRunning, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var builds []model.Build
	for rows.Next() {
		var b model.Build
		var startedAt *time.Time
		var projectID *string
		var projectServiceName *string
		if err := rows.Scan(&b.ID, &b.ImageID, &b.OwnerID, &projectID, &projectServiceName, &b.Status, &b.LogFilePath, &b.ArchiveObjectKey, &b.CreatedAt, &startedAt); err != nil {
			return nil, err
		}
		b.StartedAt = valueOrZero(startedAt)
		applyBuildProjectFields(&b, projectID, projectServiceName)
		builds = append(builds, b)
	}
	return builds, rows.Err()
}

func (r *BuildRepository) List(ctx context.Context, opts model.ListOptions) ([]model.Build, int, error) {
	var args []any
	where := ""
	if opts.OwnerID != nil {
		args = append(args, *opts.OwnerID)
		where = " WHERE owner_id = $1"
	}

	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM builds`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, image_id, owner_id, project_id, project_service_name, status, log_file_path, archive_object_key, created_at, started_at, finished_at
		FROM builds` + where + ` ORDER BY created_at DESC`
	if opts.Limit > 0 {
		args = append(args, opts.Limit, opts.Offset)
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var builds []model.Build
	for rows.Next() {
		b, err := scanBuild(rows)
		if err != nil {
			return nil, 0, err
		}
		builds = append(builds, b)
	}
	return builds, total, rows.Err()
}

func (r *BuildRepository) CountAll(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM builds`
	var count int
	err := r.db.QueryRow(ctx, query).Scan(&count)
	return count, err
}

func (r *BuildRepository) CountActiveByOwner(ctx context.Context, ownerID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM builds
		WHERE owner_id = $1 AND status IN ($2, $3)
	`, ownerID, model.BuildStatusPending, model.BuildStatusRunning).Scan(&count)
	return count, err
}

// ListActiveByOwner returns ownerID's pending and running builds, oldest first.
func (r *BuildRepository) ListActiveByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Build, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, image_id, owner_id, project_id, project_service_name, status, log_file_path, archive_object_key, created_at, started_at, finished_at
		FROM builds
		WHERE owner_id = $1 AND status IN ($2, $3)
		ORDER BY created_at
	`, ownerID, model.BuildStatusPending, model.BuildStatusRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	builds := make([]model.Build, 0)
	for rows.Next() {
		build, err := scanBuild(rows)
		if err != nil {
			return nil, err
		}
		builds = append(builds, build)
	}
	return builds, rows.Err()
}

func (r *BuildRepository) CountActive(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM builds
		WHERE status IN ($1, $2)
	`, model.BuildStatusPending, model.BuildStatusRunning).Scan(&count)
	return count, err
}

func (r *BuildRepository) AcquireCapacityLock(ctx context.Context) (func(), error) {
	return acquireAdmissionLock(ctx, r.lockDB, []string{"dcm:capacity"})
}

func (r *BuildRepository) LeasePendingBuildOutbox(ctx context.Context, limit int) ([]model.BuildQueueOutbox, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, wrapOp("build lease pending build outbox", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
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
		return nil, wrapOp("build lease pending build outbox", err)
	}
	defer rows.Close()

	var items []model.BuildQueueOutbox
	for rows.Next() {
		var item model.BuildQueueOutbox
		var lastError *string
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
			return nil, wrapOp("build lease pending build outbox", err)
		}
		item.LastError = lastError
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapOp("build lease pending build outbox", err)
	}

	if err := commitTx(ctx, tx, "lease pending build outbox"); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *BuildRepository) MarkBuildOutboxPublished(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.Exec(ctx, `
		UPDATE build_queue_outbox
		SET status = $1, published_at = NOW(), updated_at = NOW(), last_error = NULL
		WHERE id = $2
	`, model.BuildOutboxStatusPublished, id)
	if err != nil {
		return wrapOp("build mark build outbox published", err)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *BuildRepository) MarkBuildOutboxDiscarded(ctx context.Context, id uuid.UUID, cause error) error {
	var lastError *string
	if cause != nil {
		lastError = ptr(cause.Error())
	}
	res, err := r.db.Exec(ctx, `
		UPDATE build_queue_outbox
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE id = $3
	`, model.BuildOutboxStatusDiscarded, lastError, id)
	if err != nil {
		return wrapOp("build mark build outbox discarded", err)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func scanBuild(s scanner) (model.Build, error) {
	var b model.Build
	var finishedAt *time.Time
	var startedAt *time.Time
	var projectID *string
	var projectServiceName *string
	if err := s.Scan(&b.ID, &b.ImageID, &b.OwnerID, &projectID, &projectServiceName, &b.Status, &b.LogFilePath, &b.ArchiveObjectKey, &b.CreatedAt, &startedAt, &finishedAt); err != nil {
		return model.Build{}, err
	}
	b.FinishedAt = finishedAt
	b.StartedAt = valueOrZero(startedAt)
	applyBuildProjectFields(&b, projectID, projectServiceName)
	return b, nil
}

func (r *BuildRepository) MarkBuildOutboxPending(ctx context.Context, id uuid.UUID, cause error) error {
	var lastError *string
	if cause != nil {
		lastError = ptr(cause.Error())
	}

	res, err := r.db.Exec(ctx, `
		UPDATE build_queue_outbox
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE id = $3
	`, model.BuildOutboxStatusPending, lastError, id)
	if err != nil {
		return wrapOp("build mark build outbox pending", err)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func nullableUUID(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	return ptr(value.String())
}

func applyBuildProjectFields(b *model.Build, projectID *string, projectServiceName *string) {
	if projectID != nil {
		if parsed, err := uuid.Parse(*projectID); err == nil {
			b.ProjectID = &parsed
		}
	}
	if projectServiceName != nil {
		b.ProjectServiceName = *projectServiceName
	}
}
