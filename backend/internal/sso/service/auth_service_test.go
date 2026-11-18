package service_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/permissions"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/validation"
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
	svc := service.NewAuthServiceWithConfig(repo, tokens, testSessions(t, repo), service.OIDCDeps{}, service.Config{
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

func TestAuthServiceRejectsInvalidUserInput(t *testing.T) {
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
}

func TestAuthServiceRejectsPasswordsLongerThanBcryptAccepts(t *testing.T) {
	ctx := context.Background()
	// 24 three-byte runes: the limit counts bytes, not characters.
	longest := strings.Repeat("日", 24)
	tooLong := longest + "a"
	require.Len(t, longest, 72)

	paths := []struct {
		name string
		call func(svc *service.AuthService, repo *ssomocks.UserRepository, password string) error
	}{
		{name: "registration", call: func(svc *service.AuthService, _ *ssomocks.UserRepository, password string) error {
			_, err := svc.Register(ctx, "alice", "alice@example.com", password)
			return err
		}},
		{name: "administrator create", call: func(svc *service.AuthService, _ *ssomocks.UserRepository, password string) error {
			_, err := svc.CreateUser(ctx, service.CreateUserInput{
				Username: "alice", Email: "alice@example.com", Password: password, Role: model.RoleUser, Status: model.StatusActive,
			})
			return err
		}},
		{name: "user update", call: func(svc *service.AuthService, repo *ssomocks.UserRepository, password string) error {
			existing := model.User{ID: uuid.New(), Username: "alice", Email: "alice@example.com", Role: model.RoleUser, Status: model.StatusActive, AuthSource: model.AuthSourceLocal}
			repo.EXPECT().GetUserByID(mock.Anything, existing.ID).Return(existing, nil).Maybe()
			_, err := svc.UpdateUser(ctx, existing.ID, service.UpdateUserInput{
				Username: "alice", Email: "alice@example.com", Password: password, Role: model.RoleUser, Status: model.StatusActive,
			})
			return err
		}},
	}
	for _, path := range paths {
		t.Run(path.name+" accepts 72 bytes", func(t *testing.T) {
			repo := ssomocks.NewUserRepository(t)
			svc := ssotest.NewAuthService(repo, ssomocks.NewTokenProvider(t), testSessions(t, repo))
			var hash string
			repo.EXPECT().SaveUser(mock.Anything, mock.AnythingOfType("model.User")).
				Run(func(_ context.Context, user model.User) { hash = user.PasswordHash }).Return(nil).Maybe()
			repo.EXPECT().UpdateUser(mock.Anything, mock.AnythingOfType("model.User")).
				Run(func(_ context.Context, user model.User) { hash = user.PasswordHash }).Return(nil).Maybe()

			require.NoError(t, path.call(svc, repo, longest))
			require.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte(longest)))
		})
		t.Run(path.name+" rejects 73 bytes and saves nothing", func(t *testing.T) {
			// The strict mocks fail the test if anything is saved or updated.
			repo := ssomocks.NewUserRepository(t)
			svc := ssotest.NewAuthService(repo, ssomocks.NewTokenProvider(t), ssomocks.NewSessionRepository(t))

			err := path.call(svc, repo, tooLong)

			require.ErrorIs(t, err, apperrors.ErrBadRequest)
			require.EqualError(t, err, "password must be at most 72 bytes")
		})
	}

	t.Run("login with a longer password is invalid credentials", func(t *testing.T) {
		repo := ssomocks.NewUserRepository(t)
		svc := ssotest.NewAuthService(repo, ssomocks.NewTokenProvider(t), ssomocks.NewSessionRepository(t))
		repo.EXPECT().GetUserByUsername(mock.Anything, "alice").
			Return(userWithPassword(uuid.New(), "alice", longest, model.RoleUser, model.StatusActive), nil)

		_, err := svc.Login(ctx, "alice", tooLong)

		require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)
	})
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

// A sign-in returns tokens and the attempt's target, and its attempt is usable once.
func TestAuthServiceOIDCSignsInOncePerAttempt(t *testing.T) {
	_, repo, sessions := newUserStore(t)
	svc, provider, _ := provisioningService(t, repo, sessions)
	started, err := svc.StartOIDCLogin(context.Background(), "keycloak", "/")
	require.NoError(t, err)
	provider.EXPECT().ExchangeCode(mock.Anything, "code", mock.Anything, mock.Anything).Return(service.OIDCIdentity{Subject: "subject-1", PreferredUsername: "alice"}, nil).Once()

	result, err := svc.CompleteOIDCCallback(context.Background(), "keycloak", "code", started.State, started.StateBinding)
	require.NoError(t, err)
	require.Equal(t, model.OIDCCallbackSuccess, result.Outcome)
	require.Equal(t, "access", result.Tokens.AccessToken)
	require.Len(t, result.Tokens.RefreshToken, 43)
	require.Equal(t, "/", result.RedirectAfter)

	result, err = svc.CompleteOIDCCallback(context.Background(), "keycloak", "code", started.State, started.StateBinding)
	require.NoError(t, err)
	require.Equal(t, model.OIDCCallbackInvalidAttempt, result.Outcome, "an attempt is usable once")
}

