package repository

import (
	"context"
	"encoding/json"
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

type ProjectRepository struct {
	db *pgxpool.Pool
}

const projectOwnerNameUniqueIndex = "idx_projects_owner_name_unique"

func NewProjectRepository(db *pgxpool.Pool) *ProjectRepository {
	return &ProjectRepository{db: db}
}

func (r *ProjectRepository) Save(ctx context.Context, p model.Project) error {
	query := `
		INSERT INTO projects (id, owner_id, name, status, error_message) 
		VALUES ($1, $2, $3, $4, $5)
	`

	errMsg := p.ErrorMessage

	_, err := r.db.Exec(ctx, query, p.ID, p.OwnerID, p.Name, p.Status, errMsg)
	return mapProjectUniqueViolation(err)
}

func (r *ProjectRepository) CreateWithComposeDeploymentJob(ctx context.Context, p model.Project, job model.ComposeDeploymentJob, outbox model.ComposeDeploymentOutbox) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return wrapOp("project create with compose deployment job", err)
	}
	defer tx.Rollback(ctx)

	projectErrMsg := p.ErrorMessage
	if _, err := tx.Exec(ctx, `
		INSERT INTO projects (id, owner_id, name, status, error_message)
		VALUES ($1, $2, $3, $4, $5)
	`, p.ID, p.OwnerID, p.Name, p.Status, projectErrMsg); err != nil {
		return mapProjectUniqueViolation(err)
	}

	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	status := job.Status
	if status == "" {
		status = model.ComposeDeploymentStatusQueued
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO compose_deployment_jobs (
			id, project_id, owner_id, source_type, source_object_key, compose_file,
			status, attempts, cancel_requested, error_message, request_id,
			stage, plan_json, resource_map_json
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, job.ID, job.ProjectID, job.OwnerID, job.SourceType, job.SourceObjectKey, job.ComposeFile,
		status, job.Attempts, job.CancelRequested, job.ErrorMessage, job.RequestID,
		job.Stage, nullableBytes(job.PlanJSON), nullableBytes(job.ResourceMapJSON)); err != nil {
		return wrapOp("project create with compose deployment job", err)
	}

	outboxStatus := outbox.Status
	if outboxStatus == "" {
		outboxStatus = model.ComposeOutboxStatusPending
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO compose_deployment_queue_outbox (id, job_id, exchange, routing_key, payload, status, attempts, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, outbox.ID, outbox.JobID, outbox.Exchange, outbox.RoutingKey, string(outbox.Payload), outboxStatus, outbox.Attempts, outbox.LastError); err != nil {
		return wrapOp("project create with compose deployment job", err)
	}

	return commitTx(ctx, tx, "create with compose deployment job")
}

func (r *ProjectRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Project, error) {
	query := `
		SELECT id, owner_id, name, status, error_message, created_at 
		FROM projects 
		WHERE id = $1
	`

	var p model.Project
	var errMsg *string

	err := r.db.QueryRow(ctx, query, id).Scan(
		&p.ID,
		&p.OwnerID,
		&p.Name,
		&p.Status,
		&errMsg,
		&p.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Project{}, apperrors.ErrNotFound
		}
		return model.Project{}, err
	}

	p.ErrorMessage = errMsg

	return p, nil
}

func (r *ProjectRepository) GetComposeDeploymentJob(ctx context.Context, id uuid.UUID) (model.ComposeDeploymentJob, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, project_id, owner_id, source_type, source_object_key, compose_file,
			status, attempts, cancel_requested, error_message, request_id,
			stage, plan_json, resource_map_json,
			created_at, updated_at, started_at, finished_at
		FROM compose_deployment_jobs
		WHERE id = $1
	`, id)
	return scanComposeDeploymentJob(row)
}

func (r *ProjectRepository) GetActiveComposeDeploymentJobByProjectID(ctx context.Context, projectID uuid.UUID) (model.ComposeDeploymentJob, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, project_id, owner_id, source_type, source_object_key, compose_file,
			status, attempts, cancel_requested, error_message, request_id,
			stage, plan_json, resource_map_json,
			created_at, updated_at, started_at, finished_at
		FROM compose_deployment_jobs
		WHERE project_id = $1 AND status IN ($2, $3, $4)
		ORDER BY created_at DESC
		LIMIT 1
	`, projectID, model.ComposeDeploymentStatusQueued, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStatusCanceling)
	return scanComposeDeploymentJob(row)
}

