package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

type BuildRepository struct {
	db *sql.DB
}

func NewBuildRepository(db *sql.DB) *BuildRepository {
	return &BuildRepository{db: db}
}

func (r *BuildRepository) Save(ctx context.Context, b domain.Build) error {
	query := `
		INSERT INTO builds (id, image_id, status, log_file_path, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	var finishedAt sql.NullTime
	if b.FinishedAt != nil {
		finishedAt.Time = *b.FinishedAt
		finishedAt.Valid = true
	}

	_, err := r.db.ExecContext(ctx, query, b.ID, b.ImageID, b.Status, b.LogFilePath, b.StartedAt, finishedAt)
	return err
}

func (r *BuildRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	query := `
		UPDATE builds 
		SET status = $1, finished_at = $2 
		WHERE id = $3
	`

	var finishedAt sql.NullTime
	if status == domain.BuildStatusSuccess || status == domain.BuildStatusFailed {
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

func (r *BuildRepository) GetByImageID(ctx context.Context, imageID uuid.UUID) ([]domain.Build, error) {
	query := `
		SELECT id, image_id, status, log_file_path, started_at, finished_at 
		FROM builds 
		WHERE image_id = $1 
		ORDER BY started_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, imageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var builds []domain.Build

	for rows.Next() {
		var b domain.Build
		var finishedAt sql.NullTime

		if err := rows.Scan(&b.ID, &b.ImageID, &b.Status, &b.LogFilePath, &b.StartedAt, &finishedAt); err != nil {
			return nil, err
		}

		if finishedAt.Valid {
			b.FinishedAt = &finishedAt.Time
		}
		builds = append(builds, b)
	}
	return builds, rows.Err()
}
