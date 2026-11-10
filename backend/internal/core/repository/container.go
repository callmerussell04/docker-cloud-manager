package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ContainerRepository struct {
	db     *pgxpool.Pool
	lockDB *pgxpool.Pool
}

const containerDomainPrefixIndex = "idx_containers_domain_prefix"
const containerOwnerNameUniqueIndex = "idx_containers_owner_name_unique"

func NewContainerRepository(db, lockDB *pgxpool.Pool) *ContainerRepository {
	return &ContainerRepository{db: db, lockDB: lockDB}
}

func (r *ContainerRepository) Save(ctx context.Context, c model.Container) error {
	query := `
		INSERT INTO containers (
			id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix,
			status, desired_status, ttl_deadline, env_vars, base_memory_reservation, base_cpu_millicores,
			docker_generation, network_alias, command, entrypoint, restart_policy, healthcheck
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
	`

	var projectID *string
	if c.ProjectID != nil {
		projectID = ptr(c.ProjectID.String())
	}

	ttl := c.TTLDeadline

	var dockerID *string
	if c.DockerID != "" {
		dockerID = ptr(c.DockerID)
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

	_, err = r.db.Exec(ctx, query,
		c.ID, c.OwnerID, projectID, dockerID, c.Name, c.ImageTag, c.InternalPort, c.DomainPrefix,
		c.Status, desiredStatus, ttl, c.EnvVars, c.BaseMemoryReservation, c.BaseCPUReservation,
		generation, c.NetworkAlias, command, entrypoint, c.Restart, healthcheck,
	)
	if err != nil {
		return mapContainerUniqueViolation(err, c.DomainPrefix)
	}
	return nil
}

func (r *ContainerRepository) UpdateRouting(ctx context.Context, id uuid.UUID, domainPrefix string, internalPort int) error {
	query := `UPDATE containers SET domain_prefix = $1, internal_port = $2 WHERE id = $3`
	res, err := r.db.Exec(ctx, query, domainPrefix, internalPort, id)
	if err != nil {
		return mapContainerUniqueViolation(err, domainPrefix)
	}
	rowsAffected := res.RowsAffected()
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
	res, err := r.db.Exec(ctx, query, dockerID, domainPrefix, internalPort, generation, id)
	if err != nil {
		return mapContainerUniqueViolation(err, domainPrefix)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ContainerRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	query := `UPDATE containers SET status = $1, last_observed_at = NOW(), last_error = NULL WHERE id = $2`
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

func (r *ContainerRepository) UpdateStatusAndTTLDeadline(ctx context.Context, id uuid.UUID, status string, ttlDeadline *time.Time) error {
	ttl := ttlDeadline
	query := `UPDATE containers SET status = $1, ttl_deadline = $2, last_observed_at = NOW(), last_error = NULL WHERE id = $3`
	res, err := r.db.Exec(ctx, query, status, ttl, id)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ContainerRepository) UpdateStatusByDockerID(ctx context.Context, dockerID string, status string) error {
	query := `UPDATE containers SET status = $1, last_observed_at = NOW(), last_error = NULL WHERE docker_id = $2`
	res, err := r.db.Exec(ctx, query, status, dockerID)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ContainerRepository) UpdateStatusByContainerIDAndGeneration(ctx context.Context, containerID uuid.UUID, generation int, status string) error {
	query := `UPDATE containers SET status = $1, last_observed_at = NOW(), last_error = NULL WHERE id = $2 AND docker_generation = $3`
	res, err := r.db.Exec(ctx, query, status, containerID, generation)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

// The observed-status writes below replace the last error with the
// observation's cause. An observation without a cause that reports the status
// already recorded keeps the last error, so the Docker events that follow a
// stop do not erase the reason the stop recorded.

func (r *ContainerRepository) UpdateObservedStatus(ctx context.Context, id uuid.UUID, status string, exitCode *int, desiredStatus *string, cause error) error {
	query := `
		UPDATE containers
		SET status = $1::text,
			desired_status = COALESCE($2, desired_status),
			last_exit_code = $3,
			last_observed_at = NOW(),
			last_error = CASE WHEN $4::text IS NULL AND status = $1::text THEN last_error ELSE $4::text END
		WHERE id = $5
	`
	res, err := r.db.Exec(ctx, query, status, desiredStatus, nullableInt(exitCode), nullableError(cause), id)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ContainerRepository) UpdateObservedStatusByDockerID(ctx context.Context, dockerID string, status string, exitCode *int, desiredStatus *string, cause error) error {
	query := `
		UPDATE containers
		SET status = $1::text,
			desired_status = COALESCE($2, desired_status),
			last_exit_code = $3,
			last_observed_at = NOW(),
			last_error = CASE WHEN $4::text IS NULL AND status = $1::text THEN last_error ELSE $4::text END
		WHERE docker_id = $5
	`
	res, err := r.db.Exec(ctx, query, status, desiredStatus, nullableInt(exitCode), nullableError(cause), dockerID)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ContainerRepository) UpdateObservedStatusByContainerIDAndGeneration(ctx context.Context, containerID uuid.UUID, generation int, status string, exitCode *int, desiredStatus *string, cause error) error {
	query := `
		UPDATE containers
		SET status = $1::text,
			desired_status = COALESCE($2, desired_status),
			last_exit_code = $3,
			last_observed_at = NOW(),
			last_error = CASE WHEN $4::text IS NULL AND status = $1::text THEN last_error ELSE $4::text END
		WHERE id = $5 AND docker_generation = $6
	`
	res, err := r.db.Exec(ctx, query, status, desiredStatus, nullableInt(exitCode), nullableError(cause), containerID, generation)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ContainerRepository) UpdateDockerID(ctx context.Context, id uuid.UUID, dockerID string) error {
	query := `UPDATE containers SET docker_id = $1, last_observed_at = NOW(), last_error = NULL WHERE id = $2`
	res, err := r.db.Exec(ctx, query, dockerID, id)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ContainerRepository) UpdateDockerIDAndStatus(ctx context.Context, id uuid.UUID, dockerID string, status string) error {
	query := `UPDATE containers SET docker_id = $1, status = $2, desired_status = $2, last_observed_at = NOW(), last_error = NULL WHERE id = $3`
	res, err := r.db.Exec(ctx, query, dockerID, status, id)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
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
	err := r.db.QueryRow(ctx, query, ownerID, model.ContainerStatusRunning, model.ContainerStatusMissing).Scan(&totalReserved)
	return totalReserved, err
}

func (r *ContainerRepository) GetUserReservedCPU(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	query := `
		SELECT COALESCE(SUM(base_cpu_millicores), 0)
		FROM containers
		WHERE owner_id = $1 AND status != $3 AND (status = $2 OR desired_status = $2)
	`
	var totalReserved int64
	err := r.db.QueryRow(ctx, query, ownerID, model.ContainerStatusRunning, model.ContainerStatusMissing).Scan(&totalReserved)
	return totalReserved, err
}

// GetRunning returns the identity, Docker container and reservations of every
// container whose status is running, ordered by ID.
func (r *ContainerRepository) GetRunning(ctx context.Context) ([]model.Container, error) {
	query := `
		SELECT id, owner_id, docker_id, base_memory_reservation, base_cpu_millicores
		FROM containers
		WHERE status = $1
		ORDER BY id
	`
	rows, err := r.db.Query(ctx, query, model.ContainerStatusRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []model.Container
	for rows.Next() {
		var c model.Container
		var dockerID *string
		if err := rows.Scan(&c.ID, &c.OwnerID, &dockerID, &c.BaseMemoryReservation, &c.BaseCPUReservation); err != nil {
			return nil, err
		}
		if dockerID != nil {
			c.DockerID = *dockerID
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

func (r *ContainerRepository) GetRunningWithWritableVolumeMounts(ctx context.Context, ownerID uuid.UUID) ([]model.Container, error) {
	query := `
		SELECT DISTINCT c.id, c.docker_id
		FROM containers c
		JOIN volume_mounts vm ON vm.container_id = c.id
		WHERE c.owner_id = $1 AND c.status = $2 AND vm.is_readonly = false
	`
	rows, err := r.db.Query(ctx, query, ownerID, model.ContainerStatusRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var containers []model.Container
	for rows.Next() {
		var c model.Container
		var dockerID *string
		if err := rows.Scan(&c.ID, &dockerID); err != nil {
			return nil, err
		}
		if dockerID != nil {
			c.DockerID = *dockerID
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

func (r *ContainerRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Container, error) {
	query := `
		SELECT id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix,
			status, desired_status, ttl_deadline, env_vars, base_memory_reservation, base_cpu_millicores, last_observed_at, last_error,
			last_exit_code, docker_generation, network_alias, command, entrypoint, restart_policy, healthcheck
		FROM containers WHERE id = $1
	`
	c, err := scanContainerFull(r.db.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Container{}, apperrors.ErrNotFound
		}
		return model.Container{}, err
	}
	return c, nil
}

func (r *ContainerRepository) GetByDockerID(ctx context.Context, dockerID string) (model.Container, error) {
	query := `
		SELECT id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix,
			status, desired_status, ttl_deadline, env_vars, base_memory_reservation, base_cpu_millicores, last_observed_at, last_error,
			last_exit_code, docker_generation, network_alias, command, entrypoint, restart_policy, healthcheck
		FROM containers WHERE docker_id = $1
	`
	c, err := scanContainerFull(r.db.QueryRow(ctx, query, dockerID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Container{}, apperrors.ErrNotFound
		}
		return model.Container{}, err
	}
	return c, nil
}

func (r *ContainerRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM containers WHERE id = $1`
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

func (r *ContainerRepository) List(ctx context.Context, opts model.ListOptions) ([]model.Container, int, error) {
	var args []any
	var conditions []string
	if opts.OwnerID != nil {
		args = append(args, *opts.OwnerID)
		conditions = append(conditions, fmt.Sprintf("owner_id = $%d", len(args)))
	}
	if opts.ProjectID != nil {
		args = append(args, *opts.ProjectID)
		conditions = append(conditions, fmt.Sprintf("project_id = $%d", len(args)))
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM containers`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix,
			status, desired_status, ttl_deadline, base_memory_reservation, base_cpu_millicores, last_observed_at, last_error, last_exit_code, docker_generation, created_at
		FROM containers` + where + ` ORDER BY created_at DESC`
	if opts.Limit > 0 {
		args = append(args, opts.Limit, opts.Offset)
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var containers []model.Container
	for rows.Next() {
		c, err := scanContainerListItem(rows)
		if err != nil {
			return nil, 0, err
		}
		containers = append(containers, c)
	}
	return containers, total, rows.Err()
}

func (r *ContainerRepository) GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Container, error) {
	query := `
		SELECT id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix,
			status, desired_status, ttl_deadline, base_memory_reservation, base_cpu_millicores, last_observed_at, last_error, last_exit_code, docker_generation
		FROM containers 
		WHERE project_id = $1
	`
	rows, err := r.db.Query(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []model.Container
	for rows.Next() {
		var c model.Container
		var pID *string
		var dID *string
		var ttl *time.Time
		var lastObservedAt *time.Time
		var lastError *string
		var lastExitCode *int64

		if err := rows.Scan(
			&c.ID, &c.OwnerID, &pID, &dID, &c.Name, &c.ImageTag, &c.InternalPort, &c.DomainPrefix,
			&c.Status, &c.DesiredStatus, &ttl, &c.BaseMemoryReservation, &c.BaseCPUReservation, &lastObservedAt, &lastError, &lastExitCode, &c.DockerGeneration,
		); err != nil {
			return nil, err
		}
		if pID != nil {
			parsed, _ := uuid.Parse(*pID)
			c.ProjectID = &parsed
		}
		if dID != nil {
			c.DockerID = *dID
		}
		c.TTLDeadline = ttl
		c.LastObservedAt = lastObservedAt
		c.LastError = lastError
		if lastExitCode != nil {
			value := int(*lastExitCode)
			c.LastExitCode = &value
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

func (r *ContainerRepository) GetExpired(ctx context.Context) ([]model.Container, error) {
	query := `
		SELECT id, owner_id, docker_id, status, ttl_deadline FROM containers
		WHERE status = $1 AND ttl_deadline IS NOT NULL AND ttl_deadline < NOW()
	`
	rows, err := r.db.Query(ctx, query, model.ContainerStatusRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []model.Container
	for rows.Next() {
		var c model.Container
		var dockerID *string
		var ttl *time.Time
		if err := rows.Scan(&c.ID, &c.OwnerID, &dockerID, &c.Status, &ttl); err != nil {
			return nil, err
		}
		if dockerID != nil {
			c.DockerID = *dockerID
		}
		c.TTLDeadline = ttl
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

func (r *ContainerRepository) CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM containers WHERE owner_id = $1`
	var count int
	err := r.db.QueryRow(ctx, query, ownerID).Scan(&count)
	return count, err
}

func (r *ContainerRepository) GetSystemStatusCounts(ctx context.Context) (model.ContainerStatusCounts, error) {
	query := `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE status = $1),
			COUNT(*) FILTER (WHERE status = $2),
			COUNT(*) FILTER (WHERE status = $3),
			COUNT(*) FILTER (WHERE status = $4)
		FROM containers
	`
	var counts model.ContainerStatusCounts
	err := r.db.QueryRow(
		ctx,
		query,
		model.ContainerStatusRunning,
		model.ContainerStatusExited,
		model.ContainerStatusError,
		model.ContainerStatusMissing,
	).Scan(&counts.Total, &counts.Running, &counts.Stopped, &counts.Error, &counts.Missing)
	return counts, err
}

func (r *ContainerRepository) GetTotalSystemReservedMemory(ctx context.Context) (int64, error) {
	query := `
		SELECT COALESCE(SUM(base_memory_reservation), 0) 
		FROM containers 
		WHERE status != $2 AND (status = $1 OR desired_status = $1)
	`
	var totalReserved int64
	err := r.db.QueryRow(ctx, query, model.ContainerStatusRunning, model.ContainerStatusMissing).Scan(&totalReserved)
	return totalReserved, err
}

func (r *ContainerRepository) GetTotalSystemReservedCPU(ctx context.Context) (int64, error) {
	query := `
		SELECT COALESCE(SUM(base_cpu_millicores), 0)
		FROM containers
		WHERE status != $2 AND (status = $1 OR desired_status = $1)
	`
	var totalReserved int64
	err := r.db.QueryRow(ctx, query, model.ContainerStatusRunning, model.ContainerStatusMissing).Scan(&totalReserved)
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
	err := r.db.QueryRow(ctx, query, ownerID, imageTag).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *ContainerRepository) GetNonExited(ctx context.Context) ([]model.Container, error) {
	query := `
		SELECT id, project_id, docker_id, status, desired_status, last_exit_code
		FROM containers
		WHERE status != $9 AND (
			status IN ($1, $2, $3, $4, $5, $6, $7, $8, $10)
			OR desired_status IN ($2, $4, $6)
		)
	`
	rows, err := r.db.Query(ctx, query,
		model.ContainerStatusCreating,
		model.ContainerStatusCreated,
		model.ContainerStatusStarting,
		model.ContainerStatusRunning,
		model.ContainerStatusStopping,
		model.ContainerStatusDeleting,
		model.ContainerStatusReconciling,
		model.ContainerStatusExposing,
		model.ContainerStatusMissing,
		model.ContainerStatusPending,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []model.Container
	for rows.Next() {
		var c model.Container
		var projectID *string
		var dockerID *string
		var lastExitCode *int64
		if err := rows.Scan(&c.ID, &projectID, &dockerID, &c.Status, &c.DesiredStatus, &lastExitCode); err != nil {
			return nil, err
		}
		if projectID != nil {
			parsed, _ := uuid.Parse(*projectID)
			c.ProjectID = &parsed
		}
		if dockerID != nil {
			c.DockerID = *dockerID
		}
		if lastExitCode != nil {
			value := int(*lastExitCode)
			c.LastExitCode = &value
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

func (r *ContainerRepository) CheckDomainPrefixExists(ctx context.Context, prefix string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM containers WHERE domain_prefix = $1 AND domain_prefix != '')`
	var exists bool
	err := r.db.QueryRow(ctx, query, prefix).Scan(&exists)
	return exists, err
}

func (r *ContainerRepository) SaveWithMountsAndOperation(ctx context.Context, c model.Container, mounts []model.VolumeMount, op model.ResourceOperation, lockOwner, lockCapacity bool) error {
	return r.SaveWithMountsOperationAndOutbox(ctx, c, mounts, op, model.ContainerLifecycleOutbox{}, lockOwner, lockCapacity)
}

func (r *ContainerRepository) SaveWithMountsOperationAndOutbox(ctx context.Context, c model.Container, mounts []model.VolumeMount, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox, lockOwner, lockCapacity bool) error {
	pool := r.db
	if lockOwner || lockCapacity {
		pool = r.lockDB
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return wrapOp("container save with mounts operation and outbox", err)
	}
	defer tx.Rollback(ctx)

	if lockOwner {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "dcm:owner:"+c.OwnerID.String()); err != nil {
			return wrapOp("container save with mounts operation and outbox", err)
		}
	}
	if lockCapacity {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "dcm:capacity"); err != nil {
			return wrapOp("container save with mounts operation and outbox", err)
		}
	}

	if err := insertContainerTx(ctx, tx, c); err != nil {
		return wrapOp("container save with mounts operation and outbox", err)
	}

	if len(mounts) > 0 {
		insertMountSQL := `
			INSERT INTO volume_mounts (container_id, volume_id, mount_path, is_readonly)
			VALUES ($1, $2, $3, $4)
		`

		for _, m := range mounts {
			if _, err = tx.Exec(ctx, insertMountSQL, m.ContainerID, m.VolumeID, m.MountPath, m.IsReadOnly); err != nil {
				return wrapOp("container save with mounts operation and outbox", err)
			}
		}
	}

	if op.ResourceID != uuid.Nil {
		if err := insertResourceOperationTx(ctx, tx, op); err != nil {
			return wrapOp("container save with mounts operation and outbox", err)
		}
	}
	if outbox.OperationID != uuid.Nil {
		if err := insertContainerLifecycleOutboxTx(ctx, tx, outbox); err != nil {
			return wrapOp("container save with mounts operation and outbox", err)
		}
	}

	return commitTx(ctx, tx, "save with mounts operation and outbox")
}

func (r *ContainerRepository) SetDesiredStatus(ctx context.Context, id uuid.UUID, desiredStatus string) error {
	query := `UPDATE containers SET desired_status = $1, last_error = NULL WHERE id = $2`
	res, err := r.db.Exec(ctx, query, desiredStatus, id)
	if err != nil {
		return wrapOp("container set desired status", err)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ContainerRepository) MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error {
	var msg *string
	if cause != nil {
		msg = ptr(cause.Error())
	}
	query := `UPDATE containers SET status = $1, last_error = $2, last_observed_at = NOW() WHERE id = $3`
	res, err := r.db.Exec(ctx, query, status, msg, id)
	if err != nil {
		return wrapOp("container mark status error", err)
	}
	rowsAffected := res.RowsAffected()
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
	lastError := op.LastError
	_, err := r.db.Exec(ctx, query, op.ID, op.ResourceType, op.ResourceID, op.OwnerID, op.Operation, op.Status, op.Attempts, lastError)
	return mapActiveOperationError(err)
}

func (r *ContainerRepository) CompleteLatestOperation(ctx context.Context, resourceType string, resourceID uuid.UUID, status string, cause error) error {
	var lastError *string
	if cause != nil {
		lastError = ptr(cause.Error())
	}
	_, err := r.db.Exec(ctx, `
		UPDATE resource_operations
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE id = (
			SELECT id FROM resource_operations
			WHERE resource_type = $3 AND resource_id = $4 AND status IN ($5, $6)
			ORDER BY created_at DESC
			LIMIT 1
		)
	`, status, lastError, resourceType, resourceID, model.OperationStatusPending, model.OperationStatusRunning)
	return wrapOp("container complete latest operation", err)
}

func (r *ContainerRepository) GetOperationByID(ctx context.Context, id uuid.UUID) (model.ResourceOperation, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, resource_type, resource_id, owner_id, operation, status, attempts, last_error, created_at, updated_at
		FROM resource_operations
		WHERE id = $1
	`, id)
	return scanResourceOperation(row)
}

func (r *ContainerRepository) ClaimPendingOperation(ctx context.Context, id uuid.UUID, maxAttempts int) (model.ResourceOperation, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return model.ResourceOperation{}, false, wrapOp("container claim pending operation", err)
	}
	defer tx.Rollback(ctx)

	op, err := scanResourceOperation(tx.QueryRow(ctx, `
		SELECT id, resource_type, resource_id, owner_id, operation, status, attempts, last_error, created_at, updated_at
		FROM resource_operations
		WHERE id = $1
		FOR UPDATE
	`, id))
	if err != nil {
		return model.ResourceOperation{}, false, wrapOp("container claim pending operation", err)
	}
	if op.Status != model.OperationStatusPending {
		if err := commitTx(ctx, tx, "skip non-pending container operation"); err != nil {
			return model.ResourceOperation{}, false, err
		}
		return op, false, nil
	}
	if maxAttempts > 0 && op.Attempts >= maxAttempts {
		if err := commitTx(ctx, tx, "skip exhausted container operation"); err != nil {
			return model.ResourceOperation{}, false, err
		}
		return op, false, nil
	}
	op, err = scanResourceOperation(tx.QueryRow(ctx, `
		UPDATE resource_operations
		SET status = $1, attempts = attempts + 1, updated_at = NOW()
		WHERE id = $2
		RETURNING id, resource_type, resource_id, owner_id, operation, status, attempts, last_error, created_at, updated_at
	`, model.OperationStatusRunning, id))
	if err != nil {
		return model.ResourceOperation{}, false, wrapOp("container claim pending operation", err)
	}
	if err := commitTx(ctx, tx, "claim container operation"); err != nil {
		return model.ResourceOperation{}, false, err
	}
	return op, true, nil
}

func (r *ContainerRepository) CompleteOperation(ctx context.Context, id uuid.UUID, status string, cause error) error {
	res, err := r.db.Exec(ctx, `
		UPDATE resource_operations
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE id = $3
	`, status, nullableError(cause), id)
	if err != nil {
		return wrapOp("container complete operation", err)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ContainerRepository) RequeueOperation(ctx context.Context, id uuid.UUID, cause error) error {
	res, err := r.db.Exec(ctx, `
		UPDATE resource_operations
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE id = $3 AND status = $4
	`, model.OperationStatusPending, nullableError(cause), id, model.OperationStatusRunning)
	if err != nil {
		return wrapOp("container requeue operation", err)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ContainerRepository) CountActiveCreateOperationsByOwner(ctx context.Context, ownerID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM resource_operations
		WHERE owner_id = $1
			AND resource_type = $2
			AND operation = $3
			AND status IN ($4, $5)
	`, ownerID, model.ResourceTypeContainer, model.OperationCreate, model.OperationStatusPending, model.OperationStatusRunning).Scan(&count)
	return count, err
}

func (r *ContainerRepository) HasActiveOperation(ctx context.Context, resourceType string, resourceID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM resource_operations
			WHERE resource_type = $1
				AND resource_id = $2
				AND status IN ($3, $4)
		)
	`, resourceType, resourceID, model.OperationStatusPending, model.OperationStatusRunning).Scan(&exists)
	return exists, err
}

func (r *ContainerRepository) RecoverInterruptedContainerCreates(ctx context.Context, cause error) (int64, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, wrapOp("container recover interrupted container creates", err)
	}
	defer tx.Rollback(ctx)

	res, err := tx.Exec(ctx, `
		UPDATE resource_operations
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE resource_type = $3
			AND operation = $4
			AND status = $5
	`, model.OperationStatusPending, nullableError(cause), model.ResourceTypeContainer, model.OperationCreate, model.OperationStatusRunning)
	if err != nil {
		return 0, wrapOp("container recover interrupted container creates", err)
	}
	rows := res.RowsAffected()
	if _, err := tx.Exec(ctx, `
		UPDATE container_lifecycle_queue_outbox
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE operation_id IN (
			SELECT id FROM resource_operations
			WHERE resource_type = $3
				AND operation = $4
				AND status = $1
		)
	`, model.ContainerOutboxStatusPending, nullableError(cause), model.ResourceTypeContainer, model.OperationCreate); err != nil {
		return 0, wrapOp("container recover interrupted container creates", err)
	}
	return rows, commitTx(ctx, tx, "recover interrupted container creates")
}

func (r *ContainerRepository) FailInterruptedNonResumableOperations(ctx context.Context, cause error) (int64, error) {
	res, err := r.db.Exec(ctx, `
		UPDATE resource_operations
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE status = $3
			AND NOT (resource_type = $4 AND operation = $5)
			AND NOT (resource_type = $6 AND operation IN ($5, $7))
			AND NOT (resource_type = $8 AND operation = $7)
	`, model.OperationStatusFailed, nullableError(cause), model.OperationStatusRunning,
		model.ResourceTypeContainer, model.OperationCreate, model.ResourceTypeVolume, model.OperationDelete, model.ResourceTypeImage)
	if err != nil {
		return 0, wrapOp("container fail interrupted non resumable operations", err)
	}
	return res.RowsAffected(), nil
}

func (r *ContainerRepository) GetMountsByContainerID(ctx context.Context, containerID uuid.UUID) ([]model.VolumeMountParams, error) {
	rows, err := r.db.Query(ctx, `
		SELECT vm.volume_id, v.docker_name, vm.mount_path, vm.is_readonly
		FROM volume_mounts vm
		JOIN volumes v ON v.id = vm.volume_id
		WHERE vm.container_id = $1
	`, containerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	mounts := make([]model.VolumeMountParams, 0)
	for rows.Next() {
		var m model.VolumeMountParams
		if err := rows.Scan(&m.VolumeID, &m.VolumeName, &m.MountPath, &m.IsReadOnly); err != nil {
			return nil, err
		}
		mounts = append(mounts, m)
	}
	return mounts, rows.Err()
}

func (r *ContainerRepository) LeasePendingContainerOutbox(ctx context.Context, limit int) ([]model.ContainerLifecycleOutbox, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, wrapOp("container lease pending container outbox", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		WITH next AS (
			SELECT id
			FROM container_lifecycle_queue_outbox
			WHERE status = $1
			   OR (status = $2 AND updated_at < NOW() - INTERVAL '1 minute')
			ORDER BY created_at
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		UPDATE container_lifecycle_queue_outbox o
		SET status = $2, attempts = attempts + 1, updated_at = NOW()
		FROM next
		WHERE o.id = next.id
		RETURNING o.id, o.operation_id, o.container_id, o.exchange, o.routing_key, o.payload, o.status, o.attempts, o.last_error, o.created_at, o.updated_at
	`, model.ContainerOutboxStatusPending, model.ContainerOutboxStatusPublishing, limit)
	if err != nil {
		return nil, wrapOp("container lease pending container outbox", err)
	}
	defer rows.Close()
	items := make([]model.ContainerLifecycleOutbox, 0)
	for rows.Next() {
		item, err := scanContainerLifecycleOutbox(rows)
		if err != nil {
			return nil, wrapOp("container lease pending container outbox", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapOp("container lease pending container outbox", err)
	}
	if err := commitTx(ctx, tx, "lease pending container outbox"); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ContainerRepository) MarkContainerOutboxPublished(ctx context.Context, id uuid.UUID) error {
	return r.updateContainerOutboxStatus(ctx, id, model.ContainerOutboxStatusPublished, nil)
}

func (r *ContainerRepository) MarkContainerOutboxPending(ctx context.Context, id uuid.UUID, cause error) error {
	return r.updateContainerOutboxStatus(ctx, id, model.ContainerOutboxStatusPending, cause)
}

func (r *ContainerRepository) MarkContainerOutboxDiscarded(ctx context.Context, id uuid.UUID, cause error) error {
	return r.updateContainerOutboxStatus(ctx, id, model.ContainerOutboxStatusDiscarded, cause)
}

func (r *ContainerRepository) DiscardContainerOutboxByOperation(ctx context.Context, operationID uuid.UUID, cause error) error {
	_, err := r.db.Exec(ctx, `
		UPDATE container_lifecycle_queue_outbox
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE operation_id = $3 AND status IN ($4, $5)
	`, model.ContainerOutboxStatusDiscarded, nullableError(cause), operationID, model.ContainerOutboxStatusPending, model.ContainerOutboxStatusPublishing)
	return wrapOp("container discard container outbox by operation", err)
}

func (r *ContainerRepository) CancelActiveCreateAndDeleteContainer(ctx context.Context, containerID uuid.UUID, cause error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return wrapOp("container cancel active create and delete container", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id
		FROM resource_operations
		WHERE resource_type = $1
			AND resource_id = $2
			AND operation = $3
			AND status IN ($4, $5)
		FOR UPDATE
	`, model.ResourceTypeContainer, containerID, model.OperationCreate, model.OperationStatusPending, model.OperationStatusRunning)
	if err != nil {
		return wrapOp("container cancel active create and delete container", err)
	}
	var operationIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return wrapOp("container cancel active create and delete container", err)
		}
		operationIDs = append(operationIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return wrapOp("container cancel active create and delete container", err)
	}

	for _, operationID := range operationIDs {
		if _, err := tx.Exec(ctx, `
			UPDATE resource_operations
			SET status = $1, last_error = $2, updated_at = NOW()
			WHERE id = $3
		`, model.OperationStatusFailed, nullableError(cause), operationID); err != nil {
			return wrapOp("container cancel active create and delete container", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE container_lifecycle_queue_outbox
			SET status = $1, last_error = $2, updated_at = NOW()
			WHERE operation_id = $3 AND status IN ($4, $5)
		`, model.ContainerOutboxStatusDiscarded, nullableError(cause), operationID, model.ContainerOutboxStatusPending, model.ContainerOutboxStatusPublishing); err != nil {
			return wrapOp("container cancel active create and delete container", err)
		}
	}
	res, err := tx.Exec(ctx, `DELETE FROM containers WHERE id = $1`, containerID)
	if err != nil {
		return wrapOp("container cancel active create and delete container", err)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return commitTx(ctx, tx, "cancel active create and delete container")
}

func (r *ContainerRepository) updateContainerOutboxStatus(ctx context.Context, id uuid.UUID, status string, cause error) error {
	query := `
		UPDATE container_lifecycle_queue_outbox
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE id = $3
	`
	if status == model.ContainerOutboxStatusPublished {
		query = `
			UPDATE container_lifecycle_queue_outbox
			SET status = $1, published_at = NOW(), last_error = $2, updated_at = NOW()
			WHERE id = $3
		`
	}
	res, err := r.db.Exec(ctx, query, status, nullableError(cause), id)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ContainerRepository) AcquireOwnerCapacityLock(ctx context.Context, ownerID uuid.UUID) (func(), error) {
	return acquireAdmissionLock(ctx, r.lockDB, []string{"dcm:owner:" + ownerID.String(), "dcm:capacity"})
}

func (r *ContainerRepository) CreateOperationAndSetDesired(ctx context.Context, id uuid.UUID, desiredStatus string, op model.ResourceOperation) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return wrapOp("container create operation and set desired", err)
	}
	defer tx.Rollback(ctx)

	if op.ResourceID != uuid.Nil {
		if err := insertResourceOperationTx(ctx, tx, op); err != nil {
			return wrapOp("container create operation and set desired", err)
		}
	}

	res, err := tx.Exec(ctx, `UPDATE containers SET desired_status = $1, last_error = NULL WHERE id = $2`, desiredStatus, id)
	if err != nil {
		return wrapOp("container create operation and set desired", err)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return commitTx(ctx, tx, "create operation and set desired")
}

func (r *ContainerRepository) QueueContainerOperation(ctx context.Context, id uuid.UUID, status string, desiredStatus string, op model.ResourceOperation, outbox model.ContainerLifecycleOutbox) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return wrapOp("container queue container operation", err)
	}
	defer tx.Rollback(ctx)

	if err := insertResourceOperationTx(ctx, tx, op); err != nil {
		return wrapOp("container queue container operation", err)
	}

	res, err := tx.Exec(ctx, `
		UPDATE containers
		SET status = $1, desired_status = $2, last_error = NULL, last_observed_at = NOW()
		WHERE id = $3
	`, status, desiredStatus, id)
	if err != nil {
		return wrapOp("container queue container operation", err)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	if err := insertContainerLifecycleOutboxTx(ctx, tx, outbox); err != nil {
		return wrapOp("container queue container operation", err)
	}
	return commitTx(ctx, tx, "queue container operation")
}

func insertContainerTx(ctx context.Context, tx pgx.Tx, c model.Container) error {
	var projectID *string
	if c.ProjectID != nil {
		projectID = ptr(c.ProjectID.String())
	}
	ttl := c.TTLDeadline
	var dockerID *string
	if c.DockerID != "" {
		dockerID = ptr(c.DockerID)
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
	_, err = tx.Exec(ctx, `
		INSERT INTO containers (
			id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix,
			status, desired_status, ttl_deadline, env_vars, base_memory_reservation, base_cpu_millicores,
			docker_generation, network_alias, command, entrypoint, restart_policy, healthcheck
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
	`, c.ID, c.OwnerID, projectID, dockerID, c.Name, c.ImageTag, c.InternalPort, c.DomainPrefix,
		c.Status, desiredStatus, ttl, c.EnvVars, c.BaseMemoryReservation, c.BaseCPUReservation,
		generation, c.NetworkAlias, command, entrypoint, c.Restart, healthcheck)
	if err != nil {
		return mapContainerUniqueViolation(err, c.DomainPrefix)
	}
	return nil
}

func insertResourceOperationTx(ctx context.Context, tx pgx.Tx, op model.ResourceOperation) error {
	if op.ID == uuid.Nil {
		op.ID = uuid.New()
	}
	if op.Status == "" {
		op.Status = model.OperationStatusPending
	}
	lastError := op.LastError
	_, err := tx.Exec(ctx, `
		INSERT INTO resource_operations (id, resource_type, resource_id, owner_id, operation, status, attempts, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, op.ID, op.ResourceType, op.ResourceID, op.OwnerID, op.Operation, op.Status, op.Attempts, lastError)
	return mapActiveOperationError(err)
}

func insertContainerLifecycleOutboxTx(ctx context.Context, tx pgx.Tx, outbox model.ContainerLifecycleOutbox) error {
	if outbox.ID == uuid.Nil {
		outbox.ID = uuid.New()
	}
	if outbox.Status == "" {
		outbox.Status = model.ContainerOutboxStatusPending
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO container_lifecycle_queue_outbox (id, operation_id, container_id, exchange, routing_key, payload, status, attempts, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, outbox.ID, outbox.OperationID, outbox.ContainerID, outbox.Exchange, outbox.RoutingKey, outbox.Payload, outbox.Status, outbox.Attempts, outbox.LastError)
	return err
}

func scanResourceOperation(s scanner) (model.ResourceOperation, error) {
	var op model.ResourceOperation
	var lastError *string
	if err := s.Scan(&op.ID, &op.ResourceType, &op.ResourceID, &op.OwnerID, &op.Operation, &op.Status, &op.Attempts, &lastError, &op.CreatedAt, &op.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ResourceOperation{}, apperrors.ErrNotFound
		}
		return model.ResourceOperation{}, err
	}
	op.LastError = lastError
	return op, nil
}

func scanContainerLifecycleOutbox(s scanner) (model.ContainerLifecycleOutbox, error) {
	var item model.ContainerLifecycleOutbox
	var lastError *string
	if err := s.Scan(&item.ID, &item.OperationID, &item.ContainerID, &item.Exchange, &item.RoutingKey, &item.Payload, &item.Status, &item.Attempts, &lastError, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return model.ContainerLifecycleOutbox{}, err
	}
	item.LastError = lastError
	return item, nil
}

func scanContainerListItem(s scanner) (model.Container, error) {
	var c model.Container
	var projectID *string
	var dockerID *string
	var ttl *time.Time
	var lastObservedAt *time.Time
	var lastError *string
	var lastExitCode *int64
	if err := s.Scan(
		&c.ID, &c.OwnerID, &projectID, &dockerID, &c.Name, &c.ImageTag, &c.InternalPort, &c.DomainPrefix,
		&c.Status, &c.DesiredStatus, &ttl, &c.BaseMemoryReservation, &c.BaseCPUReservation, &lastObservedAt, &lastError, &lastExitCode, &c.DockerGeneration, &c.CreatedAt,
	); err != nil {
		return model.Container{}, err
	}
	if projectID != nil {
		parsed, _ := uuid.Parse(*projectID)
		c.ProjectID = &parsed
	}
	if dockerID != nil {
		c.DockerID = *dockerID
	}
	c.TTLDeadline = ttl
	c.LastObservedAt = lastObservedAt
	c.LastError = lastError
	if lastExitCode != nil {
		value := int(*lastExitCode)
		c.LastExitCode = &value
	}
	return c, nil
}

func scanContainerFull(s scanner) (model.Container, error) {
	var c model.Container
	var projectID *string
	var dockerID *string
	var ttl *time.Time
	var lastObservedAt *time.Time
	var lastError *string
	var lastExitCode *int64
	var command, entrypoint, healthcheck []byte
	if err := s.Scan(
		&c.ID, &c.OwnerID, &projectID, &dockerID, &c.Name, &c.ImageTag, &c.InternalPort, &c.DomainPrefix,
		&c.Status, &c.DesiredStatus, &ttl, &c.EnvVars, &c.BaseMemoryReservation, &c.BaseCPUReservation, &lastObservedAt, &lastError,
		&lastExitCode, &c.DockerGeneration, &c.NetworkAlias, &command, &entrypoint, &c.Restart, &healthcheck,
	); err != nil {
		return model.Container{}, err
	}
	if projectID != nil {
		parsed, _ := uuid.Parse(*projectID)
		c.ProjectID = &parsed
	}
	if dockerID != nil {
		c.DockerID = *dockerID
	}
	c.TTLDeadline = ttl
	c.LastObservedAt = lastObservedAt
	c.LastError = lastError
	if lastExitCode != nil {
		value := int(*lastExitCode)
		c.LastExitCode = &value
	}
	if err := decodeJSONColumn(command, &c.Command); err != nil {
		return model.Container{}, wrapOp("container decode command", err)
	}
	if err := decodeJSONColumn(entrypoint, &c.Entrypoint); err != nil {
		return model.Container{}, wrapOp("container decode entrypoint", err)
	}
	if err := decodeJSONColumn(healthcheck, &c.Healthcheck); err != nil {
		return model.Container{}, wrapOp("container decode healthcheck", err)
	}
	return c, nil
}

// decodeJSONColumn decodes a nullable JSONB column; SQL or JSON null leaves dst
// unchanged.
func decodeJSONColumn(raw []byte, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

func nullableInt(value *int) *int64 {
	if value == nil {
		return nil
	}
	return ptr(int64(*value))
}

func nullableError(cause error) *string {
	if cause == nil {
		return nil
	}
	return ptr(cause.Error())
}

func mapContainerUniqueViolation(err error, domainPrefix string) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if pgErr.ConstraintName == containerDomainPrefixIndex && domainPrefix != "" {
			return apperrors.New(apperrors.ErrAlreadyExists, fmt.Sprintf("subdomain %s is already in use", domainPrefix))
		}
		if pgErr.ConstraintName == containerOwnerNameUniqueIndex {
			return apperrors.New(apperrors.ErrAlreadyExists, "container name is already in use")
		}
		return apperrors.ErrAlreadyExists
	}
	return err
}

func mapActiveOperationError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return apperrors.New(apperrors.ErrConflict, "resource operation is already in progress")
	}
	return err
}
