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
		INSERT INTO containers (id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix, status, ttl_deadline, env_vars, base_memory_reservation)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	var projectID sql.NullString
	if c.ProjectID != nil {
		projectID.String = c.ProjectID.String()
		projectID.Valid = true
	}

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
		c.ID, c.OwnerID, projectID, dockerID, c.Name, c.ImageTag, c.InternalPort, c.DomainPrefix, c.Status, ttl, c.EnvVars, c.BaseMemoryReservation,
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

func (r *ContainerRepository) UpdateRouting(ctx context.Context, id uuid.UUID, domainPrefix string, internalPort int) error {
	query := `UPDATE containers SET domain_prefix = $1, internal_port = $2 WHERE id = $3`
	res, err := r.db.ExecContext(ctx, query, domainPrefix, internalPort, id)
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

func (r *ContainerRepository) UpdateStatusByDockerID(ctx context.Context, dockerID string, status string) error {
	query := `UPDATE containers SET status = $1 WHERE docker_id = $2`
	res, err := r.db.ExecContext(ctx, query, status, dockerID)
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

func (r *ContainerRepository) UpdateDockerID(ctx context.Context, id uuid.UUID, dockerID string) error {
	query := `UPDATE containers SET docker_id = $1 WHERE id = $2`
	res, err := r.db.ExecContext(ctx, query, dockerID, id)
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

func (r *ContainerRepository) UpdateDockerIDAndStatus(ctx context.Context, id uuid.UUID, dockerID string, status string) error {
	query := `UPDATE containers SET docker_id = $1, status = $2 WHERE id = $3`
	res, err := r.db.ExecContext(ctx, query, dockerID, status, id)
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

func (r *ContainerRepository) GetUserReservedMemory(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	query := `
		SELECT COALESCE(SUM(base_memory_reservation), 0) 
		FROM containers 
		WHERE owner_id = $1 AND status = $2
	`
	var totalReserved int64
	err := r.db.QueryRowContext(ctx, query, ownerID, domain.ContainerStatusRunning).Scan(&totalReserved)
	return totalReserved, err
}

func (r *ContainerRepository) GetUserRAMQuota(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	query := `SELECT quota_ram_mb FROM users WHERE id = $1`
	var quotaMB int64
	err := r.db.QueryRowContext(ctx, query, ownerID).Scan(&quotaMB)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, apperrors.ErrNotFound
		}
		return 0, err
	}
	return quotaMB * 1024 * 1024, nil
}

func (r *ContainerRepository) GetRunning(ctx context.Context) ([]domain.Container, error) {
	query := `
		SELECT id, docker_id, base_memory_reservation
		FROM containers 
		WHERE status = $1
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
		if err := rows.Scan(&c.ID, &dockerID, &c.BaseMemoryReservation); err != nil {
			return nil, err
		}
		if dockerID.Valid {
			c.DockerID = dockerID.String
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

func (r *ContainerRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.Container, error) {
	query := `
		SELECT id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix, status, base_memory_reservation
		FROM containers WHERE id = $1
	`
	var c domain.Container
	var projectID sql.NullString
	var dockerID sql.NullString

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&c.ID, &c.OwnerID, &projectID, &dockerID, &c.Name, &c.ImageTag, &c.InternalPort, &c.DomainPrefix, &c.Status, &c.BaseMemoryReservation,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Container{}, apperrors.ErrNotFound
		}
		return domain.Container{}, err
	}

	if projectID.Valid {
		parsed, _ := uuid.Parse(projectID.String)
		c.ProjectID = &parsed
	}
	if dockerID.Valid {
		c.DockerID = dockerID.String
	}
	return c, nil
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

func (r *ContainerRepository) GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Container, error) {
	query := `
		SELECT id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix, status, base_memory_reservation
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
		var projectID sql.NullString
		var dockerID sql.NullString
		if err := rows.Scan(&c.ID, &c.OwnerID, &projectID, &dockerID, &c.Name, &c.ImageTag, &c.InternalPort, &c.DomainPrefix, &c.Status, &c.BaseMemoryReservation); err != nil {
			return nil, err
		}
		if projectID.Valid {
			parsed, _ := uuid.Parse(projectID.String)
			c.ProjectID = &parsed
		}
		if dockerID.Valid {
			c.DockerID = dockerID.String
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

func (r *ContainerRepository) GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]domain.Container, error) {
	query := `
		SELECT id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix, status, base_memory_reservation
		FROM containers 
		WHERE project_id = $1
	`
	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []domain.Container
	for rows.Next() {
		var c domain.Container
		var pID sql.NullString
		var dID sql.NullString

		if err := rows.Scan(&c.ID, &c.OwnerID, &pID, &dID, &c.Name, &c.ImageTag, &c.InternalPort, &c.DomainPrefix, &c.Status, &c.BaseMemoryReservation); err != nil {
			return nil, err
		}
		if pID.Valid {
			parsed, _ := uuid.Parse(pID.String)
			c.ProjectID = &parsed
		}
		if dID.Valid {
			c.DockerID = dID.String
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
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

func (r *ContainerRepository) GetTotalSystemReservedMemory(ctx context.Context) (int64, error) {
	query := `
		SELECT COALESCE(SUM(base_memory_reservation), 0) 
		FROM containers 
		WHERE status = $1
	`
	var totalReserved int64
	err := r.db.QueryRowContext(ctx, query, domain.ContainerStatusRunning).Scan(&totalReserved)
	return totalReserved, err
}

func (r *ContainerRepository) IsImageInUse(ctx context.Context, ownerID uuid.UUID, imageTag string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM containers 
			WHERE owner_id = $1 AND image_tag = $2
		)
	`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, ownerID, imageTag).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *ContainerRepository) GetNonExited(ctx context.Context) ([]domain.Container, error) {
	query := `
		SELECT id, docker_id, status
		FROM containers
		WHERE status IN ($1, $2, $3)
	`
	rows, err := r.db.QueryContext(ctx, query, domain.ContainerStatusCreating, domain.ContainerStatusCreated, domain.ContainerStatusRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []domain.Container
	for rows.Next() {
		var c domain.Container
		var dockerID sql.NullString
		if err := rows.Scan(&c.ID, &dockerID, &c.Status); err != nil {
			return nil, err
		}
		if dockerID.Valid {
			c.DockerID = dockerID.String
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

func (r *ContainerRepository) CheckDomainPrefixExists(ctx context.Context, prefix string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM containers WHERE domain_prefix = $1 AND domain_prefix != '')`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, prefix).Scan(&exists)
	return exists, err
}
