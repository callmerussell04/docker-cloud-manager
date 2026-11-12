package repository

import (
	"context"
	"errors"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) SaveUser(ctx context.Context, user model.User) error {
	query := `
		INSERT INTO users (
			id, username, email, password_hash, role, status,
			auth_source, external_provider, external_subject, external_username, last_login_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err := r.db.Exec(ctx, query,
		user.ID,
		user.Username,
		user.Email,
		nullString(user.PasswordHash),
		user.Role,
		user.Status,
		authSource(user.AuthSource),
		nullString(user.ExternalProvider),
		nullString(user.ExternalSubject),
		nullString(user.ExternalUsername),
		nullTime(user.LastLoginAt),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrAlreadyExists
		}
		return err
	}

	return nil
}

func (r *UserRepository) GetUserByUsername(ctx context.Context, username string) (model.User, error) {
	query := `
		SELECT id, username, email, password_hash, role, status,
		       auth_source, external_provider, external_subject, external_username, last_login_at
		FROM users
		WHERE username = $1
	`

	var user model.User
	err := r.db.QueryRow(ctx, query, username).Scan(userScanDest(&user)...)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, apperrors.ErrNotFound
		}
		return model.User{}, err
	}

	return user, nil
}

func (r *UserRepository) GetUserByEmail(ctx context.Context, email string) (model.User, error) {
	query := `
		SELECT id, username, email, password_hash, role, status,
		       auth_source, external_provider, external_subject, external_username, last_login_at
		FROM users
		WHERE email = $1
	`

	var user model.User
	err := r.db.QueryRow(ctx, query, email).Scan(userScanDest(&user)...)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, apperrors.ErrNotFound
		}
		return model.User{}, err
	}

	return user, nil
}

func (r *UserRepository) GetUserByExternalIdentity(ctx context.Context, provider, subject string) (model.User, error) {
	query := `
		SELECT id, username, email, password_hash, role, status,
		       auth_source, external_provider, external_subject, external_username, last_login_at
		FROM users
		WHERE external_provider = $1 AND external_subject = $2
	`

	var user model.User
	err := r.db.QueryRow(ctx, query, provider, subject).Scan(userScanDest(&user)...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, apperrors.ErrNotFound
		}
		return model.User{}, err
	}

	return user, nil
}

func (r *UserRepository) GetUserByID(ctx context.Context, id uuid.UUID) (model.User, error) {
	query := `
		SELECT id, username, email, password_hash, role, status,
		       auth_source, external_provider, external_subject, external_username, last_login_at
		FROM users
		WHERE id = $1
	`

	var user model.User
	err := r.db.QueryRow(ctx, query, id).Scan(userScanDest(&user)...)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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
		SELECT id, username, email, password_hash, role, status,
		       auth_source, external_provider, external_subject, external_username, last_login_at
		FROM users
		WHERE id = ANY($1)
	`

	rows, err := r.db.Query(ctx, query, ids)
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
	if err := r.db.QueryRow(ctx, countQuery).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, username, email, password_hash, role, status,
		       auth_source, external_provider, external_subject, external_username, last_login_at
		FROM users
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.Query(ctx, query, opts.Limit, opts.Offset)
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
	return updateUser(ctx, r.db, user)
}

type userExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func updateUser(ctx context.Context, executor userExecutor, user model.User) error {
	query := `
		UPDATE users
		SET username = $2,
		    email = $3,
		    password_hash = $4,
		    role = $5,
		    status = $6,
		    auth_source = $7,
		    external_provider = $8,
		    external_subject = $9,
		    external_username = $10,
		    last_login_at = $11
		WHERE id = $1
	`

	result, err := executor.Exec(ctx, query,
		user.ID,
		user.Username,
		user.Email,
		nullString(user.PasswordHash),
		user.Role,
		user.Status,
		authSource(user.AuthSource),
		nullString(user.ExternalProvider),
		nullString(user.ExternalSubject),
		nullString(user.ExternalUsername),
		nullTime(user.LastLoginAt),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperrors.ErrAlreadyExists
		}
		return err
	}

	affected := result.RowsAffected()
	if affected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *UserRepository) CountActiveAdmins(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM users WHERE role = $1 AND status = $2`

	var total int
	if err := r.db.QueryRow(ctx, query, model.RoleAdmin, model.StatusActive).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func userScanDest(user *model.User) []any {
	return []any{
		&user.ID,
		&user.Username,
		&user.Email,
		nullableStringDest{value: &user.PasswordHash},
		&user.Role,
		&user.Status,
		authSourceDest{value: &user.AuthSource},
		nullableStringDest{value: &user.ExternalProvider},
		nullableStringDest{value: &user.ExternalSubject},
		nullableStringDest{value: &user.ExternalUsername},
		&user.LastLoginAt,
	}
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// nullableStringDest scans a nullable text column into a string, NULL as "".
type nullableStringDest struct {
	value *string
}

func (d nullableStringDest) ScanText(v pgtype.Text) error {
	*d.value = v.String
	return nil
}

// authSourceDest scans the auth source, treating NULL or "" as local.
type authSourceDest struct {
	value *string
}

func (d authSourceDest) ScanText(v pgtype.Text) error {
	if v.String == "" {
		*d.value = model.AuthSourceLocal
		return nil
	}
	*d.value = v.String
	return nil
}

func nullTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func authSource(value string) string {
	if value == "" {
		return model.AuthSourceLocal
	}
	return value
}
