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

type ImageRepository struct {
	db *pgxpool.Pool
}

func NewImageRepository(db *pgxpool.Pool) *ImageRepository {
	return &ImageRepository{db: db}
}

func (r *ImageRepository) Save(ctx context.Context, img model.Image) error {
	query := `
		INSERT INTO images (id, owner_id, tag, size_mb, metadata, status)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	status := img.Status
	if status == "" {
		status = model.ImageStatusAvailable
	}
	_, err := r.db.Exec(ctx, query, img.ID, img.OwnerID, img.Tag, img.SizeMB, img.Metadata, status)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (r *ImageRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Image, error) {
	query := `
		SELECT id, owner_id, tag, size_mb, metadata, status, last_observed_at, last_error, created_at
		FROM images WHERE id = $1
	`

	var img model.Image
	var lastObservedAt *time.Time
	var lastError *string
	err := r.db.QueryRow(ctx, query, id).Scan(
		&img.ID, &img.OwnerID, &img.Tag, &img.SizeMB, &img.Metadata, &img.Status, &lastObservedAt, &lastError, &img.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Image{}, apperrors.ErrNotFound
		}
		return model.Image{}, err
	}

	img.LastObservedAt = lastObservedAt
	img.LastError = lastError

	return img, nil
}

func (r *ImageRepository) List(ctx context.Context, opts model.ListOptions) ([]model.Image, int, error) {
	var args []any
	where := ""
	if opts.OwnerID != nil {
		args = append(args, *opts.OwnerID)
		where = " WHERE owner_id = $1"
	}

	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM images`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, owner_id, tag, size_mb, metadata, status, last_observed_at, last_error, created_at
		FROM images` + where + ` ORDER BY created_at DESC`
	if opts.Limit > 0 {
		args = append(args, opts.Limit, opts.Offset)
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var images []model.Image
	for rows.Next() {
		img, err := scanImage(rows)
		if err != nil {
			return nil, 0, err
		}
		images = append(images, img)
	}
	return images, total, rows.Err()
}

func (r *ImageRepository) CountAll(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM images`
	var count int
	err := r.db.QueryRow(ctx, query).Scan(&count)
	return count, err
}