func (r *ProjectRepository) ListInterruptedComposeDeploymentJobs(ctx context.Context) ([]model.ComposeDeploymentJob, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, project_id, owner_id, source_type, source_object_key, compose_file,
			status, attempts, cancel_requested, error_message, request_id,
			stage, plan_json, resource_map_json,
			created_at, updated_at, started_at, finished_at
		FROM compose_deployment_jobs
		WHERE status IN ($1, $2)
		ORDER BY updated_at ASC
	`, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStatusCanceling)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]model.ComposeDeploymentJob, 0)
	for rows.Next() {
		job, err := scanComposeDeploymentJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (r *ProjectRepository) StartComposeDeploymentJob(ctx context.Context, id uuid.UUID) (model.ComposeDeploymentJob, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return model.ComposeDeploymentJob{}, false, wrapOp("project start compose deployment job", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		SELECT id, project_id, owner_id, source_type, source_object_key, compose_file,
			status, attempts, cancel_requested, error_message, request_id,
			stage, plan_json, resource_map_json,
			created_at, updated_at, started_at, finished_at
		FROM compose_deployment_jobs
		WHERE id = $1
		FOR UPDATE
	`, id)
	job, err := scanComposeDeploymentJob(row)
	if err != nil {
		return model.ComposeDeploymentJob{}, false, wrapOp("project start compose deployment job", err)
	}
	if isTerminalComposeDeploymentStatus(job.Status) || job.CancelRequested {
		if err := commitTx(ctx, tx, "start compose deployment job"); err != nil {
			return model.ComposeDeploymentJob{}, false, err
		}
		return job, false, nil
	}
	if job.Status != model.ComposeDeploymentStatusQueued {
		if err := commitTx(ctx, tx, "start compose deployment job"); err != nil {
			return model.ComposeDeploymentJob{}, false, err
		}
		return job, false, nil
	}
	row = tx.QueryRow(ctx, `
		UPDATE compose_deployment_jobs
		SET status = $1, attempts = attempts + 1, started_at = COALESCE(started_at, NOW()), updated_at = NOW()
		WHERE id = $2
		RETURNING id, project_id, owner_id, source_type, source_object_key, compose_file,
			status, attempts, cancel_requested, error_message, request_id,
			stage, plan_json, resource_map_json,
			created_at, updated_at, started_at, finished_at
	`, model.ComposeDeploymentStatusRunning, id)
	job, err = scanComposeDeploymentJob(row)
	if err != nil {
		return model.ComposeDeploymentJob{}, false, wrapOp("project start compose deployment job", err)
	}
	if err := commitTx(ctx, tx, "start compose deployment job"); err != nil {
		return model.ComposeDeploymentJob{}, false, err
	}
	return job, true, nil
}

