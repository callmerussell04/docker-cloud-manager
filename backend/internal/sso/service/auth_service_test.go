package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/permissions"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	ssomocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/ssotest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestAuthServiceRegisterStoresPasswordHash(t *testing.T) {
	ctx := context.Background()
	repo := ssomocks.NewUserRepository(t)
	tokens := ssomocks.NewTokenProvider(t)
	svc := ssotest.NewAuthService(repo, tokens, testSessions(t, repo))
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
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(saved.PasswordHash), []byte("secret123")))

}

func TestAuthServiceRejectsDisabledOrInvalidLocalAuth(t *testing.T) {
	ctx := context.Background()
	repo := ssomocks.NewUserRepository(t)
	tokens := ssomocks.NewTokenProvider(t)
	svc := service.NewAuthServiceWithConfig(repo, tokens, testSessions(t, repo), nil, service.Config{
		LocalLoginEnabled:    false,
		LocalRegisterEnabled: false,
	})

	_, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	_, err = svc.Login(ctx, "alice", "secret123")
	require.ErrorIs(t, err, apperrors.ErrNotFound)

	localSvc := ssotest.NewAuthService(repo, tokens, testSessions(t, repo))
	repo.EXPECT().GetUserByUsername(mock.Anything, "missing").Return(model.User{}, apperrors.ErrNotFound)
	_, err = localSvc.Login(ctx, "missing", "secret123")
	require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)

	oidcUser := userWithPassword(uuid.New(), "oidc-user", "secret123", model.RoleUser, model.StatusActive)
	oidcUser.AuthSource = model.AuthSourceOIDC
	repo.EXPECT().GetUserByUsername(mock.Anything, "oidc-user").Return(oidcUser, nil)
	_, err = localSvc.Login(ctx, "oidc-user", "secret123")
	require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)
}

func TestAuthServicePermissions(t *testing.T) {
	ctx := context.Background()
	adminID := uuid.New()
	userID := uuid.New()
	repo := ssomocks.NewUserRepository(t)
	tokens := ssomocks.NewTokenProvider(t)
	sessions := ssomocks.NewSessionRepository(t)
	svc := ssotest.NewAuthService(repo, tokens, sessions)

	tokens.EXPECT().ValidateAccessToken("admin-token").Return(model.AccessIdentity{UserID: adminID}, nil)
	sessions.EXPECT().VerifySession(mock.Anything, model.AccessIdentity{UserID: adminID}, mock.Anything).Return(model.User{ID: adminID, Role: model.RoleAdmin, Status: model.StatusActive}, nil)
	user, allowed, err := svc.CheckPermission(ctx, "admin-token", permissions.UsersAdminUpdate)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, adminID, user.ID)

	tokens.EXPECT().ValidateAccessToken("user-token").Return(model.AccessIdentity{UserID: userID}, nil)
	sessions.EXPECT().VerifySession(mock.Anything, model.AccessIdentity{UserID: userID}, mock.Anything).Return(model.User{ID: userID, Role: model.RoleUser, Status: model.StatusActive}, nil)
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
	svc := ssotest.NewAuthService(repo, tokens, testSessions(t, repo))
	var created model.User

	repo.EXPECT().
		SaveUser(mock.Anything, mock.AnythingOfType("model.User")).
		Run(func(ctx context.Context, user model.User) {
			created = user
		}).
		Return(nil).
		Once()
	user, err := svc.CreateUser(ctx, service.CreateUserInput{
		Username: "admin", Email: "admin@example.com", Password: "secret123",
		Role: model.RoleAdmin, Status: model.StatusActive,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, user.ID)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(created.PasswordHash), []byte("secret123")))

	repo.EXPECT().SaveUser(mock.Anything, mock.AnythingOfType("model.User")).Return(apperrors.ErrAlreadyExists)
	_, err = svc.CreateUser(ctx, service.CreateUserInput{
		Username: "admin", Email: "admin@example.com", Password: "secret123",
		Role: model.RoleAdmin, Status: model.StatusActive,
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
	updatedUser, err := svc.UpdateUser(ctx, existing.ID, service.UpdateUserInput{
		Username: "alice2", Email: "alice2@example.com", Password: "newsecret",
		Role: model.RoleAdmin, Status: model.StatusActive,
	})
	require.NoError(t, err)
	require.Equal(t, "alice2", updatedUser.Username)
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
		input service.CreateUserInput
	}{
		{name: "bad role", input: service.CreateUserInput{Username: "u", Email: "u@example.com", Password: "secret123", Role: "owner", Status: model.StatusActive}},
		{name: "bad status", input: service.CreateUserInput{Username: "u", Email: "u@example.com", Password: "secret123", Role: model.RoleUser, Status: "locked"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := ssotest.NewAuthService(ssomocks.NewUserRepository(t), ssomocks.NewTokenProvider(t), ssomocks.NewSessionRepository(t))
			_, err := svc.CreateUser(ctx, tt.input)
			require.ErrorIs(t, err, apperrors.ErrBadRequest)
		})
	}

	admin := userWithPassword(uuid.New(), "admin", "secret123", model.RoleAdmin, model.StatusActive)
	repo := ssomocks.NewUserRepository(t)
	svc := ssotest.NewAuthService(repo, ssomocks.NewTokenProvider(t), testSessions(t, repo))
	repo.EXPECT().GetUserByID(mock.Anything, admin.ID).Return(admin, nil)
	repo.EXPECT().CountActiveAdmins(mock.Anything).Return(1, nil)
	_, err := svc.DeactivateUser(ctx, admin.ID)
	require.ErrorIs(t, err, apperrors.ErrConflict)
}