// Each attempt gets its own PKCE verifier, and the callback redeems the code
// with the nonce and verifier of the attempt it consumes.
func TestAuthServiceOIDCExchangesTheCodeWithTheAttemptsVerifier(t *testing.T) {
	ctx := context.Background()
	repo := ssomocks.NewUserRepository(t)
	provider := ssomocks.NewOIDCProvider(t)
	svc := service.NewAuthServiceWithConfig(repo, ssomocks.NewTokenProvider(t), testSessions(t, repo), service.OIDCDeps{Provider: provider, LoginAttempts: ssotest.NewLoginAttempts()}, service.Config{
		OIDCEnabled: true, OIDCProviderName: "keycloak",
	})
	expectAuthCodeURL(provider)

	firstStart, err := svc.StartOIDCLogin(ctx, "keycloak", "/")
	require.NoError(t, err)
	secondStart, err := svc.StartOIDCLogin(ctx, "keycloak", "/")
	require.NoError(t, err)
	first, second := authURLQuery(t, firstStart.AuthURL), authURLQuery(t, secondStart.AuthURL)
	require.GreaterOrEqual(t, len(first.Get("verifier")), 43, "RFC 7636 verifiers have at least 43 characters")
	require.NotEqual(t, first.Get("verifier"), second.Get("verifier"))

	provider.EXPECT().ExchangeCode(mock.Anything, "code", first.Get("nonce"), first.Get("verifier")).
		Return(service.OIDCIdentity{}, apperrors.ErrInvalidCredentials).Once()
	result, err := svc.CompleteOIDCCallback(ctx, "keycloak", "code", first.Get("state"), firstStart.StateBinding)
	require.NoError(t, err)
	require.Equal(t, model.OIDCCallbackIdentityRejected, result.Outcome)
}

func authURLQuery(t *testing.T, authURL string) url.Values {
	t.Helper()
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	return parsed.Query()
}

func TestAuthServiceOIDCProviderEnabledButUnavailable(t *testing.T) {
	ctx := context.Background()
	repo := ssomocks.NewUserRepository(t)
	provider := ssomocks.NewOIDCProvider(t)
	available := false
	provider.EXPECT().Available().RunAndReturn(func() bool { return available })
	svc := service.NewAuthServiceWithConfig(repo, ssomocks.NewTokenProvider(t), testSessions(t, repo), service.OIDCDeps{Provider: provider, LoginAttempts: ssotest.NewLoginAttempts()}, service.Config{
		OIDCEnabled: true, OIDCProviderName: "keycloak",
	})

	require.Equal(t, []model.AuthProvider{{Name: "keycloak", Available: false}}, svc.AuthProviders())
	_, err := svc.StartOIDCLogin(ctx, "keycloak", "/")
	require.ErrorIs(t, err, apperrors.ErrUnavailable)
	_, err = svc.CompleteOIDCCallback(ctx, "keycloak", "code", "state", "binding")
	require.ErrorIs(t, err, apperrors.ErrUnavailable)
	_, err = svc.StartOIDCLogin(ctx, "other", "/")
	require.ErrorIs(t, err, apperrors.ErrNotFound)

	available = true
	require.Equal(t, []model.AuthProvider{{Name: "keycloak", Available: true}}, svc.AuthProviders())
}

func TestAuthServiceOIDCDisabledHasNoProvider(t *testing.T) {
	ctx := context.Background()
	repo := ssomocks.NewUserRepository(t)
	svc := ssotest.NewAuthService(repo, ssomocks.NewTokenProvider(t), testSessions(t, repo))

	require.Empty(t, svc.AuthProviders())
	_, err := svc.StartOIDCLogin(ctx, "keycloak", "/")
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	_, err = svc.CompleteOIDCCallback(ctx, "keycloak", "code", "state", "binding")
	require.ErrorIs(t, err, apperrors.ErrNotFound)
}

