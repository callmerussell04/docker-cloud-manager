package handler

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/config"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/validation"
)

const (
	// oidcStateCookiePrefix starts the name of each attempt's browser-binding
	// cookie; the rest is derived from the attempt's state, so two sign-in tabs
	// keep separate cookies.
	oidcStateCookiePrefix = "oidc_state_"
	oidcStateCookiePath   = "/api/v1/auth/oidc/keycloak/callback"
	keycloakProvider      = "keycloak"
	roleAdmin             = "admin"
	// spaLoginPath is the SPA's login page, where the provider returns the
	// browser after a sign-out DCM started.
	spaLoginPath = "/login"
	// maxBackchannelLogoutBodyBytes bounds a back-channel logout request; a
	// logout token is a few kilobytes.
	maxBackchannelLogoutBodyBytes = 64 << 10
)

// Reasons a sign-in through the provider failed, sent to the SPA callback page
// as login=error&reason=<code>. The set is closed; the SPA shows a generic
// message for a code it does not know.
const (
	loginReasonAccessDenied        = "access_denied"
	loginReasonProviderError       = "provider_error"
	loginReasonStateInvalid        = "state_invalid"
	loginReasonIdentityRejected    = "identity_rejected"
	loginReasonAccountDeactivated  = "account_deactivated"
	loginReasonAccountConflict     = "account_conflict"
	loginReasonProviderUnavailable = "provider_unavailable"
	loginReasonRateLimited         = "rate_limited"
	loginReasonLoginFailed         = "login_failed"
)

var callbackOutcomeReasons = map[model.OIDCCallbackOutcome]string{
	model.OIDCCallbackInvalidAttempt:     loginReasonStateInvalid,
	model.OIDCCallbackIdentityRejected:   loginReasonIdentityRejected,
	model.OIDCCallbackAccountDeactivated: loginReasonAccountDeactivated,
	model.OIDCCallbackAccountConflict:    loginReasonAccountConflict,
}

type AuthService interface {
	Register(ctx context.Context, username, email, password string) (string, error)
	Login(ctx context.Context, username, password string) (model.SignIn, error)
	Refresh(ctx context.Context, refreshToken string) (model.Tokens, error)
	Logout(ctx context.Context, refreshToken, postLogoutRedirectURI string) (string, error)
	BackchannelLogout(ctx context.Context, provider, logoutToken string) (int, error)
	GetAuthConfig(ctx context.Context) (model.AuthConfig, error)
	StartOIDCLogin(ctx context.Context, provider, redirectAfter string) (model.OIDCLoginStartResult, error)
	CompleteOIDCCallback(ctx context.Context, provider, code, state, stateBinding string) (model.OIDCCallbackResult, error)
}

type AuthHandler struct {
	service        AuthService
	audit          AuditRecorder
	cookieConfig   CookieConfig
	redirectConfig AuthRedirectConfig
}

// CookieConfig is the refresh cookie configuration owned by the config package.
type CookieConfig = config.Cookie

// AuthRedirectConfig is the redirect configuration owned by the config package.
type AuthRedirectConfig = config.AuthRedirect

// NewAuthHandler serves the public auth routes and records sign-ins and
// registrations through audit.
func NewAuthHandler(service AuthService, audit AuditRecorder, cookieConfig CookieConfig, redirectConfig AuthRedirectConfig) *AuthHandler {
	if cookieConfig.Name == "" {
		cookieConfig.Name = "refresh_token"
	}
	if cookieConfig.Path == "" {
		cookieConfig.Path = "/"
	}
	if cookieConfig.SameSite == 0 {
		cookieConfig.SameSite = http.SameSiteLaxMode
	}
	if redirectConfig.CallbackPath == "" {
		redirectConfig.CallbackPath = "/auth/callback"
	}
	return &AuthHandler{
		service:        service,
		audit:          audit,
		cookieConfig:   cookieConfig,
		redirectConfig: normalizeAuthRedirectConfig(redirectConfig),
	}
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req dto.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.auditRegistration(c, "", apperrors.ErrBadRequest)
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	if err := validation.ResourceName(req.Username); err != nil {
		h.auditRegistration(c, "", apperrors.ErrBadRequest)
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.New(apperrors.ErrBadRequest, err.Error()))
		return
	}

	userID, err := h.service.Register(c.Request.Context(), req.Username, req.Email, req.Password)
	h.auditRegistration(c, userID, err)
	if err != nil {
		h.handleAuthError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"user_id": userID})
}

