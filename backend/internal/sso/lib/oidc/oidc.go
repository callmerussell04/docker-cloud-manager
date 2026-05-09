package oidc

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/sso/service"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/golang-jwt/jwt/v5"
)

const defaultScope = "openid profile email"

type Config struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	HTTPClient   *http.Client
}

type Provider struct {
	cfg       Config
	discovery discoveryDocument
	client    *http.Client
	jwksMu    sync.Mutex
	jwks      jwks
	jwksAt    time.Time
}

type discoveryDocument struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	Issuer                string `json:"issuer"`
}

type tokenResponse struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type claims struct {
	jwt.RegisteredClaims
	Nonce             string              `json:"nonce"`
	PreferredUsername string              `json:"preferred_username"`
	Name              string              `json:"name"`
	Email             string              `json:"email"`
	EmailVerified     bool                `json:"email_verified"`
	Groups            []string            `json:"groups"`
	RealmAccess       keycloakRealmAccess `json:"realm_access"`
}

type keycloakRealmAccess struct {
	Roles []string `json:"roles"`
}

type jwks struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	KID string `json:"kid"`
	KTY string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	cfg.IssuerURL = strings.TrimRight(strings.TrimSpace(cfg.IssuerURL), "/")
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.RedirectURL = strings.TrimSpace(cfg.RedirectURL)
	if cfg.IssuerURL == "" || cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURL == "" {
		return nil, fmt.Errorf("oidc issuer, client id, client secret and redirect url are required")
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	discovery, err := discover(ctx, client, cfg.IssuerURL)
	if err != nil {
		return nil, err
	}
	if discovery.Issuer != "" && discovery.Issuer != cfg.IssuerURL {
		return nil, fmt.Errorf("oidc issuer mismatch")
	}

	return &Provider{
		cfg:       cfg,
		discovery: discovery,
		client:    client,
	}, nil
}

func (p *Provider) AuthCodeURL(state, nonce string) string {
	values := url.Values{}
	values.Set("client_id", p.cfg.ClientID)
	values.Set("redirect_uri", p.cfg.RedirectURL)
	values.Set("response_type", "code")
	values.Set("scope", defaultScope)
	values.Set("state", state)
	values.Set("nonce", nonce)
	return p.discovery.AuthorizationEndpoint + "?" + values.Encode()
}

func (p *Provider) ExchangeCode(ctx context.Context, code, nonce string) (service.OIDCIdentity, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURL)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.discovery.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return service.OIDCIdentity{}, apperrors.ErrInternal
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return service.OIDCIdentity{}, apperrors.Wrap(apperrors.ErrUnavailable, "oidc token endpoint unavailable", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return service.OIDCIdentity{}, apperrors.ErrInvalidCredentials
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return service.OIDCIdentity{}, apperrors.ErrInvalidCredentials
	}
	if tokenResp.IDToken == "" {
		return service.OIDCIdentity{}, apperrors.ErrInvalidCredentials
	}

	tokenClaims, err := p.verifyIDToken(ctx, tokenResp.IDToken, nonce)
	if err != nil {
		return service.OIDCIdentity{}, err
	}

	return service.OIDCIdentity{
		Subject:           tokenClaims.Subject,
		Username:          firstNonEmpty(tokenClaims.PreferredUsername, tokenClaims.Name, tokenClaims.Subject),
		Email:             tokenClaims.Email,
		EmailVerified:     tokenClaims.EmailVerified,
		Groups:            tokenClaims.Groups,
		RealmRoles:        tokenClaims.RealmAccess.Roles,
		PreferredUsername: tokenClaims.PreferredUsername,
	}, nil
}

func discover(ctx context.Context, client *http.Client, issuerURL string) (discoveryDocument, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, issuerURL+"/.well-known/openid-configuration", nil)
	if err != nil {
		return discoveryDocument{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return discoveryDocument{}, fmt.Errorf("failed to discover oidc provider: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return discoveryDocument{}, fmt.Errorf("oidc discovery returned status %d", resp.StatusCode)
	}

	var document discoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&document); err != nil {
		return discoveryDocument{}, fmt.Errorf("failed to decode oidc discovery: %w", err)
	}
	if document.AuthorizationEndpoint == "" || document.TokenEndpoint == "" || document.JWKSURI == "" {
		return discoveryDocument{}, fmt.Errorf("oidc discovery document is missing endpoints")
	}
	return document, nil
}

func (p *Provider) verifyIDToken(ctx context.Context, rawToken, nonce string) (claims, error) {
	tokenClaims := claims{}
	token, err := jwt.ParseWithClaims(rawToken, &tokenClaims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected oidc signing method")
		}
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("oidc token is missing kid")
		}
		return p.publicKey(ctx, kid)
	}, jwt.WithAudience(p.cfg.ClientID), jwt.WithIssuer(p.cfg.IssuerURL))
	if err != nil || !token.Valid {
		return claims{}, apperrors.ErrInvalidCredentials
	}
	if tokenClaims.Nonce != nonce {
		return claims{}, apperrors.ErrInvalidCredentials
	}
	return tokenClaims, nil
}

func (p *Provider) publicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	keys, err := p.cachedJWKS(ctx)
	if err != nil {
		return nil, err
	}
	key, err := publicKeyFromJWKS(keys, kid)
	if err == nil {
		return key, nil
	}

	p.jwksMu.Lock()
	p.jwksAt = time.Time{}
	p.jwksMu.Unlock()

	keys, refreshErr := p.cachedJWKS(ctx)
	if refreshErr != nil {
		return nil, refreshErr
	}
	return publicKeyFromJWKS(keys, kid)
}

func (p *Provider) cachedJWKS(ctx context.Context) (jwks, error) {
	p.jwksMu.Lock()
	if !p.jwksAt.IsZero() && time.Since(p.jwksAt) < 10*time.Minute {
		keys := p.jwks
		p.jwksMu.Unlock()
		return keys, nil
	}
	p.jwksMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.discovery.JWKSURI, nil)
	if err != nil {
		return jwks{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return jwks{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return jwks{}, errors.New("failed to fetch oidc jwks")
	}

	var keys jwks
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		return jwks{}, err
	}

	p.jwksMu.Lock()
	p.jwks = keys
	p.jwksAt = time.Now()
	p.jwksMu.Unlock()
	return keys, nil
}

func publicKeyFromJWKS(keys jwks, kid string) (*rsa.PublicKey, error) {
	for _, key := range keys.Keys {
		if key.KID != kid || key.KTY != "RSA" {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			return nil, err
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil {
			return nil, err
		}
		e := 0
		for _, b := range eBytes {
			e = e<<8 + int(b)
		}
		if e == 0 {
			return nil, errors.New("invalid oidc jwk exponent")
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: e}, nil
	}
	return nil, errors.New("oidc jwk not found")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
