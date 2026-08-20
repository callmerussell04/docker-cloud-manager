package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/model"
	. "github.com/callmerussell04/docker-cloud-manager/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/permissions"
	ssomocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/sso/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestAuthServiceRegisterLoginRefreshAndVerify(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := ssomocks.NewUserRepository(t)
	tokens := ssomocks.NewTokenProvider(t)
	svc := NewAuthService(repo, tokens)
	var saved model.User

	repo.EXPECT().
		SaveUser(mock.Anything, mock.AnythingOfType("model.User")).
		Run(func(ctx context.Context, user model.User) {
			saved = user
		}).
		Return(nil)

	id, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, id)
	require.Equal(t, "alice", saved.Username)
	require.Equal(t, model.RoleUser, saved.Role)
	require.Equal(t, model.StatusActive, saved.Status)
	require.Equal(t, model.AuthSourceLocal, saved.AuthSource)
	require.Equal(t, int64(2048), saved.QuotaRAMMB)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(saved.PasswordHash), []byte("secret123")))

	saved.ID = userID
	repo.EXPECT().GetUserByUsername(mock.Anything, "alice").Return(saved, nil)
	tokens.EXPECT().GenerateTokens(saved).Return("access", "refresh", nil).Once()
	access, refresh, err := svc.Login(ctx, "alice", "secret123")
	require.NoError(t, err)
	require.Equal(t, "access", access)
	require.Equal(t, "refresh", refresh)

	tokens.EXPECT().ValidateRefreshToken("refresh").Return(userID, nil)
	repo.EXPECT().GetUserByID(mock.Anything, userID).Return(saved, nil).Once()
	tokens.EXPECT().GenerateTokens(saved).Return("access2", "refresh2", nil).Once()
	access, refresh, err = svc.Refresh(ctx, "refresh")
	require.NoError(t, err)
	require.Equal(t, "access2", access)
	require.Equal(t, "refresh2", refresh)

	tokens.EXPECT().ValidateAccessToken("access2").Return(userID, nil)
	repo.EXPECT().GetUserByID(mock.Anything, userID).Return(saved, nil).Once()
	verified, err := svc.VerifyAccessToken(ctx, "access2")
	require.NoError(t, err)
	require.Equal(t, userID, verified.ID)
}

func TestAuthServiceRejectsDisabledOrInvalidLocalAuth(t *testing.T) {
	ctx := context.Background()
	repo := ssomocks.NewUserRepository(t)
	tokens := ssomocks.NewTokenProvider(t)
	svc := NewAuthServiceWithConfig(repo, tokens, nil, Config{
		LocalLoginEnabled:    false,
		LocalRegisterEnabled: false,
	})

	_, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	_, _, err = svc.Login(ctx, "alice", "secret123")
	require.ErrorIs(t, err, apperrors.ErrNotFound)

	localSvc := NewAuthService(repo, tokens)
	repo.EXPECT().GetUserByUsername(mock.Anything, "missing").Return(model.User{}, apperrors.ErrNotFound)
	_, _, err = localSvc.Login(ctx, "missing", "secret123")
	require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)

	oidcUser := userWithPassword(uuid.New(), "oidc-user", "secret123", model.RoleUser, model.StatusActive)
	oidcUser.AuthSource = model.AuthSourceOIDC
	repo.EXPECT().GetUserByUsername(mock.Anything, "oidc-user").Return(oidcUser, nil)
	_, _, err = localSvc.Login(ctx, "oidc-user", "secret123")
	require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)
}

func TestAuthServicePermissions(t *testing.T) {
	ctx := context.Background()
	adminID := uuid.New()
	userID := uuid.New()
	repo := ssomocks.NewUserRepository(t)
	tokens := ssomocks.NewTokenProvider(t)
	svc := NewAuthService(repo, tokens)

	tokens.EXPECT().ValidateAccessToken("admin-token").Return(adminID, nil)
	repo.EXPECT().GetUserByID(mock.Anything, adminID).Return(model.User{ID: adminID, Role: model.RoleAdmin, Status: model.StatusActive}, nil)
	user, allowed, err := svc.CheckPermission(ctx, "admin-token", permissions.UsersAdminUpdate)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, adminID, user.ID)

	tokens.EXPECT().ValidateAccessToken("user-token").Return(userID, nil)
	repo.EXPECT().GetUserByID(mock.Anything, userID).Return(model.User{ID: userID, Role: model.RoleUser, Status: model.StatusActive}, nil)
	_, allowed, err = svc.CheckPermission(ctx, "user-token", permissions.UsersAdminUpdate)
	require.NoError(t, err)
	require.False(t, allowed)

	_, _, err = svc.CheckPermission(ctx, "token", "unknown.permission")
	require.ErrorIs(t, err, apperrors.ErrBadRequest)
}

