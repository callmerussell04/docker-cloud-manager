package service

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

func TestCreateUserHashesPasswordAndStoresFields(t *testing.T) {
	repo := newAuthRepoFake()
	svc := NewAuthService(repo, authTokenFake{userID: uuid.New()})

	user, err := svc.CreateUser(context.Background(), CreateUserInput{
		Username:    "alice",
		Email:       "alice@example.com",
		Password:    "secret123",
		Role:        model.RoleAdmin,
		Status:      model.StatusActive,
		QuotaCPU:    2,
		QuotaRAMMB:  4096,
		QuotaDiskMB: 8192,
	})
	if err != nil {
		t.Fatalf("CreateUser returned error: %v", err)
	}

	if user.PasswordHash == "secret123" {
		t.Fatal("password was stored without hashing")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("secret123")); err != nil {
		t.Fatalf("stored password hash is invalid: %v", err)
	}
	if user.Role != model.RoleAdmin || user.Status != model.StatusActive || user.QuotaRAMMB != 4096 {
		t.Fatalf("user fields were not stored: %+v", user)
	}
}

func TestCreateUserDuplicateReturnsAlreadyExists(t *testing.T) {
	repo := newAuthRepoFake()
	svc := NewAuthService(repo, authTokenFake{userID: uuid.New()})
	input := CreateUserInput{
		Username:    "alice",
		Email:       "alice@example.com",
		Password:    "secret123",
		Role:        model.RoleUser,
		Status:      model.StatusActive,
		QuotaCPU:    1,
		QuotaRAMMB:  2048,
		QuotaDiskMB: 5120,
	}

	if _, err := svc.CreateUser(context.Background(), input); err != nil {
		t.Fatalf("first CreateUser returned error: %v", err)
	}
	if _, err := svc.CreateUser(context.Background(), input); !errors.Is(err, apperrors.ErrAlreadyExists) {
		t.Fatalf("duplicate CreateUser error = %v, want ErrAlreadyExists", err)
	}
}

func TestUpdateUserChangesFieldsAndPassword(t *testing.T) {
	repo := newAuthRepoFake()
	svc := NewAuthService(repo, authTokenFake{userID: uuid.New()})
	user := seedUser(t, repo, "alice", model.RoleUser, model.StatusActive)

	updated, err := svc.UpdateUser(context.Background(), user.ID, UpdateUserInput{
		Username:    "alice2",
		Email:       "alice2@example.com",
		Password:    "newsecret",
		Role:        model.RoleAdmin,
		Status:      model.StatusActive,
		QuotaCPU:    3,
		QuotaRAMMB:  6144,
		QuotaDiskMB: 10240,
	})
	if err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}

	if updated.Username != "alice2" || updated.Role != model.RoleAdmin || updated.QuotaDiskMB != 10240 {
		t.Fatalf("updated fields mismatch: %+v", updated)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("newsecret")); err != nil {
		t.Fatalf("updated password hash is invalid: %v", err)
	}
}

func TestDeactivatedUserCannotLoginOrVerifyToken(t *testing.T) {
	repo := newAuthRepoFake()
	userID := uuid.New()
	svc := NewAuthService(repo, authTokenFake{userID: userID})
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveUser(context.Background(), model.User{
		ID:           userID,
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: string(hash),
		Role:         model.RoleUser,
		Status:       model.StatusDeactivated,
		QuotaCPU:     1,
		QuotaRAMMB:   2048,
		QuotaDiskMB:  5120,
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := svc.Login(context.Background(), "alice", "secret123"); !errors.Is(err, apperrors.ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.VerifyAccessToken(context.Background(), "token"); !errors.Is(err, apperrors.ErrInvalidToken) {
		t.Fatalf("VerifyAccessToken error = %v, want ErrInvalidToken", err)
	}
}

func TestListUsersReturnsPaginationAndTotalCount(t *testing.T) {
	repo := newAuthRepoFake()
	svc := NewAuthService(repo, authTokenFake{userID: uuid.New()})
	seedUser(t, repo, "a", model.RoleUser, model.StatusActive)
	seedUser(t, repo, "b", model.RoleUser, model.StatusActive)
	seedUser(t, repo, "c", model.RoleUser, model.StatusActive)

	users, total, err := svc.ListUsers(context.Background(), 2, 1)
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(users))
	}
}

func TestCannotDeactivateLastActiveAdmin(t *testing.T) {
	repo := newAuthRepoFake()
	svc := NewAuthService(repo, authTokenFake{userID: uuid.New()})
	admin := seedUser(t, repo, "admin", model.RoleAdmin, model.StatusActive)

	_, err := svc.DeactivateUser(context.Background(), admin.ID)
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("DeactivateUser error = %v, want ErrConflict", err)
	}
}

