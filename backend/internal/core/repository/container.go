package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type ContainerRepository struct {
	db *sql.DB
}

func NewContainerRepository(db *sql.DB) *ContainerRepository {
	return &ContainerRepository{db: db}
}

func (r *ContainerRepository) Save(ctx context.Context, c domain.Container) error {
	query := `
		INSERT INTO containers (id, owner_id, docker_id, name, image_tag, internal_port, status, ttl_deadline, env_vars, resources_config)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	var ttl sql.NullTime
	if c.TTLDeadline != nil {
		ttl.Time = *c.TTLDeadline
		ttl.Valid = true
	}

	var dockerID sql.NullString
	if c.DockerID != "" {
		dockerID.String = c.DockerID
		dockerID.Valid = true
	}

	_, err := r.db.ExecContext(ctx, query,
		c.ID, c.OwnerID, dockerID, c.Name, c.ImageTag, c.InternalPort, c.Status, ttl, c.EnvVars, c.ResourcesConfig,
	)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (r *ContainerRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.Container, error) {
	query := `
		SELECT id, owner_id, docker_id, name, image_tag, internal_port, status, ttl_deadline, env_vars, resources_config, created_at
		FROM containers WHERE id = $1
	`
	var c domain.Container
	var ttl sql.NullTime
	var dockerID sql.NullString

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&c.ID, &c.OwnerID, &dockerID, &c.Name, &c.ImageTag, &c.InternalPort, &c.Status, &ttl, &c.EnvVars, &c.ResourcesConfig, &c.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Container{}, apperrors.ErrNotFound
		}
		return domain.Container{}, err
	}

	if ttl.Valid {
		c.TTLDeadline = &ttl.Time
	}
	if dockerID.Valid {
		c.DockerID = dockerID.String
	}
	return c, nil
}

func (r *ContainerRepository) GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Container, error) {
	query := `
		SELECT id, owner_id, docker_id, name, image_tag, internal_port, status, ttl_deadline, env_vars, resources_config, created_at
		FROM containers WHERE owner_id = $1
	`
	rows, err := r.db.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []domain.Container
	for rows.Next() {
		var c domain.Container
		var ttl sql.NullTime
		var dockerID sql.NullString
		if err := rows.Scan(&c.ID, &c.OwnerID, &dockerID, &c.Name, &c.ImageTag, &c.InternalPort, &c.Status, &ttl, &c.EnvVars, &c.ResourcesConfig, &c.CreatedAt); err != nil {
			return nil, err
		}
		if ttl.Valid {
			c.TTLDeadline = &ttl.Time
		}
		if dockerID.Valid {
			c.DockerID = dockerID.String
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

func (r *ContainerRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	query := `UPDATE containers SET status = $1 WHERE id = $2`
	res, err := r.db.ExecContext(ctx, query, status, id)
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

func (r *ContainerRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM containers WHERE id = $1`
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

func (r *ContainerRepository) GetExpired(ctx context.Context) ([]domain.Container, error) {
	query := `
		SELECT id, docker_id FROM containers 
		WHERE status = $1 AND ttl_deadline < NOW()
	`
	rows, err := r.db.QueryContext(ctx, query, domain.ContainerStatusRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []domain.Container
	for rows.Next() {
		var c domain.Container
		var dockerID sql.NullString
		if err := rows.Scan(&c.ID, &dockerID); err != nil {
			return nil, err
		}
		if dockerID.Valid {
			c.DockerID = dockerID.String
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

func (r *ContainerRepository) CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM containers WHERE owner_id = $1`
	var count int
	err := r.db.QueryRowContext(ctx, query, ownerID).Scan(&count)
	return count, err
}

func (r *ContainerRepository) CountRunning(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM containers WHERE status = $1`
	var count int
	err := r.db.QueryRowContext(ctx, query, domain.ContainerStatusRunning).Scan(&count)
	return count, err
}

func (r *ContainerRepository) GetRunning(ctx context.Context) ([]domain.Container, error) {
	query := `
		SELECT id, owner_id, docker_id, name, image_tag, internal_port, status, ttl_deadline, env_vars, resources_config, created_at
		FROM containers WHERE status = $1
	`
	rows, err := r.db.QueryContext(ctx, query, domain.ContainerStatusRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []domain.Container
	for rows.Next() {
		var c domain.Container
		var ttl sql.NullTime
		var dockerID sql.NullString

		if err := rows.Scan(&c.ID, &c.OwnerID, &dockerID, &c.Name, &c.ImageTag, &c.InternalPort, &c.Status, &ttl, &c.EnvVars, &c.ResourcesConfig, &c.CreatedAt); err != nil {
			return nil, err
		}
		if ttl.Valid {
			c.TTLDeadline = &ttl.Time
		}
		if dockerID.Valid {
			c.DockerID = dockerID.String
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

func (r *ContainerRepository) UpdateResourcesConfig(ctx context.Context, id uuid.UUID, configJSON []byte) error {
	query := `UPDATE containers SET resources_config = $1 WHERE id = $2`
	res, err := r.db.ExecContext(ctx, query, configJSON, id)
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