func TestAuthServiceUserManagement(t *testing.T) {
	ctx := context.Background()
	repo := ssomocks.NewUserRepository(t)
	tokens := ssomocks.NewTokenProvider(t)
	svc := NewAuthService(repo, tokens)
	var created model.User

	repo.EXPECT().
		SaveUser(mock.Anything, mock.AnythingOfType("model.User")).
		Run(func(ctx context.Context, user model.User) {
			created = user
		}).
		Return(nil).
		Once()
	user, err := svc.CreateUser(ctx, CreateUserInput{
		Username: "admin", Email: "admin@example.com", Password: "secret123",
		Role: model.RoleAdmin, Status: model.StatusActive, QuotaCPU: 2, QuotaRAMMB: 4096, QuotaDiskMB: 8192,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, user.ID)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(created.PasswordHash), []byte("secret123")))

	repo.EXPECT().SaveUser(mock.Anything, mock.AnythingOfType("model.User")).Return(apperrors.ErrAlreadyExists)
	_, err = svc.CreateUser(ctx, CreateUserInput{
		Username: "admin", Email: "admin@example.com", Password: "secret123",
		Role: model.RoleAdmin, Status: model.StatusActive, QuotaCPU: 2, QuotaRAMMB: 4096, QuotaDiskMB: 8192,
	})
	require.ErrorIs(t, err, apperrors.ErrAlreadyExists)

	existing := userWithPassword(uuid.New(), "alice", "secret123", model.RoleUser, model.StatusActive)
	var updated model.User
	repo.EXPECT().GetUserByID(mock.Anything, existing.ID).Return(existing, nil).Once()
	repo.EXPECT().
		UpdateUser(mock.Anything, mock.AnythingOfType("model.User")).
		Run(func(ctx context.Context, user model.User) {
			updated = user
		}).
		Return(nil).
		Once()
	updatedUser, err := svc.UpdateUser(ctx, existing.ID, UpdateUserInput{
		Username: "alice2", Email: "alice2@example.com", Password: "newsecret",
		Role: model.RoleAdmin, Status: model.StatusActive, QuotaCPU: 3, QuotaRAMMB: 6144, QuotaDiskMB: 10240,
	})
	require.NoError(t, err)
	require.Equal(t, "alice2", updatedUser.Username)
	require.Equal(t, int64(10240), updated.QuotaDiskMB)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("newsecret")))

	repo.EXPECT().GetUserByID(mock.Anything, existing.ID).Return(updated, nil).Once()
	repo.EXPECT().CountActiveAdmins(mock.Anything).Return(2, nil).Once()
	repo.EXPECT().UpdateUser(mock.Anything, mock.MatchedBy(func(user model.User) bool {
		return user.ID == existing.ID && user.Status == model.StatusDeactivated
	})).Return(nil).Once()
	deactivated, err := svc.DeactivateUser(ctx, existing.ID)
	require.NoError(t, err)
	require.Equal(t, model.StatusDeactivated, deactivated.Status)

	repo.EXPECT().GetUserByID(mock.Anything, existing.ID).Return(deactivated, nil).Once()
	repo.EXPECT().UpdateUser(mock.Anything, mock.MatchedBy(func(user model.User) bool {
		return user.ID == existing.ID && user.Status == model.StatusActive
	})).Return(nil).Once()
	reactivated, err := svc.ReactivateUser(ctx, existing.ID)
	require.NoError(t, err)
	require.Equal(t, model.StatusActive, reactivated.Status)
}

