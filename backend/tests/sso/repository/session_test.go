package repository_test

import (
	"context"
	"errors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/permissions"
	ssomocks "github.com/callmerussell04/docker-cloud-manager/backend/tests/mocks/sso/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/lib/jwt"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/tests/testutil/dbtest"
	"github.com/stretchr/testify/require"
)

func TestSessionsRotateRevokeAndSurviveRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	users := repository.NewUserRepository(db)
	sessions := repository.NewSessionRepository(db)
	svc := service.NewAuthService(users, jwt.NewProvider("secret"), sessions)
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
	svc = service.NewAuthService(users, jwt.NewProvider("secret"), repository.NewSessionRepository(db))
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
	svc := service.NewAuthService(users, jwt.NewProvider("secret"), repository.NewSessionRepository(db))
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
	svc := service.NewAuthService(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db))
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
	svc := service.NewAuthServiceWithConfig(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db), nil, service.Config{
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
			svc := service.NewAuthService(users, jwt.NewProvider("secret"), repository.NewSessionRepository(db))
			id, err := svc.Register(ctx, "alice", "alice@example.com", "secret123")
			require.NoError(t, err)
			first, err := svc.Login(ctx, "alice", "secret123")
			require.NoError(t, err)
			second, err := svc.Login(ctx, "alice", "secret123")
			require.NoError(t, err)
			update := service.UpdateUserInput{Username: "alice", Email: "alice@example.com", Role: model.RoleUser, Status: model.StatusActive, QuotaCPU: 1, QuotaRAMMB: 2048, QuotaDiskMB: 5120}
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
			normal := service.NewAuthService(users, tokens, sessions)
			_, err := normal.Register(ctx, "alice", "alice@example.com", "secret123")
			require.NoError(t, err)
			pair, err := normal.Login(ctx, "alice", "secret123")
			require.NoError(t, err)
			var failing *service.AuthService
			switch kind {
			case "random":
				failing = service.NewAuthServiceWithConfig(users, tokens, sessions, nil, service.Config{Random: strings.NewReader("")})
			case "signing":
				signer := ssomocks.NewTokenProvider(t)
				signer.EXPECT().GenerateAccessToken(mock.Anything, mock.Anything, mock.Anything).Return("", errors.New("signing failed"))
				failing = service.NewAuthService(users, signer, sessions)
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
				failing = service.NewAuthService(users, tokens, store)
			case "commit-rollback":
				// A deferred constraint simulates a failure at COMMIT after all writes.
				// Fault injection only; observations below use the public auth service.
				_, err = db.ExecContext(ctx, `ALTER TABLE refresh_tokens ADD CONSTRAINT injected_commit_failure UNIQUE(session_id) DEFERRABLE INITIALLY DEFERRED`)
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
				failing = service.NewAuthService(users, tokens, store)
			}
			result, err := failing.Refresh(ctx, pair.RefreshToken)
			require.Error(t, err)
			require.Equal(t, model.Tokens{}, result)
			if kind == "commit-rollback" {
				_, err = db.ExecContext(ctx, `ALTER TABLE refresh_tokens DROP CONSTRAINT injected_commit_failure`)
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
	svc := service.NewAuthService(users, jwt.NewProvider("secret"), repository.NewSessionRepository(db))
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
	_, err = svc.UpdateUser(ctx, id, service.UpdateUserInput{Username: "alice", Email: "alice@example.com", Role: model.RoleAdmin, Status: model.StatusActive, QuotaCPU: 1, QuotaRAMMB: 2048, QuotaDiskMB: 5120})
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
	svc := service.NewAuthService(users, provider, sessions)
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
	staleLogin := service.NewAuthService(delayed, provider, sessions)
	result := make(chan error, 1)
	go func() { _, err := staleLogin.Login(ctx, "alice", "secret123"); result <- err }()
	<-lookedUp
	_, err = svc.UpdateUser(ctx, id, service.UpdateUserInput{Username: "alice", Email: "alice@example.com", Password: "newsecret", Role: model.RoleUser, Status: model.StatusActive, QuotaCPU: 1, QuotaRAMMB: 2048, QuotaDiskMB: 5120})
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
	svc := service.NewAuthService(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db))
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
	provider.EXPECT().AuthCodeURL(mock.Anything, mock.Anything).RunAndReturn(func(state, nonce string) string { return "https://idp.example/auth?state=" + state })
	provider.EXPECT().ExchangeCode(mock.Anything, "code", mock.Anything).Return(service.OIDCIdentity{Subject: "subject", Username: "external-user", Email: "external@example.com"}, nil)
	svc := service.NewAuthServiceWithConfig(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db), provider, service.Config{OIDCEnabled: true})
	login := func() model.Tokens {
		t.Helper()
		authURL, binding, err := svc.StartOIDCLogin(ctx, "keycloak", "/auth/callback")
		require.NoError(t, err)
		parsed, err := url.Parse(authURL)
		require.NoError(t, err)
		tokens, redirect, err := svc.CompleteOIDCCallback(ctx, "keycloak", "code", parsed.Query().Get("state"), binding)
		require.NoError(t, err)
		require.Equal(t, "/auth/callback", redirect)
		return tokens
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
