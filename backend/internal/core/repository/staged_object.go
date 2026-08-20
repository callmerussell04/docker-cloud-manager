package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
)

type StagedObjectRepository struct {
	db *sql.DB
}

func NewStagedObjectRepository(db *sql.DB) *StagedObjectRepository {
	return &StagedObjectRepository{db: db}
}

func (r *StagedObjectRepository) Reserve(ctx context.Context, reservation model.StagedObjectReservation, maxBytesPerUser int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "dcm:staged:"+reservation.OwnerID.String()); err != nil {
		return err
	}

	if maxBytesPerUser > 0 {
		var used int64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(bytes_reserved), 0)
			FROM staged_object_reservations
			WHERE owner_id = $1 AND status = $2
		`, reservation.OwnerID, model.StagedObjectStatusActive).Scan(&used); err != nil {
			return err
		}
		if used+reservation.BytesReserved > maxBytesPerUser {
			return apperrors.New(apperrors.ErrQuotaExceeded, "staged source quota exceeded")
		}
	}

	status := reservation.Status
	if status == "" {
		status = model.StagedObjectStatusActive
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO staged_object_reservations (id, owner_id, object_key, kind, bytes_reserved, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (object_key) DO NOTHING
	`, reservation.ID, reservation.OwnerID, reservation.ObjectKey, reservation.Kind, reservation.BytesReserved, status); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *StagedObjectRepository) UpdateBytes(ctx context.Context, objectKey string, bytesReserved int64) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE staged_object_reservations
		SET bytes_reserved = $1, updated_at = NOW()
		WHERE object_key = $2 AND status = $3
	`, bytesReserved, objectKey, model.StagedObjectStatusActive)
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

func (r *StagedObjectRepository) Release(ctx context.Context, objectKey string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE staged_object_reservations
		SET status = $1, released_at = NOW(), updated_at = NOW()
		WHERE object_key = $2 AND status = $3
	`, model.StagedObjectStatusReleased, objectKey, model.StagedObjectStatusActive)
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

func (r *StagedObjectRepository) ActiveBytesByOwner(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	var used int64
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(bytes_reserved), 0)
		FROM staged_object_reservations
		WHERE owner_id = $1 AND status = $2
	`, ownerID, model.StagedObjectStatusActive).Scan(&used)
	return used, err
}

func (r *StagedObjectRepository) ListStaleActive(ctx context.Context, cutoff time.Time, limit int) ([]model.StagedObjectReservation, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, owner_id, object_key, kind, bytes_reserved, status, created_at, updated_at, released_at
		FROM staged_object_reservations
		WHERE status = $1 AND updated_at < $2
			AND NOT EXISTS (
				SELECT 1
				FROM builds
				WHERE archive_object_key = staged_object_reservations.object_key
					AND status IN ($4, $5)
			)
			AND NOT EXISTS (
				SELECT 1
				FROM compose_deployment_jobs
				WHERE source_object_key = staged_object_reservations.object_key
					AND status IN ($6, $7, $8)
			)
		ORDER BY updated_at ASC
		LIMIT $3
	`,
		model.StagedObjectStatusActive,
		cutoff,
		limit,
		model.BuildStatusPending,
		model.BuildStatusRunning,
		model.ComposeDeploymentStatusQueued,
		model.ComposeDeploymentStatusRunning,
		model.ComposeDeploymentStatusCanceling,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reservations := make([]model.StagedObjectReservation, 0)
	for rows.Next() {
		reservation, err := scanStagedObjectReservation(rows)
		if err != nil {
			return nil, err
		}
		reservations = append(reservations, reservation)
	}
	return reservations, rows.Err()
}

func scanStagedObjectReservation(s scanner) (model.StagedObjectReservation, error) {
	var reservation model.StagedObjectReservation
	var releasedAt sql.NullTime
	if err := s.Scan(
		&reservation.ID,
		&reservation.OwnerID,
		&reservation.ObjectKey,
		&reservation.Kind,
		&reservation.BytesReserved,
		&reservation.Status,
		&reservation.CreatedAt,
		&reservation.UpdatedAt,
		&releasedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.StagedObjectReservation{}, apperrors.ErrNotFound
		}
		return model.StagedObjectReservation{}, err
	}
	if releasedAt.Valid {
		reservation.ReleasedAt = &releasedAt.Time
	}
	return reservation, nil
}
