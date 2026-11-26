// Package oidctest serves an OpenID Connect identity provider in-process so the
// SSO OIDC adapter and the sign-in flow can be tested without a live Keycloak.
//
// The provider serves discovery, the authorization endpoint (which records its
// parameters and redirects back with a code, as a browser that signed in would
// see), the token endpoint, the signing keys and an end-session endpoint. It
// signs back-channel logout tokens on request. Like a Keycloak client with
// PKCE required, it refuses an authorization request without an S256
// challenge and a code exchange whose verifier does not match it. It signs ID
// tokens with a chosen algorithm, can rotate its keys, edit or forge the next
// tokens, replace any endpoint with a scripted failure, and be taken down, in
// which case every request fails at the connection.
package oidctest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// Client registration the provider accepts unless Options say otherwise.
const (
	DefaultClientID     = "dcm"
	DefaultClientSecret = "dcm-client-secret"
)

// Endpoint paths, for Override.
const (
	PathDiscovery  = "/.well-known/openid-configuration"
	PathAuthorize  = "/auth"
	PathToken      = "/token"
	PathKeys       = "/keys"
	PathEndSession = "/logout"
)

// BackchannelLogoutEvent is the event a back-channel logout token carries.
const BackchannelLogoutEvent = "http://schemas.openid.net/event/backchannel-logout"

// Options configure a Provider. Zero values select the defaults.
type Options struct {
	ClientID     string
	ClientSecret string
	// RedirectURL is the only redirect URI the provider accepts. Empty accepts any.
	RedirectURL string
	// Down starts the provider unreachable; see SetDown.
	Down bool
	// Algorithm signs ID tokens: RS256 (the default), PS256 or ES256.
	Algorithm string
	// Advertised replaces id_token_signing_alg_values_supported in discovery.
	// Empty advertises Algorithm.
	Advertised []string
	// NoEndSession leaves end_session_endpoint out of discovery.
	NoEndSession bool
}

// Identity is the user the provider signs in at its authorization endpoint.
type Identity struct {
	Subject           string
	PreferredUsername string
	Email             string
	EmailVerified     bool
	Groups            []string
	RealmRoles        []string
	// SessionID is the provider session (sid) of the next sign-ins; empty
	// gives each sign-in a fresh one.
	SessionID string
}

// Provider is an in-process identity provider.
type Provider struct {
	t       testing.TB
	opts    Options
	server  *httptest.Server
	handler http.Handler

	mu          sync.Mutex
	down        bool
	key         signingKey
	keyCount    int
	identity    Identity
	grants      map[string]grant
	authorize   url.Values
	endSession  url.Values
	lastIDToken string
	lastSID     string
	idTokenEdit func(map[string]any)
	forge       string
	overrides   map[string]http.HandlerFunc
}

type signingKey struct {
	id      string
	private crypto.Signer
}

