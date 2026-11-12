package repository

import (
	"context"
	"errors"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserQuotaRepository stores users' custom quotas. It reads through the query
// pool, also while a caller holds the owner capacity lock.
type UserQuotaRepository struct {
	db *pgxpool.Pool
}

func NewUserQuotaRepository(db *pgxpool.Pool) *UserQuotaRepository {
	return &UserQuotaRepository{db: db}
}

// GetCustomUserQuota returns the owner's custom quota, or false when the owner
// has the default quota.
func (r *UserQuotaRepository) GetCustomUserQuota(ctx context.Context, ownerID uuid.UUID) (model.UserQuota, bool, error) {
	quota := model.UserQuota{Custom: true}
	err := r.db.QueryRow(ctx, `
		SELECT cpu_millicores, ram_mb, disk_mb
		FROM user_quotas
		WHERE owner_id = $1
	`, ownerID).Scan(&quota.CPUMillicores, &quota.RAMMB, &quota.DiskMB)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.UserQuota{}, false, nil
	}
	if err != nil {
		return model.UserQuota{}, false, wrapOp("get custom user quota", err)
	}
	return quota, true, nil
}

// SetCustomUserQuota gives the owner a custom quota, replacing any earlier one.
func (r *UserQuotaRepository) SetCustomUserQuota(ctx context.Context, ownerID uuid.UUID, quota model.UserQuota) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO user_quotas (owner_id, cpu_millicores, ram_mb, disk_mb, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (owner_id) DO UPDATE SET
			cpu_millicores = EXCLUDED.cpu_millicores,
			ram_mb = EXCLUDED.ram_mb,
			disk_mb = EXCLUDED.disk_mb,
			updated_at = EXCLUDED.updated_at
	`, ownerID, quota.CPUMillicores, quota.RAMMB, quota.DiskMB)
	return wrapOp("set custom user quota", err)
}

// GetCustomUserQuotas returns the custom quotas of those owners that have one.
// An owner missing from the result has the default quota.
func (r *UserQuotaRepository) GetCustomUserQuotas(ctx context.Context, ownerIDs []uuid.UUID) (map[uuid.UUID]model.UserQuota, error) {
	quotas := make(map[uuid.UUID]model.UserQuota, len(ownerIDs))
	if len(ownerIDs) == 0 {
		return quotas, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT owner_id, cpu_millicores, ram_mb, disk_mb
		FROM user_quotas
		WHERE owner_id = ANY($1)
	`, ownerIDs)
	if err != nil {
		return nil, wrapOp("get custom user quotas", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ownerID uuid.UUID
		quota := model.UserQuota{Custom: true}
		if err := rows.Scan(&ownerID, &quota.CPUMillicores, &quota.RAMMB, &quota.DiskMB); err != nil {
			return nil, wrapOp("scan custom user quota", err)
		}
		quotas[ownerID] = quota
	}
	if err := rows.Err(); err != nil {
		return nil, wrapOp("get custom user quotas", err)
	}
	return quotas, nil
}

// DeleteCustomUserQuota returns the owner to the default quota. It is not an
// error when the owner already has it.
func (r *UserQuotaRepository) DeleteCustomUserQuota(ctx context.Context, ownerID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM user_quotas WHERE owner_id = $1`, ownerID)
	return wrapOp("delete custom user quota", err)
}