func expectAuthCodeURL(provider *ssomocks.OIDCProvider) {
	provider.EXPECT().Available().Return(true).Maybe()
	provider.EXPECT().
		AuthCodeURL(mock.AnythingOfType("string"), mock.AnythingOfType("string"), mock.AnythingOfType("string")).
		RunAndReturn(func(state, nonce, verifier string) string {
			return "https://keycloak.example/authorize?state=" + state + "&nonce=" + nonce + "&verifier=" + verifier
		}).
		Maybe()
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
// Session lifecycle and transaction behavior, including the last-active-admin
// guard, are tested through PostgreSQL.
func testSessions(t *testing.T, repo *ssomocks.UserRepository) *ssomocks.SessionRepository {
	sessions := ssomocks.NewSessionRepository(t)
	tx := ssomocks.NewSessionTransaction(t)
	withUser := func(ctx context.Context, id uuid.UUID, operation string, fn func(model.User, service.SessionTransaction) error) error {
		return fn(model.User{ID: id, Status: model.StatusActive}, tx)
	}
	sessions.EXPECT().WithUser(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(withUser).Maybe()
	sessions.EXPECT().WithLastAdminGuard(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(withUser).Maybe()
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

// oidcTestService builds an OIDC-enabled service over attempts whose clock
// reads *now.
func oidcTestService(t *testing.T, repo *ssomocks.UserRepository, tokens *ssomocks.TokenProvider, provider *ssomocks.OIDCProvider, attempts service.LoginAttemptRepository, now *time.Time) *service.AuthService {
	t.Helper()
	expectAuthCodeURL(provider)
	return service.NewAuthServiceWithConfig(repo, tokens, testSessions(t, repo), service.OIDCDeps{Provider: provider, LoginAttempts: attempts}, service.Config{
		OIDCEnabled: true, OIDCProviderName: "keycloak", Now: func() time.Time { return *now },
	})
}

func TestAuthServiceOIDCStartReturnsTheAttemptsStateBindingAndExpiry(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := oidcTestService(t, ssomocks.NewUserRepository(t), ssomocks.NewTokenProvider(t), ssomocks.NewOIDCProvider(t), ssotest.NewLoginAttempts(), &now)

	started, err := svc.StartOIDCLogin(context.Background(), "keycloak", "http://frontend.example/auth/callback")
	require.NoError(t, err)
	require.Equal(t, authURLQuery(t, started.AuthURL).Get("state"), started.State)
	require.Len(t, started.State, 43)
	require.Len(t, started.StateBinding, 43)
	require.NotEqual(t, started.State, started.StateBinding)
	require.Equal(t, now.Add(30*time.Minute), started.ExpiresAt, "an attempt lives 30 minutes")
}

// A callback answers expected rejections as outcomes; it reports the
// attempt's target once the attempt has been consumed.
func TestAuthServiceOIDCCallbackOutcomes(t *testing.T) {
	const target = "http://frontend.example/auth/callback"
	subject := service.OIDCIdentity{Subject: "subject-1", PreferredUsername: "alice", Email: "alice@example.com"}
	for name, tc := range map[string]struct {
		prepare  func(repo *ssomocks.UserRepository, provider *ssomocks.OIDCProvider)
		code     string
		state    func(started model.OIDCLoginStart) (string, string)
		advance  time.Duration
		outcome  model.OIDCCallbackOutcome
		redirect string
	}{
		"an unknown state": {
			state:   func(model.OIDCLoginStart) (string, string) { return "unknown-state", "binding" },
			outcome: model.OIDCCallbackInvalidAttempt,
		},
		"a wrong browser binding": {
			state:   func(s model.OIDCLoginStart) (string, string) { return s.State, "other-binding" },
			outcome: model.OIDCCallbackInvalidAttempt,
		},
		"a missing code": {
			code:    " ",
			outcome: model.OIDCCallbackInvalidAttempt,
		},
		"an attempt returned to after 30 minutes": {
			advance: 30 * time.Minute,
			outcome: model.OIDCCallbackInvalidAttempt,
		},
		"an identity the provider rejects": {
			prepare: func(_ *ssomocks.UserRepository, provider *ssomocks.OIDCProvider) {
				provider.EXPECT().ExchangeCode(mock.Anything, "code", mock.Anything, mock.Anything).Return(service.OIDCIdentity{}, apperrors.ErrInvalidCredentials).Once()
			},
			outcome:  model.OIDCCallbackIdentityRejected,
			redirect: target,
		},
		"a deactivated account": {
			prepare: func(repo *ssomocks.UserRepository, provider *ssomocks.OIDCProvider) {
				provider.EXPECT().ExchangeCode(mock.Anything, "code", mock.Anything, mock.Anything).Return(subject, nil).Once()
				existing := model.User{ID: uuid.New(), Username: "alice", Email: "alice@example.com", Role: model.RoleUser, Status: model.StatusDeactivated, AuthSource: model.AuthSourceOIDC}
				repo.EXPECT().GetUserByExternalIdentity(mock.Anything, "keycloak", "subject-1").Return(existing, nil).Once()
			},
			outcome:  model.OIDCCallbackAccountDeactivated,
			redirect: target,
		},
		"an account that collides with another": {
			prepare: func(repo *ssomocks.UserRepository, provider *ssomocks.OIDCProvider) {
				provider.EXPECT().ExchangeCode(mock.Anything, "code", mock.Anything, mock.Anything).Return(subject, nil).Once()
				// The save hits another user: re-reading the binding finds none.
				repo.EXPECT().GetUserByExternalIdentity(mock.Anything, "keycloak", "subject-1").Return(model.User{}, apperrors.ErrNotFound).Twice()
				repo.EXPECT().GetUserByUsername(mock.Anything, mock.Anything).Return(model.User{}, apperrors.ErrNotFound).Maybe()
				repo.EXPECT().GetUserByEmail(mock.Anything, mock.Anything).Return(model.User{}, apperrors.ErrNotFound).Maybe()
				repo.EXPECT().SaveUser(mock.Anything, mock.Anything).Return(apperrors.ErrAlreadyExists).Once()
			},
			outcome:  model.OIDCCallbackAccountConflict,
			redirect: target,
		},
	} {
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			repo := ssomocks.NewUserRepository(t)
			provider := ssomocks.NewOIDCProvider(t)
			svc := oidcTestService(t, repo, ssomocks.NewTokenProvider(t), provider, ssotest.NewLoginAttempts(), &now)
			if tc.prepare != nil {
				tc.prepare(repo, provider)
			}
			started, err := svc.StartOIDCLogin(context.Background(), "keycloak", target)
			require.NoError(t, err)
			state, binding := started.State, started.StateBinding
			if tc.state != nil {
				state, binding = tc.state(started)
			}
			code := "code"
			if tc.code != "" {
				code = tc.code
			}
			now = now.Add(tc.advance)

			result, err := svc.CompleteOIDCCallback(context.Background(), "keycloak", code, state, binding)
			require.NoError(t, err)
			require.Equal(t, tc.outcome, result.Outcome)
			require.Equal(t, tc.redirect, result.RedirectAfter)
			require.Empty(t, result.Tokens)
		})
	}
}

func TestAuthServiceOIDCCallbackReportsProviderFailuresAsErrors(t *testing.T) {
	for _, failure := range []error{apperrors.ErrUnavailable, apperrors.ErrTimeout} {
		t.Run(failure.Error(), func(t *testing.T) {
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			provider := ssomocks.NewOIDCProvider(t)
			svc := oidcTestService(t, ssomocks.NewUserRepository(t), ssomocks.NewTokenProvider(t), provider, ssotest.NewLoginAttempts(), &now)
			provider.EXPECT().ExchangeCode(mock.Anything, "code", mock.Anything, mock.Anything).Return(service.OIDCIdentity{}, failure).Once()
			started, err := svc.StartOIDCLogin(context.Background(), "keycloak", "/")
			require.NoError(t, err)

			_, err = svc.CompleteOIDCCallback(context.Background(), "keycloak", "code", started.State, started.StateBinding)
			require.ErrorIs(t, err, failure)
		})
	}
}

// A refused callback with the wrong binding leaves the attempt for its own
// browser, and an attempt started by one service instance completes on another
// over the same store.
func TestAuthServiceOIDCAttemptOutlivesItsServiceAndAWrongBinding(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	attempts := ssotest.NewLoginAttempts()
	repo := ssomocks.NewUserRepository(t)
	tokens := ssomocks.NewTokenProvider(t)
	provider := ssomocks.NewOIDCProvider(t)
	before := oidcTestService(t, repo, tokens, provider, attempts, &now)
	started, err := before.StartOIDCLogin(context.Background(), "keycloak", "/")
	require.NoError(t, err)

	after := oidcTestService(t, repo, tokens, provider, attempts, &now)
	refused, err := after.CompleteOIDCCallback(context.Background(), "keycloak", "code", started.State, "other-binding")
	require.NoError(t, err)
	require.Equal(t, model.OIDCCallbackInvalidAttempt, refused.Outcome)

	provider.EXPECT().ExchangeCode(mock.Anything, "code", mock.Anything, mock.Anything).Return(service.OIDCIdentity{Subject: "subject-1", PreferredUsername: "alice", Email: "alice@example.com", EmailVerified: true}, nil).Once()
	repo.EXPECT().GetUserByExternalIdentity(mock.Anything, "keycloak", "subject-1").Return(model.User{}, apperrors.ErrNotFound).Once()
	repo.EXPECT().GetUserByUsername(mock.Anything, "alice").Return(model.User{}, apperrors.ErrNotFound).Once()
	repo.EXPECT().GetUserByEmail(mock.Anything, "alice@example.com").Return(model.User{}, apperrors.ErrNotFound).Once()
	repo.EXPECT().SaveUser(mock.Anything, mock.Anything).Return(nil).Once()
	repo.EXPECT().UpdateUser(mock.Anything, mock.Anything).Return(nil).Once() // last sign-in
	tokens.EXPECT().GenerateAccessToken(mock.Anything, mock.Anything, mock.Anything).Return("access", nil).Once()
	result, err := after.CompleteOIDCCallback(context.Background(), "keycloak", "code", started.State, started.StateBinding)
	require.NoError(t, err)
	require.Equal(t, model.OIDCCallbackSuccess, result.Outcome)
	require.Equal(t, "access", result.Tokens.AccessToken)
	require.Equal(t, "/", result.RedirectAfter)
}

// Session cleanup also deletes expired attempts: once it has run, the attempt
// is gone even for a clock that reads before its expiry.
func TestAuthServiceCleanupDeletesExpiredLoginAttempts(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	repo := ssomocks.NewUserRepository(t)
	sessions := ssomocks.NewSessionRepository(t)
	provider := ssomocks.NewOIDCProvider(t)
	expectAuthCodeURL(provider)
	svc := service.NewAuthServiceWithConfig(repo, ssomocks.NewTokenProvider(t), sessions, service.OIDCDeps{Provider: provider, LoginAttempts: ssotest.NewLoginAttempts()}, service.Config{
		OIDCEnabled: true, OIDCProviderName: "keycloak", Now: func() time.Time { return now },
	})
	started, err := svc.StartOIDCLogin(context.Background(), "keycloak", "/")
	require.NoError(t, err)

	now = start.Add(service.OIDCLoginAttemptTTL)
	sessions.EXPECT().DeleteExpired(mock.Anything, now, 100).Return(nil).Once()
	require.NoError(t, svc.CleanupSessions(context.Background()))

	now = start
	result, err := svc.CompleteOIDCCallback(context.Background(), "keycloak", "code", started.State, started.StateBinding)
	require.NoError(t, err)
	require.Equal(t, model.OIDCCallbackInvalidAttempt, result.Outcome)
}

// userStore backs the generated repository and session mocks with users kept
// in memory, so provisioning tests read back what a sign-in stored. writes
// counts every user save and update.
type userStore struct {
	users map[uuid.UUID]model.User
	// beforeSave runs once before the next SaveUser, as a competing writer would.
	beforeSave func()
	writes     int
}

func newUserStore(t *testing.T, users ...model.User) (*userStore, *ssomocks.UserRepository, *ssomocks.SessionRepository) {
	t.Helper()
	store := &userStore{users: map[uuid.UUID]model.User{}}
	for _, user := range users {
		store.users[user.ID] = user
	}
	find := func(match func(model.User) bool) (model.User, error) {
		for _, user := range store.users {
			if match(user) {
				return user, nil
			}
		}
		return model.User{}, apperrors.ErrNotFound
	}
	repo := ssomocks.NewUserRepository(t)
	repo.EXPECT().GetUserByUsername(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, username string) (model.User, error) {
		return find(func(u model.User) bool { return u.Username == username })
	}).Maybe()
	repo.EXPECT().GetUserByEmail(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, email string) (model.User, error) {
		return find(func(u model.User) bool { return u.Email == email })
	}).Maybe()
	repo.EXPECT().GetUserByExternalIdentity(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, provider, subject string) (model.User, error) {
		return find(func(u model.User) bool { return u.ExternalProvider == provider && u.ExternalSubject == subject })
	}).Maybe()
	repo.EXPECT().GetUserByID(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, id uuid.UUID) (model.User, error) {
		return find(func(u model.User) bool { return u.ID == id })
	}).Maybe()
	repo.EXPECT().SaveUser(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, user model.User) error {
		if store.beforeSave != nil {
			competing := store.beforeSave
			store.beforeSave = nil
			competing()
		}
		return store.put(user)
	}).Maybe()
	repo.EXPECT().UpdateUser(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, user model.User) error {
		return store.put(user)
	}).Maybe()

	sessions := ssomocks.NewSessionRepository(t)
	tx := ssomocks.NewSessionTransaction(t)
	withUser := func(_ context.Context, id uuid.UUID, _ string, fn func(model.User, service.SessionTransaction) error) error {
		current, ok := store.users[id]
		if !ok {
			return apperrors.ErrNotFound
		}
		return fn(current, tx)
	}
	sessions.EXPECT().WithUser(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(withUser).Maybe()
	sessions.EXPECT().WithLastAdminGuard(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(withUser).Maybe()
	tx.EXPECT().CreateSession(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	tx.EXPECT().UpdateUser(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, user model.User) error {
		return store.put(user)
	}).Maybe()
	tx.EXPECT().RevokeAll(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	tx.EXPECT().CountActiveAdmins(mock.Anything).RunAndReturn(func(context.Context) (int, error) {
		count := 0
		for _, user := range store.users {
			if user.Role == model.RoleAdmin && user.Status == model.StatusActive {
				count++
			}
		}
		return count, nil
	}).Maybe()
	return store, repo, sessions
}

// put saves user, refusing a username, email or external identity another
// user holds, as the unique indexes do.
func (s *userStore) put(user model.User) error {
	for id, other := range s.users {
		if id == user.ID {
			continue
		}
		if other.Username == user.Username || other.Email == user.Email ||
			(user.ExternalSubject != "" && other.ExternalProvider == user.ExternalProvider && other.ExternalSubject == user.ExternalSubject) {
			return apperrors.ErrAlreadyExists
		}
	}
	s.users[user.ID] = user
	s.writes++
	return nil
}

func (s *userStore) bySubject(t *testing.T, subject string) model.User {
	t.Helper()
	for _, user := range s.users {
		if user.ExternalSubject == subject {
			return user
		}
	}
	t.Fatalf("no user for subject %q", subject)
	return model.User{}
}

func oidcUser(subject, username, role string) model.User {
	return model.User{
		ID: uuid.New(), Username: username, Email: username + "@example.com", Role: role, Status: model.StatusActive,
		AuthSource: model.AuthSourceOIDC, ExternalProvider: "keycloak", ExternalSubject: subject,
	}
}

func localUser(username, role string) model.User {
	return model.User{
		ID: uuid.New(), Username: username, Email: username + "@example.com", Role: role, Status: model.StatusActive,
		AuthSource: model.AuthSourceLocal, PasswordHash: "hash",
	}
}

// provisioningService is an OIDC-enabled service over store's mocks. Admin
// groups make roles provider-managed; logs go to the returned buffer.
func provisioningService(t *testing.T, repo *ssomocks.UserRepository, sessions *ssomocks.SessionRepository, adminGroups ...string) (*service.AuthService, *ssomocks.OIDCProvider, *strings.Builder) {
	t.Helper()
	provider := ssomocks.NewOIDCProvider(t)
	expectAuthCodeURL(provider)
	tokens := ssomocks.NewTokenProvider(t)
	tokens.EXPECT().GenerateAccessToken(mock.Anything, mock.Anything, mock.Anything).Return("access", nil).Maybe()
	logs := &strings.Builder{}
	svc := service.NewAuthServiceWithConfig(repo, tokens, sessions, service.OIDCDeps{Provider: provider, LoginAttempts: ssotest.NewLoginAttempts()}, service.Config{
		OIDCEnabled: true, OIDCProviderName: "keycloak", OIDCAdminGroups: adminGroups,
		Logger: slog.New(slog.NewTextHandler(logs, nil)),
	})
	return svc, provider, logs
}

// signInAs completes one OIDC sign-in in which the provider vouches for identity.
func signInAs(t *testing.T, svc *service.AuthService, provider *ssomocks.OIDCProvider, identity service.OIDCIdentity) model.OIDCCallbackResult {
	t.Helper()
	started, err := svc.StartOIDCLogin(context.Background(), "keycloak", "/")
	require.NoError(t, err)
	code := "code-" + uuid.NewString()
	provider.EXPECT().ExchangeCode(mock.Anything, code, mock.Anything, mock.Anything).Return(identity, nil).Once()
	result, err := svc.CompleteOIDCCallback(context.Background(), "keycloak", code, started.State, started.StateBinding)
	require.NoError(t, err)
	return result
}

// Every derived username passes the rule for local usernames.
func TestOIDCSignInDerivesAUsernameThatFollowsTheLocalRule(t *testing.T) {
	for _, tc := range []struct {
		name, preferred, want string
	}{
		{name: "a Latin name keeps its separators", preferred: "Alice.External", want: "alice.external"},
		{name: "an email-style name keeps the part before the first @", preferred: "Bob@Example.com", want: "bob"},
		{name: "a non-Latin name falls back to the identity hash", preferred: "Иван", want: "oidc-32c485b7"},
		{name: "letters outside a-z are dropped", preferred: "José.García", want: "jos.garca"},
		{name: "a too-short name falls back to the identity hash", preferred: "ab", want: "oidc-32c485b7"},
		{name: "a missing name falls back to the identity hash", preferred: "", want: "oidc-32c485b7"},
		{name: "leading non-alphanumerics and trailing separators are trimmed", preferred: "--_Carol__", want: "carol"},
		{name: "an over-long name is cut to 30 characters", preferred: strings.Repeat("d", 40), want: strings.Repeat("d", 30)},
		{name: "a separator left at the cut is trimmed", preferred: "abcdefghijklmnopqrstuvwxyz012-xyz", want: "abcdefghijklmnopqrstuvwxyz012"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, repo, sessions := newUserStore(t)
			svc, provider, _ := provisioningService(t, repo, sessions)

			result := signInAs(t, svc, provider, service.OIDCIdentity{Subject: "subject-1", PreferredUsername: tc.preferred})

			require.Equal(t, model.OIDCCallbackSuccess, result.Outcome)
			username := store.bySubject(t, "subject-1").Username
			require.Equal(t, tc.want, username)
			require.NoError(t, validation.ResourceName(username))
		})
	}
}

// A taken username resolves to the base with 8 characters of the identity
// hash, then to "oidc-" and 24 of them; past both, an existing user keeps their
// username and a new user is an account conflict.
func TestOIDCSignInResolvesAUsernameCollision(t *testing.T) {
	identity := service.OIDCIdentity{Subject: "subject-1", PreferredUsername: "alice"}

	t.Run("the base gets the identity hash", func(t *testing.T) {
		store, repo, sessions := newUserStore(t, localUser("alice", model.RoleUser))
		svc, provider, _ := provisioningService(t, repo, sessions)
		require.Equal(t, model.OIDCCallbackSuccess, signInAs(t, svc, provider, identity).Outcome)
		require.Equal(t, "alice-32c485b7", store.bySubject(t, "subject-1").Username)
	})

	t.Run("a long base is cut to make room for the hash", func(t *testing.T) {
		long := strings.Repeat("a", 30)
		store, repo, sessions := newUserStore(t, localUser(long, model.RoleUser))
		svc, provider, _ := provisioningService(t, repo, sessions)
		require.Equal(t, model.OIDCCallbackSuccess, signInAs(t, svc, provider, service.OIDCIdentity{Subject: "subject-1", PreferredUsername: long}).Outcome)
		require.Equal(t, strings.Repeat("a", 21)+"-32c485b7", store.bySubject(t, "subject-1").Username)
	})

	t.Run("then oidc- and the longer hash", func(t *testing.T) {
		store, repo, sessions := newUserStore(t, localUser("alice", model.RoleUser), localUser("alice-32c485b7", model.RoleUser))
		svc, provider, _ := provisioningService(t, repo, sessions)
		require.Equal(t, model.OIDCCallbackSuccess, signInAs(t, svc, provider, identity).Outcome)
		require.Equal(t, "oidc-32c485b7a31284158eec5433", store.bySubject(t, "subject-1").Username)
	})

	taken := []model.User{localUser("alice", model.RoleUser), localUser("alice-32c485b7", model.RoleUser), localUser("oidc-32c485b7a31284158eec5433", model.RoleUser)}

	t.Run("an existing user keeps their username", func(t *testing.T) {
		store, repo, sessions := newUserStore(t, append(taken, oidcUser("subject-1", "alice.old", model.RoleUser))...)
		svc, provider, _ := provisioningService(t, repo, sessions)
		require.Equal(t, model.OIDCCallbackSuccess, signInAs(t, svc, provider, identity).Outcome)
		require.Equal(t, "alice.old", store.bySubject(t, "subject-1").Username)
	})

	t.Run("a new user is an account conflict", func(t *testing.T) {
		_, repo, sessions := newUserStore(t, taken...)
		svc, provider, _ := provisioningService(t, repo, sessions)
		require.Equal(t, model.OIDCCallbackAccountConflict, signInAs(t, svc, provider, identity).Outcome)
	})
}

// Users of one Keycloak user federation share the leading characters of their
// subjects; two of them with one name both sign in.
func TestOIDCSignInSeparatesFederatedUsersWithOneName(t *testing.T) {
	store, repo, sessions := newUserStore(t, localUser("dave", model.RoleUser))
	svc, provider, _ := provisioningService(t, repo, sessions)
	const first, second = "f:6f1c2b9e-4d3a-4c55-9a7e-0b1d2c3e4f50:dave", "f:6f1c2b9e-4d3a-4c55-9a7e-0b1d2c3e4f50:erin"

	require.Equal(t, model.OIDCCallbackSuccess, signInAs(t, svc, provider, service.OIDCIdentity{Subject: first, PreferredUsername: "dave"}).Outcome)
	require.Equal(t, model.OIDCCallbackSuccess, signInAs(t, svc, provider, service.OIDCIdentity{Subject: second, PreferredUsername: "dave"}).Outcome)

	require.Equal(t, "dave-12858549", store.bySubject(t, first).Username)
	require.Equal(t, "dave-d713ee80", store.bySubject(t, second).Username)
}

// An OIDC user whose stored username breaks the local rule gets a valid one at
// their next sign-in.
func TestOIDCSignInCorrectsAnExistingUsernameThatBreaksTheRule(t *testing.T) {
	store, repo, sessions := newUserStore(t, oidcUser("subject-1", "иван", model.RoleUser))
	svc, provider, _ := provisioningService(t, repo, sessions)

	require.Equal(t, model.OIDCCallbackSuccess, signInAs(t, svc, provider, service.OIDCIdentity{Subject: "subject-1", PreferredUsername: "Иван"}).Outcome)

	require.Equal(t, "oidc-32c485b7", store.bySubject(t, "subject-1").Username)
}

// The provider's email is stored only when it is verified, valid and no other
// user holds it; otherwise the user gets the fallback address.
func TestOIDCSignInStoresOnlyAVerifiedFreeEmail(t *testing.T) {
	const fallback = "oidc-32c485b7a31284158eec5433@oidc.local"
	for _, tc := range []struct {
		name     string
		email    string
		verified bool
		want     string
	}{
		{name: "a verified free address is stored", email: "Alice@Example.com", verified: true, want: "alice@example.com"},
		{name: "an unverified address is not", email: "alice@example.com", verified: false, want: fallback},
		{name: "an invalid address is not", email: "not an address", verified: true, want: fallback},
		{name: "an address another user holds is not", email: "taken@example.com", verified: true, want: fallback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, repo, sessions := newUserStore(t, localUser("taken", model.RoleUser))
			svc, provider, _ := provisioningService(t, repo, sessions)

			result := signInAs(t, svc, provider, service.OIDCIdentity{Subject: "subject-1", PreferredUsername: "alice", Email: tc.email, EmailVerified: tc.verified})

			require.Equal(t, model.OIDCCallbackSuccess, result.Outcome)
			require.Equal(t, tc.want, store.bySubject(t, "subject-1").Email)
		})
	}
}