func (h *AuthHandler) Login(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.auditLoginFailure(c, apperrors.ErrBadRequest)
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	if err := validation.ResourceName(req.Username); err != nil {
		h.auditLoginFailure(c, apperrors.ErrBadRequest)
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.New(apperrors.ErrBadRequest, err.Error()))
		return
	}

	signIn, err := h.service.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		h.auditLoginFailure(c, err)
		h.handleAuthError(c, err)
		return
	}

	if !h.setRefreshTokenCookie(c, signIn.Tokens) {
		h.auditLoginFailure(c, apperrors.ErrUnavailable)
		return
	}
	h.auditSignInSuccess(c, auditlog.ActionAuthLogin, nil, signIn.User)
	c.JSON(http.StatusOK, gin.H{"access_token": signIn.Tokens.AccessToken})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !h.requireCookieOrigin(c) {
		return
	}
	refreshToken, err := c.Cookie(h.cookieConfig.Name)
	if err != nil {
		h.clearRefreshTokenCookie(c)
		httpresponse.Respond(c, http.StatusUnauthorized, apperrors.ErrUnauthorized)
		return
	}

	tokens, err := h.service.Refresh(c.Request.Context(), refreshToken)
	if err != nil {
		if errors.Is(err, apperrors.ErrInvalidToken) || errors.Is(err, apperrors.ErrUnauthorized) {
			h.clearRefreshTokenCookie(c)
		}
		h.handleAuthError(c, err)
		return
	}

	if !h.setRefreshTokenCookie(c, tokens) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"access_token": tokens.AccessToken})
}

func (h *AuthHandler) Providers(c *gin.Context) {
	cfg, err := h.service.GetAuthConfig(c.Request.Context())
	if err != nil {
		h.handleAuthError(c, err)
		return
	}

	providers := make([]gin.H, 0, len(cfg.OIDCProviders))
	for _, provider := range cfg.OIDCProviders {
		providers = append(providers, gin.H{"name": provider.Name, "available": provider.Available})
	}

	c.JSON(http.StatusOK, gin.H{
		"local_login_enabled":    cfg.LocalLoginEnabled,
		"local_register_enabled": cfg.LocalRegisterEnabled,
		"oidc_providers":         providers,
	})
}

// StartKeycloakLogin is a browser navigation: with the provider enabled it
// answers only redirects, to the provider or, on failure, to the SPA callback
// page with a reason. With OIDC off SSO answers not found, and so does it.
func (h *AuthHandler) StartKeycloakLogin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	redirectAfter, err := h.safeRedirectAfter(c.Query("redirect_after"))
	if err != nil {
		h.redirectLoginFailure(c, h.defaultRedirectAfter(), loginReasonLoginFailed)
		return
	}

	result, err := h.service.StartOIDCLogin(c.Request.Context(), keycloakProvider, redirectAfter)
	if err != nil {
		h.failNavigation(c, redirectAfter, err)
		return
	}
	cookieName, valid := oidcStateCookieName(result.State)
	maxAge := int(time.Until(result.ExpiresAt) / time.Second)
	if !valid || maxAge <= 0 || result.StateBinding == "" || result.AuthURL == "" {
		h.redirectLoginFailure(c, redirectAfter, loginReasonLoginFailed)
		return
	}
	h.setOIDCStateCookie(c, cookieName, result.StateBinding, maxAge)
	c.Redirect(http.StatusFound, result.AuthURL)
}