func TestAuthServiceRejectsInvalidUserInputAndLastAdminChanges(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		input CreateUserInput
	}{
		{name: "bad role", input: CreateUserInput{Username: "u", Email: "u@example.com", Password: "secret123", Role: "owner", Status: model.StatusActive, QuotaCPU: 1, QuotaRAMMB: 1, QuotaDiskMB: 1}},
		{name: "bad status", input: CreateUserInput{Username: "u", Email: "u@example.com", Password: "secret123", Role: model.RoleUser, Status: "locked", QuotaCPU: 1, QuotaRAMMB: 1, QuotaDiskMB: 1}},
		{name: "bad quota", input: CreateUserInput{Username: "u", Email: "u@example.com", Password: "secret123", Role: model.RoleUser, Status: model.StatusActive, QuotaCPU: 0, QuotaRAMMB: 1, QuotaDiskMB: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewAuthService(ssomocks.NewUserRepository(t), ssomocks.NewTokenProvider(t))
			_, err := svc.CreateUser(ctx, tt.input)
			require.ErrorIs(t, err, apperrors.ErrBadRequest)
		})
	}

	admin := userWithPassword(uuid.New(), "admin", "secret123", model.RoleAdmin, model.StatusActive)
	repo := ssomocks.NewUserRepository(t)
	svc := NewAuthService(repo, ssomocks.NewTokenProvider(t))
	repo.EXPECT().GetUserByID(mock.Anything, admin.ID).Return(admin, nil)
	repo.EXPECT().CountActiveAdmins(mock.Anything).Return(1, nil)
	_, err := svc.DeactivateUser(ctx, admin.ID)
	require.ErrorIs(t, err, apperrors.ErrConflict)
}

func TestAuthServiceEnsureBootstrapAdmin(t *testing.T) {
	ctx := context.Background()
	t.Run("creates missing admin", func(t *testing.T) {
		repo := ssomocks.NewUserRepository(t)
		svc := NewAuthService(repo, ssomocks.NewTokenProvider(t))
		var saved model.User
		repo.EXPECT().GetUserByUsername(mock.Anything, "admin").Return(model.User{}, apperrors.ErrNotFound)
		repo.EXPECT().SaveUser(mock.Anything, mock.AnythingOfType("model.User")).Run(func(ctx context.Context, user model.User) {
			saved = user
		}).Return(nil)

		err := svc.EnsureBootstrapAdmin(ctx, BootstrapAdminConfig{Username: "admin", Email: "admin@example.com", Password: "secret123"})
		require.NoError(t, err)
		require.Equal(t, model.RoleAdmin, saved.Role)
		require.Equal(t, model.StatusActive, saved.Status)
		require.Equal(t, model.AuthSourceLocal, saved.AuthSource)
	})

	t.Run("reactivates existing admin", func(t *testing.T) {
		repo := ssomocks.NewUserRepository(t)
		svc := NewAuthService(repo, ssomocks.NewTokenProvider(t))
		admin := userWithPassword(uuid.New(), "admin", "secret123", model.RoleAdmin, model.StatusDeactivated)
		repo.EXPECT().GetUserByUsername(mock.Anything, "admin").Return(admin, nil)
		repo.EXPECT().UpdateUser(mock.Anything, mock.MatchedBy(func(user model.User) bool {
			return user.ID == admin.ID && user.Status == model.StatusActive
		})).Return(nil)
		require.NoError(t, svc.EnsureBootstrapAdmin(ctx, BootstrapAdminConfig{Username: "admin", Email: "admin@example.com", Password: "secret123"}))
	})

	t.Run("rejects non-admin username", func(t *testing.T) {
		repo := ssomocks.NewUserRepository(t)
		svc := NewAuthService(repo, ssomocks.NewTokenProvider(t))
		repo.EXPECT().GetUserByUsername(mock.Anything, "admin").Return(model.User{Username: "admin", Role: model.RoleUser, Status: model.StatusActive}, nil)
		err := svc.EnsureBootstrapAdmin(ctx, BootstrapAdminConfig{Username: "admin", Email: "admin@example.com", Password: "secret123"})
		require.ErrorIs(t, err, apperrors.ErrConflict)
	})
}

