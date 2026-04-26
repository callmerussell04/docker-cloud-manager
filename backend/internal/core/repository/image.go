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

type ImageRepository struct {
	db *sql.DB
}

func NewImageRepository(db *sql.DB) *ImageRepository {
	return &ImageRepository{db: db}
}

func (r *ImageRepository) Save(ctx context.Context, img model.Image) error {
	query := `
		INSERT INTO images (id, owner_id, tag, size_mb, is_custom, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.db.ExecContext(ctx, query, img.ID, img.OwnerID, img.Tag, img.SizeMB, img.IsCustom, img.Metadata)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (r *ImageRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Image, error) {
	query := `
		SELECT id, owner_id, tag, size_mb, is_custom, metadata, created_at 
		FROM images WHERE id = $1
	`

	var img model.Image
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&img.ID, &img.OwnerID, &img.Tag, &img.SizeMB, &img.IsCustom, &img.Metadata, &img.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Image{}, apperrors.ErrNotFound
		}
		return model.Image{}, err
	}

	return img, nil
}

func (r *ImageRepository) GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]model.Image, error) {
	query := `
		SELECT id, owner_id, tag, size_mb, is_custom, metadata, created_at 
		FROM images WHERE owner_id = $1 OR is_custom = false
	`
	rows, err := r.db.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var images []model.Image
	for rows.Next() {
		var img model.Image
		if err := rows.Scan(&img.ID, &img.OwnerID, &img.Tag, &img.SizeMB, &img.IsCustom, &img.Metadata, &img.CreatedAt); err != nil {
			return nil, err
		}
		images = append(images, img)
	}
	return images, rows.Err()
}

func (r *ImageRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM images WHERE id = $1 AND is_custom = true`
	res, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return apperrors.New(apperrors.ErrResourceInUse, "image is currently used by a container")
		}
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

func (r *ImageRepository) UpdateSize(ctx context.Context, id uuid.UUID, sizeMB int) error {
	query := `UPDATE images SET size_mb = $1 WHERE id = $2`
	res, err := r.db.ExecContext(ctx, query, sizeMB, id)
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

func (r *ImageRepository) GetUserUsedDiskSpace(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	query := `SELECT COALESCE(SUM(size_mb), 0) FROM images WHERE owner_id = $1`
	var usedMB int64
	err := r.db.QueryRowContext(ctx, query, ownerID).Scan(&usedMB)
	return usedMB, err
}

func (r *ImageRepository) UpdateBuildAndImageSizeTx(ctx context.Context, buildID, imageID uuid.UUID, status string, sizeMB int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var finishedAt sql.NullTime
	if model.IsBuildTerminalStatus(status) {
		finishedAt.Time = time.Now()
		finishedAt.Valid = true
	}

	_, err = tx.ExecContext(ctx, "UPDATE builds SET status = $1, finished_at = $2 WHERE id = $3", status, finishedAt, buildID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, "UPDATE images SET size_mb = $1 WHERE id = $2", sizeMB, imageID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *ImageRepository) MarkBuildFailedAndDeleteImageTx(ctx context.Context, buildID, imageID uuid.UUID, status string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var finishedAt sql.NullTime
	if model.IsBuildTerminalStatus(status) {
		finishedAt.Time = time.Now()
		finishedAt.Valid = true
	}

	_, err = tx.ExecContext(ctx, "UPDATE builds SET status = $1, finished_at = $2 WHERE id = $3", status, finishedAt, buildID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, "DELETE FROM images WHERE id = $1 AND is_custom = true", imageID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *ImageRepository) GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Image, int, error) {
	var total int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM images`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	query := `
		SELECT i.id, i.owner_id, i.tag, i.size_mb, i.is_custom, i.metadata, i.created_at
		FROM images i
		ORDER BY i.created_at DESC LIMIT $1 OFFSET $2
	`
	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var images []model.Image
	for rows.Next() {
		var img model.Image
		if err := rows.Scan(&img.ID, &img.OwnerID, &img.Tag, &img.SizeMB, &img.IsCustom, &img.Metadata, &img.CreatedAt); err != nil {
			return nil, 0, err
		}
		images = append(images, img)
	}
	return images, total, rows.Err()
}
