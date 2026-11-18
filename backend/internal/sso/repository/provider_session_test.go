package repository_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/jwt"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/repository"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/dbtest"
)

// fakeProvider is an OIDC provider that signs in whichever identity was
// registered for a code, verifies the logout tokens it was given, builds a
// sign-out URL from the ID token, and can be made unreachable.
type fakeProvider struct {
	mu          sync.Mutex
	identities  map[string]service.OIDCIdentity
	logouts     map[string]service.OIDCLogout
	signOuts    []string
	unavailable atomic.Bool
}

var _ service.OIDCProvider = (*fakeProvider)(nil)

func (p *fakeProvider) Available() bool { return !p.unavailable.Load() }

func (p *fakeProvider) AuthCodeURL(string, string, string) string { return "https://idp.example/auth" }

func (p *fakeProvider) ExchangeCode(_ context.Context, code, _, _ string) (service.OIDCIdentity, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	identity, ok := p.identities[code]
	if !ok {
		return service.OIDCIdentity{}, apperrors.ErrInvalidCredentials
	}
	return identity, nil
}

// EndSessionURL records each sign-out URL it builds.
func (p *fakeProvider) EndSessionURL(idTokenHint, postLogoutRedirectURI string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	signOut := "https://idp.example/logout?id_token_hint=" + idTokenHint + "&post_logout_redirect_uri=" + postLogoutRedirectURI
	p.signOuts = append(p.signOuts, signOut)
	return signOut
}

func (p *fakeProvider) VerifyLogoutToken(_ context.Context, token string) (service.OIDCLogout, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	logout, ok := p.logouts[token]
	if !ok {
		return service.OIDCLogout{}, apperrors.ErrInvalidToken
	}
	return logout, nil
}

// oidcSessions signs users in through OIDC callbacks against a fake provider,
// over PostgreSQL and on an injected clock. Local sign-in is on too.
type oidcSessions struct {
	t        *testing.T
	ctx      context.Context
	svc      *service.AuthService
	provider *fakeProvider
}

func newOIDCSessions(t *testing.T, ctx context.Context, db *pgxpool.Pool, now *time.Time) *oidcSessions {
	t.Helper()
	provider := &fakeProvider{identities: map[string]service.OIDCIdentity{}, logouts: map[string]service.OIDCLogout{}}
	svc := service.NewAuthServiceWithConfig(repository.NewUserRepository(db), jwt.NewProvider("secret"), repository.NewSessionRepository(db),
		service.OIDCDeps{Provider: provider, LoginAttempts: repository.NewLoginAttemptRepository(db)},
		service.Config{LocalLoginEnabled: true, LocalRegisterEnabled: true, OIDCEnabled: true, Now: func() time.Time { return *now }})
	return &oidcSessions{t: t, ctx: ctx, svc: svc, provider: provider}
}

// signIn completes one OIDC sign-in of identity and returns its tokens.
func (s *oidcSessions) signIn(identity service.OIDCIdentity) model.Tokens {
	s.t.Helper()
	started, err := s.svc.StartOIDCLogin(s.ctx, "keycloak", "/auth/callback")
	require.NoError(s.t, err)
	s.provider.mu.Lock()
	code := fmt.Sprintf("code-%d", len(s.provider.identities))
	s.provider.identities[code] = identity
	s.provider.mu.Unlock()
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
	s.provider.mu.Lock()
	defer s.provider.mu.Unlock()
	s.provider.logouts[token] = logout
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

	for name, tokens := range map[string]model.Tokens{
		"local": local, "ended at the provider": endedAtProvider, "without an ID token": withoutIDToken, "provider unreachable": whileUnreachable,
	} {
		s.provider.unavailable.Store(name == "provider unreachable")
		signOutURL, err := s.svc.Logout(ctx, tokens.RefreshToken, loginPage)
		require.NoError(t, err, name)
		require.Empty(t, signOutURL, name)
		s.requireValid(tokens.AccessToken, false)
	}
	require.Len(t, s.provider.signOuts, 1, "only the first sign-out of a live OIDC session with an ID token builds a URL")
	var idTokens int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE id_token IS NOT NULL`).Scan(&idTokens))
	require.Zero(t, idTokens)
}
