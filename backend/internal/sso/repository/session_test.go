package repository_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/permissions"
	ssomocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/sso/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/jwt"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/ssotest"
	"github.com/stretchr/testify/require"
)

func TestSessionsRotateRevokeAndSurviveRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	users := repository.NewUserRepository(db)
	sessions := repository.NewSessionRepository(db)
	svc := ssotest.NewAuthService(users, jwt.NewProvider("secret"), sessions)
	_, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
	require.NoError(t, err)
	first, err := svc.Login(ctx, "alice", "secret123")
	require.NoError(t, err)
	other, err := svc.Login(ctx, "alice", "secret123")
	require.NoError(t, err)
	require.NotEqual(t, first.AccessToken, other.AccessToken)
	require.NotEqual(t, first.RefreshToken, other.RefreshToken)
	next, err := svc.Refresh(ctx, first.RefreshToken)
	require.NoError(t, err)
	require.NotEqual(t, first.RefreshToken, next.RefreshToken)
	_, err = svc.Refresh(ctx, first.RefreshToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	_, err = svc.VerifyAccessToken(ctx, next.AccessToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	svc = ssotest.NewAuthService(users, jwt.NewProvider("secret"), repository.NewSessionRepository(db))
	_, err = svc.Refresh(ctx, next.RefreshToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	_, err = svc.VerifyAccessToken(ctx, other.AccessToken)
	require.NoError(t, err)
	require.NoError(t, svc.Logout(ctx, other.RefreshToken))
	require.NoError(t, svc.Logout(ctx, other.RefreshToken))
	_, err = svc.VerifyAccessToken(ctx, other.AccessToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
}

func TestConcurrentRefreshRevokesWinnerAndOtherSessionsSurvive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	users := repository.NewUserRepository(db)
	svc := ssotest.NewAuthService(users, jwt.NewProvider("secret"), repository.NewSessionRepository(db))
	_, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
	require.NoError(t, err)
	first, err := svc.Login(ctx, "alice", "secret123")
	require.NoError(t, err)
	other, err := svc.Login(ctx, "alice", "secret123")
	require.NoError(t, err)
	type result struct {
		tokens model.Tokens
		err    error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		go func() { <-start; tokens, err := svc.Refresh(ctx, first.RefreshToken); results <- result{tokens, err} }()
	}
	close(start)
	var winner model.Tokens
	successes := 0
	for range 2 {
		select {
		case result := <-results:
			if result.err == nil {
				successes++
				winner = result.tokens
			} else {
				require.ErrorIs(t, result.err, apperrors.ErrInvalidToken)
			}
		case <-ctx.Done():
			t.Fatal("refresh did not terminate")
		}
	}
	require.Equal(t, 1, successes)
	_, err = svc.VerifyAccessToken(ctx, winner.AccessToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	_, err = svc.Refresh(ctx, winner.RefreshToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	_, err = svc.VerifyAccessToken(ctx, other.AccessToken)
	require.NoError(t, err)
}

func TestLogoutRacingRefreshCannotLeaveUsableTokens(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	svc := ssotest.NewAuthService(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db))
	_, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
	require.NoError(t, err)
	for range 8 {
		first, err := svc.Login(ctx, "alice", "secret123")
		require.NoError(t, err)
		type result struct {
			tokens model.Tokens
			err    error
		}
		refreshed := make(chan result, 1)
		loggedOut := make(chan error, 1)
		start := make(chan struct{})
		go func() { <-start; tokens, err := svc.Refresh(ctx, first.RefreshToken); refreshed <- result{tokens, err} }()
		go func() { <-start; loggedOut <- svc.Logout(ctx, first.RefreshToken) }()
		close(start)
		require.NoError(t, <-loggedOut)
		next := <-refreshed
		if next.err == nil {
			_, err = svc.VerifyAccessToken(ctx, next.tokens.AccessToken)
			require.ErrorIs(t, err, apperrors.ErrInvalidToken)
			_, err = svc.Refresh(ctx, next.tokens.RefreshToken)
			require.ErrorIs(t, err, apperrors.ErrInvalidToken)
		} else {
			require.ErrorIs(t, next.err, apperrors.ErrInvalidToken)
		}
		_, err = svc.VerifyAccessToken(ctx, first.AccessToken)
		require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	}
}

func TestSessionExpiryBoundariesAndAbsoluteCap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	now := time.Now().UTC().Truncate(time.Second)
	beginning := now
	svc := service.NewAuthServiceWithConfig(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db), service.OIDCDeps{}, service.Config{
		LocalLoginEnabled: true, LocalRegisterEnabled: true, Now: func() time.Time { return now },
	})
	_, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
	require.NoError(t, err)
	pair, err := svc.Login(ctx, "alice", "secret123")
	require.NoError(t, err)
	require.Equal(t, beginning.Add(7*24*time.Hour), pair.RefreshExpiresAt)
	idle, err := svc.Login(ctx, "alice", "secret123")
	require.NoError(t, err)
	now = beginning.Add(6 * 24 * time.Hour)
	pair, err = svc.Refresh(ctx, pair.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, beginning.Add(13*24*time.Hour), pair.RefreshExpiresAt)
	now = beginning.Add(7 * 24 * time.Hour)
	_, err = svc.VerifyAccessToken(ctx, idle.AccessToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	_, err = svc.Refresh(ctx, idle.RefreshToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	for _, day := range []int{12, 18, 24, 29} {
		now = beginning.Add(time.Duration(day) * 24 * time.Hour)
		pair, err = svc.Refresh(ctx, pair.RefreshToken)
		require.NoError(t, err)
	}
	require.Equal(t, beginning.Add(30*24*time.Hour), pair.RefreshExpiresAt)
	now = beginning.Add(30 * 24 * time.Hour)
	_, err = svc.Refresh(ctx, pair.RefreshToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	_, err = svc.VerifyAccessToken(ctx, pair.AccessToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	require.NoError(t, svc.CleanupSessions(ctx))
	_, err = svc.Refresh(ctx, pair.RefreshToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
}

func TestPasswordAndStatusChangesInvalidateEverySession(t *testing.T) {
	for _, kind := range []string{"password", "deactivate", "update-deactivate"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			db := dbtest.OpenSSOPostgres(t)
			users := repository.NewUserRepository(db)
			svc := ssotest.NewAuthService(users, jwt.NewProvider("secret"), repository.NewSessionRepository(db))
			id, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
			require.NoError(t, err)
			first, err := svc.Login(ctx, "alice", "secret123")
			require.NoError(t, err)
			second, err := svc.Login(ctx, "alice", "secret123")
			require.NoError(t, err)
			update := service.UpdateUserInput{Username: "alice", Email: "alice@example.com", Role: model.RoleUser, Status: model.StatusActive}
			if kind == "password" {
				update.Password = "newsecret"
			} else {
				update.Status = model.StatusDeactivated
			}
			if kind == "deactivate" {
				_, err = svc.DeactivateUser(ctx, id)
			} else {
				_, err = svc.UpdateUser(ctx, id, update)
			}
			require.NoError(t, err)
			if kind != "password" {
				_, err = svc.ReactivateUser(ctx, id)
				require.NoError(t, err)
			}
			for _, pair := range []model.Tokens{first, second} {
				_, err = svc.VerifyAccessToken(ctx, pair.AccessToken)
				require.ErrorIs(t, err, apperrors.ErrInvalidToken)
				_, err = svc.Refresh(ctx, pair.RefreshToken)
				require.ErrorIs(t, err, apperrors.ErrInvalidToken)
			}
		})
	}
}

func TestRefreshFailuresPreserveRollbackAndHandleUncertainCommit(t *testing.T) {
	for _, kind := range []string{"random", "signing", "write", "commit-rollback", "commit-uncertain"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			db := dbtest.OpenSSOPostgres(t)
			users := repository.NewUserRepository(db)
			sessions := repository.NewSessionRepository(db)
			tokens := jwt.NewProvider("secret")
			normal := ssotest.NewAuthService(users, tokens, sessions)
			_, err := normal.Register(ctx, "alice", "alice@example.com", "secret123")
			require.NoError(t, err)
			pair, err := normal.Login(ctx, "alice", "secret123")
			require.NoError(t, err)
			var failing *service.AuthService
			switch kind {
			case "random":
				failing = service.NewAuthServiceWithConfig(users, tokens, sessions, service.OIDCDeps{}, service.Config{Random: strings.NewReader("")})
			case "signing":
				signer := ssomocks.NewTokenProvider(t)
				signer.EXPECT().GenerateAccessToken(mock.Anything, mock.Anything, mock.Anything).Return("", errors.New("signing failed"))
				failing = ssotest.NewAuthService(users, signer, sessions)
			case "write":
				store := ssomocks.NewSessionRepository(t)
				store.EXPECT().FindRefresh(mock.Anything, mock.Anything).RunAndReturn(sessions.FindRefresh)
				store.EXPECT().WithUser(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, id uuid.UUID, operation string, fn func(model.User, service.SessionTransaction) error) error {
					return sessions.WithUser(ctx, id, operation, func(user model.User, tx service.SessionTransaction) error {
						wrapper := ssomocks.NewSessionTransaction(t)
						wrapper.EXPECT().GetRefresh(mock.Anything, mock.Anything).RunAndReturn(tx.GetRefresh)
						wrapper.EXPECT().Rotate(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, s model.Session, old, next model.RefreshToken) error {
							if err := tx.Rotate(ctx, s, old, next); err != nil {
								return err
							}
							return apperrors.ErrUnavailable
						})
						return fn(user, wrapper)
					})
				})
				failing = ssotest.NewAuthService(users, tokens, store)
			case "commit-rollback":
				// A deferred constraint simulates a failure at COMMIT after all writes.
				// Fault injection only; observations below use the public auth service.
				_, err = db.Exec(ctx, `ALTER TABLE refresh_tokens ADD CONSTRAINT injected_commit_failure UNIQUE(session_id) DEFERRABLE INITIALLY DEFERRED`)
				require.NoError(t, err)
				failing = normal
			case "commit-uncertain":
				store := ssomocks.NewSessionRepository(t)
				store.EXPECT().FindRefresh(mock.Anything, mock.Anything).RunAndReturn(sessions.FindRefresh)
				store.EXPECT().WithUser(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(ctx context.Context, id uuid.UUID, operation string, fn func(model.User, service.SessionTransaction) error) error {
					if err := sessions.WithUser(ctx, id, operation, fn); err != nil {
						return err
					}
					return apperrors.ErrTimeout
				})
				failing = ssotest.NewAuthService(users, tokens, store)
			}
			result, err := failing.Refresh(ctx, pair.RefreshToken)
			require.Error(t, err)
			require.Equal(t, model.Tokens{}, result)
			if kind == "commit-rollback" {
				_, err = db.Exec(ctx, `ALTER TABLE refresh_tokens DROP CONSTRAINT injected_commit_failure`)
				require.NoError(t, err)
			}
			if kind == "commit-uncertain" {
				_, err = normal.Refresh(ctx, pair.RefreshToken)
				require.ErrorIs(t, err, apperrors.ErrInvalidToken)
				_, err = normal.VerifyAccessToken(ctx, pair.AccessToken)
				require.ErrorIs(t, err, apperrors.ErrInvalidToken)
			} else {
				_, err = normal.Refresh(ctx, pair.RefreshToken)
				require.NoError(t, err)
			}
		})
	}
}

