// Package oidc is SSO's OpenID Connect adapter. Discovery, signing keys and
// ID-token verification come from github.com/coreos/go-oidc/v3, and the
// authorization-code exchange with PKCE from golang.org/x/oauth2.
package oidc

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/service"
)

// backchannelLogoutEvent is the event every back-channel logout token carries.
const backchannelLogoutEvent = "http://schemas.openid.net/event/backchannel-logout"

// Token kinds, as rejection logs name them.
const (
	kindIDToken     = "id token"
	kindLogoutToken = "logout token"
)

// Discovery retries back off from discoveryInitialBackoff, doubling up to discoveryMaxBackoff.
const (
	discoveryInitialBackoff = time.Second
	discoveryMaxBackoff     = time.Minute
)

// Every provider request is bounded in time and size and follows no redirect.
const (
	requestTimeout   = 10 * time.Second
	maxResponseBytes = 1 << 20
	// maxLoggedReason bounds provider-supplied text in logs and errors.
	maxLoggedReason = 256
)

// signingAlgorithms are the asymmetric algorithms an ID token may use; none
// and HMAC are never accepted.
var signingAlgorithms = map[string]bool{
	gooidc.RS256: true, gooidc.RS384: true, gooidc.RS512: true,
	gooidc.PS256: true, gooidc.PS384: true, gooidc.PS512: true,
	gooidc.ES256: true, gooidc.ES384: true, gooidc.ES512: true,
	gooidc.EdDSA: true,
}

var (
	errRedirect         = errors.New("oidc provider redirects are not followed")
	errResponseTooLarge = errors.New("oidc provider response exceeds 1 MiB")
)

type Config struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// Transport carries provider requests; nil uses a copy of http.DefaultTransport.
	// The adapter adds the timeout, the redirect refusal and the body cap.
	Transport http.RoundTripper
	// Now is SSO's clock, against which ID-token expiry is checked; nil is time.Now.
	Now func() time.Time
	// Logger receives provider failures; nil discards them.
	Logger *slog.Logger
}

// Provider is the OIDC adapter. It is unavailable until discovery succeeds;
// discovery runs outside startup (see Run), so an unreachable provider never
// stops SSO from starting.
type Provider struct {
	cfg        Config
	httpClient *http.Client
	logger     *slog.Logger
	// client is nil until discovery succeeds, then never changes.
	client atomic.Pointer[discoveredClient]
}

// discoveredClient is what discovery yields: the OAuth client for the
// provider's endpoints, the verifier for its ID and logout tokens, and its
// end-session endpoint, nil when it advertises none.
type discoveredClient struct {
	oauth      oauth2.Config
	verifier   *gooidc.IDTokenVerifier
	endSession *url.URL
}

