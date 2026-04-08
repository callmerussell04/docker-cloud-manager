package repository

import (
	"context"
	"database/sql"
	"errors"
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

func (r *BuildRepository) GetUserBuilds(ctx context.Context, ownerID uuid.UUID) ([]domain.Build, error) {
	query := `
		SELECT b.id, b.image_id, b.status, b.log_file_path, b.started_at, b.finished_at 
		FROM builds b
		JOIN images i ON b.image_id = i.id
		WHERE i.owner_id = $1 
		ORDER BY b.started_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, ownerID)
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

func (r *BuildRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.Build, error) {
	query := `
		SELECT id, image_id, status, log_file_path, started_at, finished_at 
		FROM builds 
		WHERE id = $1
	`
	var b domain.Build
	var finishedAt sql.NullTime

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&b.ID, &b.ImageID, &b.Status, &b.LogFilePath, &b.StartedAt, &finishedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Build{}, apperrors.ErrNotFound
		}
		return domain.Build{}, err
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

func (r *BuildRepository) GetStaleBuilds(ctx context.Context, threshold time.Time) ([]domain.Build, error) {
	query := `
		SELECT id, image_id, status, log_file_path, started_at 
		FROM builds 
		WHERE status IN ($1, $2) AND started_at < $3
	`
	rows, err := r.db.QueryContext(ctx, query, domain.BuildStatusPending, domain.BuildStatusRunning, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var builds []domain.Build
	for rows.Next() {
		var b domain.Build
		if err := rows.Scan(&b.ID, &b.ImageID, &b.Status, &b.LogFilePath, &b.StartedAt); err != nil {
			return nil, err
		}
		builds = append(builds, b)
	}
	return builds, rows.Err()
}

func (r *BuildRepository) GetAllPaginated(ctx context.Context, limit, offset int) ([]domain.Build, int, error) {
	var total int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM builds`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, image_id, status, log_file_path, started_at, finished_at 
		FROM builds 
		ORDER BY started_at DESC LIMIT $1 OFFSET $2
	`
	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var builds []domain.Build
	for rows.Next() {
		var b domain.Build
		var finishedAt sql.NullTime
		if err := rows.Scan(&b.ID, &b.ImageID, &b.Status, &b.LogFilePath, &b.StartedAt, &finishedAt); err != nil {
			return nil, 0, err
		}
		if finishedAt.Valid {
			b.FinishedAt = &finishedAt.Time
		}
		builds = append(builds, b)
	}
	return builds, total, rows.Err()
}