// CompleteKeycloakCallback is a browser navigation like StartKeycloakLogin. It
// clears the attempt's binding cookie on every outcome, and sends SSO only a
// callback that carries a code, a well-formed state and that state's cookie.
func (h *AuthHandler) CompleteKeycloakCallback(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	state := c.Query("state")
	cookieName, stateValid := oidcStateCookieName(state)
	var stateBinding string
	if stateValid {
		stateBinding, _ = c.Cookie(cookieName)
		h.clearOIDCStateCookie(c, cookieName)
	}
	if providerError := c.Query("error"); providerError != "" {
		reason := loginReasonProviderError
		if providerError == "access_denied" {
			reason = loginReasonAccessDenied
		}
		h.auditCallbackFailure(c, h.refuseCallback(c, reason))
		return
	}
	code := c.Query("code")
	if !stateValid || stateBinding == "" || code == "" {
		h.auditCallbackFailure(c, h.refuseCallback(c, loginReasonStateInvalid))
		return
	}

	result, err := h.service.CompleteOIDCCallback(c.Request.Context(), keycloakProvider, code, state, stateBinding)
	if err != nil {
		h.auditCallbackFailure(c, h.failNavigation(c, h.defaultRedirectAfter(), err))
		return
	}
	target := h.defaultRedirectAfter()
	if result.RedirectAfter != "" {
		if safe, err := h.safeRedirectAfter(result.RedirectAfter); err == nil {
			target = safe
		}
	}
	if result.Outcome != model.OIDCCallbackSuccess {
		reason, known := callbackOutcomeReasons[result.Outcome]
		if !known {
			reason = loginReasonLoginFailed
		}
		h.auditCallbackFailure(c, reason)
		h.redirectLoginFailure(c, target, reason)
		return
	}
	if !h.writeRefreshTokenCookie(c, result.Tokens) {
		h.auditCallbackFailure(c, loginReasonLoginFailed)
		h.redirectLoginFailure(c, target, loginReasonLoginFailed)
		return
	}
	h.auditSignInSuccess(c, auditlog.ActionAuthOIDCCallback, keycloakAuditDetails(), result.User)
	c.Redirect(http.StatusFound, redirectWithLoginResult(target, ""))
}

// RejectRateLimitedNavigation answers a sign-in navigation over its rate limit
// with a redirect the SPA can show, instead of a JSON 429.
func (h *AuthHandler) RejectRateLimitedNavigation(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if cookieName, valid := oidcStateCookieName(c.Query("state")); valid {
		h.clearOIDCStateCookie(c, cookieName)
	}
	h.redirectLoginFailure(c, h.defaultRedirectAfter(), loginReasonRateLimited)
}

// refuseCallback refuses a callback without sending it to SSO's callback. It
// still asks SSO whether the provider is enabled, so with OIDC off the route
// is not found on every path. It returns the reason it redirected with, empty
// when it answered not found.
func (h *AuthHandler) refuseCallback(c *gin.Context, reason string) string {
	cfg, err := h.service.GetAuthConfig(c.Request.Context())
	if err != nil {
		h.redirectLoginFailure(c, h.defaultRedirectAfter(), loginReasonProviderUnavailable)
		return loginReasonProviderUnavailable
	}
	for _, provider := range cfg.OIDCProviders {
		if provider.Name == keycloakProvider {
			h.redirectLoginFailure(c, h.defaultRedirectAfter(), reason)
			return reason
		}
	}
	httpresponse.Respond(c, http.StatusNotFound, apperrors.ErrNotFound)
	return ""
}

// failNavigation answers an SSO error on a sign-in navigation: not found with
// OIDC off, otherwise a redirect to target with the error's reason. It
// returns that reason, empty when it answered not found.
func (h *AuthHandler) failNavigation(c *gin.Context, target string, err error) string {
	if errors.Is(err, apperrors.ErrNotFound) {
		httpresponse.Respond(c, http.StatusNotFound, apperrors.ErrNotFound)
		return ""
	}
	reason := loginReasonForError(err)
	h.redirectLoginFailure(c, target, reason)
	return reason
}

// Sign-in audit events record only what SSO answered, never what the
// browser sent: no attempted username, password, code, state or token.

// auditSignInSuccess attributes a sign-in to the user SSO signed in, with that
// user as the resource. A user without a valid ID, which SSO never answers, is
// recorded under system scope rather than dropped.
func (h *AuthHandler) auditSignInSuccess(c *gin.Context, action string, details map[string]string, user model.SignedInUser) {
	input := auditInput{Action: action, ResourceType: auditlog.ResourceAuth, DetailsJSON: auditlog.SafeDetailsJSON(details)}
	scope, ok := signedInScope(user)
	if ok {
		input.ResourceID = user.ID
		input.OwnerID = user.ID
	} else {
		scope = accessscope.Scope{Kind: accessscope.KindSystem}
	}
	recordAuditAs(c, h.audit, scope, input)
}

