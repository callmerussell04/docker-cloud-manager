package service

import (
	"context"
	"errors"
	"sort"
	"strings"
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

func TestLocalAuthDisabled(t *testing.T) {
	repo := newAuthRepoFake()
	svc := NewAuthServiceWithConfig(repo, authTokenFake{userID: uuid.New()}, nil, Config{
		LocalLoginEnabled:    false,
		LocalRegisterEnabled: false,
		OIDCDefaultRole:      model.RoleUser,
	})

	if _, err := svc.Register(context.Background(), "alice", "alice@example.com", "secret123"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("Register error = %v, want ErrNotFound", err)
	}
	if _, _, err := svc.Login(context.Background(), "alice", "secret123"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("Login error = %v, want ErrNotFound", err)
	}
}

func TestOIDCCallbackCreatesAndUpdatesUser(t *testing.T) {
	repo := newAuthRepoFake()
	provider := &oidcProviderFake{
		identity: OIDCIdentity{
			Subject:           "keycloak-subject-1",
			PreferredUsername: "Alice.External",
			Email:             "alice@example.com",
		},
	}
	svc := NewAuthServiceWithConfig(repo, authTokenFake{userID: uuid.New()}, provider, Config{
		LocalLoginEnabled:    true,
		LocalRegisterEnabled: true,
		OIDCEnabled:          true,
		OIDCProviderName:     "keycloak",
		OIDCDefaultRole:      model.RoleUser,
	})

	state, stateBinding := startOIDCTestLogin(t, svc)
	accessToken, refreshToken, _, err := svc.CompleteOIDCCallback(context.Background(), "keycloak", "code", state, stateBinding)
	if err != nil {
		t.Fatalf("CompleteOIDCCallback error = %v", err)
	}
	if accessToken != "access" || refreshToken != "refresh" {
		t.Fatalf("tokens = %q/%q, want access/refresh", accessToken, refreshToken)
	}

	user, err := repo.GetUserByExternalIdentity(context.Background(), "keycloak", "keycloak-subject-1")
	if err != nil {
		t.Fatal(err)
	}
	if user.AuthSource != model.AuthSourceOIDC || user.Username != "alice-external" || user.Role != model.RoleUser || user.LastLoginAt == nil {
		t.Fatalf("unexpected oidc user: %+v", user)
	}

	provider.identity.Email = "alice.updated@example.com"
	provider.identity.PreferredUsername = "alice-updated"
	state, stateBinding = startOIDCTestLogin(t, svc)
	if _, _, _, err := svc.CompleteOIDCCallback(context.Background(), "keycloak", "code", state, stateBinding); err != nil {
		t.Fatalf("second CompleteOIDCCallback error = %v", err)
	}
	updated, err := repo.GetUserByExternalIdentity(context.Background(), "keycloak", "keycloak-subject-1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != user.ID || updated.Email != "alice.updated@example.com" || updated.Username != "alice-updated" {
		t.Fatalf("user was not updated in place: before=%+v after=%+v", user, updated)
	}
}

func TestOIDCCallbackMapsAdminGroup(t *testing.T) {
	repo := newAuthRepoFake()
	provider := &oidcProviderFake{
		identity: OIDCIdentity{
			Subject:           "admin-subject",
			PreferredUsername: "admin-user",
			Email:             "admin.oidc@example.com",
			Groups:            []string{"/dcm-admins"},
		},
	}
	svc := NewAuthServiceWithConfig(repo, authTokenFake{userID: uuid.New()}, provider, Config{
		OIDCEnabled:      true,
		OIDCProviderName: "keycloak",
		OIDCAdminGroups:  []string{"/dcm-admins"},
		OIDCDefaultRole:  model.RoleUser,
	})

	state, stateBinding := startOIDCTestLogin(t, svc)
	if _, _, _, err := svc.CompleteOIDCCallback(context.Background(), "keycloak", "code", state, stateBinding); err != nil {
		t.Fatalf("CompleteOIDCCallback error = %v", err)
	}
	user, err := repo.GetUserByExternalIdentity(context.Background(), "keycloak", "admin-subject")
	if err != nil {
		t.Fatal(err)
	}
	if user.Role != model.RoleAdmin {
		t.Fatalf("role = %q, want admin", user.Role)
	}
}

func TestOIDCCallbackBlocksDeactivatedUser(t *testing.T) {
	repo := newAuthRepoFake()
	user := model.User{
		ID:               uuid.New(),
		Username:         "blocked-user",
		Email:            "blocked@example.com",
		Role:             model.RoleUser,
		Status:           model.StatusDeactivated,
		QuotaCPU:         1,
		QuotaRAMMB:       2048,
		QuotaDiskMB:      5120,
		AuthSource:       model.AuthSourceOIDC,
		ExternalProvider: "keycloak",
		ExternalSubject:  "blocked-subject",
	}
	if err := repo.SaveUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	provider := &oidcProviderFake{
		identity: OIDCIdentity{
			Subject:           "blocked-subject",
			PreferredUsername: "blocked-user",
			Email:             "blocked@example.com",
		},
	}
	svc := NewAuthServiceWithConfig(repo, authTokenFake{userID: uuid.New()}, provider, Config{
		OIDCEnabled:      true,
		OIDCProviderName: "keycloak",
		OIDCDefaultRole:  model.RoleUser,
	})

	state, stateBinding := startOIDCTestLogin(t, svc)
	if _, _, _, err := svc.CompleteOIDCCallback(context.Background(), "keycloak", "code", state, stateBinding); !errors.Is(err, apperrors.ErrInvalidCredentials) {
		t.Fatalf("CompleteOIDCCallback error = %v, want ErrInvalidCredentials", err)
	}
}

func TestOIDCCallbackRequiresStateBinding(t *testing.T) {
	repo := newAuthRepoFake()
	provider := &oidcProviderFake{
		identity: OIDCIdentity{
			Subject:           "bound-subject",
			PreferredUsername: "bound-user",
			Email:             "bound@example.com",
		},
	}
	svc := NewAuthServiceWithConfig(repo, authTokenFake{userID: uuid.New()}, provider, Config{
		OIDCEnabled:      true,
		OIDCProviderName: "keycloak",
		OIDCDefaultRole:  model.RoleUser,
	})

	state, stateBinding := startOIDCTestLogin(t, svc)
	if _, _, _, err := svc.CompleteOIDCCallback(context.Background(), "keycloak", "code", state, "wrong-binding"); !errors.Is(err, apperrors.ErrInvalidCredentials) {
		t.Fatalf("CompleteOIDCCallback with wrong binding error = %v, want ErrInvalidCredentials", err)
	}
	if _, _, _, err := svc.CompleteOIDCCallback(context.Background(), "keycloak", "code", state, stateBinding); err != nil {
		t.Fatalf("CompleteOIDCCallback with correct binding after failed attempt error = %v", err)
	}
}

func TestOIDCCallbackPreventsLastAdminDowngrade(t *testing.T) {
	repo := newAuthRepoFake()
	admin := model.User{
		ID:               uuid.New(),
		Username:         "oidc-admin",
		Email:            "oidc-admin@example.com",
		Role:             model.RoleAdmin,
		Status:           model.StatusActive,
		QuotaCPU:         1,
		QuotaRAMMB:       2048,
		QuotaDiskMB:      5120,
		AuthSource:       model.AuthSourceOIDC,
		ExternalProvider: "keycloak",
		ExternalSubject:  "oidc-admin-subject",
	}
	if err := repo.SaveUser(context.Background(), admin); err != nil {
		t.Fatal(err)
	}
	provider := &oidcProviderFake{
		identity: OIDCIdentity{
			Subject:           "oidc-admin-subject",
			PreferredUsername: "oidc-admin",
			Email:             "oidc-admin@example.com",
		},
	}
	svc := NewAuthServiceWithConfig(repo, authTokenFake{userID: uuid.New()}, provider, Config{
		OIDCEnabled:      true,
		OIDCProviderName: "keycloak",
		OIDCDefaultRole:  model.RoleUser,
	})

	state, stateBinding := startOIDCTestLogin(t, svc)
	if _, _, _, err := svc.CompleteOIDCCallback(context.Background(), "keycloak", "code", state, stateBinding); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("CompleteOIDCCallback error = %v, want ErrConflict", err)
	}
	updated, err := repo.GetUserByID(context.Background(), admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Role != model.RoleAdmin {
		t.Fatalf("role = %q, want admin", updated.Role)
	}
}

func TestOIDCCallbackAllowsAdminDowngradeWhenAnotherAdminExists(t *testing.T) {
	repo := newAuthRepoFake()
	seedUser(t, repo, "local-admin", model.RoleAdmin, model.StatusActive)
	oidcAdmin := model.User{
		ID:               uuid.New(),
		Username:         "oidc-admin",
		Email:            "oidc-admin@example.com",
		Role:             model.RoleAdmin,
		Status:           model.StatusActive,
		QuotaCPU:         1,
		QuotaRAMMB:       2048,
		QuotaDiskMB:      5120,
		AuthSource:       model.AuthSourceOIDC,
		ExternalProvider: "keycloak",
		ExternalSubject:  "oidc-admin-subject",
	}
	if err := repo.SaveUser(context.Background(), oidcAdmin); err != nil {
		t.Fatal(err)
	}
	provider := &oidcProviderFake{
		identity: OIDCIdentity{
			Subject:           "oidc-admin-subject",
			PreferredUsername: "oidc-admin",
			Email:             "oidc-admin@example.com",
		},
	}
	svc := NewAuthServiceWithConfig(repo, authTokenFake{userID: uuid.New()}, provider, Config{
		OIDCEnabled:      true,
		OIDCProviderName: "keycloak",
		OIDCDefaultRole:  model.RoleUser,
	})

	state, stateBinding := startOIDCTestLogin(t, svc)
	if _, _, _, err := svc.CompleteOIDCCallback(context.Background(), "keycloak", "code", state, stateBinding); err != nil {
		t.Fatalf("CompleteOIDCCallback error = %v", err)
	}
	updated, err := repo.GetUserByID(context.Background(), oidcAdmin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Role != model.RoleUser {
		t.Fatalf("role = %q, want user", updated.Role)
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
		if user.ExternalProvider != "" && user.ExternalSubject != "" &&
			existing.ExternalProvider == user.ExternalProvider && existing.ExternalSubject == user.ExternalSubject {
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
		if id != user.ID && user.ExternalProvider != "" && user.ExternalSubject != "" &&
			existing.ExternalProvider == user.ExternalProvider && existing.ExternalSubject == user.ExternalSubject {
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

func (r *authRepoFake) GetUserByEmail(ctx context.Context, email string) (model.User, error) {
	for _, user := range r.users {
		if user.Email == email {
			return user, nil
		}
	}
	return model.User{}, apperrors.ErrNotFound
}

func (r *authRepoFake) GetUserByExternalIdentity(ctx context.Context, provider, subject string) (model.User, error) {
	for _, user := range r.users {
		if user.ExternalProvider == provider && user.ExternalSubject == subject {
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

type oidcProviderFake struct {
	identity OIDCIdentity
}

func (f *oidcProviderFake) AuthCodeURL(state, nonce string) string {
	return "https://keycloak.example/authorize?state=" + state + "&nonce=" + nonce
}

func (f *oidcProviderFake) ExchangeCode(ctx context.Context, code, nonce string) (OIDCIdentity, error) {
	return f.identity, nil
}

func startOIDCTestLogin(t *testing.T, svc *AuthService) (string, string) {
	t.Helper()
	authURL, stateBinding, err := svc.StartOIDCLogin(context.Background(), "keycloak", "/")
	if err != nil {
		t.Fatalf("StartOIDCLogin error = %v", err)
	}
	if stateBinding == "" {
		t.Fatal("StartOIDCLogin returned empty state binding")
	}
	prefix := "state="
	idx := strings.Index(authURL, prefix)
	if idx < 0 {
		t.Fatalf("auth url does not contain state: %s", authURL)
	}
	state := authURL[idx+len(prefix):]
	if end := strings.IndexByte(state, '&'); end >= 0 {
		state = state[:end]
	}
	return state, stateBinding
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
		AuthSource:   model.AuthSourceLocal,
	}
	if err := repo.SaveUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	return user
}