func TestAuthServiceEnsureBootstrapAdmin(t *testing.T) {
	ctx := context.Background()
	t.Run("creates missing admin", func(t *testing.T) {
		repo := ssomocks.NewUserRepository(t)
		svc := ssotest.NewAuthService(repo, ssomocks.NewTokenProvider(t), testSessions(t, repo))
		var saved model.User
		repo.EXPECT().GetUserByUsername(mock.Anything, "admin").Return(model.User{}, apperrors.ErrNotFound)
		repo.EXPECT().SaveUser(mock.Anything, mock.AnythingOfType("model.User")).Run(func(ctx context.Context, user model.User) {
			saved = user
		}).Return(nil)

		err := svc.EnsureBootstrapAdmin(ctx, service.BootstrapAdminConfig{Username: "admin", Email: "admin@example.com", Password: "secret123"})
		require.NoError(t, err)
		require.Equal(t, model.RoleAdmin, saved.Role)
		require.Equal(t, model.StatusActive, saved.Status)
		require.Equal(t, model.AuthSourceLocal, saved.AuthSource)
	})

	t.Run("reactivates existing admin", func(t *testing.T) {
		repo := ssomocks.NewUserRepository(t)
		svc := ssotest.NewAuthService(repo, ssomocks.NewTokenProvider(t), testSessions(t, repo))
		admin := userWithPassword(uuid.New(), "admin", "secret123", model.RoleAdmin, model.StatusDeactivated)
		repo.EXPECT().GetUserByUsername(mock.Anything, "admin").Return(admin, nil)
		repo.EXPECT().UpdateUser(mock.Anything, mock.MatchedBy(func(user model.User) bool {
			return user.ID == admin.ID && user.Status == model.StatusActive
		})).Return(nil)
		require.NoError(t, svc.EnsureBootstrapAdmin(ctx, service.BootstrapAdminConfig{Username: "admin", Email: "admin@example.com", Password: "secret123"}))
	})

	t.Run("rejects non-admin username", func(t *testing.T) {
		repo := ssomocks.NewUserRepository(t)
		svc := ssotest.NewAuthService(repo, ssomocks.NewTokenProvider(t), testSessions(t, repo))
		repo.EXPECT().GetUserByUsername(mock.Anything, "admin").Return(model.User{Username: "admin", Role: model.RoleUser, Status: model.StatusActive}, nil)
		err := svc.EnsureBootstrapAdmin(ctx, service.BootstrapAdminConfig{Username: "admin", Email: "admin@example.com", Password: "secret123"})
		require.ErrorIs(t, err, apperrors.ErrConflict)
	})
}

