package oidc_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/oidc"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/oidctest"
	"github.com/stretchr/testify/require"
)

const (
	testNonce    = "nonce-value-0123456789"
	testState    = "state-value-0123456789"
	testVerifier = "verifier-value-0123456789-0123456789-0123456789"
)

// discovered returns an available adapter for idp.
func discovered(t *testing.T, idp *oidctest.Provider, edit ...func(*oidc.Config)) *oidc.Provider {
	t.Helper()
	cfg := fakeProviderConfig(idp)
	for _, e := range edit {
		e(&cfg)
	}
	provider, err := oidc.NewProvider(cfg)
	require.NoError(t, err)
	require.NoError(t, provider.Discover(context.Background()))
	require.True(t, provider.Available())
	return provider
}

// signInCode plays the browser through the provider and returns the code it
// sends back to DCM.
func signInCode(t *testing.T, idp *oidctest.Provider, provider *oidc.Provider) string {
	t.Helper()
	back := idp.SignIn(provider.AuthCodeURL(testState, testNonce, testVerifier))
	require.Equal(t, testState, back.Query().Get("state"))
	return back.Query().Get("code")
}

func TestAuthCodeURLCarriesAPKCEChallenge(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{})
	provider := discovered(t, idp)

	parsed, err := url.Parse(provider.AuthCodeURL(testState, testNonce, testVerifier))

	require.NoError(t, err)
	query := parsed.Query()
	require.Equal(t, idp.Issuer()+oidctest.PathAuthorize, parsed.Scheme+"://"+parsed.Host+parsed.Path)
	require.Equal(t, idp.ClientID(), query.Get("client_id"))
	require.Equal(t, "http://gateway.example/callback", query.Get("redirect_uri"))
	require.Equal(t, "code", query.Get("response_type"))
	require.Equal(t, "openid profile email", query.Get("scope"))
	require.Equal(t, testState, query.Get("state"))
	require.Equal(t, testNonce, query.Get("nonce"))
	require.Equal(t, "S256", query.Get("code_challenge_method"))
	digest := sha256.Sum256([]byte(testVerifier))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(digest[:]), query.Get("code_challenge"))
}

func TestExchangeCodeReturnsTheIdentityFromTheIDToken(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{})
	idp.SetIdentity(oidctest.Identity{
		Subject: "subject-1", PreferredUsername: "alice", Email: "alice@example.com", EmailVerified: true,
		Groups: []string{"/dcm-admins"}, RealmRoles: []string{"realm-admin"},
	})
	provider := discovered(t, idp)

	identity, err := provider.ExchangeCode(context.Background(), signInCode(t, idp, provider), testNonce, testVerifier)

	require.NoError(t, err)
	require.Equal(t, "subject-1", identity.Subject)
	require.Equal(t, "alice", identity.PreferredUsername)
	require.Equal(t, "alice@example.com", identity.Email)
	require.True(t, identity.EmailVerified)
	require.Equal(t, []string{"/dcm-admins"}, identity.Groups)
	require.Equal(t, []string{"realm-admin"}, identity.RealmRoles)
}

func TestExchangeCodeWithAnotherPKCEVerifierRejectsTheIdentity(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{})
	provider := discovered(t, idp)

	_, err := provider.ExchangeCode(context.Background(), signInCode(t, idp, provider), testNonce, testVerifier+"-other")

	require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)
}