func TestUnknownSecretAndRoleUpdatesPreserveOtherSessions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	users := repository.NewUserRepository(db)
	svc := ssotest.NewAuthService(users, jwt.NewProvider("secret"), repository.NewSessionRepository(db))
	id, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
	require.NoError(t, err)
	pair, err := svc.Login(ctx, "alice", "secret123")
	require.NoError(t, err)
	unknown := strings.Repeat("A", 43)
	_, err = svc.Refresh(ctx, unknown)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	require.NoError(t, svc.Logout(ctx, unknown))
	require.NoError(t, svc.Logout(ctx, ""))
	for _, value := range []string{"legacy.jwt.token", strings.Repeat("x", 5000)} {
		_, err = svc.Refresh(ctx, value)
		require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	}
	_, err = svc.UpdateUser(ctx, id, service.UpdateUserInput{Username: "alice", Email: "alice@example.com", Role: model.RoleAdmin, Status: model.StatusActive})
	require.NoError(t, err)
	user, allowed, err := svc.CheckPermission(ctx, pair.AccessToken, permissions.UsersAdminList)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, model.RoleAdmin, user.Role)
}

func TestPasswordChangeRejectsLoginUsingEarlierCredentialSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	users := repository.NewUserRepository(db)
	sessions := repository.NewSessionRepository(db)
	provider := jwt.NewProvider("secret")
	svc := ssotest.NewAuthService(users, provider, sessions)
	id, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
	require.NoError(t, err)
	lookedUp := make(chan struct{})
	resume := make(chan struct{})
	delayed := ssomocks.NewUserRepository(t)
	delayed.EXPECT().GetUserByUsername(mock.Anything, "alice").RunAndReturn(func(ctx context.Context, name string) (model.User, error) {
		user, err := users.GetUserByUsername(ctx, name)
		close(lookedUp)
		select {
		case <-resume:
			return user, err
		case <-ctx.Done():
			return model.User{}, ctx.Err()
		}
	})
	staleLogin := ssotest.NewAuthService(delayed, provider, sessions)
	result := make(chan error, 1)
	go func() { _, err := staleLogin.Login(ctx, "alice", "secret123"); result <- err }()
	<-lookedUp
	_, err = svc.UpdateUser(ctx, id, service.UpdateUserInput{Username: "alice", Email: "alice@example.com", Password: "newsecret", Role: model.RoleUser, Status: model.StatusActive})
	close(resume)
	require.NoError(t, err)
	require.ErrorIs(t, <-result, apperrors.ErrInvalidCredentials)
	_, err = svc.Login(ctx, "alice", "newsecret")
	require.NoError(t, err)
}