// With administrator groups configured, groups and realm roles decide the role
// at every sign-in.
func TestOIDCSignInTakesTheRoleFromTheProviderWhenAdminGroupsAreConfigured(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing *model.User
		identity service.OIDCIdentity
		want     string
	}{
		{name: "a new user in an admin group is an admin", identity: service.OIDCIdentity{Groups: []string{"/dcm-admins"}}, want: model.RoleAdmin},
		{name: "a new user with an admin realm role is an admin", identity: service.OIDCIdentity{RealmRoles: []string{"dcm-admin"}}, want: model.RoleAdmin},
		{name: "a new user in no admin group is a user", identity: service.OIDCIdentity{Groups: []string{"/staff"}}, want: model.RoleUser},
		{name: "an admin who left the group is demoted", existing: new(oidcUser("subject-1", "alice", model.RoleAdmin)), want: model.RoleUser},
		{name: "a user who joined the group is promoted", existing: new(oidcUser("subject-1", "alice", model.RoleUser)), identity: service.OIDCIdentity{Groups: []string{"/dcm-admins"}}, want: model.RoleAdmin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users := []model.User{localUser("root", model.RoleAdmin)}
			if tc.existing != nil {
				users = append(users, *tc.existing)
			}
			store, repo, sessions := newUserStore(t, users...)
			svc, provider, _ := provisioningService(t, repo, sessions, "/dcm-admins", "dcm-admin")
			tc.identity.Subject, tc.identity.PreferredUsername = "subject-1", "alice"

			require.Equal(t, model.OIDCCallbackSuccess, signInAs(t, svc, provider, tc.identity).Outcome)

			require.Equal(t, tc.want, store.bySubject(t, "subject-1").Role)
		})
	}
}

