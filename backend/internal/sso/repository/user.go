package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/domain"
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

func (r *UserRepository) SaveUser(ctx context.Context, user domain.User) error {
	query := `
		INSERT INTO users (id, username, email, password_hash, role)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := r.db.ExecContext(ctx, query, user.ID, user.Username, user.Email, user.PasswordHash, user.Role)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrAlreadyExists
		}
		return err
	}

	return nil
}

func (r *UserRepository) GetUserByUsername(ctx context.Context, username string) (domain.User, error) {
	query := `
		SELECT id, username, email, password_hash, role, quota_cpu, quota_ram_mb, quota_disk_mb
		FROM users
		WHERE username = $1
	`

	var user domain.User
	err := r.db.QueryRowContext(ctx, query, username).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.QuotaCPU,
		&user.QuotaRAMMB,
		&user.QuotaDiskMB,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, apperrors.ErrNotFound
		}
		return domain.User{}, err
	}

	return user, nil
}

func (r *UserRepository) GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	query := `
		SELECT id, username, email, password_hash, role, quota_cpu, quota_ram_mb, quota_disk_mb
		FROM users
		WHERE id = $1
	`

	var user domain.User
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.QuotaCPU,
		&user.QuotaRAMMB,
		&user.QuotaDiskMB,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, apperrors.ErrNotFound
		}
		return domain.User{}, err
	}

	return user, nil
}

func (r *UserRepository) GetUsersByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.User, error) {
	if len(ids) == 0 {
		return []domain.User{}, nil
	}

	query := `
		SELECT id, username, email, password_hash, role, quota_cpu, quota_ram_mb, quota_disk_mb
		FROM users
		WHERE id = ANY($1)
	`

	rows, err := r.db.QueryContext(ctx, query, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []domain.User
	for rows.Next() {
		var user domain.User
		if err := rows.Scan(
			&user.ID,
			&user.Username,
			&user.Email,
			&user.PasswordHash,
			&user.Role,
			&user.QuotaCPU,
			&user.QuotaRAMMB,
			&user.QuotaDiskMB,
		); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}