func TestAuthServiceOIDCCreateUpdateStateAndCollisions(t *testing.T) {
	ctx := context.Background()
	repo := ssomocks.NewUserRepository(t)
	tokens := ssomocks.NewTokenProvider(t)
	provider := ssomocks.NewOIDCProvider(t)
	svc := service.NewAuthServiceWithConfig(repo, tokens, testSessions(t, repo), provider, service.Config{
		OIDCEnabled:      true,
		OIDCProviderName: "keycloak",
		OIDCAdminGroups:  []string{"/dcm-admins"},
		OIDCDefaultRole:  model.RoleUser,
	})
	expectAuthCodeURL(provider)

	state, binding := startOIDCTestLogin(t, svc)
	provider.EXPECT().ExchangeCode(mock.Anything, "code", mock.AnythingOfType("string")).Return(service.OIDCIdentity{
		Subject: "subject-1", PreferredUsername: "Alice.External", Email: "alice@example.com", Groups: []string{"/dcm-admins"},
	}, nil).Once()
	repo.EXPECT().GetUserByExternalIdentity(mock.Anything, "keycloak", "subject-1").Return(model.User{}, apperrors.ErrNotFound).Once()
	repo.EXPECT().GetUserByUsername(mock.Anything, "alice-external").Return(model.User{}, apperrors.ErrNotFound).Once()
	repo.EXPECT().GetUserByEmail(mock.Anything, "alice@example.com").Return(model.User{}, apperrors.ErrNotFound).Once()
	var created model.User
	repo.EXPECT().SaveUser(mock.Anything, mock.AnythingOfType("model.User")).Run(func(ctx context.Context, user model.User) {
		created = user
	}).Return(nil).Once()
	tokens.EXPECT().GenerateAccessToken(mock.AnythingOfType("model.User"), mock.Anything, mock.Anything).Return("access", nil).Once()

	pair, redirectAfter, err := svc.CompleteOIDCCallback(ctx, "keycloak", "code", state, binding)
	require.NoError(t, err)
	require.Equal(t, "access", pair.AccessToken)
	require.Len(t, pair.RefreshToken, 43)
	require.Equal(t, "/", redirectAfter)
	require.Equal(t, "alice-external", created.Username)
	require.Equal(t, "alice@example.com", created.Email)
	require.Equal(t, model.RoleAdmin, created.Role)
	require.Equal(t, model.AuthSourceOIDC, created.AuthSource)
	require.NotNil(t, created.LastLoginAt)

	_, _, err = svc.CompleteOIDCCallback(ctx, "keycloak", "code", state, binding)
	require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)

	existing := created
	existing.Role = model.RoleAdmin
	state, binding = startOIDCTestLogin(t, svc)
	provider.EXPECT().ExchangeCode(mock.Anything, "code2", mock.AnythingOfType("string")).Return(service.OIDCIdentity{
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
	tokens.EXPECT().GenerateAccessToken(mock.AnythingOfType("model.User"), mock.Anything, mock.Anything).Return("access2", nil).Once()
	_, _, err = svc.CompleteOIDCCallback(ctx, "keycloak", "code2", state, binding)
	require.NoError(t, err)
	require.Equal(t, existing.ID, updated.ID)
	require.Equal(t, "alice-updated", updated.Username)
	require.Equal(t, model.RoleUser, updated.Role)

	state, binding = startOIDCTestLogin(t, svc)
	provider.EXPECT().ExchangeCode(mock.Anything, "code3", mock.AnythingOfType("string")).Return(service.OIDCIdentity{
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
	tokens.EXPECT().GenerateAccessToken(mock.AnythingOfType("model.User"), mock.Anything, mock.Anything).Return("access3", nil).Once()
	_, _, err = svc.CompleteOIDCCallback(ctx, "keycloak", "code3", state, binding)
	require.NoError(t, err)
	require.Equal(t, "alice-external-subjectx", collided.Username)
	require.True(t, strings.HasSuffix(collided.Email, "@oidc.local"))
}

func TestAuthServiceOIDCRejectsInvalidStateExpiredStateAndLastAdminDowngrade(t *testing.T) {
	ctx := context.Background()
	repo := ssomocks.NewUserRepository(t)
	tokens := ssomocks.NewTokenProvider(t)
	provider := ssomocks.NewOIDCProvider(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := service.NewAuthServiceWithConfig(repo, tokens, testSessions(t, repo), provider, service.Config{
		OIDCEnabled:      true,
		OIDCProviderName: "keycloak",
		OIDCDefaultRole:  model.RoleUser,
		OIDCStateTTL:     time.Minute,
		Now:              func() time.Time { return now },
	})
	expectAuthCodeURL(provider)

	state, _ := startOIDCTestLogin(t, svc)
	_, _, err := svc.CompleteOIDCCallback(ctx, "keycloak", "code", state, "bad-binding")
	require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)

	state, binding := startOIDCTestLogin(t, svc)
	now = now.Add(time.Minute + time.Nanosecond)
	_, _, err = svc.CompleteOIDCCallback(ctx, "keycloak", "code", state, binding)
	require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)

	svc = service.NewAuthServiceWithConfig(repo, tokens, testSessions(t, repo), provider, service.Config{OIDCEnabled: true, OIDCProviderName: "keycloak", OIDCDefaultRole: model.RoleUser})
	expectAuthCodeURL(provider)
	admin := model.User{ID: uuid.New(), Username: "admin", Email: "admin@example.com", Role: model.RoleAdmin, Status: model.StatusActive, AuthSource: model.AuthSourceOIDC, ExternalProvider: "keycloak", ExternalSubject: "admin-subject"}
	state, binding = startOIDCTestLogin(t, svc)
	provider.EXPECT().ExchangeCode(mock.Anything, "code", mock.AnythingOfType("string")).Return(service.OIDCIdentity{
		Subject: "admin-subject", PreferredUsername: "admin", Email: "admin@example.com",
	}, nil).Once()
	repo.EXPECT().GetUserByExternalIdentity(mock.Anything, "keycloak", "admin-subject").Return(admin, nil)
	repo.EXPECT().GetUserByUsername(mock.Anything, "admin").Return(admin, nil)
	repo.EXPECT().GetUserByEmail(mock.Anything, "admin@example.com").Return(admin, nil)
	repo.EXPECT().CountActiveAdmins(mock.Anything).Return(1, nil)
	_, _, err = svc.CompleteOIDCCallback(ctx, "keycloak", "code", state, binding)
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

func startOIDCTestLogin(t *testing.T, svc *service.AuthService) (string, string) {
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
		AuthSource:   model.AuthSourceLocal,
	}
}

func TestAuthServiceMapsTokenAndRepositoryErrors(t *testing.T) {
	ctx := context.Background()
	repo := ssomocks.NewUserRepository(t)
	tokens := ssomocks.NewTokenProvider(t)
	sessions := ssomocks.NewSessionRepository(t)
	svc := ssotest.NewAuthService(repo, tokens, sessions)
	_, err := svc.Refresh(ctx, "bad-refresh")
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	tokens.EXPECT().ValidateAccessToken("bad-access").Return(model.AccessIdentity{}, errors.New("invalid"))
	_, err = svc.VerifyAccessToken(ctx, "bad-access")
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	identity := model.AccessIdentity{UserID: uuid.New(), SessionID: uuid.New()}
	tokens.EXPECT().ValidateAccessToken("access").Return(identity, nil)
	sessions.EXPECT().VerifySession(mock.Anything, identity, mock.Anything).Return(model.User{}, apperrors.ErrUnavailable)
	_, err = svc.VerifyAccessToken(ctx, "access")
	require.ErrorIs(t, err, apperrors.ErrUnavailable)
}

// These fixtures isolate profile/provisioning tests from persistence mechanics.
// Session lifecycle and transaction behavior are tested through PostgreSQL.
func testSessions(t *testing.T, repo *ssomocks.UserRepository) *ssomocks.SessionRepository {
	sessions := ssomocks.NewSessionRepository(t)
	tx := ssomocks.NewSessionTransaction(t)
	sessions.EXPECT().WithUser(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, id uuid.UUID, operation string, fn func(model.User, service.SessionTransaction) error) error {
		return fn(model.User{ID: id, Status: model.StatusActive}, tx)
	}).Maybe()
	tx.EXPECT().CreateSession(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	tx.EXPECT().UpdateUser(mock.Anything, mock.Anything).RunAndReturn(repo.UpdateUser).Maybe()
	tx.EXPECT().RevokeAll(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	return sessions
}

func TestLoginPreservesSafeLookupTimeoutClassification(t *testing.T) {
	for _, kind := range []string{"deadline-error", "expired-context"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			lookupError := fmt.Errorf("lookup failed: %w", context.DeadlineExceeded)
			if kind == "expired-context" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
				lookupError = errors.New("driver query canceled")
			}
			repo := ssomocks.NewUserRepository(t)
			repo.EXPECT().GetUserByUsername(mock.Anything, "alice").Return(model.User{}, lookupError)
			svc := ssotest.NewAuthService(repo, ssomocks.NewTokenProvider(t), ssomocks.NewSessionRepository(t))
			result, err := svc.Login(ctx, "alice", "secret123")
			require.Equal(t, model.Tokens{}, result)
			require.ErrorIs(t, err, apperrors.ErrTimeout)
		})
	}
}