// With no administrator groups, a new OIDC user is a user and sign-in never
// changes an existing user's role, so a promotion made in DCM stays.
func TestOIDCSignInKeepsTheRoleDCMGaveWhenNoAdminGroupsAreConfigured(t *testing.T) {
	store, repo, sessions := newUserStore(t, localUser("root", model.RoleAdmin))
	svc, provider, _ := provisioningService(t, repo, sessions)
	identity := service.OIDCIdentity{Subject: "subject-1", PreferredUsername: "alice", Groups: []string{"/dcm-admins"}}

	require.Equal(t, model.OIDCCallbackSuccess, signInAs(t, svc, provider, identity).Outcome)
	alice := store.bySubject(t, "subject-1")
	require.Equal(t, model.RoleUser, alice.Role)

	_, err := svc.UpdateUser(context.Background(), alice.ID, service.UpdateUserInput{Username: alice.Username, Email: alice.Email, Role: model.RoleAdmin, Status: model.StatusActive})
	require.NoError(t, err)
	require.Equal(t, model.OIDCCallbackSuccess, signInAs(t, svc, provider, identity).Outcome)
	require.Equal(t, model.RoleAdmin, store.bySubject(t, "subject-1").Role)

	_, err = svc.UpdateUser(context.Background(), alice.ID, service.UpdateUserInput{Username: alice.Username, Email: alice.Email, Role: model.RoleUser, Status: model.StatusActive})
	require.NoError(t, err)
	require.Equal(t, model.OIDCCallbackSuccess, signInAs(t, svc, provider, identity).Outcome)
	require.Equal(t, model.RoleUser, store.bySubject(t, "subject-1").Role)
}

