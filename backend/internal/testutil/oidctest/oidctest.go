// Package oidctest serves an OpenID Connect identity provider in-process so the
// SSO OIDC adapter and the sign-in flow can be tested without a live Keycloak.
//
// The provider serves discovery, the authorization endpoint (which records its
// parameters and redirects back with a code, as a browser that signed in would
// see), the token endpoint and the signing keys. It can be taken down, in which
// case every request fails at the connection.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Client registration the provider accepts unless Options say otherwise.
const (
	DefaultClientID     = "dcm"
	DefaultClientSecret = "dcm-client-secret"
)

const keyID = "oidctest-key"

// Options configure a Provider. Zero values select the defaults.
type Options struct {
	ClientID     string
	ClientSecret string
	// RedirectURL is the only redirect URI the provider accepts. Empty accepts any.
	RedirectURL string
	// Down starts the provider unreachable; see SetDown.
	Down bool
}

// Identity is the user the provider signs in at its authorization endpoint.
type Identity struct {
	Subject           string
	PreferredUsername string
	Email             string
	EmailVerified     bool
	Groups            []string
	RealmRoles        []string
}

// Provider is an in-process identity provider.
type Provider struct {
	t       testing.TB
	opts    Options
	key     *rsa.PrivateKey
	server  *httptest.Server
	handler http.Handler

	mu        sync.Mutex
	down      bool
	identity  Identity
	grants    map[string]grant
	authorize url.Values
}

type grant struct {
	nonce       string
	redirectURI string
	identity    Identity
}

// New starts a provider on a loopback listener and stops it at the end of the test.
func New(t testing.TB, opts Options) *Provider {
	t.Helper()
	if opts.ClientID == "" {
		opts.ClientID = DefaultClientID
	}
	if opts.ClientSecret == "" {
		opts.ClientSecret = DefaultClientSecret
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate oidctest signing key: %v", err)
	}
	p := &Provider{
		t:        t,
		opts:     opts,
		key:      key,
		down:     opts.Down,
		identity: Identity{Subject: "subject-1", PreferredUsername: "alice", Email: "alice@example.com", EmailVerified: true},
		grants:   map[string]grant{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.serveDiscovery)
	mux.HandleFunc("GET /auth", p.serveAuthorize)
	mux.HandleFunc("POST /token", p.serveToken)
	mux.HandleFunc("GET /keys", p.serveKeys)
	p.handler = mux
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.isDown() {
			dropConnection(w)
			return
		}
		p.handler.ServeHTTP(w, r)
	}))
	t.Cleanup(p.server.Close)
	return p
}

// Issuer is the provider's issuer URL, the value of SSO_OIDC_ISSUER_URL.
func (p *Provider) Issuer() string { return p.server.URL }

// ClientID and ClientSecret are the registration the provider accepts.
func (p *Provider) ClientID() string     { return p.opts.ClientID }
func (p *Provider) ClientSecret() string { return p.opts.ClientSecret }

// Client returns an HTTP client that reaches the provider without a socket, so
// it also works inside a testing/synctest bubble. While the provider is down
// every request fails as a connection error.
func (p *Provider) Client() *http.Client {
	return &http.Client{Transport: inProcess{p}}
}

// SetDown makes the provider unreachable (true) or reachable again (false).
func (p *Provider) SetDown(down bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.down = down
}

// SetIdentity chooses the user the authorization endpoint signs in next.
func (p *Provider) SetIdentity(identity Identity) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.identity = identity
}

// LastAuthorization returns the query of the last authorization request.
func (p *Provider) LastAuthorization() url.Values {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.authorize
}

