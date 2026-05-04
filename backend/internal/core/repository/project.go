package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

type ProjectRepository struct {
	db *sql.DB
}

func NewProjectRepository(db *sql.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

func (r *ProjectRepository) Save(ctx context.Context, p model.Project) error {
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

func (r *ProjectRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Project, error) {
	query := `
		SELECT id, owner_id, name, status, error_message, created_at 
		FROM projects 
		WHERE id = $1
	`

	var p model.Project
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
			return model.Project{}, apperrors.ErrNotFound
		}
		return model.Project{}, err
	}

	if errMsg.Valid {
		p.ErrorMessage = &errMsg.String
	}

	return p, nil
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

func (r *ProjectRepository) SaveServiceGraph(ctx context.Context, projectID uuid.UUID, services []model.ProjectServiceNode) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM project_service_dependencies WHERE project_id = $1`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_services WHERE project_id = $1`, projectID); err != nil {
		return err
	}

	serviceStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO project_services (project_id, container_id, service_name, start_order)
		VALUES ($1, $2, $3, $4)
	`)
	if err != nil {
		return err
	}
	defer serviceStmt.Close()

	depStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO project_service_dependencies (project_id, container_id, depends_on_container_id, condition, required)
		VALUES ($1, $2, $3, $4, $5)
	`)
	if err != nil {
		return err
	}
	defer depStmt.Close()

	for _, svc := range services {
		if _, err := serviceStmt.ExecContext(ctx, projectID, svc.ContainerID, svc.ServiceName, svc.StartOrder); err != nil {
			return err
		}
		for _, dep := range svc.Dependencies {
			if !model.IsValidComposeDependencyCondition(dep.Condition) {
				return fmt.Errorf("%w: invalid compose dependency condition", apperrors.ErrBadRequest)
			}
			if _, err := depStmt.ExecContext(ctx, projectID, svc.ContainerID, dep.DependsOnContainerID, dep.Condition, !dep.Optional); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func (r *ProjectRepository) GetServiceGraph(ctx context.Context, projectID uuid.UUID) ([]model.ProjectServiceNode, error) {
	rows, err := r.db.QueryContext(ctx, `
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

	depRows, err := r.db.QueryContext(ctx, `
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
		WHERE status IN ($3, $4)
	`
	_, err := r.db.ExecContext(ctx, query, model.ProjectStatusFailed, errorMessage, model.ProjectStatusBuilding, model.ProjectStatusDeploying)
	return err
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

func (r *ProjectRepository) List(ctx context.Context, opts model.ListOptions) ([]model.Project, int, error) {
	var args []any
	where := ""
	if opts.OwnerID != nil {
		args = append(args, *opts.OwnerID)
		where = " WHERE owner_id = $1"
	}

	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, owner_id, name, status, error_message, created_at
		FROM projects` + where + ` ORDER BY created_at DESC`
	if opts.Limit > 0 {
		args = append(args, opts.Limit, opts.Offset)
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
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

func scanProject(s scanner) (model.Project, error) {
	var p model.Project
	var errMsg sql.NullString
	if err := s.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Status, &errMsg, &p.CreatedAt); err != nil {
		return model.Project{}, err
	}
	if errMsg.Valid {
		p.ErrorMessage = &errMsg.String
	}
	return p, nil
}