func TestAuthServiceOIDCCreateUpdateStateAndCollisions(t *testing.T) {
	ctx := context.Background()
	repo := ssomocks.NewUserRepository(t)
	tokens := ssomocks.NewTokenProvider(t)
	provider := ssomocks.NewOIDCProvider(t)
	svc := NewAuthServiceWithConfig(repo, tokens, provider, Config{
		OIDCEnabled:      true,
		OIDCProviderName: "keycloak",
		OIDCAdminGroups:  []string{"/dcm-admins"},
		OIDCDefaultRole:  model.RoleUser,
	})
	expectAuthCodeURL(provider)

	state, binding := startOIDCTestLogin(t, svc)
	provider.EXPECT().ExchangeCode(mock.Anything, "code", mock.AnythingOfType("string")).Return(OIDCIdentity{
		Subject: "subject-1", PreferredUsername: "Alice.External", Email: "alice@example.com", Groups: []string{"/dcm-admins"},
	}, nil).Once()
	repo.EXPECT().GetUserByExternalIdentity(mock.Anything, "keycloak", "subject-1").Return(model.User{}, apperrors.ErrNotFound).Once()
	repo.EXPECT().GetUserByUsername(mock.Anything, "alice-external").Return(model.User{}, apperrors.ErrNotFound).Once()
	repo.EXPECT().GetUserByEmail(mock.Anything, "alice@example.com").Return(model.User{}, apperrors.ErrNotFound).Once()
	var created model.User
	repo.EXPECT().SaveUser(mock.Anything, mock.AnythingOfType("model.User")).Run(func(ctx context.Context, user model.User) {
		created = user
	}).Return(nil).Once()
	tokens.EXPECT().GenerateTokens(mock.AnythingOfType("model.User")).Return("access", "refresh", nil).Once()

	access, refresh, redirectAfter, err := svc.CompleteOIDCCallback(ctx, "keycloak", "code", state, binding)
	require.NoError(t, err)
	require.Equal(t, "access", access)
	require.Equal(t, "refresh", refresh)
	require.Equal(t, "/", redirectAfter)
	require.Equal(t, "alice-external", created.Username)
	require.Equal(t, "alice@example.com", created.Email)
	require.Equal(t, model.RoleAdmin, created.Role)
	require.Equal(t, model.AuthSourceOIDC, created.AuthSource)
	require.NotNil(t, created.LastLoginAt)

	_, _, _, err = svc.CompleteOIDCCallback(ctx, "keycloak", "code", state, binding)
	require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)

	existing := created
	existing.Role = model.RoleAdmin
	state, binding = startOIDCTestLogin(t, svc)
	provider.EXPECT().ExchangeCode(mock.Anything, "code2", mock.AnythingOfType("string")).Return(OIDCIdentity{
		Subject: "subject-1", PreferredUsername: "Alice.Updated", Email: "alice.updated@example.com",
	}, nil).Once()
	repo.EXPECT().GetUserByExternalIdentity(mock.Anything, "keycloak", "subject-1").Return(existing, nil).Once()
	repo.EXPECT().GetUserByUsername(mock.Anything, "alice-updated").Return(model.User{}, apperrors.ErrNotFound).Once()
	repo.EXPECT().GetUserByEmail(mock.Anything, "alice.updated@example.com").Return(model.User{}, apperrors.ErrNotFound).Once()
	repo.EXPECT().CountActiveAdmins(mock.Anything).Return(2, nil).Once()
	var updated model.User
	repo.EXPECT().UpdateUser(mock.Anything, mock.AnythingOfType("model.User")).Run(func(ctx context.Context, user model.User) {
		updated = user
	}).Return(nil).Once()
	tokens.EXPECT().GenerateTokens(mock.AnythingOfType("model.User")).Return("access2", "refresh2", nil).Once()
	_, _, _, err = svc.CompleteOIDCCallback(ctx, "keycloak", "code2", state, binding)
	require.NoError(t, err)
	require.Equal(t, existing.ID, updated.ID)
	require.Equal(t, "alice-updated", updated.Username)
	require.Equal(t, model.RoleUser, updated.Role)

	state, binding = startOIDCTestLogin(t, svc)
	provider.EXPECT().ExchangeCode(mock.Anything, "code3", mock.AnythingOfType("string")).Return(OIDCIdentity{
		Subject: "Subject-XYZ-123", PreferredUsername: "Alice.External", Email: "alice@example.com",
	}, nil).Once()
	repo.EXPECT().GetUserByExternalIdentity(mock.Anything, "keycloak", "Subject-XYZ-123").Return(model.User{}, apperrors.ErrNotFound).Once()
	repo.EXPECT().GetUserByUsername(mock.Anything, "alice-external").Return(model.User{ID: uuid.New()}, nil).Once()
	repo.EXPECT().GetUserByUsername(mock.Anything, "alice-external-subjectx").Return(model.User{}, apperrors.ErrNotFound).Once()
	repo.EXPECT().GetUserByEmail(mock.Anything, "alice@example.com").Return(model.User{ID: uuid.New()}, nil).Once()
	repo.EXPECT().GetUserByEmail(mock.Anything, mock.MatchedBy(func(email string) bool {
		return strings.HasPrefix(email, "oidc-") && strings.HasSuffix(email, "@oidc.local")
	})).Return(model.User{}, apperrors.ErrNotFound).Once()
	var collided model.User
	repo.EXPECT().SaveUser(mock.Anything, mock.AnythingOfType("model.User")).Run(func(ctx context.Context, user model.User) {
		collided = user
	}).Return(nil).Once()
	tokens.EXPECT().GenerateTokens(mock.AnythingOfType("model.User")).Return("access3", "refresh3", nil).Once()
	_, _, _, err = svc.CompleteOIDCCallback(ctx, "keycloak", "code3", state, binding)
	require.NoError(t, err)
	require.Equal(t, "alice-external-subjectx", collided.Username)
	require.True(t, strings.HasSuffix(collided.Email, "@oidc.local"))
}