// idClaims are the ID-token claims DCM reads besides sub and nonce.
type idClaims struct {
	SessionID         string   `json:"sid"`
	PreferredUsername string   `json:"preferred_username"`
	Email             string   `json:"email"`
	EmailVerified     bool     `json:"email_verified"`
	Groups            []string `json:"groups"`
	RealmAccess       struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

// NewProvider checks the configuration and makes no provider call.
func NewProvider(cfg Config) (*Provider, error) {
	cfg.IssuerURL = strings.TrimRight(strings.TrimSpace(cfg.IssuerURL), "/")
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.RedirectURL = strings.TrimSpace(cfg.RedirectURL)
	if cfg.IssuerURL == "" || cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURL == "" {
		return nil, fmt.Errorf("oidc issuer, client id, client secret and redirect url are required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	transport := cfg.Transport
	if transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	httpClient := &http.Client{
		Transport:     cappedTransport{base: transport},
		Timeout:       requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errRedirect },
	}
	return &Provider{cfg: cfg, httpClient: httpClient, logger: logger}, nil
}

// Available reports whether discovery has succeeded. Once true it stays true.
func (p *Provider) Available() bool {
	return p.client.Load() != nil
}

// Discover makes one discovery attempt and, on success, makes the provider available.
func (p *Provider) Discover(ctx context.Context) error {
	if p.Available() {
		return nil
	}
	ctx = gooidc.ClientContext(ctx, p.httpClient)
	provider, err := gooidc.NewProvider(ctx, p.cfg.IssuerURL)
	if err != nil {
		// go-oidc puts the response body in its error, so the text is bounded.
		return boundedError{prefix: "oidc discovery failed", cause: err}
	}
	var metadata struct {
		JWKSURI    string   `json:"jwks_uri"`
		Algorithms []string `json:"id_token_signing_alg_values_supported"`
		EndSession string   `json:"end_session_endpoint"`
	}
	if err := provider.Claims(&metadata); err != nil {
		return fmt.Errorf("oidc discovery document is malformed: %w", err)
	}
	endpoint := provider.Endpoint()
	if endpoint.AuthURL == "" || endpoint.TokenURL == "" || metadata.JWKSURI == "" {
		return fmt.Errorf("oidc discovery document is missing endpoints")
	}
	// Client authentication is fixed to credentials in the request body.
	endpoint.AuthStyle = oauth2.AuthStyleInParams

	// The key set keeps ctx's HTTP client but not its cancellation, and
	// refetches the keys when a token names a key ID it does not know.
	keys := probingKeySet{keys: gooidc.NewRemoteKeySet(ctx, metadata.JWKSURI)}
	verifier := gooidc.NewVerifier(p.cfg.IssuerURL, keys, &gooidc.Config{
		ClientID:             p.cfg.ClientID,
		SupportedSigningAlgs: asymmetricAlgorithms(metadata.Algorithms),
		Now:                  p.cfg.Now,
	})
	p.client.CompareAndSwap(nil, &discoveredClient{
		oauth: oauth2.Config{
			ClientID:     p.cfg.ClientID,
			ClientSecret: p.cfg.ClientSecret,
			Endpoint:     endpoint,
			RedirectURL:  p.cfg.RedirectURL,
			Scopes:       []string{gooidc.ScopeOpenID, "profile", "email"},
		},
		verifier:   verifier,
		endSession: p.endSessionEndpoint(metadata.EndSession),
	})
	return nil
}

// endSessionEndpoint parses the advertised end-session endpoint. A missing or
// unusable one gives no provider sign-out, and an unusable one is logged.
func (p *Provider) endSessionEndpoint(raw string) *url.URL {
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		p.logger.Warn("oidc discovery advertises an unusable end_session_endpoint; DCM sign-out will not end the provider session")
		return nil
	}
	return parsed
}

// Run attempts discovery at once and then, while it fails, again after a
// backoff that doubles from one second up to one minute. It returns when
// discovery succeeds or ctx ends. Each failure is logged as a warning and the
// success once.
func (p *Provider) Run(ctx context.Context) {
	logger := p.logger
	backoff := discoveryInitialBackoff
	for {
		err := p.Discover(ctx)
		if err == nil {
			logger.Info("oidc provider available")
			return
		}
		if ctx.Err() != nil {
			return
		}
		logger.Warn("oidc provider discovery failed", "error", err, "retry_in", backoff.String())
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(2*backoff, discoveryMaxBackoff)
	}
}

// AuthCodeURL returns the authorization request URL with an S256 PKCE
// challenge for verifier, or "" while the provider is unavailable; callers
// check Available first.
func (p *Provider) AuthCodeURL(state, nonce, verifier string) string {
	client := p.client.Load()
	if client == nil {
		return ""
	}
	return client.oauth.AuthCodeURL(state, gooidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
}

// ExchangeCode redeems code with the PKCE verifier and returns the identity of
// the verified ID token. An identity the provider or the token checks reject is
// ErrInvalidCredentials; a provider that cannot be used is ErrUnavailable or
// ErrTimeout.
func (p *Provider) ExchangeCode(ctx context.Context, code, nonce, verifier string) (service.OIDCIdentity, error) {
	client := p.client.Load()
	if client == nil {
		return service.OIDCIdentity{}, unavailable(nil)
	}
	token, err := client.oauth.Exchange(context.WithValue(ctx, oauth2.HTTPClient, p.httpClient), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return service.OIDCIdentity{}, p.tokenEndpointFailure(err)
	}
	rawIDToken, _ := token.Extra("id_token").(string)
	if rawIDToken == "" {
		p.logger.Error("oidc token response has no id token; check that the client requests the openid scope")
		return service.OIDCIdentity{}, unavailable(nil)
	}

	probe := &keyProbe{}
	idToken, err := client.verifier.Verify(context.WithValue(ctx, keyProbeKey{}, probe), rawIDToken)
	if err != nil {
		return service.OIDCIdentity{}, p.verificationFailure(kindIDToken, err, probe)
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 || nonce == "" {
		return service.OIDCIdentity{}, p.rejected("nonce")
	}
	if strings.TrimSpace(idToken.Subject) == "" {
		return service.OIDCIdentity{}, p.rejected("subject")
	}
	var claims idClaims
	if err := idToken.Claims(&claims); err != nil {
		return service.OIDCIdentity{}, p.rejected("claims")
	}
	return service.OIDCIdentity{
		Subject:           idToken.Subject,
		Email:             claims.Email,
		EmailVerified:     claims.EmailVerified,
		Groups:            claims.Groups,
		RealmRoles:        claims.RealmAccess.Roles,
		PreferredUsername: claims.PreferredUsername,
		SessionID:         claims.SessionID,
		IDToken:           rawIDToken,
	}, nil
}

// EndSessionURL builds the provider sign-out URL from the advertised
// end-session endpoint with id_token_hint, client_id and, when set,
// post_logout_redirect_uri. It is empty before discovery or without an
// advertised endpoint.
func (p *Provider) EndSessionURL(idTokenHint, postLogoutRedirectURI string) string {
	client := p.client.Load()
	if client == nil || client.endSession == nil {
		return ""
	}
	endpoint := *client.endSession
	query := endpoint.Query()
	query.Set("id_token_hint", idTokenHint)
	query.Set("client_id", p.cfg.ClientID)
	if postLogoutRedirectURI != "" {
		query.Set("post_logout_redirect_uri", postLogoutRedirectURI)
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String()
}

// VerifyLogoutToken verifies a back-channel logout token as an ID token is
// verified (signature, issuer, audience, expiry on SSO's clock) and also
// requires the back-channel logout event, a sub or sid, and no nonce. A token
// these checks refuse is ErrInvalidToken; keys that cannot be fetched make
// the provider unavailable.
func (p *Provider) VerifyLogoutToken(ctx context.Context, rawToken string) (service.OIDCLogout, error) {
	client := p.client.Load()
	if client == nil {
		return service.OIDCLogout{}, unavailable(nil)
	}
	probe := &keyProbe{}
	token, err := client.verifier.Verify(context.WithValue(ctx, keyProbeKey{}, probe), rawToken)
	if err != nil {
		err = p.verificationFailure(kindLogoutToken, err, probe)
		if errors.Is(err, apperrors.ErrInvalidCredentials) {
			return service.OIDCLogout{}, apperrors.ErrInvalidToken
		}
		return service.OIDCLogout{}, err
	}
	var claims map[string]json.RawMessage
	if err := token.Claims(&claims); err != nil {
		return service.OIDCLogout{}, p.refuseLogoutToken("claims")
	}
	if _, hasNonce := claims["nonce"]; hasNonce {
		return service.OIDCLogout{}, p.refuseLogoutToken("nonce")
	}
	var events map[string]json.RawMessage
	if json.Unmarshal(claims["events"], &events) != nil || !isJSONObject(events[backchannelLogoutEvent]) {
		return service.OIDCLogout{}, p.refuseLogoutToken("events")
	}
	var sessionID string
	if raw, ok := claims["sid"]; ok && json.Unmarshal(raw, &sessionID) != nil {
		return service.OIDCLogout{}, p.refuseLogoutToken("claims")
	}
	logout := service.OIDCLogout{Subject: strings.TrimSpace(token.Subject), SessionID: strings.TrimSpace(sessionID)}
	if logout.Subject == "" && logout.SessionID == "" {
		return service.OIDCLogout{}, p.refuseLogoutToken("subject")
	}
	return logout, nil
}

func (p *Provider) refuseLogoutToken(reason string) error {
	p.rejectedToken(kindLogoutToken, reason)
	return apperrors.ErrInvalidToken
}

// isJSONObject reports whether raw is a JSON object.
func isJSONObject(raw json.RawMessage) bool {
	var object map[string]json.RawMessage
	return len(raw) > 0 && json.Unmarshal(raw, &object) == nil && object != nil
}

// tokenEndpointFailure classifies a failed code exchange: invalid_grant rejects
// the identity, a 5xx or an unreachable provider is unavailable, and any other
// OAuth error or an answer that cannot be read is a DCM misconfiguration,
// unavailable to the user and logged as an error.
func (p *Provider) tokenEndpointFailure(err error) error {
	var retrieve *oauth2.RetrieveError
	if !errors.As(err, &retrieve) {
		var transportErr *url.Error
		if errors.As(err, &transportErr) {
			return p.unreachable("oidc token endpoint", err)
		}
		p.logger.Error("oidc token endpoint answer is unreadable; check the issuer and the client configuration", "error", bounded(err.Error()))
		return unavailable(err)
	}
	status := 0
	if retrieve.Response != nil {
		status = retrieve.Response.StatusCode
	}
	attrs := []any{
		"status", status,
		"provider_error", bounded(retrieve.ErrorCode),
		"provider_error_description", bounded(retrieve.ErrorDescription),
	}
	switch {
	// RetrieveError's own text carries the response body, so it is never logged.
	case status >= http.StatusInternalServerError:
		p.logger.Warn("oidc token endpoint unavailable", attrs...)
		return unavailable(err)
	case retrieve.ErrorCode == "invalid_grant":
		p.logger.Warn("oidc provider refused the authorization code", attrs...)
		return apperrors.Wrap(apperrors.ErrInvalidCredentials, "", err)
	default:
		p.logger.Error("oidc provider refused the token request; check the DCM client configuration", attrs...)
		return unavailable(err)
	}
}

// unreachable logs and classifies a provider request that got no usable answer.
func (p *Provider) unreachable(what string, err error) error {
	if isTimeout(err) {
		p.logger.Warn(what + " timed out")
		return apperrors.Wrap(apperrors.ErrTimeout, "identity provider timed out", err)
	}
	p.logger.Warn(what+" unavailable", "error", bounded(err.Error()))
	return unavailable(err)
}

// verificationFailure classifies a failed verification of a token of kind:
// signing keys that cannot be fetched make the provider unavailable; anything
// else rejects the token as ErrInvalidCredentials.
func (p *Provider) verificationFailure(kind string, err error, probe *keyProbe) error {
	if probe.keysErr != nil {
		return p.unreachable("oidc signing keys", probe.keysErr)
	}
	reject := func(reason string) error {
		p.rejectedToken(kind, reason)
		return apperrors.ErrInvalidCredentials
	}
	if probe.payload == nil {
		return reject("signature")
	}
	// The signature verified, so a claim check failed; the verified claims say which.
	var expired *gooidc.TokenExpiredError
	var claims struct {
		Issuer   string          `json:"iss"`
		Audience json.RawMessage `json:"aud"`
	}
	switch {
	case errors.As(err, &expired):
		return reject("expiry")
	case json.Unmarshal(probe.payload, &claims) != nil:
		return reject("claims")
	case claims.Issuer != p.cfg.IssuerURL:
		return reject("issuer")
	case !audienceIncludes(claims.Audience, p.cfg.ClientID):
		return reject("audience")
	default:
		return reject("claims")
	}
}

// audienceIncludes reads aud, a string or an array of strings.
func audienceIncludes(raw json.RawMessage, clientID string) bool {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return single == clientID
	}
	var many []string
	return json.Unmarshal(raw, &many) == nil && slices.Contains(many, clientID)
}

func (p *Provider) rejected(reason string) error {
	p.rejectedToken(kindIDToken, reason)
	return apperrors.ErrInvalidCredentials
}

// rejectedToken logs a refused token of kind with the reason category only.
func (p *Provider) rejectedToken(kind, reason string) {
	p.logger.Warn("oidc "+kind+" rejected", "reason", reason)
}

func unavailable(cause error) error {
	return apperrors.Wrap(apperrors.ErrUnavailable, "identity provider is not available", cause)
}

// boundedError shows a bounded copy of its cause's text and keeps the cause.
type boundedError struct {
	prefix string
	cause  error
}

func (e boundedError) Error() string { return e.prefix + ": " + bounded(e.cause.Error()) }
func (e boundedError) Unwrap() error { return e.cause }

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
}

// keyProbe records, for one verification, the payload whose signature
// verified and a failure to fetch the signing keys; go-oidc reports both only
// as text.
type keyProbe struct {
	payload []byte
	keysErr error
}

type keyProbeKey struct{}

type probingKeySet struct{ keys *gooidc.RemoteKeySet }

func (k probingKeySet) VerifySignature(ctx context.Context, jwt string) ([]byte, error) {
	payload, err := k.keys.VerifySignature(ctx, jwt)
	if probe, ok := ctx.Value(keyProbeKey{}).(*keyProbe); ok {
		if err == nil {
			probe.payload = payload
		}
		// RemoteKeySet prefixes key-fetch failures with "fetching keys" and wraps
		// the cause; a signature no key verifies is a different error.
		if err != nil && (strings.HasPrefix(err.Error(), "fetching keys") || ctx.Err() != nil) {
			probe.keysErr = err
		}
	}
	return payload, err
}

func asymmetricAlgorithms(advertised []string) []string {
	var allowed []string
	for _, alg := range advertised {
		if signingAlgorithms[alg] {
			allowed = append(allowed, alg)
		}
	}
	if len(allowed) == 0 {
		// The one algorithm every provider must support.
		return []string{gooidc.RS256}
	}
	return allowed
}

// cappedTransport fails a response body that exceeds maxResponseBytes.
type cappedTransport struct{ base http.RoundTripper }

func (t cappedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	response.Body = &cappedBody{body: response.Body}
	return response, nil
}

type cappedBody struct {
	body io.ReadCloser
	read int64
}

func (b *cappedBody) Read(buf []byte) (int, error) {
	n, err := b.body.Read(buf)
	b.read += int64(n)
	if b.read > maxResponseBytes {
		return 0, errResponseTooLarge
	}
	return n, err
}

func (b *cappedBody) Close() error { return b.body.Close() }

// bounded makes provider-supplied text safe to log: control characters become
// spaces and the result is cut to maxLoggedReason bytes.
func bounded(text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(text, ""))
	if len(text) <= maxLoggedReason {
		return text
	}
	cut := maxLoggedReason
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}