// auditSignInFailure records a failed sign-in under system scope with its code.
func (h *AuthHandler) auditSignInFailure(c *gin.Context, action string, details map[string]string, errorCode string) {
	recordAuditAs(c, h.audit, accessscope.Scope{Kind: accessscope.KindSystem}, auditInput{
		Action:       action,
		ResourceType: auditlog.ResourceAuth,
		Outcome:      auditlog.OutcomeFailure,
		ErrorCode:    errorCode,
		DetailsJSON:  auditlog.SafeDetailsJSON(details),
	})
}

// auditLoginFailure records a failed local sign-in with the error's code.
func (h *AuthHandler) auditLoginFailure(c *gin.Context, err error) {
	h.auditSignInFailure(c, auditlog.ActionAuthLogin, nil, apperrors.Code(err))
}

// auditCallbackFailure records a failed callback with its reason code. An
// empty reason is a route answered as not found, which is no sign-in.
func (h *AuthHandler) auditCallbackFailure(c *gin.Context, reason string) {
	if reason == "" {
		return
	}
	h.auditSignInFailure(c, auditlog.ActionAuthOIDCCallback, keycloakAuditDetails(), reason)
}

// auditRegistration records a registration under system scope, with the new
// user as the resource on success and the error's code on failure.
func (h *AuthHandler) auditRegistration(c *gin.Context, userID string, err error) {
	input := auditInput{Action: auditlog.ActionAuthRegister, ResourceType: auditlog.ResourceUser}
	if err != nil {
		input.Outcome = auditlog.OutcomeFailure
		input.ErrorCode = apperrors.Code(err)
	} else {
		input.ResourceID = userID
		input.OwnerID = userID
	}
	recordAuditAs(c, h.audit, accessscope.Scope{Kind: accessscope.KindSystem}, input)
}

func keycloakAuditDetails() map[string]string {
	return map[string]string{auditlog.DetailProvider: keycloakProvider}
}

// signedInScope is the audit scope of a sign-in: admin when SSO signed in an
// administrator, user otherwise. Request scopes come from the route instead;
// a sign-in has no route scope, so the role picks it. It is false when SSO
// named no valid user.
func signedInScope(user model.SignedInUser) (accessscope.Scope, bool) {
	userID, err := uuid.Parse(user.ID)
	if err != nil || userID == uuid.Nil {
		return accessscope.Scope{}, false
	}
	kind := accessscope.KindUser
	if user.Role == roleAdmin {
		kind = accessscope.KindAdmin
	}
	return accessscope.Scope{Kind: kind, UserID: userID, Username: user.Username, Role: user.Role}, true
}

func (h *AuthHandler) redirectLoginFailure(c *gin.Context, target, reason string) {
	c.Redirect(http.StatusFound, redirectWithLoginResult(target, reason))
}

// loginReasonForError names an SSO failure: the provider or SSO being
// unreachable or slow is provider_unavailable, anything else login_failed.
func loginReasonForError(err error) string {
	if errors.Is(err, apperrors.ErrUnavailable) || errors.Is(err, apperrors.ErrTimeout) {
		return loginReasonProviderUnavailable
	}
	return loginReasonLoginFailed
}

// oidcStateCookieName derives an attempt's cookie name from its state, which
// must be 43 base64url characters (32 random bytes).
func oidcStateCookieName(state string) (string, bool) {
	if len(state) != 43 {
		return "", false
	}
	if decoded, err := base64.RawURLEncoding.Strict().DecodeString(state); err != nil || len(decoded) != 32 {
		return "", false
	}
	sum := sha256.Sum256([]byte(state))
	return oidcStateCookiePrefix + hex.EncodeToString(sum[:])[:16], true
}