func TestAuthServiceOIDCRejectsInvalidStateExpiredStateAndLastAdminDowngrade(t *testing.T) {
	ctx := context.Background()
	repo := ssomocks.NewUserRepository(t)
	tokens := ssomocks.NewTokenProvider(t)
	provider := ssomocks.NewOIDCProvider(t)
	svc := NewAuthServiceWithConfig(repo, tokens, provider, Config{
		OIDCEnabled:      true,
		OIDCProviderName: "keycloak",
		OIDCDefaultRole:  model.RoleUser,
		OIDCStateTTL:     time.Nanosecond,
	})
	expectAuthCodeURL(provider)

	state, binding := startOIDCTestLogin(t, svc)
	_, _, _, err := svc.CompleteOIDCCallback(ctx, "keycloak", "code", state, "bad-binding")
	require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)

	time.Sleep(time.Millisecond)
	_, _, _, err = svc.CompleteOIDCCallback(ctx, "keycloak", "code", state, binding)
	require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)

	svc = NewAuthServiceWithConfig(repo, tokens, provider, Config{OIDCEnabled: true, OIDCProviderName: "keycloak", OIDCDefaultRole: model.RoleUser})
	expectAuthCodeURL(provider)
	admin := model.User{ID: uuid.New(), Username: "admin", Email: "admin@example.com", Role: model.RoleAdmin, Status: model.StatusActive, AuthSource: model.AuthSourceOIDC, ExternalProvider: "keycloak", ExternalSubject: "admin-subject"}
	state, binding = startOIDCTestLogin(t, svc)
	provider.EXPECT().ExchangeCode(mock.Anything, "code", mock.AnythingOfType("string")).Return(OIDCIdentity{
		Subject: "admin-subject", PreferredUsername: "admin", Email: "admin@example.com",
	}, nil).Once()
	repo.EXPECT().GetUserByExternalIdentity(mock.Anything, "keycloak", "admin-subject").Return(admin, nil)
	repo.EXPECT().GetUserByUsername(mock.Anything, "admin").Return(admin, nil)
	repo.EXPECT().GetUserByEmail(mock.Anything, "admin@example.com").Return(admin, nil)
	repo.EXPECT().CountActiveAdmins(mock.Anything).Return(1, nil)
	_, _, _, err = svc.CompleteOIDCCallback(ctx, "keycloak", "code", state, binding)
	require.ErrorIs(t, err, apperrors.ErrConflict)
}

func expectAuthCodeURL(provider *ssomocks.OIDCProvider) {
	provider.EXPECT().
		AuthCodeURL(mock.AnythingOfType("string"), mock.AnythingOfType("string")).
		RunAndReturn(func(state, nonce string) string {
			return "https://keycloak.example/authorize?state=" + state + "&nonce=" + nonce
		}).
		Maybe()
}

func startOIDCTestLogin(t *testing.T, svc *AuthService) (string, string) {
	t.Helper()
	authURL, binding, err := svc.StartOIDCLogin(context.Background(), "keycloak", "/")
	require.NoError(t, err)
	require.NotEmpty(t, binding)
	state := authURL[strings.Index(authURL, "state=")+len("state="):]
	if end := strings.IndexByte(state, '&'); end >= 0 {
		state = state[:end]
	}
	require.NotEmpty(t, state)
	return state, binding
}

func userWithPassword(id uuid.UUID, username, password, role, status string) model.User {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return model.User{
		ID:           id,
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
}

func TestAuthServiceMapsTokenAndRepositoryErrors(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := ssomocks.NewUserRepository(t)
	tokens := ssomocks.NewTokenProvider(t)
	svc := NewAuthService(repo, tokens)

	tokens.EXPECT().ValidateRefreshToken("bad-refresh").Return(uuid.Nil, errors.New("invalid"))
	_, _, err := svc.Refresh(ctx, "bad-refresh")
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)

	tokens.EXPECT().ValidateAccessToken("token").Return(userID, nil)
	repo.EXPECT().GetUserByID(mock.Anything, userID).Return(model.User{ID: userID, Status: model.StatusDeactivated}, nil)
	_, err = svc.VerifyAccessToken(ctx, "token")
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
}
