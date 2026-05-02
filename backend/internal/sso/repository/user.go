package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) SaveUser(ctx context.Context, user model.User) error {
	query := `
		INSERT INTO users (id, username, email, password_hash, role, status, quota_cpu, quota_ram_mb, quota_disk_mb)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := r.db.ExecContext(ctx, query, user.ID, user.Username, user.Email, user.PasswordHash, user.Role, user.Status, user.QuotaCPU, user.QuotaRAMMB, user.QuotaDiskMB)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrAlreadyExists
		}
		return err
	}

	return nil
}

func (r *UserRepository) GetUserByUsername(ctx context.Context, username string) (model.User, error) {
	query := `
		SELECT id, username, email, password_hash, role, status, quota_cpu, quota_ram_mb, quota_disk_mb
		FROM users
		WHERE username = $1
	`

	var user model.User
	err := r.db.QueryRowContext(ctx, query, username).Scan(userScanDest(&user)...)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.User{}, apperrors.ErrNotFound
		}
		return model.User{}, err
	}

	return user, nil
}

func (r *UserRepository) GetUserByID(ctx context.Context, id uuid.UUID) (model.User, error) {
	query := `
		SELECT id, username, email, password_hash, role, status, quota_cpu, quota_ram_mb, quota_disk_mb
		FROM users
		WHERE id = $1
	`

	var user model.User
	err := r.db.QueryRowContext(ctx, query, id).Scan(userScanDest(&user)...)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.User{}, apperrors.ErrNotFound
		}
		return model.User{}, err
	}

	return user, nil
}

func (r *UserRepository) GetUsersByIDs(ctx context.Context, ids []uuid.UUID) ([]model.User, error) {
	if len(ids) == 0 {
		return []model.User{}, nil
	}

	query := `
		SELECT id, username, email, password_hash, role, status, quota_cpu, quota_ram_mb, quota_disk_mb
		FROM users
		WHERE id = ANY($1)
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []model.User
	for rows.Next() {
		var user model.User
		if err := rows.Scan(userScanDest(&user)...); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (r *UserRepository) ListUsers(ctx context.Context, opts model.ListUsersOptions) ([]model.User, int, error) {
	countQuery := `SELECT COUNT(*) FROM users`

	var total int
	if err := r.db.QueryRowContext(ctx, countQuery).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, username, email, password_hash, role, status, quota_cpu, quota_ram_mb, quota_disk_mb
		FROM users
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, opts.Limit, opts.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	users := make([]model.User, 0, opts.Limit)
	for rows.Next() {
		var user model.User
		if err := rows.Scan(userScanDest(&user)...); err != nil {
			return nil, 0, err
		}
		users = append(users, user)
	}
	return users, total, rows.Err()
}

func (r *UserRepository) UpdateUser(ctx context.Context, user model.User) error {
	query := `
		UPDATE users
		SET username = $2,
		    email = $3,
		    password_hash = $4,
		    role = $5,
		    status = $6,
		    quota_cpu = $7,
		    quota_ram_mb = $8,
		    quota_disk_mb = $9
		WHERE id = $1
	`

	result, err := r.db.ExecContext(ctx, query, user.ID, user.Username, user.Email, user.PasswordHash, user.Role, user.Status, user.QuotaCPU, user.QuotaRAMMB, user.QuotaDiskMB)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrAlreadyExists
		}
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *UserRepository) CountActiveAdmins(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM users WHERE role = $1 AND status = $2`

	var total int
	if err := r.db.QueryRowContext(ctx, query, model.RoleAdmin, model.StatusActive).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func userScanDest(user *model.User) []any {
	return []any{
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.Status,
		&user.QuotaCPU,
		&user.QuotaRAMMB,
		&user.QuotaDiskMB,
	}
}