// Logout revokes the browser's session. For a session created through OIDC it
// answers the provider sign-out URL, which returns the browser to the login
// page of the validated request origin; the SPA navigates to it after this
// sign-out succeeds. A local session gets an empty object.
func (h *AuthHandler) Logout(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	requestOrigin, ok := h.cookieOrigin(c)
	if !ok {
		return
	}
	value, _ := c.Cookie(h.cookieConfig.Name)
	providerLogoutURL, err := h.service.Logout(c.Request.Context(), value, requestOrigin+spaLoginPath)
	if err != nil {
		h.handleAuthError(c, err)
		return
	}
	h.clearRefreshTokenCookie(c)
	body := gin.H{}
	if providerLogoutURL != "" {
		body["provider_logout_url"] = providerLogoutURL
	}
	c.JSON(http.StatusOK, body)
}

// KeycloakBackchannelLogout receives the provider's back-channel logout: a
// form-encoded logout_token posted by the provider's server, so it needs no
// cookie and no browser origin. A body it cannot read sends SSO no token, so
// the answer stays not found with OIDC off. It answers 200 for a valid token
// even when nothing matched, 400 for an invalid one, 503 while the provider's
// keys are unavailable and 404 with OIDC off, and records a verified logout.
func (h *AuthHandler) KeycloakBackchannelLogout(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBackchannelLogoutBodyBytes)
	var logoutToken string
	if err := c.Request.ParseForm(); err == nil {
		logoutToken = c.Request.PostForm.Get("logout_token")
	}
	revoked, err := h.service.BackchannelLogout(c.Request.Context(), keycloakProvider, logoutToken)
	switch {
	case err == nil:
	case errors.Is(err, apperrors.ErrNotFound):
		httpresponse.Respond(c, http.StatusNotFound, apperrors.ErrNotFound)
		return
	case errors.Is(err, apperrors.ErrBadRequest):
		httpresponse.Respond(c, http.StatusBadRequest, err)
		return
	case errors.Is(err, apperrors.ErrUnavailable), errors.Is(err, apperrors.ErrTimeout):
		httpresponse.Respond(c, http.StatusServiceUnavailable, apperrors.ErrUnavailable)
		return
	default:
		h.handleAuthError(c, err)
		return
	}
	recordAuditAs(c, h.audit, accessscope.Scope{Kind: accessscope.KindSystem}, auditInput{
		Action:       auditlog.ActionAuthProviderLogout,
		ResourceType: auditlog.ResourceAuth,
		DetailsJSON: auditlog.SafeDetailsJSON(map[string]string{
			auditlog.DetailProvider:        keycloakProvider,
			auditlog.DetailRevokedSessions: auditlog.IntDetail(revoked),
		}),
	})
	c.Status(http.StatusOK)
}

// Cookie-authenticated mutations require explicit trusted browser origin evidence.
func (h *AuthHandler) requireCookieOrigin(c *gin.Context) bool {
	_, ok := h.cookieOrigin(c)
	return ok
}

// cookieOrigin returns the trusted browser origin of a cookie-authenticated
// request, from Origin or, when it is absent, Referer; otherwise it answers
// 403 and reports false.
func (h *AuthHandler) cookieOrigin(c *gin.Context) (string, bool) {
	raw := c.GetHeader("Origin")
	fromReferer := raw == ""
	if fromReferer {
		raw = c.GetHeader("Referer")
	}
	parsed, err := url.Parse(raw)
	allowed := err == nil && parsed.Host != "" && parsed.User == nil && (parsed.Scheme == "http" || parsed.Scheme == "https")
	if allowed && !fromReferer {
		allowed = raw == origin(parsed)
	}
	if allowed {
		for _, candidate := range h.redirectConfig.CookieAllowedOrigins {
			if candidate == origin(parsed) {
				return candidate, true
			}
		}
	}
	httpresponse.Respond(c, http.StatusForbidden, apperrors.ErrForbidden)
	return "", false
}

func (h *AuthHandler) handleAuthError(c *gin.Context, err error) {
	httpresponse.Respond(c, httpresponse.Status(err), err)
}

func (h *AuthHandler) setRefreshTokenCookie(c *gin.Context, tokens model.Tokens) bool {
	if !h.writeRefreshTokenCookie(c, tokens) {
		httpresponse.Respond(c, http.StatusServiceUnavailable, apperrors.ErrUnavailable)
		return false
	}
	return true
}

