package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
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

func (r *ContainerRepository) Save(ctx context.Context, c model.Container) error {
	query := `
		INSERT INTO containers (
			id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix,
			status, desired_status, ttl_deadline, env_vars, base_memory_reservation,
			docker_generation, network_alias, command, entrypoint, restart_policy, healthcheck
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
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
	desiredStatus := c.DesiredStatus
	if desiredStatus == "" {
		desiredStatus = c.Status
	}
	if desiredStatus == "" {
		desiredStatus = model.ContainerStatusCreated
	}
	generation := c.DockerGeneration
	if generation <= 0 {
		generation = 1
	}
	command, err := json.Marshal(c.Command)
	if err != nil {
		return err
	}
	entrypoint, err := json.Marshal(c.Entrypoint)
	if err != nil {
		return err
	}
	healthcheck, err := json.Marshal(c.Healthcheck)
	if err != nil {
		return err
	}

	_, err = r.db.ExecContext(ctx, query,
		c.ID, c.OwnerID, projectID, dockerID, c.Name, c.ImageTag, c.InternalPort, c.DomainPrefix,
		c.Status, desiredStatus, ttl, c.EnvVars, c.BaseMemoryReservation,
		generation, c.NetworkAlias, command, entrypoint, c.Restart, healthcheck,
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

func (r *ContainerRepository) UpdateDockerIDRoutingAndGeneration(ctx context.Context, id uuid.UUID, dockerID string, domainPrefix string, internalPort int, generation int) error {
	query := `
		UPDATE containers
		SET docker_id = $1, domain_prefix = $2, internal_port = $3, docker_generation = $4,
			last_observed_at = NOW(), last_error = NULL
		WHERE id = $5
	`
	res, err := r.db.ExecContext(ctx, query, dockerID, domainPrefix, internalPort, generation, id)
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
	query := `UPDATE containers SET status = $1, last_observed_at = NOW(), last_error = NULL WHERE id = $2`
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
	query := `UPDATE containers SET status = $1, last_observed_at = NOW(), last_error = NULL WHERE docker_id = $2`
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
	query := `UPDATE containers SET docker_id = $1, last_observed_at = NOW(), last_error = NULL WHERE id = $2`
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
	query := `UPDATE containers SET docker_id = $1, status = $2, desired_status = $2, last_observed_at = NOW(), last_error = NULL WHERE id = $3`
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
		WHERE owner_id = $1 AND status != $3 AND (status = $2 OR desired_status = $2)
	`
	var totalReserved int64
	err := r.db.QueryRowContext(ctx, query, ownerID, model.ContainerStatusRunning, model.ContainerStatusMissing).Scan(&totalReserved)
	return totalReserved, err
}