type grant struct {
	nonce         string
	redirectURI   string
	codeChallenge string
	identity      Identity
	sessionID     string
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
	if opts.Algorithm == "" {
		opts.Algorithm = string(jose.RS256)
	}
	if len(opts.Advertised) == 0 {
		opts.Advertised = []string{opts.Algorithm}
	}
	p := &Provider{
		t:         t,
		opts:      opts,
		down:      opts.Down,
		identity:  Identity{Subject: "subject-1", PreferredUsername: "alice", Email: "alice@example.com", EmailVerified: true},
		grants:    map[string]grant{},
		overrides: map[string]http.HandlerFunc{},
	}
	p.key = p.newKey()
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+PathDiscovery, p.serveDiscovery)
	mux.HandleFunc("GET "+PathAuthorize, p.serveAuthorize)
	mux.HandleFunc("POST "+PathToken, p.serveToken)
	mux.HandleFunc("GET "+PathKeys, p.serveKeys)
	mux.HandleFunc("GET "+PathEndSession, p.serveEndSession)
	p.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		override := p.overrides[r.URL.Path]
		p.mu.Unlock()
		if override != nil {
			override(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
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

// Transport reaches the provider without a socket, so it also works inside a
// testing/synctest bubble. While the provider is down every request fails as a
// connection error.
func (p *Provider) Transport() http.RoundTripper { return inProcess{p} }

// Client returns an HTTP client over Transport.
func (p *Provider) Client() *http.Client {
	return &http.Client{Transport: p.Transport()}
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

// EditIDToken edits the claims of every ID token issued from now on before it
// is signed; delete a key to leave the claim out. Nil stops editing.
func (p *Provider) EditIDToken(edit func(claims map[string]any)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.idTokenEdit = edit
}

// Forge makes the token endpoint issue ID tokens with the algorithm alg
// instead of a real signature: "none" leaves them unsigned and "HS256" signs
// them with the client secret, repeated to HMAC's minimum key length. Empty stops forging.
func (p *Provider) Forge(alg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.forge = alg
}

// RotateKeys replaces the signing key with a new one under a new key ID. The
// keys endpoint publishes only the new key.
func (p *Provider) RotateKeys() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.key = p.newKey()
}

// Override replaces the endpoint at path with handler; nil restores it.
func (p *Provider) Override(path string, handler http.HandlerFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if handler == nil {
		delete(p.overrides, path)
		return
	}
	p.overrides[path] = handler
}

// LastIDToken returns the last ID token the token endpoint issued.
func (p *Provider) LastIDToken() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastIDToken
}

// LastSessionID returns the provider session (sid) of the last sign-in.
func (p *Provider) LastSessionID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastSID
}

// LastEndSession returns the query of the last end-session request.
func (p *Provider) LastEndSession() url.Values {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.endSession
}

// LogoutToken signs a back-channel logout token for subject and provider
// session sessionID; an empty value leaves its claim out. edit changes the
// claims before signing, as EditIDToken does, and Forge applies too.
func (p *Provider) LogoutToken(subject, sessionID string, edit func(claims map[string]any)) string {
	p.t.Helper()
	now := time.Now()
	claims := map[string]any{
		"iss":    p.Issuer(),
		"aud":    p.opts.ClientID,
		"iat":    now.Unix(),
		"exp":    now.Add(2 * time.Minute).Unix(),
		"jti":    rand.Text(),
		"events": map[string]any{BackchannelLogoutEvent: map[string]any{}},
	}
	if subject != "" {
		claims["sub"] = subject
	}
	if sessionID != "" {
		claims["sid"] = sessionID
	}
	if edit != nil {
		edit(claims)
	}
	token, err := p.signClaims(claims)
	if err != nil {
		p.t.Fatalf("sign oidctest logout token: %v", err)
	}
	return token
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

// OAuthError answers status with an OAuth error body.
func OAuthError(status int, code, description string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, status, map[string]string{"error": code, "error_description": description})
	}
}

// Status answers status with an empty JSON object.
func Status(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, status, map[string]string{}) }
}