// writeRefreshTokenCookie sets the refresh cookie and reports false, writing
// nothing, when SSO answered tokens Gateway cannot use.
func (h *AuthHandler) writeRefreshTokenCookie(c *gin.Context, tokens model.Tokens) bool {
	seconds := int(time.Until(tokens.RefreshExpiresAt) / time.Second)
	if seconds <= 0 || tokens.RefreshToken == "" || tokens.AccessToken == "" {
		return false
	}
	cfg := h.cookieConfig
	c.SetSameSite(cfg.SameSite)
	c.SetCookie(cfg.Name, tokens.RefreshToken, seconds, cfg.Path, cfg.Domain, cfg.Secure, cfg.HTTPOnly)
	return true
}

func (h *AuthHandler) clearRefreshTokenCookie(c *gin.Context) {
	cfg := h.cookieConfig
	c.SetSameSite(cfg.SameSite)
	c.SetCookie(cfg.Name, "", -1, cfg.Path, cfg.Domain, cfg.Secure, cfg.HTTPOnly)
}

func (h *AuthHandler) setOIDCStateCookie(c *gin.Context, name, stateBinding string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, stateBinding, maxAge, oidcStateCookiePath, h.cookieConfig.Domain, h.cookieConfig.Secure, true)
}

func (h *AuthHandler) clearOIDCStateCookie(c *gin.Context, name string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, "", -1, oidcStateCookiePath, h.cookieConfig.Domain, h.cookieConfig.Secure, true)
}

func (h *AuthHandler) safeRedirectAfter(rawRedirect string) (string, error) {
	rawRedirect = strings.TrimSpace(rawRedirect)
	if rawRedirect == "" {
		return h.defaultRedirectAfter(), nil
	}

	if strings.HasPrefix(rawRedirect, "//") {
		return "", apperrors.New(apperrors.ErrBadRequest, "invalid auth redirect target")
	}

	parsed, err := url.Parse(rawRedirect)
	if err != nil {
		return "", apperrors.New(apperrors.ErrBadRequest, "invalid auth redirect target")
	}

	if parsed.IsAbs() {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", apperrors.New(apperrors.ErrBadRequest, "invalid auth redirect target")
		}
		if parsed.Path != h.redirectConfig.CallbackPath {
			return "", apperrors.New(apperrors.ErrBadRequest, "invalid auth redirect target")
		}
		if _, ok := h.allowedOrigins()[origin(parsed)]; !ok {
			return "", apperrors.New(apperrors.ErrBadRequest, "invalid auth redirect target")
		}
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String(), nil
	}

	if parsed.Host != "" || rawRedirect != h.redirectConfig.CallbackPath {
		return "", apperrors.New(apperrors.ErrBadRequest, "invalid auth redirect target")
	}
	return h.redirectConfig.CallbackPath, nil
}

func (h *AuthHandler) defaultRedirectAfter() string {
	return h.redirectConfig.CallbackPath
}

func (h *AuthHandler) allowedOrigins() map[string]struct{} {
	allowed := make(map[string]struct{}, len(h.redirectConfig.AllowedOrigins))
	for _, rawOrigin := range h.redirectConfig.AllowedOrigins {
		if parsed, err := url.Parse(rawOrigin); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			allowed[origin(parsed)] = struct{}{}
		}
	}
	return allowed
}

// redirectWithLoginResult adds login=success to a validated target, or
// login=error and reason when reason is set.
func redirectWithLoginResult(rawRedirect, reason string) string {
	parsed, err := url.Parse(rawRedirect)
	if err != nil {
		parsed = &url.URL{Path: "/auth/callback"}
	}
	query := parsed.Query()
	if reason == "" {
		query.Set("login", "success")
	} else {
		query.Set("login", "error")
		query.Set("reason", reason)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func normalizeAuthRedirectConfig(cfg AuthRedirectConfig) AuthRedirectConfig {
	cfg.CallbackPath = "/" + strings.TrimLeft(strings.TrimSpace(cfg.CallbackPath), "/")
	origins := make([]string, 0, len(cfg.AllowedOrigins))
	for _, rawOrigin := range cfg.AllowedOrigins {
		rawOrigin = strings.TrimSpace(rawOrigin)
		if rawOrigin != "" {
			origins = append(origins, strings.TrimRight(rawOrigin, "/"))
		}
	}
	cfg.AllowedOrigins = origins
	return cfg
}

func origin(u *url.URL) string {
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}
