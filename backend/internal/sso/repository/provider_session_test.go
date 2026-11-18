package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/jwt"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
	ssomocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/sso/service"
)

// oidcSessions signs users in through OIDC callbacks against the generated
// provider mock, over PostgreSQL and on an injected clock. Local sign-in is on
// too. The provider is reachable until a test makes it unreachable, and each
// sign-in and logout scripts the provider answer it needs.
type oidcSessions struct {
	t         *testing.T
	ctx       context.Context
	svc       *service.AuthService
	provider  *ssomocks.OIDCProvider
	available *mock.Call
	codes     int
}

func newOIDCSessions(t *testing.T, ctx context.Context, db *pgxpool.Pool, now *time.Time) *oidcSessions {
	t.Helper()
	provider := ssomocks.NewOIDCProvider(t)
	available := provider.EXPECT().Available().Return(true).Maybe()
	provider.EXPECT().AuthCodeURL(mock.Anything, mock.Anything, mock.Anything).Return("https://idp.example/auth").Maybe()
	svc := service.NewAuthServiceWithConfig(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db),
		service.OIDCDeps{Provider: provider, LoginAttempts: repository.NewLoginAttemptRepository(db)},
		service.Config{LocalLoginEnabled: true, LocalRegisterEnabled: true, OIDCEnabled: true, Now: func() time.Time { return *now }})
	return &oidcSessions{t: t, ctx: ctx, svc: svc, provider: provider, available: available}
}

// makeUnreachable makes the provider report itself unavailable from now on.
func (s *oidcSessions) makeUnreachable() {
	s.available.Unset()
	s.provider.EXPECT().Available().Return(false).Maybe()
}

// signIn completes one OIDC sign-in of identity and returns its tokens.
func (s *oidcSessions) signIn(identity service.OIDCIdentity) model.Tokens {
	s.t.Helper()
	started, err := s.svc.StartOIDCLogin(s.ctx, "keycloak", "/auth/callback")
	require.NoError(s.t, err)
	s.codes++
	code := fmt.Sprintf("code-%d", s.codes)
	s.provider.EXPECT().ExchangeCode(mock.Anything, code, mock.Anything, mock.Anything).Return(identity, nil).Once()
	result, err := s.svc.CompleteOIDCCallback(s.ctx, "keycloak", code, started.State, started.StateBinding)
	require.NoError(s.t, err)
	require.Equal(s.t, model.OIDCCallbackSuccess, result.Outcome)
	return result.Tokens
}

func aliceAtTheProvider() service.OIDCIdentity {
	return service.OIDCIdentity{Subject: "subject-alice", PreferredUsername: "alice", Email: "alice@example.com", EmailVerified: true}
}

// A session created by an OIDC callback ends 12 hours after sign-in, however
// recently it was refreshed; a local session signed in at the same moment
// keeps going.
func TestOIDCSessionsEndTwelveHoursAfterSignIn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	now := time.Now().UTC().Truncate(time.Second)
	beginning := now
	s := newOIDCSessions(t, ctx, db, &now)
	_, err := s.svc.Register(ctx, "bob", "bob@example.com", "secret123")
	require.NoError(t, err)
	local, err := loginTokens(s.svc.Login(ctx, "bob", "secret123"))
	require.NoError(t, err)
	oidc := s.signIn(aliceAtTheProvider())
	require.Equal(t, beginning.Add(12*time.Hour), oidc.RefreshExpiresAt, "the refresh cookie lives no longer than the session")
	require.Equal(t, beginning.Add(7*24*time.Hour), local.RefreshExpiresAt)

	now = beginning.Add(12*time.Hour - time.Second)
	oidc, err = s.svc.Refresh(ctx, oidc.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, beginning.Add(12*time.Hour), oidc.RefreshExpiresAt)
	_, err = s.svc.VerifyAccessToken(ctx, oidc.AccessToken)
	require.NoError(t, err)

	now = beginning.Add(12 * time.Hour)
	_, err = s.svc.VerifyAccessToken(ctx, oidc.AccessToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	_, err = s.svc.Refresh(ctx, oidc.RefreshToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)

	local, err = s.svc.Refresh(ctx, local.RefreshToken)
	require.NoError(t, err)
	_, err = s.svc.VerifyAccessToken(ctx, local.AccessToken)
	require.NoError(t, err)
}

// scriptLogout makes the provider verify token as the logout of a provider
// session (sid) or, without one, of a subject.
func (s *oidcSessions) scriptLogout(token string, logout service.OIDCLogout) {
	s.provider.EXPECT().VerifyLogoutToken(mock.Anything, token).Return(logout, nil)
}

func (s *oidcSessions) requireValid(access string, valid bool) {
	s.t.Helper()
	_, err := s.svc.VerifyAccessToken(s.ctx, access)
	if valid {
		require.NoError(s.t, err)
		return
	}
	require.ErrorIs(s.t, err, apperrors.ErrInvalidToken)
}

