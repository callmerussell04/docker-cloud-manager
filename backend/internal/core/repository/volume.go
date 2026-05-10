package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type VolumeRepository struct {
	db *sql.DB
}

const volumeColumns = `id, owner_id, project_id, docker_name, status, last_observed_at, last_error, used_bytes, usage_observed_at, created_at`

type scanner interface {
	Scan(dest ...any) error
}

func NewVolumeRepository(db *sql.DB) *VolumeRepository {
	return &VolumeRepository{db: db}
}

func (r *VolumeRepository) Save(ctx context.Context, vol model.Volume) error {
	query := `
		INSERT INTO volumes (id, owner_id, project_id, docker_name, status)
		VALUES ($1, $2, $3, $4, $5)
	`

	var projectID sql.NullString
	if vol.ProjectID != nil {
		projectID.String = vol.ProjectID.String()
		projectID.Valid = true
	}

	status := vol.Status
	if status == "" {
		status = model.VolumeStatusAvailable
	}

	_, err := r.db.ExecContext(ctx, query, vol.ID, vol.OwnerID, projectID, vol.DockerName, status)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (r *VolumeRepository) GetByID(ctx context.Context, id uuid.UUID) (model.Volume, error) {
	query := `SELECT ` + volumeColumns + ` FROM volumes WHERE id = $1`

	v, err := scanVolume(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Volume{}, apperrors.ErrNotFound
		}
		return model.Volume{}, err
	}
	return v, nil
}

func (r *VolumeRepository) GetReconcileCandidates(ctx context.Context) ([]model.Volume, error) {
	query := `SELECT ` + volumeColumns + ` FROM volumes WHERE status IN ($1, $2)`
	rows, err := r.db.QueryContext(ctx, query, model.VolumeStatusCreating, model.VolumeStatusAvailable)
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
	res, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return apperrors.New(apperrors.ErrResourceInUse, "volume is currently used by a container")
		}
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

func (r *VolumeRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	query := `UPDATE volumes SET status = $1, last_observed_at = NOW(), last_error = NULL WHERE id = $2`
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

func (r *VolumeRepository) MarkStatusError(ctx context.Context, id uuid.UUID, status string, cause error) error {
	var msg sql.NullString
	if cause != nil {
		msg.String = cause.Error()
		msg.Valid = true
	}
	query := `UPDATE volumes SET status = $1, last_observed_at = NOW(), last_error = $2 WHERE id = $3`
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

func (r *VolumeRepository) SaveMounts(ctx context.Context, mounts []model.VolumeMount) error {
	if len(mounts) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO volume_mounts (container_id, volume_id, mount_path, is_readonly)
		VALUES ($1, $2, $3, $4)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, m := range mounts {
		_, err = stmt.ExecContext(ctx, m.ContainerID, m.VolumeID, m.MountPath, m.IsReadOnly)
		if err != nil {
			var pgErr *pq.Error
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				continue
			}
			return err
		}
	}

	return tx.Commit()
}

func (r *VolumeRepository) CountByOwnerID(ctx context.Context, ownerID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM volumes WHERE owner_id = $1`
	var count int
	err := r.db.QueryRowContext(ctx, query, ownerID).Scan(&count)
	return count, err
}

func (r *VolumeRepository) CountAll(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM volumes`
	var count int
	err := r.db.QueryRowContext(ctx, query).Scan(&count)
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
	err := r.db.QueryRowContext(ctx, query, volumeID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *VolumeRepository) GetByProjectID(ctx context.Context, projectID uuid.UUID) ([]model.Volume, error) {
	query := `SELECT ` + volumeColumns + ` FROM volumes WHERE project_id = $1`
	rows, err := r.db.QueryContext(ctx, query, projectID)
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
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM volumes`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT ` + volumeColumns + ` FROM volumes` + where + ` ORDER BY created_at DESC`
	if opts.Limit > 0 {
		args = append(args, opts.Limit, opts.Offset)
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
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
	res, err := r.db.ExecContext(ctx, query, usedBytes, id)
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

func (r *VolumeRepository) GetUserUsedVolumeBytes(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	query := `SELECT COALESCE(SUM(used_bytes), 0) FROM volumes WHERE owner_id = $1 AND status != $2`
	var usedBytes int64
	err := r.db.QueryRowContext(ctx, query, ownerID, model.VolumeStatusDeleting).Scan(&usedBytes)
	return usedBytes, err
}

func (r *VolumeRepository) GetTotalUsedVolumeBytes(ctx context.Context) (int64, error) {
	query := `SELECT COALESCE(SUM(used_bytes), 0) FROM volumes WHERE status != $1`
	var usedBytes int64
	err := r.db.QueryRowContext(ctx, query, model.VolumeStatusDeleting).Scan(&usedBytes)
	return usedBytes, err
}

func (r *VolumeRepository) GetOwnersWithVolumes(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT owner_id FROM volumes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var owners []uuid.UUID
	for rows.Next() {
		var ownerID uuid.UUID
		if err := rows.Scan(&ownerID); err != nil {
			return nil, err
		}
		owners = append(owners, ownerID)
	}
	return owners, rows.Err()
}

func scanVolume(s scanner) (model.Volume, error) {
	var v model.Volume
	var projectID sql.NullString
	var lastObservedAt sql.NullTime
	var lastError sql.NullString
	var usageObservedAt sql.NullTime

	if err := s.Scan(
		&v.ID, &v.OwnerID, &projectID, &v.DockerName, &v.Status,
		&lastObservedAt, &lastError, &v.UsedBytes, &usageObservedAt, &v.CreatedAt,
	); err != nil {
		return model.Volume{}, err
	}
	if projectID.Valid {
		parsed, _ := uuid.Parse(projectID.String)
		v.ProjectID = &parsed
	}
	if lastObservedAt.Valid {
		v.LastObservedAt = &lastObservedAt.Time
	}
	if lastError.Valid {
		v.LastError = &lastError.String
	}
	if usageObservedAt.Valid {
		v.UsageObservedAt = &usageObservedAt.Time
	}
	return v, nil
}