// SignIn plays the browser at the provider: it follows authURL, which must
// point at the authorization endpoint, and returns the redirect back to the
// relying party with the code and state.
func (p *Provider) SignIn(authURL string) *url.URL {
	p.t.Helper()
	request, err := http.NewRequest(http.MethodGet, authURL, nil)
	if err != nil {
		p.t.Fatalf("oidctest sign-in request: %v", err)
	}
	client := p.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		p.t.Fatalf("oidctest sign-in: %v", err)
	}
	_ = response.Body.Close() // in-memory body
	if response.StatusCode != http.StatusFound {
		p.t.Fatalf("oidctest sign-in answered %d", response.StatusCode)
	}
	location, err := response.Location()
	if err != nil {
		p.t.Fatalf("oidctest sign-in redirect: %v", err)
	}
	return location
}

func (p *Provider) isDown() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.down
}

func (p *Provider) serveDiscovery(w http.ResponseWriter, _ *http.Request) {
	issuer := p.Issuer()
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/auth",
		"token_endpoint":                        issuer + "/token",
		"jwks_uri":                              issuer + "/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (p *Provider) serveAuthorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("client_id") != p.opts.ClientID || query.Get("response_type") != "code" ||
		(p.opts.RedirectURL != "" && query.Get("redirect_uri") != p.opts.RedirectURL) {
		http.Error(w, "invalid authorization request", http.StatusBadRequest)
		return
	}
	redirect, err := url.Parse(query.Get("redirect_uri"))
	if err != nil || !redirect.IsAbs() {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	code := rand.Text()
	p.mu.Lock()
	p.authorize = query
	p.grants[code] = grant{nonce: query.Get("nonce"), redirectURI: query.Get("redirect_uri"), identity: p.identity}
	p.mu.Unlock()

	back := redirect.Query()
	back.Set("code", code)
	back.Set("state", query.Get("state"))
	redirect.RawQuery = back.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (p *Provider) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if r.PostForm.Get("client_id") != p.opts.ClientID || r.PostForm.Get("client_secret") != p.opts.ClientSecret {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	p.mu.Lock()
	issued, ok := p.grants[r.PostForm.Get("code")]
	delete(p.grants, r.PostForm.Get("code"))
	p.mu.Unlock()
	if r.PostForm.Get("grant_type") != "authorization_code" || !ok || issued.redirectURI != r.PostForm.Get("redirect_uri") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	idToken, err := p.signIDToken(issued)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": rand.Text(),
		"token_type":   "Bearer",
		"expires_in":   300,
		"id_token":     idToken,
	})
}

func (p *Provider) signIDToken(issued grant) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":                p.Issuer(),
		"aud":                p.opts.ClientID,
		"sub":                issued.identity.Subject,
		"iat":                now.Unix(),
		"exp":                now.Add(5 * time.Minute).Unix(),
		"nonce":              issued.nonce,
		"preferred_username": issued.identity.PreferredUsername,
		"email":              issued.identity.Email,
		"email_verified":     issued.identity.EmailVerified,
		"groups":             issued.identity.Groups,
		"realm_access":       map[string]any{"roles": issued.identity.RealmRoles},
	})
	token.Header["kid"] = keyID
	return token.SignedString(p.key)
}

func (p *Provider) serveKeys(w http.ResponseWriter, _ *http.Request) {
	public := p.key.PublicKey
	writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]string{{
		"kid": keyID,
		"kty": "RSA",
		"alg": "RS256",
		"use": "sig",
		"n":   base64.RawURLEncoding.EncodeToString(public.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(public.E)).Bytes()),
	}}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body) // the test client reads what arrives
}

// dropConnection closes the connection without an answer, as an unreachable
// provider would.
func dropConnection(w http.ResponseWriter) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "provider down", http.StatusServiceUnavailable)
		return
	}
	conn, _, err := hijacker.Hijack()
	if err == nil {
		_ = conn.Close() // the point is to drop it
	}
}

// errDown is the connection error the in-process client reports while down.
var errDown = errors.New("oidctest: connection refused")

type inProcess struct{ p *Provider }

func (t inProcess) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.p.isDown() {
		return nil, errDown
	}
	recorder := httptest.NewRecorder()
	t.p.handler.ServeHTTP(recorder, r)
	response := recorder.Result()
	response.Request = r
	return response, nil
}