// Oversized answers 200 with a JSON body of size bytes.
func Oversized(size int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"padding":"` + strings.Repeat("a", max(size-14, 0)) + `"}`)) // the test client reads what arrives
	}
}

// Redirect answers with a redirect to target.
func Redirect(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	}
}

// Hang answers nothing until the request is cancelled.
func Hang() http.HandlerFunc {
	return func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }
}

func (p *Provider) isDown() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.down
}

func (p *Provider) newKey() signingKey {
	p.t.Helper()
	p.keyCount++
	var private crypto.Signer
	var err error
	switch jose.SignatureAlgorithm(p.opts.Algorithm) {
	case jose.RS256, jose.PS256:
		private, err = rsa.GenerateKey(rand.Reader, 2048)
	case jose.ES256:
		private, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	default:
		p.t.Fatalf("oidctest: unsupported signing algorithm %q", p.opts.Algorithm)
	}
	if err != nil {
		p.t.Fatalf("generate oidctest signing key: %v", err)
	}
	return signingKey{id: fmt.Sprintf("oidctest-key-%d", p.keyCount), private: private}
}

func (p *Provider) serveDiscovery(w http.ResponseWriter, _ *http.Request) {
	issuer := p.Issuer()
	document := map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + PathAuthorize,
		"token_endpoint":                        issuer + PathToken,
		"jwks_uri":                              issuer + PathKeys,
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": p.opts.Advertised,
		"code_challenge_methods_supported":      []string{"S256"},
	}
	if !p.opts.NoEndSession {
		document["end_session_endpoint"] = issuer + PathEndSession
	}
	writeJSON(w, http.StatusOK, document)
}

func (p *Provider) serveAuthorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("client_id") != p.opts.ClientID || query.Get("response_type") != "code" ||
		(p.opts.RedirectURL != "" && query.Get("redirect_uri") != p.opts.RedirectURL) {
		http.Error(w, "invalid authorization request", http.StatusBadRequest)
		return
	}
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" {
		http.Error(w, "PKCE S256 challenge required", http.StatusBadRequest)
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
	sessionID := p.identity.SessionID
	if sessionID == "" {
		sessionID = rand.Text()
	}
	p.lastSID = sessionID
	p.grants[code] = grant{
		nonce: query.Get("nonce"), redirectURI: query.Get("redirect_uri"),
		codeChallenge: query.Get("code_challenge"), identity: p.identity, sessionID: sessionID,
	}
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
	verifier := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(verifier[:]) != issued.codeChallenge {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": "PKCE verification failed"})
		return
	}
	idToken, err := p.signIDToken(issued)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
		return
	}
	p.mu.Lock()
	p.lastIDToken = idToken
	p.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": rand.Text(),
		"token_type":   "Bearer",
		"expires_in":   300,
		"id_token":     idToken,
	})
}

func (p *Provider) signIDToken(issued grant) (string, error) {
	now := time.Now()
	claims := map[string]any{
		"iss":                p.Issuer(),
		"aud":                p.opts.ClientID,
		"sub":                issued.identity.Subject,
		"iat":                now.Unix(),
		"exp":                now.Add(5 * time.Minute).Unix(),
		"nonce":              issued.nonce,
		"sid":                issued.sessionID,
		"preferred_username": issued.identity.PreferredUsername,
		"email":              issued.identity.Email,
		"email_verified":     issued.identity.EmailVerified,
		"groups":             issued.identity.Groups,
		"realm_access":       map[string]any{"roles": issued.identity.RealmRoles},
	}
	p.mu.Lock()
	edit := p.idTokenEdit
	p.mu.Unlock()
	if edit != nil {
		edit(claims)
	}
	return p.signClaims(claims)
}

// signClaims signs claims with the current key, or forges them as Forge set.
func (p *Provider) signClaims(claims map[string]any) (string, error) {
	p.mu.Lock()
	forge, key := p.forge, p.key
	p.mu.Unlock()
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	switch forge {
	case "":
	case "none":
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","kid":"` + key.id + `"}`))
		return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".", nil
	default:
		// go-jose refuses HMAC keys shorter than the hash.
		return sign(jose.SignatureAlgorithm(forge), key.id, []byte(strings.Repeat(p.opts.ClientSecret, 4)), payload)
	}
	return sign(jose.SignatureAlgorithm(p.opts.Algorithm), key.id, key.private, payload)
}

func sign(alg jose.SignatureAlgorithm, keyID string, key any, payload []byte) (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: jose.JSONWebKey{Key: key, KeyID: keyID}}, nil)
	if err != nil {
		return "", err
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		return "", err
	}
	return signed.CompactSerialize()
}

// serveEndSession records the sign-out request and, like Keycloak, sends the
// browser to post_logout_redirect_uri when there is one.
func (p *Provider) serveEndSession(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	p.mu.Lock()
	p.endSession = query
	p.mu.Unlock()
	if target := query.Get("post_logout_redirect_uri"); target != "" {
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (p *Provider) serveKeys(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	key := p.key
	p.mu.Unlock()
	writeJSON(w, http.StatusOK, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: key.private.Public(), KeyID: key.id, Algorithm: p.opts.Algorithm, Use: "sig",
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
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	response := recorder.Result()
	response.Request = r
	return response, nil
}