// A role synchronization that would demote the last active administrator is
// skipped with a warning, and the sign-in succeeds.
func TestOIDCSignInKeepsTheLastActiveAdminAndWarns(t *testing.T) {
	store, repo, sessions := newUserStore(t, oidcUser("subject-1", "alice", model.RoleAdmin), localUser("bob", model.RoleUser))
	svc, provider, logs := provisioningService(t, repo, sessions, "/dcm-admins")

	result := signInAs(t, svc, provider, service.OIDCIdentity{Subject: "subject-1", PreferredUsername: "alice"})

	require.Equal(t, model.OIDCCallbackSuccess, result.Outcome)
	require.Equal(t, model.RoleAdmin, store.bySubject(t, "subject-1").Role)
	require.Contains(t, logs.String(), "level=WARN")
	require.Contains(t, logs.String(), "last active admin")
}

// A deactivated OIDC user is refused before any write: profile, role and last
// sign-in stay as they were.
func TestOIDCSignInRefusesADeactivatedUserBeforeAnyWrite(t *testing.T) {
	deactivated := oidcUser("subject-1", "alice", model.RoleUser)
	deactivated.Status = model.StatusDeactivated
	store, repo, sessions := newUserStore(t, deactivated, localUser("root", model.RoleAdmin))
	svc, provider, _ := provisioningService(t, repo, sessions, "/dcm-admins")

	result := signInAs(t, svc, provider, service.OIDCIdentity{Subject: "subject-1", PreferredUsername: "alice.new", Email: "new@example.com", EmailVerified: true, Groups: []string{"/dcm-admins"}})

	require.Equal(t, model.OIDCCallbackAccountDeactivated, result.Outcome)
	require.Zero(t, store.writes)
	require.Equal(t, deactivated, store.bySubject(t, "subject-1"))
}