func TestDeactivationRacingRefreshRevokesEveryResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	svc := ssotest.NewAuthService(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db))
	id, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
	require.NoError(t, err)
	first, err := svc.Login(ctx, "alice", "secret123")
	require.NoError(t, err)
	type result struct {
		tokens model.Tokens
		err    error
	}
	refreshed := make(chan result, 1)
	deactivated := make(chan error, 1)
	start := make(chan struct{})
	go func() { <-start; tokens, err := svc.Refresh(ctx, first.RefreshToken); refreshed <- result{tokens, err} }()
	go func() { <-start; _, err := svc.DeactivateUser(ctx, id); deactivated <- err }()
	close(start)
	require.NoError(t, <-deactivated)
	next := <-refreshed
	_, err = svc.ReactivateUser(ctx, id)
	require.NoError(t, err)
	_, err = svc.VerifyAccessToken(ctx, first.AccessToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	if next.err == nil {
		_, err = svc.VerifyAccessToken(ctx, next.tokens.AccessToken)
		require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	} else {
		require.ErrorIs(t, next.err, apperrors.ErrInvalidToken)
	}
}

func TestOIDCLoginsUseIndependentDurableSessions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	provider := ssomocks.NewOIDCProvider(t)
	provider.EXPECT().Available().Return(true).Maybe()
	provider.EXPECT().AuthCodeURL(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(state, nonce, verifier string) string { return "https://idp.example/auth?state=" + state })
	provider.EXPECT().ExchangeCode(mock.Anything, "code", mock.Anything, mock.Anything).Return(service.OIDCIdentity{Subject: "subject", PreferredUsername: "external-user", Email: "external@example.com"}, nil)
	svc := service.NewAuthServiceWithConfig(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db), service.OIDCDeps{Provider: provider, LoginAttempts: repository.NewLoginAttemptRepository(db)}, service.Config{OIDCEnabled: true})
	login := func() model.Tokens {
		t.Helper()
		started, err := svc.StartOIDCLogin(ctx, "keycloak", "/auth/callback")
		require.NoError(t, err)
		result, err := svc.CompleteOIDCCallback(ctx, "keycloak", "code", started.State, started.StateBinding)
		require.NoError(t, err)
		require.Equal(t, model.OIDCCallbackSuccess, result.Outcome)
		require.Equal(t, "/auth/callback", result.RedirectAfter)
		return result.Tokens
	}
	first := login()
	second := login()
	require.NotEqual(t, first.AccessToken, second.AccessToken)
	require.NotEqual(t, first.RefreshToken, second.RefreshToken)
	require.NoError(t, svc.Logout(ctx, first.RefreshToken))
	_, err := svc.VerifyAccessToken(ctx, first.AccessToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	_, err = svc.VerifyAccessToken(ctx, second.AccessToken)
	require.NoError(t, err)
	third := login() // A fresh provider login is allowed after DCM logout.
	_, err = svc.VerifyAccessToken(ctx, third.AccessToken)
	require.NoError(t, err)
}