// A back-channel logout naming a provider session ends only the DCM sessions
// signed in with it; one naming only a subject ends every session of that user
// signed in through the provider. Other users and local sessions keep working,
// and a repeated logout changes nothing.
func TestBackchannelLogoutRevokesTheSessionsItNames(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	now := time.Now().UTC().Truncate(time.Second)
	s := newOIDCSessions(t, ctx, db, &now)
	_, err := s.svc.Register(ctx, "carol", "carol@example.com", "secret123")
	require.NoError(t, err)
	carol, err := loginTokens(s.svc.Login(ctx, "carol", "secret123"))
	require.NoError(t, err)
	alice := aliceAtTheProvider()
	alice.SessionID, alice.IDToken = "provider-session-a", "id-token-a"
	aliceA := s.signIn(alice)
	alice.SessionID, alice.IDToken = "provider-session-b", "id-token-b"
	aliceB := s.signIn(alice)
	bob := service.OIDCIdentity{Subject: "subject-bob", PreferredUsername: "bob", SessionID: "provider-session-c", IDToken: "id-token-c"}
	bobC := s.signIn(bob)

	s.scriptLogout("logout-a", service.OIDCLogout{Subject: "subject-alice", SessionID: "provider-session-a"})
	revoked, err := s.svc.BackchannelLogout(ctx, "keycloak", "logout-a")
	require.NoError(t, err)
	require.Equal(t, 1, revoked)
	s.requireValid(aliceA.AccessToken, false)
	_, err = s.svc.Refresh(ctx, aliceA.RefreshToken)
	require.ErrorIs(t, err, apperrors.ErrInvalidToken)
	s.requireValid(aliceB.AccessToken, true)
	s.requireValid(bobC.AccessToken, true)

	revoked, err = s.svc.BackchannelLogout(ctx, "keycloak", "logout-a")
	require.NoError(t, err)
	require.Zero(t, revoked, "a repeated logout finds nothing left to revoke")

	alice.SessionID, alice.IDToken = "provider-session-d", "id-token-d"
	aliceD := s.signIn(alice)
	s.scriptLogout("logout-alice", service.OIDCLogout{Subject: "subject-alice"})
	revoked, err = s.svc.BackchannelLogout(ctx, "keycloak", "logout-alice")
	require.NoError(t, err)
	require.Equal(t, 2, revoked)
	s.requireValid(aliceB.AccessToken, false)
	s.requireValid(aliceD.AccessToken, false)
	s.requireValid(bobC.AccessToken, true)
	s.requireValid(carol.AccessToken, true)

	s.scriptLogout("logout-unknown-session", service.OIDCLogout{SessionID: "provider-session-unknown"})
	s.scriptLogout("logout-unknown-subject", service.OIDCLogout{Subject: "subject-unknown"})
	for _, token := range []string{"logout-unknown-session", "logout-unknown-subject"} {
		revoked, err = s.svc.BackchannelLogout(ctx, "keycloak", token)
		require.NoError(t, err)
		require.Zero(t, revoked)
	}
	s.requireValid(bobC.AccessToken, true)

	// Provider-driven revocations carry their own reason, and no revoked
	// session keeps its ID token.
	var providerLogouts, idTokens int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE revoked_reason='provider_logout'),
 count(*) FILTER (WHERE id_token IS NOT NULL) FROM sessions`).Scan(&providerLogouts, &idTokens))
	require.Equal(t, 3, providerLogouts)
	require.Equal(t, 1, idTokens, "only bob's live session keeps its ID token")
}

const loginPage = "http://frontend.example/login"

// Signing out of a session created through OIDC answers the provider's
// sign-out URL for that session's ID token, once: the ID token goes with the
// revocation. A local session, a session the provider already ended, a
// session signed in without an ID token and a sign-out while the provider is
// unreachable get no URL; each sign-out still revokes the session.
func TestLogoutOfAnOIDCSessionAnswersTheProviderSignOutURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := dbtest.OpenSSOPostgres(t)
	now := time.Now().UTC().Truncate(time.Second)
	s := newOIDCSessions(t, ctx, db, &now)
	alice := aliceAtTheProvider()
	alice.SessionID, alice.IDToken = "provider-session-a", "id-token-a"
	signedIn := s.signIn(alice)
	// Strict: a second sign-out URL, for any session, fails the test.
	s.provider.EXPECT().EndSessionURL("id-token-a", loginPage).
		Return("https://idp.example/logout?id_token_hint=id-token-a&post_logout_redirect_uri=" + loginPage).Once()

	signOutURL, err := s.svc.Logout(ctx, signedIn.RefreshToken, loginPage)
	require.NoError(t, err)
	require.Equal(t, "https://idp.example/logout?id_token_hint=id-token-a&post_logout_redirect_uri="+loginPage, signOutURL)
	s.requireValid(signedIn.AccessToken, false)
	signOutURL, err = s.svc.Logout(ctx, signedIn.RefreshToken, loginPage)
	require.NoError(t, err)
	require.Empty(t, signOutURL, "the ID token went with the first sign-out")

	_, err = s.svc.Register(ctx, "carol", "carol@example.com", "secret123")
	require.NoError(t, err)
	local, err := loginTokens(s.svc.Login(ctx, "carol", "secret123"))
	require.NoError(t, err)
	alice.SessionID, alice.IDToken = "provider-session-b", "id-token-b"
	endedAtProvider := s.signIn(alice)
	s.scriptLogout("logout-b", service.OIDCLogout{SessionID: "provider-session-b"})
	_, err = s.svc.BackchannelLogout(ctx, "keycloak", "logout-b")
	require.NoError(t, err)
	alice.SessionID, alice.IDToken = "provider-session-c", ""
	withoutIDToken := s.signIn(alice)
	alice.SessionID, alice.IDToken = "provider-session-d", "id-token-d"
	whileUnreachable := s.signIn(alice)

	signOutWithoutURL := func(name string, tokens model.Tokens) {
		t.Helper()
		signOutURL, err := s.svc.Logout(ctx, tokens.RefreshToken, loginPage)
		require.NoError(t, err, name)
		require.Empty(t, signOutURL, name)
		s.requireValid(tokens.AccessToken, false)
	}
	signOutWithoutURL("local", local)
	signOutWithoutURL("ended at the provider", endedAtProvider)
	signOutWithoutURL("without an ID token", withoutIDToken)
	s.makeUnreachable()
	signOutWithoutURL("provider unreachable", whileUnreachable)
	var idTokens int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE id_token IS NOT NULL`).Scan(&idTokens))
	require.Zero(t, idTokens)
}
