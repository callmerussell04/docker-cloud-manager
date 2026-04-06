package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

type ProjectRepository struct {
	db *sql.DB
}

func NewProjectRepository(db *sql.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

func (r *ProjectRepository) Save(ctx context.Context, p domain.Project) error {
	query := `
		INSERT INTO projects (id, owner_id, name, status, error_message) 
		VALUES ($1, $2, $3, $4, $5)
	`

	var errMsg sql.NullString
	if p.ErrorMessage != nil {
		errMsg.String = *p.ErrorMessage
		errMsg.Valid = true
	}

	_, err := r.db.ExecContext(ctx, query, p.ID, p.OwnerID, p.Name, p.Status, errMsg)
	return err
}

func (r *ProjectRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.Project, error) {
	query := `
		SELECT id, owner_id, name, status, error_message, created_at 
		FROM projects 
		WHERE id = $1
	`

	var p domain.Project
	var errMsg sql.NullString

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&p.ID,
		&p.OwnerID,
		&p.Name,
		&p.Status,
		&errMsg,
		&p.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Project{}, apperrors.ErrNotFound
		}
		return domain.Project{}, err
	}

	if errMsg.Valid {
		p.ErrorMessage = &errMsg.String
	}

	return p, nil
}

func (r *ProjectRepository) GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Project, error) {
	query := `
		SELECT id, owner_id, name, status, error_message, created_at 
		FROM projects 
		WHERE owner_id = $1 
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []domain.Project
	for rows.Next() {
		var p domain.Project
		var errMsg sql.NullString

		if err := rows.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Status, &errMsg, &p.CreatedAt); err != nil {
			return nil, err
		}

		if errMsg.Valid {
			p.ErrorMessage = &errMsg.String
		}

		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (r *ProjectRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string, errorMsg *string) error {
	query := `
		UPDATE projects 
		SET status = $1, error_message = $2 
		WHERE id = $3
	`

	var errMsg sql.NullString
	if errorMsg != nil {
		errMsg.String = *errorMsg
		errMsg.Valid = true
	}

	res, err := r.db.ExecContext(ctx, query, status, errMsg, id)
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

func (r *ProjectRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM projects WHERE id = $1`

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