func (r *ImageRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM images WHERE id = $1`
	res, err := r.db.Exec(ctx, query, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return apperrors.New(apperrors.ErrResourceInUse, "image is currently used by a container")
		}
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ImageRepository) UpdateSize(ctx context.Context, id uuid.UUID, sizeMB int) error {
	query := `UPDATE images SET size_mb = $1, last_observed_at = NOW(), last_error = NULL WHERE id = $2`
	res, err := r.db.Exec(ctx, query, sizeMB, id)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ImageRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	query := `UPDATE images SET status = $1, last_observed_at = NOW(), last_error = NULL WHERE id = $2`
	res, err := r.db.Exec(ctx, query, status, id)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ImageRepository) MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error {
	var msg *string
	if cause != nil {
		msg = ptr(cause.Error())
	}
	query := `UPDATE images SET status = $1, last_observed_at = NOW(), last_error = $2 WHERE id = $3`
	res, err := r.db.Exec(ctx, query, status, msg, id)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ImageRepository) GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	query := `SELECT COALESCE(SUM(size_mb), 0) FROM images WHERE owner_id = $1 AND status != $2`
	var usedMB int64
	err := r.db.QueryRow(ctx, query, ownerID, model.ImageStatusDeleting).Scan(&usedMB)
	return usedMB, err
}

func (r *ImageRepository) GetReplacementImageSizeMB(ctx context.Context, ownerID uuid.UUID, tag string, replacementImageID uuid.UUID) (int64, error) {
	query := `
		SELECT COALESCE(SUM(size_mb), 0)
		FROM images
		WHERE owner_id = $1
			AND tag = $2
			AND id != $3
			AND status != $4
			AND status != $5
	`
	var sizeMB int64
	err := r.db.QueryRow(ctx, query, ownerID, tag, replacementImageID, model.ImageStatusBuilding, model.ImageStatusDeleting).Scan(&sizeMB)
	return sizeMB, err
}

func (r *ImageRepository) GetTotalUsedDiskSpace(ctx context.Context) (int64, error) {
	query := `SELECT COALESCE(SUM(size_mb), 0) FROM images WHERE status != $1`
	var usedMB int64
	err := r.db.QueryRow(ctx, query, model.ImageStatusDeleting).Scan(&usedMB)
	return usedMB, err
}

func (r *ImageRepository) UpdateBuildAndImageSizeTx(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return wrapOp("image update build and image size", err)
	}
	defer tx.Rollback(ctx)

	var finishedAt *time.Time
	if model.IsBuildTerminalStatus(status) {
		finishedAt = ptr(time.Now())
	}

	res, err := tx.Exec(ctx, `
		UPDATE builds
		SET status = $1, finished_at = $2
		WHERE id = $3
				AND status NOT IN ($4, $5, $6, $7, $8, $9, $10)
	`, status, finishedAt, buildID,
		model.BuildStatusSuccess,
		model.BuildStatusCanceled,
		model.BuildStatusFailed,
		model.BuildStatusFailedTimeout,
		model.BuildStatusFailedQuotaExceeded,
		model.BuildStatusFailedResourceExhausted,
		model.BuildStatusFailedInternal,
	)
	if err != nil {
		return wrapOp("image update build and image size", err)
	}
	affected := res.RowsAffected()
	if affected == 0 {
		return commitTx(ctx, tx, "update build and image size")
	}

	if _, err = tx.Exec(ctx, `
		DELETE FROM images
		WHERE id != $1
			AND owner_id = (SELECT owner_id FROM images WHERE id = $1)
			AND tag = (SELECT tag FROM images WHERE id = $1)
	`, imageID); err != nil {
		return wrapOp("image update build and image size", err)
	}

	if _, err = tx.Exec(ctx, "UPDATE images SET size_mb = $1, status = $2, last_observed_at = NOW(), last_error = NULL WHERE id = $3", sizeMB, model.ImageStatusAvailable, imageID); err != nil {
		return wrapOp("image update build and image size", err)
	}

	return commitTx(ctx, tx, "update build and image size")
}

func (r *ImageRepository) MarkBuildFailedAndDeleteImageTx(ctx context.Context, buildID, imageID uuid.UUID, status string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return wrapOp("image mark build failed and delete image", err)
	}
	defer tx.Rollback(ctx)

	var finishedAt *time.Time
	if model.IsBuildTerminalStatus(status) {
		finishedAt = ptr(time.Now())
	}

	res, err := tx.Exec(ctx, `
		UPDATE builds
		SET status = $1, finished_at = $2
		WHERE id = $3
				AND status NOT IN ($4, $5, $6, $7, $8, $9, $10)
	`, status, finishedAt, buildID,
		model.BuildStatusSuccess,
		model.BuildStatusCanceled,
		model.BuildStatusFailed,
		model.BuildStatusFailedTimeout,
		model.BuildStatusFailedQuotaExceeded,
		model.BuildStatusFailedResourceExhausted,
		model.BuildStatusFailedInternal,
	)
	if err != nil {
		return wrapOp("image mark build failed and delete image", err)
	}
	affected := res.RowsAffected()
	if affected == 0 {
		return commitTx(ctx, tx, "mark build failed and delete image")
	}

	_, err = tx.Exec(ctx, "DELETE FROM images WHERE id = $1", imageID)
	if err != nil {
		return wrapOp("image mark build failed and delete image", err)
	}

	return commitTx(ctx, tx, "mark build failed and delete image")
}

func scanImage(s scanner) (model.Image, error) {
	var img model.Image
	var lastObservedAt *time.Time
	var lastError *string
	if err := s.Scan(&img.ID, &img.OwnerID, &img.Tag, &img.SizeMB, &img.Metadata, &img.Status, &lastObservedAt, &lastError, &img.CreatedAt); err != nil {
		return model.Image{}, err
	}
	img.LastObservedAt = lastObservedAt
	img.LastError = lastError
	return img, nil
}