type authRepoFake struct {
	users map[uuid.UUID]model.User
}

func newAuthRepoFake() *authRepoFake {
	return &authRepoFake{users: map[uuid.UUID]model.User{}}
}

func (r *authRepoFake) SaveUser(ctx context.Context, user model.User) error {
	for _, existing := range r.users {
		if existing.Username == user.Username || existing.Email == user.Email {
			return apperrors.ErrAlreadyExists
		}
	}
	r.users[user.ID] = user
	return nil
}

func (r *authRepoFake) UpdateUser(ctx context.Context, user model.User) error {
	if _, ok := r.users[user.ID]; !ok {
		return apperrors.ErrNotFound
	}
	for id, existing := range r.users {
		if id != user.ID && (existing.Username == user.Username || existing.Email == user.Email) {
			return apperrors.ErrAlreadyExists
		}
	}
	r.users[user.ID] = user
	return nil
}

func (r *authRepoFake) GetUserByUsername(ctx context.Context, username string) (model.User, error) {
	for _, user := range r.users {
		if user.Username == username {
			return user, nil
		}
	}
	return model.User{}, apperrors.ErrNotFound
}

func (r *authRepoFake) GetUserByID(ctx context.Context, id uuid.UUID) (model.User, error) {
	user, ok := r.users[id]
	if !ok {
		return model.User{}, apperrors.ErrNotFound
	}
	return user, nil
}

func (r *authRepoFake) GetUsersByIDs(ctx context.Context, ids []uuid.UUID) ([]model.User, error) {
	users := make([]model.User, 0, len(ids))
	for _, id := range ids {
		if user, ok := r.users[id]; ok {
			users = append(users, user)
		}
	}
	return users, nil
}

func (r *authRepoFake) ListUsers(ctx context.Context, opts model.ListUsersOptions) ([]model.User, int, error) {
	users := make([]model.User, 0, len(r.users))
	for _, user := range r.users {
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].Username < users[j].Username
	})
	total := len(users)
	if opts.Offset >= total {
		return []model.User{}, total, nil
	}
	end := opts.Offset + opts.Limit
	if end > total {
		end = total
	}
	return users[opts.Offset:end], total, nil
}

func (r *authRepoFake) CountActiveAdmins(ctx context.Context) (int, error) {
	var count int
	for _, user := range r.users {
		if user.Role == model.RoleAdmin && user.Status == model.StatusActive {
			count++
		}
	}
	return count, nil
}

type authTokenFake struct {
	userID uuid.UUID
}

func (f authTokenFake) GenerateTokens(user model.User) (string, string, error) {
	return "access", "refresh", nil
}

func (f authTokenFake) ValidateAccessToken(token string) (uuid.UUID, error) {
	return f.userID, nil
}

func (f authTokenFake) ValidateRefreshToken(token string) (uuid.UUID, error) {
	return f.userID, nil
}

func seedUser(t *testing.T, repo *authRepoFake, username, role, status string) model.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	user := model.User{
		ID:           uuid.New(),
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: string(hash),
		Role:         role,
		Status:       status,
		QuotaCPU:     1,
		QuotaRAMMB:   2048,
		QuotaDiskMB:  5120,
	}
	if err := repo.SaveUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	return user
}