func TestIDTokenClaimsThatRejectTheIdentity(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(map[string]any)
		nonce string
		now   time.Time
	}{
		{name: "missing expiry", edit: func(c map[string]any) { delete(c, "exp") }},
		{name: "past expiry", edit: func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() }},
		{name: "expired on the injected clock", now: time.Now().Add(time.Hour)},
		{name: "other issuer", edit: func(c map[string]any) { c["iss"] = "https://issuer.example" }},
		{name: "other audience", edit: func(c map[string]any) { c["aud"] = "other-client" }},
		{name: "missing audience", edit: func(c map[string]any) { delete(c, "aud") }},
		{name: "other nonce", nonce: "another-nonce"},
		{name: "missing nonce", edit: func(c map[string]any) { delete(c, "nonce") }},
		{name: "empty subject", edit: func(c map[string]any) { c["sub"] = "" }},
		{name: "missing subject", edit: func(c map[string]any) { delete(c, "sub") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idp := oidctest.New(t, oidctest.Options{})
			idp.EditIDToken(tt.edit)
			provider := discovered(t, idp, func(cfg *oidc.Config) {
				if !tt.now.IsZero() {
					cfg.Now = func() time.Time { return tt.now }
				}
			})
			nonce := testNonce
			if tt.nonce != "" {
				nonce = tt.nonce
			}
			code := signInCode(t, idp, provider)

			_, err := provider.ExchangeCode(context.Background(), code, nonce, testVerifier)

			require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)
		})
	}
}

func TestIDTokenExpiryIsCheckedOnTheInjectedClock(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{})
	idp.EditIDToken(func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() })
	provider := discovered(t, idp, func(cfg *oidc.Config) {
		cfg.Now = func() time.Time { return time.Now().Add(-time.Hour) }
	})

	_, err := provider.ExchangeCode(context.Background(), signInCode(t, idp, provider), testNonce, testVerifier)

	require.NoError(t, err)
}

func TestAdvertisedAsymmetricAlgorithmsAreAccepted(t *testing.T) {
	for _, alg := range []string{"RS256", "PS256", "ES256"} {
		t.Run(alg, func(t *testing.T) {
			idp := oidctest.New(t, oidctest.Options{Algorithm: alg})
			provider := discovered(t, idp)

			identity, err := provider.ExchangeCode(context.Background(), signInCode(t, idp, provider), testNonce, testVerifier)

			require.NoError(t, err)
			require.Equal(t, "subject-1", identity.Subject)
		})
	}
}

func TestUnsignedAndHMACTokensAreRefusedEvenWhenAdvertised(t *testing.T) {
	for _, alg := range []string{"none", "HS256"} {
		t.Run(alg, func(t *testing.T) {
			idp := oidctest.New(t, oidctest.Options{Advertised: []string{"RS256", "HS256", "none"}})
			idp.Forge(alg)
			provider := discovered(t, idp)

			_, err := provider.ExchangeCode(context.Background(), signInCode(t, idp, provider), testNonce, testVerifier)

			require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)
		})
	}
}

func TestATokenSignedWithARotatedKeyIsAccepted(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{})
	provider := discovered(t, idp)
	_, err := provider.ExchangeCode(context.Background(), signInCode(t, idp, provider), testNonce, testVerifier)
	require.NoError(t, err, "the first sign-in caches the first key")

	idp.RotateKeys()
	_, err = provider.ExchangeCode(context.Background(), signInCode(t, idp, provider), testNonce, testVerifier)

	require.NoError(t, err)
}