func TestAuthServiceStoresAndLooksUpIdentityInLowercase(t *testing.T) {
	ctx := context.Background()

	t.Run("register", func(t *testing.T) {
		repo := ssomocks.NewUserRepository(t)
		svc := ssotest.NewAuthService(repo, ssomocks.NewTokenProvider(t), testSessions(t, repo))
		var saved model.User
		repo.EXPECT().SaveUser(mock.Anything, mock.AnythingOfType("model.User")).
			Run(func(_ context.Context, user model.User) { saved = user }).Return(nil)

		_, err := svc.Register(ctx, " Alice ", " Alice@Example.COM ", "secret123")

		require.NoError(t, err)
		require.Equal(t, "alice", saved.Username)
		require.Equal(t, "alice@example.com", saved.Email)
	})

	t.Run("administrator create", func(t *testing.T) {
		repo := ssomocks.NewUserRepository(t)
		svc := ssotest.NewAuthService(repo, ssomocks.NewTokenProvider(t), testSessions(t, repo))
		repo.EXPECT().SaveUser(mock.Anything, mock.AnythingOfType("model.User")).Return(nil)

		created, err := svc.CreateUser(ctx, service.CreateUserInput{
			Username: "Bob", Email: "Bob@Example.com", Password: "secret123", Role: model.RoleUser, Status: model.StatusActive,
		})

		require.NoError(t, err)
		require.Equal(t, "bob", created.Username)
		require.Equal(t, "bob@example.com", created.Email)
	})

	t.Run("login in any case", func(t *testing.T) {
		repo := ssomocks.NewUserRepository(t)
		svc := ssotest.NewAuthService(repo, ssomocks.NewTokenProvider(t), testSessions(t, repo))
		// Only the lowercase name is looked up; an unknown user is an invalid credential.
		repo.EXPECT().GetUserByUsername(mock.Anything, "alice").Return(model.User{}, apperrors.ErrNotFound)

		_, err := svc.Login(ctx, " ALICE", "secret123")

		require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)
	})
}