// Last sign-in time is written when a sign-in succeeds, for a new and for an
// existing user.
func TestOIDCSignInRecordsTheLastSignInOnSuccess(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	store, repo, sessions := newUserStore(t)
	provider := ssomocks.NewOIDCProvider(t)
	expectAuthCodeURL(provider)
	tokens := ssomocks.NewTokenProvider(t)
	tokens.EXPECT().GenerateAccessToken(mock.Anything, mock.Anything, mock.Anything).Return("access", nil)
	svc := service.NewAuthServiceWithConfig(repo, tokens, sessions, service.OIDCDeps{Provider: provider, LoginAttempts: ssotest.NewLoginAttempts()}, service.Config{
		OIDCEnabled: true, OIDCProviderName: "keycloak", Now: func() time.Time { return now },
	})
	identity := service.OIDCIdentity{Subject: "subject-1", PreferredUsername: "alice"}

	require.Equal(t, model.OIDCCallbackSuccess, signInAs(t, svc, provider, identity).Outcome)
	require.Equal(t, now, *store.bySubject(t, "subject-1").LastLoginAt)

	now = now.Add(time.Hour)
	require.Equal(t, model.OIDCCallbackSuccess, signInAs(t, svc, provider, identity).Outcome)
	require.Equal(t, now, *store.bySubject(t, "subject-1").LastLoginAt)
}