func TestProviderFailuresAreClassified(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		handler http.HandlerFunc
		down    bool
		want    error
	}{
		{name: "provider down at the token endpoint", down: true, want: apperrors.ErrUnavailable},
		{name: "token endpoint 500", path: oidctest.PathToken, handler: oidctest.Status(http.StatusInternalServerError), want: apperrors.ErrUnavailable},
		{name: "token endpoint 503 with an error code", path: oidctest.PathToken, handler: oidctest.OAuthError(http.StatusServiceUnavailable, "temporarily_unavailable", ""), want: apperrors.ErrUnavailable},
		{name: "invalid_grant", path: oidctest.PathToken, handler: oidctest.OAuthError(http.StatusBadRequest, "invalid_grant", "Code not valid"), want: apperrors.ErrInvalidCredentials},
		{name: "invalid_client", path: oidctest.PathToken, handler: oidctest.OAuthError(http.StatusUnauthorized, "invalid_client", "Invalid client credentials"), want: apperrors.ErrUnavailable},
		{name: "unauthorized_client", path: oidctest.PathToken, handler: oidctest.OAuthError(http.StatusBadRequest, "unauthorized_client", ""), want: apperrors.ErrUnavailable},
		{name: "invalid_request", path: oidctest.PathToken, handler: oidctest.OAuthError(http.StatusBadRequest, "invalid_request", "Incorrect redirect_uri"), want: apperrors.ErrUnavailable},
		{name: "token response without an ID token", path: oidctest.PathToken, handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"a","token_type":"Bearer"}`))
		}, want: apperrors.ErrUnavailable},
		{name: "oversized token response", path: oidctest.PathToken, handler: oidctest.Oversized(2 << 20), want: apperrors.ErrUnavailable},
		{name: "keys endpoint 500", path: oidctest.PathKeys, handler: oidctest.Status(http.StatusInternalServerError), want: apperrors.ErrUnavailable},
		{name: "oversized keys response", path: oidctest.PathKeys, handler: oidctest.Oversized(2 << 20), want: apperrors.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idp := oidctest.New(t, oidctest.Options{})
			provider := discovered(t, idp)
			code := signInCode(t, idp, provider)
			if tt.handler != nil {
				idp.Override(tt.path, tt.handler)
			}
			idp.SetDown(tt.down)

			_, err := provider.ExchangeCode(context.Background(), code, testNonce, testVerifier)

			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestProviderRedirectsAreNotFollowed(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{})
	provider := discovered(t, idp)
	code := signInCode(t, idp, provider)
	var followed atomic.Int32
	idp.Override("/elsewhere", func(w http.ResponseWriter, _ *http.Request) {
		followed.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	idp.Override(oidctest.PathToken, oidctest.Redirect(idp.Issuer()+"/elsewhere"))

	_, err := provider.ExchangeCode(context.Background(), code, testNonce, testVerifier)

	require.ErrorIs(t, err, apperrors.ErrUnavailable)
	require.Zero(t, followed.Load())
}

func TestDiscoveryRefusesRedirectsAndOversizedDocuments(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"redirect":  oidctest.Redirect("https://elsewhere.example/.well-known/openid-configuration"),
		"oversized": oidctest.Oversized(2 << 20),
	} {
		t.Run(name, func(t *testing.T) {
			idp := oidctest.New(t, oidctest.Options{})
			idp.Override(oidctest.PathDiscovery, handler)
			provider, err := oidc.NewProvider(fakeProviderConfig(idp))
			require.NoError(t, err)

			require.Error(t, provider.Discover(context.Background()))
			require.False(t, provider.Available())
		})
	}
}

func TestProviderRequestsTimeOutAfterTenSeconds(t *testing.T) {
	// The fake's listener lives outside the bubble; inside, only its in-process transport is used.
	idp := oidctest.New(t, oidctest.Options{})
	provider := discovered(t, idp)
	code := signInCode(t, idp, provider)
	idp.Override(oidctest.PathToken, oidctest.Hang())
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()

		_, err := provider.ExchangeCode(context.Background(), code, testNonce, testVerifier)

		require.ErrorIs(t, err, apperrors.ErrTimeout)
		require.Equal(t, 10*time.Second, time.Since(start))
	})
}

func TestDiscoveryValidatesTheIssuer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 "https://wrong-issuer.example",
			"authorization_endpoint": "https://wrong-issuer.example/auth",
			"token_endpoint":         "https://wrong-issuer.example/token",
			"jwks_uri":               "https://wrong-issuer.example/keys",
		})
	}))
	t.Cleanup(server.Close)
	provider, err := oidc.NewProvider(oidc.Config{
		IssuerURL: server.URL, ClientID: "dcm-client", ClientSecret: "secret", RedirectURL: "http://gateway.example/callback",
	})
	require.NoError(t, err)

	require.Error(t, provider.Discover(context.Background()))
	require.False(t, provider.Available())

	_, err = oidc.NewProvider(oidc.Config{})
	require.Error(t, err)
}

// logLines records what the adapter logs as decoded JSON lines.
type logLines struct{ buf bytes.Buffer }

func (l *logLines) logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&l.buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func (l *logLines) entries(t *testing.T) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(l.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		entries = append(entries, entry)
	}
	return entries
}

func TestMisconfigurationIsLoggedAsAnErrorWithABoundedProviderReason(t *testing.T) {
	logs := &logLines{}
	idp := oidctest.New(t, oidctest.Options{})
	provider := discovered(t, idp, func(cfg *oidc.Config) { cfg.Logger = logs.logger() })
	code := signInCode(t, idp, provider)
	idp.Override(oidctest.PathToken, oidctest.OAuthError(http.StatusUnauthorized, "invalid_client", "Invalid client secret "+strings.Repeat("x", 4096)))

	_, err := provider.ExchangeCode(context.Background(), code, testNonce, testVerifier)

	require.ErrorIs(t, err, apperrors.ErrUnavailable)
	entries := logs.entries(t)
	require.Len(t, entries, 1)
	require.Equal(t, "ERROR", entries[0]["level"])
	require.Equal(t, "invalid_client", entries[0]["provider_error"])
	description, _ := entries[0]["provider_error_description"].(string)
	require.True(t, strings.HasPrefix(description, "Invalid client secret"), description)
	require.LessOrEqual(t, len(description), 256)
	requireNoSecrets(t, logs, idp, code)
}

func TestRejectedIdentitiesAreLoggedAsWarningsWithAReason(t *testing.T) {
	tests := []struct {
		name   string
		edit   func(map[string]any)
		forge  string
		reason string
	}{
		{name: "signature", forge: "HS256", reason: "signature"},
		{name: "issuer", edit: func(c map[string]any) { c["iss"] = "https://issuer.example" }, reason: "issuer"},
		{name: "audience", edit: func(c map[string]any) { c["aud"] = "other" }, reason: "audience"},
		{name: "expiry", edit: func(c map[string]any) { delete(c, "exp") }, reason: "expiry"},
		{name: "nonce", edit: func(c map[string]any) { c["nonce"] = "other" }, reason: "nonce"},
		{name: "subject", edit: func(c map[string]any) { delete(c, "sub") }, reason: "subject"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := &logLines{}
			idp := oidctest.New(t, oidctest.Options{})
			idp.EditIDToken(tt.edit)
			idp.Forge(tt.forge)
			provider := discovered(t, idp, func(cfg *oidc.Config) { cfg.Logger = logs.logger() })
			code := signInCode(t, idp, provider)

			_, err := provider.ExchangeCode(context.Background(), code, testNonce, testVerifier)

			require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)
			entries := logs.entries(t)
			require.Len(t, entries, 1)
			require.Equal(t, "WARN", entries[0]["level"])
			require.Equal(t, tt.reason, entries[0]["reason"])
			requireNoSecrets(t, logs, idp, code)
		})
	}
}

func TestInvalidGrantIsLoggedWithTheProviderCode(t *testing.T) {
	logs := &logLines{}
	idp := oidctest.New(t, oidctest.Options{})
	provider := discovered(t, idp, func(cfg *oidc.Config) { cfg.Logger = logs.logger() })
	code := signInCode(t, idp, provider)

	_, err := provider.ExchangeCode(context.Background(), code, testNonce, testVerifier+"-other")

	require.ErrorIs(t, err, apperrors.ErrInvalidCredentials)
	entries := logs.entries(t)
	require.Len(t, entries, 1)
	require.Equal(t, "invalid_grant", entries[0]["provider_error"])
	requireNoSecrets(t, logs, idp, code)
}

func requireNoSecrets(t *testing.T, logs *logLines, idp *oidctest.Provider, code string) {
	t.Helper()
	out := logs.buf.String()
	for _, secret := range []string{code, testState, testNonce, testVerifier, idp.ClientSecret(), "eyJ"} {
		require.NotContains(t, out, secret)
	}
}

func TestExchangeCodeIsUnavailableBeforeDiscovery(t *testing.T) {
	idp := oidctest.New(t, oidctest.Options{Down: true})
	provider, err := oidc.NewProvider(fakeProviderConfig(idp))
	require.NoError(t, err)

	_, err = provider.ExchangeCode(context.Background(), "code", testNonce, testVerifier)

	require.True(t, errors.Is(err, apperrors.ErrUnavailable), "got %v", err)
	require.Empty(t, provider.AuthCodeURL(testState, testNonce, testVerifier))
}
