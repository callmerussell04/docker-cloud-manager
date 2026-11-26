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

// ListReservingByOwner returns the identity, Docker container and statuses of
// every container of ownerID that reserves capacity, ordered by ID: not
// missing, and status or desired status running, as the reservation sums.
func (r *ContainerRepository) ListReservingByOwner(ctx context.Context, ownerID uuid.UUID) ([]model.Container, error) {
	query := `
		SELECT id, owner_id, docker_id, status, desired_status
		FROM containers
		WHERE owner_id = $1 AND status != $3 AND (status = $2 OR desired_status = $2)
		ORDER BY id
	`
	rows, err := r.db.Query(ctx, query, ownerID, model.ContainerStatusRunning, model.ContainerStatusMissing)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []model.Container
	for rows.Next() {
		var c model.Container
		var dockerID *string
		if err := rows.Scan(&c.ID, &c.OwnerID, &dockerID, &c.Status, &c.DesiredStatus); err != nil {
			return nil, err
		}
		if dockerID != nil {
			c.DockerID = *dockerID
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

// ListRunningWritersOfVolume lists the running containers that mount
// volumeID writable.
func (r *ContainerRepository) ListRunningWritersOfVolume(ctx context.Context, volumeID uuid.UUID) ([]model.Container, error) {
	query := `
		SELECT c.id, c.owner_id, c.docker_id, c.status
		FROM containers c
		WHERE c.status = $2 AND EXISTS (
			SELECT 1 FROM volume_mounts vm
			WHERE vm.container_id = c.id AND vm.volume_id = $1 AND vm.is_readonly = false
		)
		ORDER BY c.id
	`
	rows, err := r.db.Query(ctx, query, volumeID, model.ContainerStatusRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var containers []model.Container
	for rows.Next() {
		var c model.Container
		var dockerID *string
		if err := rows.Scan(&c.ID, &c.OwnerID, &dockerID, &c.Status); err != nil {
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

// List reads one page of the containers list and the count of every status
// filter. Counts cover the owner, the project and the search but not the
// status filter; the total is the counts of the filter's classes. It expects
// a normalized filter (model.ContainerListFilter.Normalize).
func (r *ContainerRepository) List(ctx context.Context, opts model.ContainerListOptions) (model.ContainerPage, error) {
	counts, err := r.countFilters(ctx, opts)
	if err != nil {
		return model.ContainerPage{}, err
	}
	page := model.ContainerPage{Counts: counts, Total: counts.Shown(opts.Status)}
	if page.Total == 0 {
		return page, nil
	}

	q := &sqlArgs{}
	conditions := containerListConditions(q, opts)
	if condition := containerStatusCondition(q, opts.Status); condition != "" {
		conditions = append(conditions, condition)
	}
	query := `
		SELECT id, owner_id, project_id, docker_id, name, image_tag, internal_port, domain_prefix,
			status, desired_status, ttl_deadline, base_memory_reservation, base_cpu_millicores, last_observed_at, last_error, last_exit_code, docker_generation, created_at
		FROM containers` + whereClause(conditions) + ` ORDER BY ` + containerOrderBy(q, opts.ContainerListFilter)
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %s OFFSET %s", q.add(opts.Limit), q.add(opts.Offset))
	}

	rows, err := r.db.Query(ctx, query, q.args...)
	if err != nil {
		return model.ContainerPage{}, fmt.Errorf("list containers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scanContainerListItem(rows)
		if err != nil {
			return model.ContainerPage{}, fmt.Errorf("scan container list item: %w", err)
		}
		page.Containers = append(page.Containers, c)
	}
	if err := rows.Err(); err != nil {
		return model.ContainerPage{}, fmt.Errorf("list containers: %w", err)
	}
	return page, nil
}

// CountFiltersByOwnerID counts the owner's containers under every status
// filter.
func (r *ContainerRepository) CountFiltersByOwnerID(ctx context.Context, ownerID uuid.UUID) (model.ContainerFilterCounts, error) {
	return r.countFilters(ctx, model.ContainerListOptions{ListOptions: model.ListOptions{OwnerID: &ownerID}})
}

// containerRefsByKey runs query, which takes an array of IDs as $1 and
// selects a key ID, a container ID and the container's name, and groups the
// containers by key, in the query's order.
func containerRefsByKey(ctx context.Context, db *pgxpool.Pool, query string, keys []uuid.UUID) (map[uuid.UUID][]model.ContainerRef, error) {
	rows, err := db.Query(ctx, query, keys)
	if err != nil {
		return nil, fmt.Errorf("query container refs: %w", err)
	}
	defer rows.Close()
	refs := make(map[uuid.UUID][]model.ContainerRef, len(keys))
	for rows.Next() {
		var key uuid.UUID
		var ref model.ContainerRef
		if err := rows.Scan(&key, &ref.ContainerID, &ref.Name); err != nil {
			return nil, fmt.Errorf("scan container ref: %w", err)
		}
		refs[key] = append(refs[key], ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query container refs: %w", err)
	}
	return refs, nil
}

// countFilters counts the containers under every status filter, within the
// owner, the project and the search; it ignores the status filter, the sort
// and the page.
func (r *ContainerRepository) countFilters(ctx context.Context, opts model.ContainerListOptions) (model.ContainerFilterCounts, error) {
	q := &sqlArgs{}
	query := `SELECT ` + containerClassExpr(q) + ` AS class, COUNT(*) FROM containers` +
		whereClause(containerListConditions(q, opts)) + ` GROUP BY class`

	rows, err := r.db.Query(ctx, query, q.args...)
	if err != nil {
		return model.ContainerFilterCounts{}, fmt.Errorf("count containers by status: %w", err)
	}
	defer rows.Close()
	var counts model.ContainerFilterCounts
	for rows.Next() {
		var class, n int
		if err := rows.Scan(&class, &n); err != nil {
			return model.ContainerFilterCounts{}, fmt.Errorf("scan container status count: %w", err)
		}
		counts.Add(model.ContainerStatusClass(class), n)
	}
	if err := rows.Err(); err != nil {
		return model.ContainerFilterCounts{}, fmt.Errorf("count containers by status: %w", err)
	}
	return counts, nil
}

// sqlArgs collects a statement's parameters and names their placeholders.
type sqlArgs struct {
	args []any
}

func (q *sqlArgs) add(value any) string {
	q.args = append(q.args, value)
	return fmt.Sprintf("$%d", len(q.args))
}

func whereClause(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conditions, " AND ")
}

// containerListConditions narrows the list to the owner, the project and
// the search; the status filter is added only for the page.
func containerListConditions(q *sqlArgs, opts model.ContainerListOptions) []string {
	var conditions []string
	if opts.OwnerID != nil {
		conditions = append(conditions, "owner_id = "+q.add(*opts.OwnerID))
	}
	if opts.ProjectID != nil {
		conditions = append(conditions, "project_id = "+q.add(*opts.ProjectID))
	}
	if opts.Search != "" {
		pattern := q.add("%" + likeEscaper.Replace(opts.Search) + "%")
		conditions = append(conditions, fmt.Sprintf(`(name ILIKE %[1]s ESCAPE '\' OR image_tag ILIKE %[1]s ESCAPE '\')`, pattern))
	}
	return conditions
}

// likeEscaper makes a search literal text in a LIKE pattern.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// containerClassExpr is a container's status class (model.ContainerStatusClass)
// in SQL, from the model's own lists: busy is every status no class lists.
func containerClassExpr(q *sqlArgs) string {
	var b strings.Builder
	b.WriteString("CASE")
	for _, class := range model.ContainerStatusClasses {
		if statuses := model.ContainerClassStatuses(class); statuses != nil {
			fmt.Fprintf(&b, " WHEN status = ANY(%s) THEN %d", q.add(statuses), class)
		}
	}
	fmt.Fprintf(&b, " ELSE %d END", model.ContainerClassBusy)
	return b.String()
}

// containerStatusCondition keeps the classes a status filter shows, by
// status so the status index serves it. A filter showing busy containers
// keeps every status the other classes do not list.
func containerStatusCondition(q *sqlArgs, filter model.ContainerStatusFilter) string {
	showsBusy := filter.Shows(model.ContainerClassBusy)
	var statuses []string
	for _, class := range model.ContainerStatusClasses {
		if class != model.ContainerClassBusy && filter.Shows(class) != showsBusy {
			statuses = append(statuses, model.ContainerClassStatuses(class)...)
		}
	}
	switch {
	case showsBusy && len(statuses) == 0:
		return ""
	case showsBusy:
		return "NOT (status = ANY(" + q.add(statuses) + "))"
	default:
		return "status = ANY(" + q.add(statuses) + ")"
	}
}

// containerOrderBy orders the list by one column, then by name (natural and
// case-insensitive) and id, which never reverse, so equal rows keep one order
// across pages. Ascending status is failing first; ascending creation is the
// newest first; ascending address puts unrouted containers last.
func containerOrderBy(q *sqlArgs, filter model.ContainerListFilter) string {
	dir, reverse := "ASC", "DESC"
	if filter.Order == model.SortDescending {
		dir, reverse = reverse, dir
	}
	const byName = "name COLLATE natural_order ASC, id ASC"
	switch filter.Sort {
	case model.ContainerSortName:
		return "name COLLATE natural_order " + dir + ", id ASC"
	case model.ContainerSortAddress:
		return "(domain_prefix = '') " + dir + ", domain_prefix COLLATE natural_order " + dir + ", " + byName
	case model.ContainerSortMemory:
		return "base_memory_reservation " + dir + ", " + byName
	case model.ContainerSortCreated:
		return "created_at " + reverse + ", " + byName
	default:
		return containerClassExpr(q) + " " + dir + ", " + byName
	}
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

// RecoverInterruptedContainerCreates returns running container creates in
// scope to pending, returning their attempt when the scope says so, and marks
// outbox records pending so their messages are published again. Without an
// age bound, as at startup, every pending create's record is marked;
// with one, as in the operation recovery pass, only the recovered creates'
// records, so a create still waiting for its message is not published twice.
func (r *ContainerRepository) RecoverInterruptedContainerCreates(ctx context.Context, cause error, scope model.OperationRecoveryScope) ([]model.RecoveredOperation, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, wrapOp("container recover interrupted container creates", err)
	}
	defer tx.Rollback(ctx)

	args := append([]any{model.OperationStatusPending, nullableError(cause), model.OperationStatusRunning, scope.ReturnAttempt, ageSeconds(scope.RunningLongerThan)},
		resumableContainerOperationArgs()...)
	rows, err := tx.Query(ctx, `
		UPDATE resource_operations op
		SET status = $1,
			attempts = CASE WHEN $4 THEN GREATEST(op.attempts - 1, 0) ELSE op.attempts END,
			last_error = $2, updated_at = NOW()
		WHERE op.status = $3
			AND `+claimedBefore("op.updated_at", 5)+`
			AND `+resumableContainerOperation(6)+`
		RETURNING op.id, op.resource_type, op.resource_id, op.operation
	`, args...)
	if err != nil {
		return nil, wrapOp("container recover interrupted container creates", err)
	}
	recovered, err := collectRecoveredOperations(rows)
	if err != nil {
		return nil, wrapOp("container recover interrupted container creates", err)
	}
	args = append([]any{model.ContainerOutboxStatusPending, nullableError(cause), model.OperationStatusPending, ageSeconds(scope.RunningLongerThan), recoveredIDs(recovered)},
		resumableContainerOperationArgs()...)
	if _, err := tx.Exec(ctx, `
		UPDATE container_lifecycle_queue_outbox
		SET status = $1, published_at = NULL, last_error = $2, updated_at = NOW()
		WHERE operation_id IN (
			SELECT op.id FROM resource_operations op
			WHERE op.status = $3
				AND ($4::double precision = 0 OR op.id = ANY($5))
				AND `+resumableContainerOperation(6)+`
		)
	`, args...); err != nil {
		return nil, wrapOp("container recover interrupted container creates", err)
	}
	if err := commitTx(ctx, tx, "recover interrupted container creates"); err != nil {
		return nil, err
	}
	return recovered, nil
}

// FailInterruptedNonResumableOperations fails every running operation that
// is not resumable (anything but a container create, a volume create or
// delete and an image delete) and was claimed longer ago than
// runningLongerThan on the database clock; zero fails them all. Resource rows
// are untouched: with no active operation, state sync records what Docker
// reports.
func (r *ContainerRepository) FailInterruptedNonResumableOperations(ctx context.Context, cause error, runningLongerThan time.Duration) ([]model.RecoveredOperation, error) {
	args := append([]any{model.OperationStatusFailed, nullableError(cause), model.OperationStatusRunning, ageSeconds(runningLongerThan)},
		resumableOperationArgs()...)
	rows, err := r.db.Query(ctx, `
		UPDATE resource_operations op
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE op.status = $3
			AND `+claimedBefore("op.updated_at", 4)+`
			AND NOT `+resumableOperation(5)+`
		RETURNING op.id, op.resource_type, op.resource_id, op.operation
	`, args...)
	if err != nil {
		return nil, wrapOp("container fail interrupted non resumable operations", err)
	}
	failed, err := collectRecoveredOperations(rows)
	if err != nil {
		return nil, wrapOp("container fail interrupted non resumable operations", err)
	}
	return failed, nil
}

// RecoverLostContainerDeliveries finds pending container operations whose
// message is no longer in the queue, judged on the database clock. One whose
// outbox record was published or discarded longer ago than olderThan has the
// record returned to pending with its payload, for the relay to publish
// again. One whose container row is gone (its outbox record cascades with
// it) and that was last updated longer ago than olderThan is ended: done for
// a delete, failed with cause otherwise.
func (r *ContainerRepository) RecoverLostContainerDeliveries(ctx context.Context, cause error, olderThan time.Duration) (model.LostDeliveryRecovery, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return model.LostDeliveryRecovery{}, wrapOp("container recover lost deliveries", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		UPDATE container_lifecycle_queue_outbox o
		SET status = $1, published_at = NULL, updated_at = NOW()
		FROM resource_operations op
		WHERE o.operation_id = op.id
			AND op.status = $2
			AND o.status IN ($3, $4)
			AND `+updatedBefore("o.updated_at", 5)+`
		RETURNING op.id, op.resource_type, op.resource_id, op.operation
	`, model.ContainerOutboxStatusPending, model.OperationStatusPending,
		model.ContainerOutboxStatusPublished, model.ContainerOutboxStatusDiscarded, ageSeconds(olderThan))
	if err != nil {
		return model.LostDeliveryRecovery{}, wrapOp("container recover lost deliveries", err)
	}
	republished, err := collectRecoveredOperations(rows)
	if err != nil {
		return model.LostDeliveryRecovery{}, wrapOp("container recover lost deliveries", err)
	}
	rows, err = tx.Query(ctx, `
		UPDATE resource_operations op
		SET status = CASE WHEN op.operation = $1 THEN $2 ELSE $3 END,
			last_error = CASE WHEN op.operation = $1 THEN NULL ELSE $4 END,
			updated_at = NOW()
		WHERE op.resource_type = $5
			AND op.status = $6
			AND `+updatedBefore("op.updated_at", 7)+`
			AND NOT EXISTS (SELECT 1 FROM containers c WHERE c.id = op.resource_id)
		RETURNING op.id, op.resource_type, op.resource_id, op.operation
	`, model.OperationDelete, model.OperationStatusDone, model.OperationStatusFailed, nullableError(cause),
		model.ResourceTypeContainer, model.OperationStatusPending, ageSeconds(olderThan))
	if err != nil {
		return model.LostDeliveryRecovery{}, wrapOp("container recover lost deliveries", err)
	}
	ended, err := collectRecoveredOperations(rows)
	if err != nil {
		return model.LostDeliveryRecovery{}, wrapOp("container recover lost deliveries", err)
	}
	if err := commitTx(ctx, tx, "recover lost container deliveries"); err != nil {
		return model.LostDeliveryRecovery{}, err
	}
	return model.LostDeliveryRecovery{Republished: republished, Ended: ended}, nil
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