func (r *ContainerRepository) GetRunning(ctx context.Context) ([]model.Container, error) {
	query := `
		SELECT id, docker_id, base_memory_reservation
		FROM containers 
		WHERE status = $1
	`
	rows, err := r.db.QueryContext(ctx, query, model.ContainerStatusRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []model.Container
	for rows.Next() {
		var c model.Container
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

func (r *ContainerRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Container, error) {
	query := `
		SELECT id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix,
			status, desired_status, base_memory_reservation, last_observed_at, last_error,
			docker_generation, network_alias, command, entrypoint, restart_policy, healthcheck
		FROM containers WHERE id = $1
	`
	var c model.Container
	var projectID sql.NullString
	var dockerID sql.NullString
	var lastObservedAt sql.NullTime
	var lastError sql.NullString
	var command, entrypoint, healthcheck []byte

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&c.ID, &c.OwnerID, &projectID, &dockerID, &c.Name, &c.ImageTag, &c.InternalPort, &c.DomainPrefix,
		&c.Status, &c.DesiredStatus, &c.BaseMemoryReservation, &lastObservedAt, &lastError,
		&c.DockerGeneration, &c.NetworkAlias, &command, &entrypoint, &c.Restart, &healthcheck,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Container{}, apperrors.ErrNotFound
		}
		return model.Container{}, err
	}

	if projectID.Valid {
		parsed, _ := uuid.Parse(projectID.String)
		c.ProjectID = &parsed
	}
	if dockerID.Valid {
		c.DockerID = dockerID.String
	}
	if lastObservedAt.Valid {
		c.LastObservedAt = &lastObservedAt.Time
	}
	if lastError.Valid {
		c.LastError = &lastError.String
	}
	_ = json.Unmarshal(command, &c.Command)
	_ = json.Unmarshal(entrypoint, &c.Entrypoint)
	if len(healthcheck) > 0 && string(healthcheck) != "null" {
		_ = json.Unmarshal(healthcheck, &c.Healthcheck)
	}
	return c, nil
}

func (r *ContainerRepository) GetByDockerID(ctx context.Context, dockerID string) (model.Container, error) {
	query := `
		SELECT id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix,
			status, desired_status, base_memory_reservation, last_observed_at, last_error,
			docker_generation, network_alias, command, entrypoint, restart_policy, healthcheck
		FROM containers WHERE docker_id = $1
	`
	var c model.Container
	var projectID sql.NullString
	var dbDockerID sql.NullString
	var lastObservedAt sql.NullTime
	var lastError sql.NullString
	var command, entrypoint, healthcheck []byte

	err := r.db.QueryRowContext(ctx, query, dockerID).Scan(
		&c.ID, &c.OwnerID, &projectID, &dbDockerID, &c.Name, &c.ImageTag, &c.InternalPort, &c.DomainPrefix,
		&c.Status, &c.DesiredStatus, &c.BaseMemoryReservation, &lastObservedAt, &lastError,
		&c.DockerGeneration, &c.NetworkAlias, &command, &entrypoint, &c.Restart, &healthcheck,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Container{}, apperrors.ErrNotFound
		}
		return model.Container{}, err
	}

	if projectID.Valid {
		parsed, _ := uuid.Parse(projectID.String)
		c.ProjectID = &parsed
	}
	if dbDockerID.Valid {
		c.DockerID = dbDockerID.String
	}
	if lastObservedAt.Valid {
		c.LastObservedAt = &lastObservedAt.Time
	}
	if lastError.Valid {
		c.LastError = &lastError.String
	}
	_ = json.Unmarshal(command, &c.Command)
	_ = json.Unmarshal(entrypoint, &c.Entrypoint)
	if len(healthcheck) > 0 && string(healthcheck) != "null" {
		_ = json.Unmarshal(healthcheck, &c.Healthcheck)
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

func (r *ContainerRepository) GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]model.Container, error) {
	query := `
		SELECT id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix,
			status, desired_status, base_memory_reservation, last_observed_at, last_error, docker_generation
		FROM containers WHERE owner_id = $1
	`
	rows, err := r.db.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []model.Container
	for rows.Next() {
		var c model.Container
		var projectID sql.NullString
		var dockerID sql.NullString
		var lastObservedAt sql.NullTime
		var lastError sql.NullString
		if err := rows.Scan(
			&c.ID, &c.OwnerID, &projectID, &dockerID, &c.Name, &c.ImageTag, &c.InternalPort, &c.DomainPrefix,
			&c.Status, &c.DesiredStatus, &c.BaseMemoryReservation, &lastObservedAt, &lastError, &c.DockerGeneration,
		); err != nil {
			return nil, err
		}
		if projectID.Valid {
			parsed, _ := uuid.Parse(projectID.String)
			c.ProjectID = &parsed
		}
		if dockerID.Valid {
			c.DockerID = dockerID.String
		}
		if lastObservedAt.Valid {
			c.LastObservedAt = &lastObservedAt.Time
		}
		if lastError.Valid {
			c.LastError = &lastError.String
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

func (r *ContainerRepository) GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Container, error) {
	query := `
		SELECT id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix,
			status, desired_status, base_memory_reservation, last_observed_at, last_error, docker_generation
		FROM containers 
		WHERE project_id = $1
	`
	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []model.Container
	for rows.Next() {
		var c model.Container
		var pID sql.NullString
		var dID sql.NullString
		var lastObservedAt sql.NullTime
		var lastError sql.NullString

		if err := rows.Scan(
			&c.ID, &c.OwnerID, &pID, &dID, &c.Name, &c.ImageTag, &c.InternalPort, &c.DomainPrefix,
			&c.Status, &c.DesiredStatus, &c.BaseMemoryReservation, &lastObservedAt, &lastError, &c.DockerGeneration,
		); err != nil {
			return nil, err
		}
		if pID.Valid {
			parsed, _ := uuid.Parse(pID.String)
			c.ProjectID = &parsed
		}
		if dID.Valid {
			c.DockerID = dID.String
		}
		if lastObservedAt.Valid {
			c.LastObservedAt = &lastObservedAt.Time
		}
		if lastError.Valid {
			c.LastError = &lastError.String
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

func (r *ContainerRepository) GetExpired(ctx context.Context) ([]model.Container, error) {
	query := `
		SELECT id, docker_id FROM containers 
		WHERE status = $1 AND ttl_deadline < NOW()
	`
	rows, err := r.db.QueryContext(ctx, query, model.ContainerStatusRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []model.Container
	for rows.Next() {
		var c model.Container
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
		WHERE status != $2 AND (status = $1 OR desired_status = $1)
	`
	var totalReserved int64
	err := r.db.QueryRowContext(ctx, query, model.ContainerStatusRunning, model.ContainerStatusMissing).Scan(&totalReserved)
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

func (r *ContainerRepository) GetNonExited(ctx context.Context) ([]model.Container, error) {
	query := `
		SELECT id, project_id, docker_id, status, desired_status
		FROM containers
		WHERE status != $6 AND (
			status IN ($1, $2, $3, $4, $5)
			OR desired_status IN ($2, $3, $4)
		)
	`
	rows, err := r.db.QueryContext(ctx, query,
		model.ContainerStatusCreating,
		model.ContainerStatusCreated,
		model.ContainerStatusRunning,
		model.ContainerStatusDeleting,
		model.ContainerStatusReconciling,
		model.ContainerStatusMissing,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []model.Container
	for rows.Next() {
		var c model.Container
		var projectID sql.NullString
		var dockerID sql.NullString
		if err := rows.Scan(&c.ID, &projectID, &dockerID, &c.Status, &c.DesiredStatus); err != nil {
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

func (r *ContainerRepository) CheckDomainPrefixExists(ctx context.Context, prefix string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM containers WHERE domain_prefix = $1 AND domain_prefix != '')`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, prefix).Scan(&exists)
	return exists, err
}

func (r *ContainerRepository) GetAllPaginated(ctx context.Context, limit, offset int) ([]model.Container, int, error) {
	var total int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM containers`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	query := `
		SELECT c.id, c.owner_id, c.project_id, c.docker_id, c.name, c.image_tag, c.internal_port,
			c.domain_prefix, c.status, c.desired_status, c.base_memory_reservation, c.last_observed_at,
			c.last_error, c.docker_generation, c.created_at
		FROM containers c
		ORDER BY c.created_at DESC LIMIT $1 OFFSET $2
	`
	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var containers []model.Container
	for rows.Next() {
		var c model.Container
		var projectID sql.NullString
		var dockerID sql.NullString
		var lastObservedAt sql.NullTime
		var lastError sql.NullString
		if err := rows.Scan(
			&c.ID, &c.OwnerID, &projectID, &dockerID, &c.Name, &c.ImageTag, &c.InternalPort,
			&c.DomainPrefix, &c.Status, &c.DesiredStatus, &c.BaseMemoryReservation, &lastObservedAt,
			&lastError, &c.DockerGeneration, &c.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		if projectID.Valid {
			parsed, _ := uuid.Parse(projectID.String)
			c.ProjectID = &parsed
		}
		if dockerID.Valid {
			c.DockerID = dockerID.String
		}
		if lastObservedAt.Valid {
			c.LastObservedAt = &lastObservedAt.Time
		}
		if lastError.Valid {
			c.LastError = &lastError.String
		}
		containers = append(containers, c)
	}
	return containers, total, rows.Err()
}

func (r *ContainerRepository) SaveWithMountsAndOperation(ctx context.Context, c model.Container, mounts []model.VolumeMount, op model.ResourceOperation, lockOwner, lockCapacity bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if lockOwner {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "dcm:owner:"+c.OwnerID.String()); err != nil {
			return err
		}
	}
	if lockCapacity {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "dcm:capacity"); err != nil {
			return err
		}
	}

	if err := insertContainerTx(ctx, tx, c); err != nil {
		return err
	}

	if len(mounts) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO volume_mounts (container_id, volume_id, mount_path, is_readonly)
			VALUES ($1, $2, $3, $4)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, m := range mounts {
			if _, err = stmt.ExecContext(ctx, m.ContainerID, m.VolumeID, m.MountPath, m.IsReadOnly); err != nil {
				return err
			}
		}
	}

	if op.ResourceID != uuid.Nil {
		if err := insertResourceOperationTx(ctx, tx, op); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *ContainerRepository) SetDesiredStatus(ctx context.Context, id uuid.UUID, desiredStatus string) error {
	query := `UPDATE containers SET desired_status = $1, last_error = NULL WHERE id = $2`
	res, err := r.db.ExecContext(ctx, query, desiredStatus, id)
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

func (r *ContainerRepository) MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error {
	var msg sql.NullString
	if cause != nil {
		msg.String = cause.Error()
		msg.Valid = true
	}
	query := `UPDATE containers SET status = $1, last_error = $2, last_observed_at = NOW() WHERE id = $3`
	res, err := r.db.ExecContext(ctx, query, status, msg, id)
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

func (r *ContainerRepository) CreateOperation(ctx context.Context, op model.ResourceOperation) error {
	if op.ID == uuid.Nil {
		op.ID = uuid.New()
	}
	if op.Status == "" {
		op.Status = model.OperationStatusPending
	}
	query := `
		INSERT INTO resource_operations (id, resource_type, resource_id, owner_id, operation, status, attempts, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	var lastError sql.NullString
	if op.LastError != nil {
		lastError.String = *op.LastError
		lastError.Valid = true
	}
	_, err := r.db.ExecContext(ctx, query, op.ID, op.ResourceType, op.ResourceID, op.OwnerID, op.Operation, op.Status, op.Attempts, lastError)
	return err
}

func (r *ContainerRepository) CompleteLatestOperation(ctx context.Context, resourceType string, resourceID uuid.UUID, status string, cause error) error {
	var lastError sql.NullString
	if cause != nil {
		lastError.String = cause.Error()
		lastError.Valid = true
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE resource_operations
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE id = (
			SELECT id FROM resource_operations
			WHERE resource_type = $3 AND resource_id = $4 AND status IN ($5, $6)
			ORDER BY created_at DESC
			LIMIT 1
		)
	`, status, lastError, resourceType, resourceID, model.OperationStatusPending, model.OperationStatusRunning)
	return err
}

func (r *ContainerRepository) AcquireOwnerCapacityLock(ctx context.Context, ownerID uuid.UUID) (func(), error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	unlock := func() {
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtext($1))`, "dcm:capacity")
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtext($1))`, "dcm:owner:"+ownerID.String())
		_ = conn.Close()
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext($1))`, "dcm:owner:"+ownerID.String()); err != nil {
		unlock()
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext($1))`, "dcm:capacity"); err != nil {
		unlock()
		return nil, err
	}
	return unlock, nil
}

func insertContainerTx(ctx context.Context, tx *sql.Tx, c model.Container) error {
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
	desiredStatus := c.DesiredStatus
	if desiredStatus == "" {
		desiredStatus = c.Status
	}
	if desiredStatus == "" {
		desiredStatus = model.ContainerStatusCreated
	}
	generation := c.DockerGeneration
	if generation <= 0 {
		generation = 1
	}
	command, err := json.Marshal(c.Command)
	if err != nil {
		return err
	}
	entrypoint, err := json.Marshal(c.Entrypoint)
	if err != nil {
		return err
	}
	healthcheck, err := json.Marshal(c.Healthcheck)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO containers (
			id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix,
			status, desired_status, ttl_deadline, env_vars, base_memory_reservation,
			docker_generation, network_alias, command, entrypoint, restart_policy, healthcheck
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
	`, c.ID, c.OwnerID, projectID, dockerID, c.Name, c.ImageTag, c.InternalPort, c.DomainPrefix,
		c.Status, desiredStatus, ttl, c.EnvVars, c.BaseMemoryReservation,
		generation, c.NetworkAlias, command, entrypoint, c.Restart, healthcheck)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func insertResourceOperationTx(ctx context.Context, tx *sql.Tx, op model.ResourceOperation) error {
	if op.ID == uuid.Nil {
		op.ID = uuid.New()
	}
	if op.Status == "" {
		op.Status = model.OperationStatusPending
	}
	var lastError sql.NullString
	if op.LastError != nil {
		lastError.String = *op.LastError
		lastError.Valid = true
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO resource_operations (id, resource_type, resource_id, owner_id, operation, status, attempts, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, op.ID, op.ResourceType, op.ResourceID, op.OwnerID, op.Operation, op.Status, op.Attempts, lastError)
	return err
}
