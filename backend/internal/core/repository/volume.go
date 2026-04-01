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

type VolumeRepository struct {
	db *sql.DB
}

func NewVolumeRepository(db *sql.DB) *VolumeRepository {
	return &VolumeRepository{db: db}
}

func (r *VolumeRepository) Save(ctx context.Context, vol domain.Volume) error {
	query := `
		INSERT INTO volumes (id, owner_id, docker_name, driver, driver_opts)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := r.db.ExecContext(ctx, query, vol.ID, vol.OwnerID, vol.DockerName, vol.Driver, vol.DriverOpts)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (r *VolumeRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.Volume, error) {
	query := `
		SELECT id, owner_id, docker_name, driver, driver_opts, created_at 
		FROM volumes WHERE id = $1
	`

	var v domain.Volume
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&v.ID, &v.OwnerID, &v.DockerName, &v.Driver, &v.DriverOpts, &v.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Volume{}, apperrors.ErrNotFound
		}
		return domain.Volume{}, err
	}

	return v, nil
}

func (r *VolumeRepository) GetByOwnerID(ctx context.Context, ownerID uuid.UUID) ([]domain.Volume, error) {
	query := `
		SELECT id, owner_id, docker_name, driver, driver_opts, created_at 
		FROM volumes WHERE owner_id = $1
	`
	rows, err := r.db.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var volumes []domain.Volume
	for rows.Next() {
		var v domain.Volume
		if err := rows.Scan(&v.ID, &v.OwnerID, &v.DockerName, &v.Driver, &v.DriverOpts, &v.CreatedAt); err != nil {
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
			return errors.New("volume is in use")
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

func (r *VolumeRepository) SaveMounts(ctx context.Context, mounts []domain.VolumeMount) error {
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