// When another sign-in of the same new user saves it first, provisioning reads
// the identity binding again and signs in the user that sign-in created.
func TestOIDCSignInJoinsASimultaneousFirstSignIn(t *testing.T) {
	store, repo, sessions := newUserStore(t)
	svc, provider, _ := provisioningService(t, repo, sessions)
	winner := oidcUser("subject-1", "alice", model.RoleUser)
	store.beforeSave = func() { require.NoError(t, store.put(winner)) }

	result := signInAs(t, svc, provider, service.OIDCIdentity{Subject: "subject-1", PreferredUsername: "alice"})

	require.Equal(t, model.OIDCCallbackSuccess, result.Outcome)
	require.Len(t, store.users, 1)
	require.Equal(t, winner.ID, store.bySubject(t, "subject-1").ID)
}

const (
	roleManagedMessage    = "role of this user is managed by the identity provider"
	profileManagedMessage = "username, email and password of this user are managed by the identity provider"
)

// An administrator update of an OIDC user is refused when it changes what the
// provider owns: username, email and password always, and the role while
// administrator groups are configured. Status changes stay allowed.
func TestAuthServiceRefusesAdminEditsOfProviderManagedFields(t *testing.T) {
	alice := oidcUser("subject-1", "alice", model.RoleUser)
	unchanged := service.UpdateUserInput{Username: "alice", Email: "alice@example.com", Role: model.RoleUser, Status: model.StatusActive}
	edit := func(change func(*service.UpdateUserInput)) service.UpdateUserInput {
		input := unchanged
		change(&input)
		return input
	}
	for _, tc := range []struct {
		name        string
		adminGroups []string
		input       service.UpdateUserInput
		refusal     string
	}{
		{name: "a changed username", input: edit(func(i *service.UpdateUserInput) { i.Username = "alice2" }), refusal: profileManagedMessage},
		{name: "a changed email", input: edit(func(i *service.UpdateUserInput) { i.Email = "other@example.com" }), refusal: profileManagedMessage},
		{name: "a new password", input: edit(func(i *service.UpdateUserInput) { i.Password = "secret123" }), refusal: profileManagedMessage},
		{name: "a role change under provider-managed roles", adminGroups: []string{"/dcm-admins"}, input: edit(func(i *service.UpdateUserInput) { i.Role = model.RoleAdmin }), refusal: roleManagedMessage},
		{name: "a role change under DCM-managed roles", input: edit(func(i *service.UpdateUserInput) { i.Role = model.RoleAdmin })},
		{name: "a status change under provider-managed roles", adminGroups: []string{"/dcm-admins"}, input: edit(func(i *service.UpdateUserInput) { i.Status = model.StatusDeactivated })},
		{name: "the same values in another case", adminGroups: []string{"/dcm-admins"}, input: edit(func(i *service.UpdateUserInput) { i.Username, i.Email = " Alice", "ALICE@example.com" })},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, repo, sessions := newUserStore(t, alice, localUser("root", model.RoleAdmin))
			svc, _, _ := provisioningService(t, repo, sessions, tc.adminGroups...)

			_, err := svc.UpdateUser(context.Background(), alice.ID, tc.input)

			if tc.refusal == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, apperrors.ErrBadRequest)
			require.Equal(t, tc.refusal, apperrors.SafeMessage(err))
			require.Equal(t, alice, store.users[alice.ID], "a refused update writes nothing")
		})
	}

	t.Run("a local user's profile stays editable", func(t *testing.T) {
		bob := localUser("bob", model.RoleUser)
		store, repo, sessions := newUserStore(t, bob)
		svc, _, _ := provisioningService(t, repo, sessions, "/dcm-admins")
		_, err := svc.UpdateUser(context.Background(), bob.ID, service.UpdateUserInput{Username: "bob2", Email: "bob2@example.com", Password: "secret123", Role: model.RoleAdmin, Status: model.StatusActive})
		require.NoError(t, err)
		require.Equal(t, "bob2", store.users[bob.ID].Username)
	})

	t.Run("deactivation and reactivation still work", func(t *testing.T) {
		store, repo, sessions := newUserStore(t, alice)
		svc, _, _ := provisioningService(t, repo, sessions, "/dcm-admins")
		_, err := svc.DeactivateUser(context.Background(), alice.ID)
		require.NoError(t, err)
		require.Equal(t, model.StatusDeactivated, store.users[alice.ID].Status)
		_, err = svc.ReactivateUser(context.Background(), alice.ID)
		require.NoError(t, err)
		require.Equal(t, model.StatusActive, store.users[alice.ID].Status)
	})
}

// The provider owns an OIDC user's role only while administrator groups are configured.
func TestAuthServiceReportsWhetherTheProviderManagesARole(t *testing.T) {
	_, repo, sessions := newUserStore(t)
	providerManaged, _, _ := provisioningService(t, repo, sessions, "/dcm-admins")
	dcmManaged, _, _ := provisioningService(t, repo, sessions)
	oidc, local := oidcUser("subject-1", "alice", model.RoleUser), localUser("bob", model.RoleUser)

	require.True(t, providerManaged.RoleManagedByProvider(oidc))
	require.False(t, providerManaged.RoleManagedByProvider(local))
	require.False(t, dcmManaged.RoleManagedByProvider(oidc))
}