func (r *ProjectRepository) CompleteComposeDeploymentJob(ctx context.Context, id uuid.UUID, status string, errorMsg *string) error {
	res, err := r.db.Exec(ctx, `
		UPDATE compose_deployment_jobs
		SET status = $1, error_message = $2, finished_at = NOW(), updated_at = NOW()
		WHERE id = $3
	`, status, errorMsg, id)
	if err != nil {
		return wrapOp("project complete compose deployment job", err)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ProjectRepository) RequestComposeDeploymentCancel(ctx context.Context, projectID uuid.UUID) error {
	res, err := r.db.Exec(ctx, `
		UPDATE compose_deployment_jobs
		SET cancel_requested = TRUE, status = CASE
			WHEN status IN ($1, $2) THEN $3
			ELSE status
		END, updated_at = NOW()
		WHERE project_id = $4 AND status IN ($1, $2, $3)
	`, model.ComposeDeploymentStatusQueued, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStatusCanceling, projectID)
	if err != nil {
		return wrapOp("project request compose deployment cancel", err)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ProjectRepository) CountActiveComposeDeploymentsByOwner(ctx context.Context, ownerID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM compose_deployment_jobs
		WHERE owner_id = $1 AND status IN ($2, $3, $4)
	`, ownerID, model.ComposeDeploymentStatusQueued, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStatusCanceling).Scan(&count)
	return count, err
}

func (r *ProjectRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string, errorMsg *string) error {
	query := `
		UPDATE projects 
		SET status = $1, error_message = $2 
		WHERE id = $3
	`

	errMsg := errorMsg

	res, err := r.db.Exec(ctx, query, status, errMsg, id)
	if err != nil {
		return err
	}

	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}

	return nil
}

func (r *ProjectRepository) LeasePendingComposeOutbox(ctx context.Context, limit int) ([]model.ComposeDeploymentOutbox, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, wrapOp("project lease pending compose outbox", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		WITH next AS (
			SELECT id
			FROM compose_deployment_queue_outbox
			WHERE status = $1
			   OR (status = $2 AND updated_at < NOW() - INTERVAL '1 minute')
			ORDER BY created_at
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		UPDATE compose_deployment_queue_outbox o
		SET status = $2, attempts = attempts + 1, updated_at = NOW()
		FROM next
		WHERE o.id = next.id
		RETURNING o.id, o.job_id, o.exchange, o.routing_key, o.payload, o.status, o.attempts, o.last_error, o.created_at, o.updated_at
	`, model.ComposeOutboxStatusPending, model.ComposeOutboxStatusPublishing, limit)
	if err != nil {
		return nil, wrapOp("project lease pending compose outbox", err)
	}
	defer rows.Close()

	items := make([]model.ComposeDeploymentOutbox, 0)
	for rows.Next() {
		item, err := scanComposeOutbox(rows)
		if err != nil {
			return nil, wrapOp("project lease pending compose outbox", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapOp("project lease pending compose outbox", err)
	}
	if err := commitTx(ctx, tx, "lease pending compose outbox"); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ProjectRepository) MarkComposeOutboxPublished(ctx context.Context, id uuid.UUID) error {
	return r.updateComposeOutboxStatus(ctx, id, model.ComposeOutboxStatusPublished, nil)
}

func (r *ProjectRepository) MarkComposeOutboxPending(ctx context.Context, id uuid.UUID, cause error) error {
	return r.updateComposeOutboxStatus(ctx, id, model.ComposeOutboxStatusPending, cause)
}

func (r *ProjectRepository) MarkComposeOutboxDiscarded(ctx context.Context, id uuid.UUID, cause error) error {
	return r.updateComposeOutboxStatus(ctx, id, model.ComposeOutboxStatusDiscarded, cause)
}

func (r *ProjectRepository) SaveComposeDeploymentPlan(ctx context.Context, id uuid.UUID, stage string, planJSON, resourceMapJSON []byte) error {
	res, err := r.db.Exec(ctx, `
		UPDATE compose_deployment_jobs
		SET stage = $1, plan_json = $2, resource_map_json = $3, updated_at = NOW()
		WHERE id = $4 AND status = $5
	`, stage, nullableBytes(planJSON), nullableBytes(resourceMapJSON), id, model.ComposeDeploymentStatusRunning)
	if err != nil {
		return wrapOp("project save compose deployment plan", err)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *ProjectRepository) UpdateComposeDeploymentProgress(ctx context.Context, id uuid.UUID, projectID uuid.UUID, projectStatus string, stage string, planJSON, resourceMapJSON []byte) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return wrapOp("project update compose deployment progress", err)
	}
	defer tx.Rollback(ctx)

	res, err := tx.Exec(ctx, `
		UPDATE compose_deployment_jobs
		SET stage = $1, plan_json = $2, resource_map_json = $3, updated_at = NOW()
		WHERE id = $4 AND status IN ($5, $6)
	`, stage, nullableBytes(planJSON), nullableBytes(resourceMapJSON), id, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStatusCanceling)
	if err != nil {
		return wrapOp("project update compose deployment progress", err)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	if projectStatus != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE projects
			SET status = $1, error_message = NULL
			WHERE id = $2
		`, projectStatus, projectID); err != nil {
			return wrapOp("project update compose deployment progress", err)
		}
	}
	return commitTx(ctx, tx, "update compose deployment progress")
}

func (r *ProjectRepository) ListActiveComposeDeploymentJobs(ctx context.Context, limit int) ([]model.ComposeDeploymentJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, project_id, owner_id, source_type, source_object_key, compose_file,
			status, attempts, cancel_requested, error_message, request_id,
			stage, plan_json, resource_map_json,
			created_at, updated_at, started_at, finished_at
		FROM compose_deployment_jobs
		WHERE status IN ($1, $2)
			AND stage <> ''
		ORDER BY updated_at ASC
		LIMIT $3
	`, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStatusCanceling, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]model.ComposeDeploymentJob, 0)
	for rows.Next() {
		job, err := scanComposeDeploymentJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (r *ProjectRepository) RecoverInterruptedComposeDeployments(ctx context.Context, maxAttempts int, errorMessage string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return wrapOp("project recover interrupted compose deployments", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id, project_id, attempts, stage, error_message
		FROM compose_deployment_jobs
		WHERE status IN ($1, $2)
		FOR UPDATE
	`, model.ComposeDeploymentStatusRunning, model.ComposeDeploymentStatusCanceling)
	if err != nil {
		return wrapOp("project recover interrupted compose deployments", err)
	}
	type interruptedJob struct {
		id        uuid.UUID
		projectID uuid.UUID
		attempts  int
		stage     string
		errorMsg  *string
	}
	var jobs []interruptedJob
	for rows.Next() {
		var item interruptedJob
		if err := rows.Scan(&item.id, &item.projectID, &item.attempts, &item.stage, &item.errorMsg); err != nil {
			rows.Close()
			return wrapOp("project recover interrupted compose deployments", err)
		}
		jobs = append(jobs, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return wrapOp("project recover interrupted compose deployments", err)
	}

	for _, job := range jobs {
		if job.stage != "" {
			continue
		}
		hasPartialWork, err := composeDeploymentHasPartialWork(ctx, tx, job.projectID)
		if err != nil {
			return wrapOp("project recover interrupted compose deployments", err)
		}
		if hasPartialWork || (maxAttempts > 0 && job.attempts >= maxAttempts) {
			message := errorMessage
			if job.errorMsg != nil && *job.errorMsg != "" {
				message = *job.errorMsg
			}
			if _, err := tx.Exec(ctx, `
				UPDATE compose_deployment_jobs
				SET status = $1, error_message = $2, finished_at = NOW(), updated_at = NOW()
				WHERE id = $3
			`, model.ComposeDeploymentStatusFailed, message, job.id); err != nil {
				return wrapOp("project recover interrupted compose deployments", err)
			}
			if _, err := tx.Exec(ctx, `
				UPDATE projects SET status = $1, error_message = $2 WHERE id = $3
			`, model.ProjectStatusFailed, message, job.projectID); err != nil {
				return wrapOp("project recover interrupted compose deployments", err)
			}
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE compose_deployment_jobs
			SET status = $1, error_message = NULL, updated_at = NOW()
			WHERE id = $2
		`, model.ComposeDeploymentStatusQueued, job.id); err != nil {
			return wrapOp("project recover interrupted compose deployments", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE projects SET status = $1, error_message = NULL WHERE id = $2
		`, model.ProjectStatusBuilding, job.projectID); err != nil {
			return wrapOp("project recover interrupted compose deployments", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE compose_deployment_queue_outbox
			SET status = $1, updated_at = NOW(), last_error = NULL
			WHERE job_id = $2
		`, model.ComposeOutboxStatusPending, job.id); err != nil {
			return wrapOp("project recover interrupted compose deployments", err)
		}
	}

	return commitTx(ctx, tx, "recover interrupted compose deployments")
}

func composeDeploymentHasPartialWork(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM containers WHERE project_id = $1
			UNION ALL
			SELECT 1 FROM project_volumes WHERE project_id = $1
			UNION ALL
			SELECT 1 FROM builds WHERE project_id = $1
		)
	`, projectID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (r *ProjectRepository) SaveServiceGraph(ctx context.Context, projectID uuid.UUID, services []model.ProjectServiceNode) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return wrapOp("project save service graph", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM project_service_dependencies WHERE project_id = $1`, projectID); err != nil {
		return wrapOp("project save service graph", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM project_services WHERE project_id = $1`, projectID); err != nil {
		return wrapOp("project save service graph", err)
	}

	insertServiceSQL := `
		INSERT INTO project_services (project_id, container_id, service_name, start_order)
		VALUES ($1, $2, $3, $4)
	`

	insertDependencySQL := `
		INSERT INTO project_service_dependencies (project_id, container_id, depends_on_container_id, condition, required)
		VALUES ($1, $2, $3, $4, $5)
	`

	for _, svc := range services {
		if _, err := tx.Exec(ctx, insertServiceSQL, projectID, svc.ContainerID, svc.ServiceName, svc.StartOrder); err != nil {
			return wrapOp("project save service graph", err)
		}
		for _, dep := range svc.Dependencies {
			if !model.IsValidComposeDependencyCondition(dep.Condition) {
				return fmt.Errorf("%w: invalid compose dependency condition", apperrors.ErrBadRequest)
			}
			if _, err := tx.Exec(ctx, insertDependencySQL, projectID, svc.ContainerID, dep.DependsOnContainerID, dep.Condition, !dep.Optional); err != nil {
				return wrapOp("project save service graph", err)
			}
		}
	}

	return commitTx(ctx, tx, "save service graph")
}

func (r *ProjectRepository) GetServiceGraph(ctx context.Context, projectID uuid.UUID) ([]model.ProjectServiceNode, error) {
	rows, err := r.db.Query(ctx, `
		SELECT container_id, service_name, start_order
		FROM project_services
		WHERE project_id = $1
		ORDER BY start_order ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	services := make([]model.ProjectServiceNode, 0)
	byContainer := make(map[uuid.UUID]int)
	for rows.Next() {
		var svc model.ProjectServiceNode
		svc.ProjectID = projectID
		if err := rows.Scan(&svc.ContainerID, &svc.ServiceName, &svc.StartOrder); err != nil {
			return nil, err
		}
		byContainer[svc.ContainerID] = len(services)
		services = append(services, svc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("%w: project service graph not found", apperrors.ErrNotFound)
	}

	depRows, err := r.db.Query(ctx, `
		SELECT d.container_id, d.depends_on_container_id, dep.service_name, d.condition, d.required
		FROM project_service_dependencies d
		JOIN project_services dep
			ON dep.project_id = d.project_id
			AND dep.container_id = d.depends_on_container_id
		WHERE d.project_id = $1
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer depRows.Close()

	for depRows.Next() {
		var containerID uuid.UUID
		var required bool
		var dep model.ProjectServiceDependency
		dep.ProjectID = projectID
		if err := depRows.Scan(&containerID, &dep.DependsOnContainerID, &dep.DependsOnServiceName, &dep.Condition, &required); err != nil {
			return nil, err
		}
		dep.ContainerID = containerID
		dep.Optional = !required
		idx, ok := byContainer[containerID]
		if !ok {
			continue
		}
		services[idx].Dependencies = append(services[idx].Dependencies, dep)
	}
	if err := depRows.Err(); err != nil {
		return nil, err
	}

	return services, nil
}

func (r *ProjectRepository) FailActiveDeployments(ctx context.Context, errorMessage string) error {
	query := `
		UPDATE projects
		SET status = $1, error_message = $2
		WHERE status IN ($3, $4, $5)
	`
	_, err := r.db.Exec(ctx, query, model.ProjectStatusFailed, errorMessage, model.ProjectStatusBuilding, model.ProjectStatusDeploying, model.ProjectStatusCanceling)
	return wrapOp("project fail active deployments", err)
}

func (r *ProjectRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM projects WHERE id = $1`

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

func (r *ProjectRepository) List(ctx context.Context, opts model.ListOptions) ([]model.Project, int, error) {
	var args []any
	where := ""
	if opts.OwnerID != nil {
		args = append(args, *opts.OwnerID)
		where = " WHERE owner_id = $1"
	}

	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM projects`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, owner_id, name, status, error_message, created_at
		FROM projects` + where + ` ORDER BY created_at DESC`
	if opts.Limit > 0 {
		args = append(args, opts.Limit, opts.Offset)
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var projects []model.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, 0, err
		}
		projects = append(projects, p)
	}
	return projects, total, rows.Err()
}

func (r *ProjectRepository) ListByStatuses(ctx context.Context, statuses []string, limit int) ([]model.Project, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, owner_id, name, status, error_message, created_at
		FROM projects
		WHERE status = ANY($1)
		ORDER BY created_at
		LIMIT $2
	`, statuses, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []model.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (r *ProjectRepository) CountAll(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM projects`
	var count int
	err := r.db.QueryRow(ctx, query).Scan(&count)
	return count, err
}

func scanProject(s scanner) (model.Project, error) {
	var p model.Project
	var errMsg *string
	if err := s.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Status, &errMsg, &p.CreatedAt); err != nil {
		return model.Project{}, err
	}
	p.ErrorMessage = errMsg
	return p, nil
}

func scanComposeDeploymentJob(s scanner) (model.ComposeDeploymentJob, error) {
	var job model.ComposeDeploymentJob
	var errMsg *string
	var planJSON []byte
	var resourceMapJSON []byte
	var startedAt *time.Time
	var finishedAt *time.Time
	err := s.Scan(
		&job.ID,
		&job.ProjectID,
		&job.OwnerID,
		&job.SourceType,
		&job.SourceObjectKey,
		&job.ComposeFile,
		&job.Status,
		&job.Attempts,
		&job.CancelRequested,
		&errMsg,
		&job.RequestID,
		&job.Stage,
		&planJSON,
		&resourceMapJSON,
		&job.CreatedAt,
		&job.UpdatedAt,
		&startedAt,
		&finishedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ComposeDeploymentJob{}, apperrors.ErrNotFound
		}
		return model.ComposeDeploymentJob{}, err
	}
	job.ErrorMessage = errMsg
	job.PlanJSON = append(job.PlanJSON, planJSON...)
	job.ResourceMapJSON = append(job.ResourceMapJSON, resourceMapJSON...)
	job.StartedAt = startedAt
	job.FinishedAt = finishedAt
	return job, nil
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

func scanComposeOutbox(s scanner) (model.ComposeDeploymentOutbox, error) {
	var item model.ComposeDeploymentOutbox
	var payload []byte
	var lastError *string
	if err := s.Scan(
		&item.ID,
		&item.JobID,
		&item.Exchange,
		&item.RoutingKey,
		&payload,
		&item.Status,
		&item.Attempts,
		&lastError,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.ComposeDeploymentOutbox{}, err
	}
	item.Payload = json.RawMessage(payload)
	item.LastError = lastError
	return item, nil
}

func (r *ProjectRepository) updateComposeOutboxStatus(ctx context.Context, id uuid.UUID, status string, cause error) error {
	var lastError *string
	if cause != nil {
		lastError = ptr(cause.Error())
	}
	query := `
		UPDATE compose_deployment_queue_outbox
		SET status = $1, last_error = $2, updated_at = NOW()
		WHERE id = $3
	`
	args := []any{status, lastError, id}
	if status == model.ComposeOutboxStatusPublished {
		query = `
			UPDATE compose_deployment_queue_outbox
			SET status = $1, published_at = NOW(), last_error = $2, updated_at = NOW()
			WHERE id = $3
		`
	}
	res, err := r.db.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func mapProjectUniqueViolation(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if pgErr.ConstraintName == projectOwnerNameUniqueIndex {
			return apperrors.New(apperrors.ErrAlreadyExists, "project name is already in use")
		}
		return apperrors.ErrAlreadyExists
	}
	return err
}

func isTerminalComposeDeploymentStatus(status string) bool {
	return model.IsComposeDeploymentTerminalStatus(status)
}