// Two writes that each remove a different active administrator must not both
// pass the last-administrator check. A blocker holds both rows so the two
// writes are in flight together before either can commit.
func TestConcurrentAdminRemovalsKeepOneActiveAdmin(t *testing.T) {
	type change func(ctx context.Context, svc *service.AuthService, admin model.User) error
	demote := func(ctx context.Context, svc *service.AuthService, admin model.User) error {
		_, err := svc.UpdateUser(ctx, admin.ID, service.UpdateUserInput{Username: admin.Username, Email: admin.Email, Role: model.RoleUser, Status: model.StatusActive})
		return err
	}
	deactivateByStatus := func(ctx context.Context, svc *service.AuthService, admin model.User) error {
		_, err := svc.UpdateUser(ctx, admin.ID, service.UpdateUserInput{Username: admin.Username, Email: admin.Email, Role: model.RoleAdmin, Status: model.StatusDeactivated})
		return err
	}
	deactivate := func(ctx context.Context, svc *service.AuthService, admin model.User) error {
		_, err := svc.DeactivateUser(ctx, admin.ID)
		return err
	}
	tests := []struct {
		name          string
		first, second change
		secondIsOIDC  bool
	}{
		{name: "deactivation and role change", first: deactivate, second: demote},
		{name: "two status changes", first: deactivateByStatus, second: deactivateByStatus},
		{name: "role change and OIDC sign-in demotion", first: demote, secondIsOIDC: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			db := dbtest.OpenSSOPostgres(t)
			users := repository.NewUserRepository(db)
			provider := ssomocks.NewOIDCProvider(t)
			provider.EXPECT().Available().Return(true).Maybe()
			provider.EXPECT().AuthCodeURL(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(func(state, nonce, verifier string) string { return "https://idp.example/auth?state=" + state }).Maybe()
			svc := service.NewAuthServiceWithConfig(users, jwt.NewProvider("secret"), repository.NewSessionRepository(db), service.OIDCDeps{Provider: provider, LoginAttempts: repository.NewLoginAttemptRepository(db)}, service.Config{
				OIDCEnabled: true, OIDCProviderName: "keycloak", OIDCAdminGroups: []string{"dcm-admins"},
			})

			first := adminUser("admin-a")
			second := adminUser("admin-b")
			if tt.secondIsOIDC {
				second.PasswordHash = ""
				second.AuthSource = model.AuthSourceOIDC
				second.ExternalProvider = "keycloak"
				second.ExternalSubject = "admin-b-subject"
				// The provider no longer lists the administrator group, so sign-in demotes.
				provider.EXPECT().ExchangeCode(mock.Anything, "code", mock.Anything, mock.Anything).Return(service.OIDCIdentity{
					Subject: second.ExternalSubject, PreferredUsername: second.Username, Email: second.Email, EmailVerified: true,
				}, nil)
				started, err := svc.StartOIDCLogin(ctx, "keycloak", "/")
				require.NoError(t, err)
				tt.second = func(ctx context.Context, svc *service.AuthService, _ model.User) error {
					result, err := svc.CompleteOIDCCallback(ctx, "keycloak", "code", started.State, started.StateBinding)
					if err != nil {
						return err
					}
					switch result.Outcome {
					case model.OIDCCallbackSuccess:
						return nil
					case model.OIDCCallbackAccountConflict:
						// A refused sign-in is an outcome, not an error.
						return fmt.Errorf("oidc sign-in refused: %w", apperrors.ErrConflict)
					default:
						return fmt.Errorf("oidc sign-in ended with %s", result.Outcome)
					}
				}
			}
			require.NoError(t, users.SaveUser(ctx, first))
			require.NoError(t, users.SaveUser(ctx, second))

			blocker, err := db.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = blocker.Rollback(ctx) }()
			var blockerPID int
			require.NoError(t, blocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID))
			_, err = blocker.Exec(ctx, `SELECT 1 FROM users WHERE id = ANY($1) FOR UPDATE`, []uuid.UUID{first.ID, second.ID})
			require.NoError(t, err)

			errs := make(chan error, 2)
			go func() { errs <- tt.first(ctx, svc, first) }()
			go func() { errs <- tt.second(ctx, svc, second) }()
			require.Eventually(t, func() bool {
				return backendsWaitingOn(ctx, db, blockerPID) == 2
			}, 5*time.Second, 5*time.Millisecond, "both changes must be in flight before the blocker releases them")
			require.NoError(t, blocker.Rollback(ctx))

			var refused []error
			for range 2 {
				if err := <-errs; err != nil {
					refused = append(refused, err)
				}
			}
			if tt.secondIsOIDC {
				// The sign-in is never refused: it demotes first and the role
				// change is refused, or it finds the last active administrator
				// and keeps the role.
				require.LessOrEqual(t, len(refused), 1)
			} else {
				require.Len(t, refused, 1, "exactly one change must succeed")
			}
			for _, err := range refused {
				require.EqualError(t, err, "cannot deactivate or demote the last active admin")
			}
			stored, err := svc.GetUsers(ctx, []uuid.UUID{first.ID, second.ID})
			require.NoError(t, err)
			active := 0
			for _, u := range stored {
				if u.Role == model.RoleAdmin && u.Status == model.StatusActive {
					active++
				}
			}
			require.Equal(t, 1, active)
		})
	}
}

func adminUser(username string) model.User {
	return model.User{
		ID: uuid.New(), Username: username, Email: username + "@example.com", PasswordHash: "hash-" + username,
		Role: model.RoleAdmin, Status: model.StatusActive, AuthSource: model.AuthSourceLocal,
	}
}

// backendsWaitingOn counts the backends blocked by pid, directly or behind
// another backend it blocks.
func backendsWaitingOn(ctx context.Context, db *pgxpool.Pool, pid int) int {
	var waiting int
	err := db.QueryRow(ctx, `WITH RECURSIVE waiting(pid) AS (
		SELECT a.pid FROM pg_stat_activity a WHERE $1 = ANY(pg_blocking_pids(a.pid))
		UNION
		SELECT a.pid FROM pg_stat_activity a JOIN waiting w ON w.pid = ANY(pg_blocking_pids(a.pid))
	) SELECT count(*) FROM waiting`, pid).Scan(&waiting)
	if err != nil {
		return -1
	}
	return waiting
}
