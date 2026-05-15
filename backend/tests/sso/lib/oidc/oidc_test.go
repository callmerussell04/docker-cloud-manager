package oidc_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	ssooidc "github.com/callmerussell04/docker-cloud-manager/internal/sso/lib/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestProviderAuthCodeURLAndExchangeCode(t *testing.T) {
	server := newOIDCTestServer(t, oidcServerConfig{nonce: "nonce-1"})
	provider := newProvider(t, server.URL)

	authURL := provider.AuthCodeURL("state-1", "nonce-1")
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	require.Equal(t, "/auth", parsed.Path)
	require.Equal(t, "dcm-client", parsed.Query().Get("client_id"))
	require.Equal(t, "http://gateway.example/callback", parsed.Query().Get("redirect_uri"))
	require.Equal(t, "code", parsed.Query().Get("response_type"))
	require.Equal(t, "openid profile email", parsed.Query().Get("scope"))
	require.Equal(t, "state-1", parsed.Query().Get("state"))
	require.Equal(t, "nonce-1", parsed.Query().Get("nonce"))

	identity, err := provider.ExchangeCode(context.Background(), "auth-code", "nonce-1")
	require.NoError(t, err)
	require.Equal(t, "subject-1", identity.Subject)
	require.Equal(t, "alice", identity.PreferredUsername)
	require.Equal(t, "alice@example.com", identity.Email)
	require.True(t, identity.EmailVerified)
	require.Equal(t, []string{"/dcm-admins"}, identity.Groups)
	require.Equal(t, []string{"realm-admin"}, identity.RealmRoles)
}

func TestProviderRejectsOIDCErrors(t *testing.T) {
	tests := []struct {
		name string
		cfg  oidcServerConfig
	}{
		{name: "token endpoint non-2xx", cfg: oidcServerConfig{tokenStatus: http.StatusUnauthorized}},
		{name: "missing id token", cfg: oidcServerConfig{missingIDToken: true}},
		{name: "nonce mismatch", cfg: oidcServerConfig{nonce: "different-nonce"}},
		{name: "issuer mismatch", cfg: oidcServerConfig{tokenIssuerOverride: "https://issuer.example"}},
		{name: "audience mismatch", cfg: oidcServerConfig{tokenAudience: "other-client"}},
		{name: "missing signing key", cfg: oidcServerConfig{nonce: "nonce-1", tokenKID: "missing-kid", jwksKID: "test-kid"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newOIDCTestServer(t, tt.cfg)
			provider := newProvider(t, server.URL)
			_, err := provider.ExchangeCode(context.Background(), "auth-code", "nonce-1")
			require.Error(t, err)
		})
	}
}

func TestProviderDiscoveryValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": "https://wrong-issuer.example"})
	}))
	t.Cleanup(server.Close)

	_, err := ssooidc.NewProvider(context.Background(), ssooidc.Config{
		IssuerURL:    server.URL,
		ClientID:     "dcm-client",
		ClientSecret: "secret",
		RedirectURL:  "http://gateway.example/callback",
	})
	require.Error(t, err)

	_, err = ssooidc.NewProvider(context.Background(), ssooidc.Config{})
	require.Error(t, err)
}

type oidcServerConfig struct {
	nonce               string
	tokenStatus         int
	missingIDToken      bool
	issuerOverride      string
	tokenIssuerOverride string
	tokenAudience       string
	tokenKID            string
	jwksKID             string
}

func newProvider(t *testing.T, issuerURL string) *ssooidc.Provider {
	t.Helper()
	provider, err := ssooidc.NewProvider(context.Background(), ssooidc.Config{
		IssuerURL:    issuerURL,
		ClientID:     "dcm-client",
		ClientSecret: "secret",
		RedirectURL:  "http://gateway.example/callback",
	})
	require.NoError(t, err)
	return provider
}

func newOIDCTestServer(t *testing.T, cfg oidcServerConfig) *httptest.Server {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	if cfg.nonce == "" {
		cfg.nonce = "nonce-1"
	}
	if cfg.tokenKID == "" {
		cfg.tokenKID = "test-kid"
	}
	if cfg.jwksKID == "" {
		cfg.jwksKID = cfg.tokenKID
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			issuer := server.URL
			if cfg.issuerOverride != "" {
				issuer = cfg.issuerOverride
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer":                 issuer,
				"authorization_endpoint": server.URL + "/auth",
				"token_endpoint":         server.URL + "/token",
				"jwks_uri":               server.URL + "/jwks",
			})
		case "/token":
			if cfg.tokenStatus != 0 {
				w.WriteHeader(cfg.tokenStatus)
				return
			}
			require.NoError(t, r.ParseForm())
			require.Equal(t, "authorization_code", r.Form.Get("grant_type"))
			require.Equal(t, "auth-code", r.Form.Get("code"))
			if cfg.missingIDToken {
				_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "access"})
				return
			}
			issuer := server.URL
			if cfg.tokenIssuerOverride != "" {
				issuer = cfg.tokenIssuerOverride
			}
			audience := cfg.tokenAudience
			if audience == "" {
				audience = "dcm-client"
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id_token": signedIDToken(t, privateKey, cfg.tokenKID, issuer, audience, cfg.nonce)})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{jwkFromKey(cfg.jwksKID, &privateKey.PublicKey)}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func signedIDToken(t *testing.T, key *rsa.PrivateKey, kid, issuer, audience, nonce string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":                issuer,
		"aud":                audience,
		"sub":                "subject-1",
		"exp":                time.Now().Add(time.Hour).Unix(),
		"nonce":              nonce,
		"preferred_username": "alice",
		"email":              "alice@example.com",
		"email_verified":     true,
		"groups":             []string{"/dcm-admins"},
		"realm_access":       map[string]any{"roles": []string{"realm-admin"}},
	})
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	require.NoError(t, err)
	return raw
}

func jwkFromKey(kid string, key *rsa.PublicKey) map[string]string {
	return map[string]string{
		"kid": kid,
		"kty": "RSA",
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}
