package repository

import (
	"context"
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

type VolumeRepository struct {
	db *pgxpool.Pool
}

// volumeProjectColumn is the project a volume reads as belonging to: its
// oldest link in project_volumes, the only record of project membership.
const volumeProjectColumn = `(SELECT pv.project_id FROM project_volumes pv WHERE pv.volume_id = volumes.id ORDER BY pv.created_at, pv.project_id LIMIT 1)`

const volumeColumns = `id, owner_id, ` + volumeProjectColumn + `, name, docker_name, status, last_observed_at, last_error, used_bytes, usage_observed_at, size_bytes, size_enforced, created_at`
const qualifiedVolumeColumns = `volumes.id, volumes.owner_id, ` + volumeProjectColumn + `, volumes.name, volumes.docker_name, volumes.status, volumes.last_observed_at, volumes.last_error, volumes.used_bytes, volumes.usage_observed_at, volumes.size_bytes, volumes.size_enforced, volumes.created_at`

type scanner interface {
	Scan(dest ...any) error
}

func NewVolumeRepository(db *pgxpool.Pool) *VolumeRepository {
	return &VolumeRepository{db: db}
}

func (r *VolumeRepository) Save(ctx context.Context, vol model.Volume) error {
	return insertVolume(ctx, r.db, vol)
}

// insertVolume stores a volume. vol.ProjectID, when set, is the project the
// volume is created in and becomes its first project link.
func insertVolume(ctx context.Context, db executor, vol model.Volume) error {
	status := vol.Status
	if status == "" {
		status = model.VolumeStatusAvailable
	}

	// The volume and, for a volume created inside a project, its link row are
	// written by one statement.
	_, err := db.Exec(ctx, `
		WITH created AS (
			INSERT INTO volumes (id, owner_id, name, docker_name, status, size_bytes, size_enforced)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id
		)
		INSERT INTO project_volumes (project_id, volume_id)
		SELECT $8::uuid, id FROM created WHERE $8::uuid IS NOT NULL
	`, vol.ID, vol.OwnerID, vol.Name, vol.DockerName, status, vol.SizeBytes, vol.SizeEnforced, vol.ProjectID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func insertVolumeTx(ctx context.Context, tx pgx.Tx, vol model.Volume) error {
	return insertVolume(ctx, tx, vol)
}

func (r *VolumeRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Volume, error) {
	query := `SELECT ` + volumeColumns + ` FROM volumes WHERE id = $1`

	v, err := scanVolume(r.db.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Volume{}, apperrors.ErrNotFound
		}
		return model.Volume{}, err
	}
	return v, nil
}

func (r *VolumeRepository) GetByName(ctx context.Context, ownerID uuid.UUID, name string) (model.Volume, error) {
	query := `SELECT ` + volumeColumns + ` FROM volumes WHERE owner_id = $1 AND name = $2`

	v, err := scanVolume(r.db.QueryRow(ctx, query, ownerID, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Volume{}, apperrors.ErrNotFound
		}
		return model.Volume{}, err
	}
	return v, nil
}

func (r *VolumeRepository) LinkProjectVolume(ctx context.Context, projectID, volumeID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO project_volumes (project_id, volume_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, projectID, volumeID)
	return err
}

func (r *VolumeRepository) UnlinkProjectVolume(ctx context.Context, projectID, volumeID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM project_volumes WHERE project_id = $1 AND volume_id = $2`, projectID, volumeID)
	return err
}

func (r *VolumeRepository) IsLinkedToProject(ctx context.Context, projectID, volumeID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM project_volumes
			WHERE project_id = $1 AND volume_id = $2
		)
	`, projectID, volumeID).Scan(&exists)
	return exists, err
}

func (r *VolumeRepository) GetReconcileCandidates(ctx context.Context) ([]model.Volume, error) {
	query := `SELECT ` + volumeColumns + ` FROM volumes WHERE status IN ($1, $2)`
	rows, err := r.db.Query(ctx, query, model.VolumeStatusCreating, model.VolumeStatusAvailable)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var volumes []model.Volume
	for rows.Next() {
		v, err := scanVolume(rows)
		if err != nil {
			return nil, err
		}
		volumes = append(volumes, v)
	}
	return volumes, rows.Err()
}

func (r *VolumeRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM volumes WHERE id = $1`
	res, err := r.db.Exec(ctx, query, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return apperrors.New(apperrors.ErrResourceInUse, "volume is currently used by a container")
		}
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *VolumeRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	query := `UPDATE volumes SET status = $1, last_observed_at = NOW(), last_error = NULL WHERE id = $2`
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

// MarkAvailable records a volume Docker has created as available, with
// whether its size is kernel-enforced.
func (r *VolumeRepository) MarkAvailable(ctx context.Context, id uuid.UUID, sizeEnforced bool) error {
	query := `UPDATE volumes SET status = $1, size_enforced = $2, last_observed_at = NOW(), last_error = NULL WHERE id = $3`
	res, err := r.db.Exec(ctx, query, model.VolumeStatusAvailable, sizeEnforced, id)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *VolumeRepository) MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error {
	var msg *string
	if cause != nil {
		msg = ptr(cause.Error())
	}
	query := `UPDATE volumes SET status = $1, last_observed_at = NOW(), last_error = $2 WHERE id = $3`
	res, err := r.db.Exec(ctx, query, status, msg, id)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *VolumeRepository) CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM volumes WHERE owner_id = $1`
	var count int
	err := r.db.QueryRow(ctx, query, ownerID).Scan(&count)
	return count, err
}

func (r *VolumeRepository) CountAll(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM volumes`
	var count int
	err := r.db.QueryRow(ctx, query).Scan(&count)
	return count, err
}

func (r *VolumeRepository) IsVolumeInUse(ctx context.Context, volumeID uuid.UUID) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM volume_mounts 
			WHERE volume_id = $1
		)
	`
	var exists bool
	err := r.db.QueryRow(ctx, query, volumeID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *VolumeRepository) GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Volume, error) {
	query := `
		SELECT ` + qualifiedVolumeColumns + `
		FROM volumes
		JOIN project_volumes pv ON pv.volume_id = volumes.id
		WHERE pv.project_id = $1
		ORDER BY volumes.created_at DESC
	`
	rows, err := r.db.Query(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var volumes []model.Volume
	for rows.Next() {
		v, err := scanVolume(rows)
		if err != nil {
			return nil, err
		}
		volumes = append(volumes, v)
	}
	return volumes, rows.Err()
}

func (r *VolumeRepository) List(ctx context.Context, opts model.ListOptions) ([]model.Volume, int, error) {
	var args []any
	where := ""
	if opts.OwnerID != nil {
		args = append(args, *opts.OwnerID)
		where = " WHERE owner_id = $1"
	}

	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM volumes`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT ` + volumeColumns + ` FROM volumes` + where + ` ORDER BY created_at DESC`
	if opts.Limit > 0 {
		args = append(args, opts.Limit, opts.Offset)
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var volumes []model.Volume
	for rows.Next() {
		v, err := scanVolume(rows)
		if err != nil {
			return nil, 0, err
		}
		volumes = append(volumes, v)
	}
	return volumes, total, rows.Err()
}

func (r *VolumeRepository) UpdateUsage(ctx context.Context, id uuid.UUID, usedBytes int64) error {
	query := `UPDATE volumes SET used_bytes = $1, usage_observed_at = NOW(), last_observed_at = NOW(), last_error = NULL WHERE id = $2`
	res, err := r.db.Exec(ctx, query, usedBytes, id)
	if err != nil {
		return err
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

// GetUserReservedVolumeBytes sums, over the owner's volumes not being deleted,
// each volume's size or its measured usage when that is larger. It is the
// volume part of the owner's reserved disk.
func (r *VolumeRepository) GetUserReservedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	query := `SELECT COALESCE(SUM(GREATEST(size_bytes, used_bytes)), 0)::bigint FROM volumes WHERE owner_id = $1 AND status != $2`
	var reserved int64
	err := r.db.QueryRow(ctx, query, ownerID, model.VolumeStatusDeleting).Scan(&reserved)
	return reserved, err
}

// GetTotalVolumeSizeBytes sums the sizes of all volumes not being deleted.
func (r *VolumeRepository) GetTotalVolumeSizeBytes(ctx context.Context) (int64, error) {
	query := `SELECT COALESCE(SUM(size_bytes), 0)::bigint FROM volumes WHERE status != $1`
	var total int64
	err := r.db.QueryRow(ctx, query, model.VolumeStatusDeleting).Scan(&total)
	return total, err
}

// GetUnusedVolumePromiseBytes sums, over every owner's volumes not being
// deleted, each volume's size less its measured usage, never below zero: the
// disk the host has promised to volumes and they have not used yet.
func (r *VolumeRepository) GetUnusedVolumePromiseBytes(ctx context.Context) (int64, error) {
	query := `SELECT COALESCE(SUM(GREATEST(size_bytes - used_bytes, 0)), 0)::bigint FROM volumes WHERE status != $1`
	var promise int64
	err := r.db.QueryRow(ctx, query, model.VolumeStatusDeleting).Scan(&promise)
	return promise, err
}

func (r *VolumeRepository) GetTotalUsedVolumeBytes(ctx context.Context) (int64, error) {
	query := `SELECT COALESCE(SUM(used_bytes), 0) FROM volumes WHERE status != $1`
	var usedBytes int64
	err := r.db.QueryRow(ctx, query, model.VolumeStatusDeleting).Scan(&usedBytes)
	return usedBytes, err
}

func scanVolume(s scanner) (model.Volume, error) {
	var v model.Volume
	var projectID *string
	var lastObservedAt *time.Time
	var lastError *string
	var usageObservedAt *time.Time

	if err := s.Scan(
		&v.ID, &v.OwnerID, &projectID, &v.Name, &v.DockerName, &v.Status,
		&lastObservedAt, &lastError, &v.UsedBytes, &usageObservedAt, &v.SizeBytes, &v.SizeEnforced, &v.CreatedAt,
	); err != nil {
		return model.Volume{}, err
	}
	if projectID != nil {
		parsed, _ := uuid.Parse(*projectID)
		v.ProjectID = &parsed
	}
	v.LastObservedAt = lastObservedAt
	v.LastError = lastError
	v.UsageObservedAt = usageObservedAt
	return v, nil
}
