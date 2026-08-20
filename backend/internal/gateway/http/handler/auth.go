package handler

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
)

const (
	oidcStateCookieName   = "oidc_state"
	oidcStateCookiePath   = "/api/v1/auth/oidc/keycloak/callback"
	oidcStateCookieMaxAge = 600
)

type AuthService interface {
	Register(ctx context.Context, username, email, password string) (string, error)
	Login(ctx context.Context, username, password string) (model.Tokens, error)
	Refresh(ctx context.Context, refreshToken string) (model.Tokens, error)
	GetAuthConfig(ctx context.Context) (model.AuthConfig, error)
	StartOIDCLogin(ctx context.Context, provider, redirectAfter string) (model.OIDCLoginStartResult, error)
	CompleteOIDCCallback(ctx context.Context, provider, code, state, stateBinding string) (model.OIDCCallbackResult, error)
}

type AuthHandler struct {
	service        AuthService
	cookieConfig   CookieConfig
	redirectConfig AuthRedirectConfig
}

type CookieConfig struct {
	Name     string
	Path     string
	Domain   string
	MaxAge   int
	Secure   bool
	HTTPOnly bool
	SameSite http.SameSite
}

type AuthRedirectConfig struct {
	AllowedOrigins []string
	CallbackPath   string
}

func NewAuthHandler(service AuthService, cookieConfig CookieConfig, redirectConfig AuthRedirectConfig) *AuthHandler {
	if cookieConfig.Name == "" {
		cookieConfig.Name = "refresh_token"
	}
	if cookieConfig.Path == "" {
		cookieConfig.Path = "/"
	}
	if cookieConfig.MaxAge <= 0 {
		cookieConfig.MaxAge = 30 * 24 * 3600
	}
	if cookieConfig.SameSite == 0 {
		cookieConfig.SameSite = http.SameSiteLaxMode
	}
	if redirectConfig.CallbackPath == "" {
		redirectConfig.CallbackPath = "/auth/callback"
	}
	return &AuthHandler{
		service:        service,
		cookieConfig:   cookieConfig,
		redirectConfig: normalizeAuthRedirectConfig(redirectConfig),
	}
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req dto.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	if err := validation.ResourceName(req.Username); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.New(apperrors.ErrBadRequest, err.Error()))
		return
	}

	userID, err := h.service.Register(c.Request.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		h.handleAuthError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"user_id": userID})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	if err := validation.ResourceName(req.Username); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.New(apperrors.ErrBadRequest, err.Error()))
		return
	}

	tokens, err := h.service.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		h.handleAuthError(c, err)
		return
	}

	h.setRefreshTokenCookie(c, tokens.RefreshToken)
	c.JSON(http.StatusOK, gin.H{"access_token": tokens.AccessToken})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie(h.cookieConfig.Name)
	if err != nil {
		httpresponse.Respond(c, http.StatusUnauthorized, apperrors.ErrUnauthorized)
		return
	}

	tokens, err := h.service.Refresh(c.Request.Context(), refreshToken)
	if err != nil {
		h.handleAuthError(c, err)
		return
	}

	h.setRefreshTokenCookie(c, tokens.RefreshToken)
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
		providers = append(providers, gin.H{"name": provider.Name})
	}

	c.JSON(http.StatusOK, gin.H{
		"local_login_enabled":    cfg.LocalLoginEnabled,
		"local_register_enabled": cfg.LocalRegisterEnabled,
		"oidc_providers":         providers,
	})
}

func (h *AuthHandler) StartKeycloakLogin(c *gin.Context) {
	redirectAfter, err := h.safeRedirectAfter(c.Query("redirect_after"))
	if err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, err)
		return
	}

	result, err := h.service.StartOIDCLogin(c.Request.Context(), "keycloak", redirectAfter)
	if err != nil {
		h.handleAuthError(c, err)
		return
	}
	h.setOIDCStateCookie(c, result.StateBinding)
	c.Redirect(http.StatusFound, result.AuthURL)
}

func (h *AuthHandler) CompleteKeycloakCallback(c *gin.Context) {
	if c.Query("error") != "" {
		h.clearOIDCStateCookie(c)
		httpresponse.Respond(c, http.StatusUnauthorized, apperrors.ErrInvalidCredentials)
		return
	}

	stateBinding, err := c.Cookie(oidcStateCookieName)
	if err != nil {
		h.clearOIDCStateCookie(c)
		httpresponse.Respond(c, http.StatusUnauthorized, apperrors.ErrInvalidCredentials)
		return
	}

	result, err := h.service.CompleteOIDCCallback(c.Request.Context(), "keycloak", c.Query("code"), c.Query("state"), stateBinding)
	if err != nil {
		h.clearOIDCStateCookie(c)
		h.handleAuthError(c, err)
		return
	}

	h.clearOIDCStateCookie(c)
	h.setRefreshTokenCookie(c, result.Tokens.RefreshToken)
	redirectAfter, err := h.safeRedirectAfter(result.RedirectAfter)
	if err != nil {
		redirectAfter = h.defaultRedirectAfter()
	}
	c.Redirect(http.StatusFound, redirectWithLoginSuccess(redirectAfter))
}

func (h *AuthHandler) Logout(c *gin.Context) {
	h.clearRefreshTokenCookie(c)
	c.Status(http.StatusOK)
}

func (h *AuthHandler) handleAuthError(c *gin.Context, err error) {
	httpresponse.Respond(c, httpresponse.Status(err), err)
}

func (h *AuthHandler) setRefreshTokenCookie(c *gin.Context, token string) {
	cfg := h.cookieConfig
	c.SetSameSite(cfg.SameSite)
	c.SetCookie(cfg.Name, token, cfg.MaxAge, cfg.Path, cfg.Domain, cfg.Secure, cfg.HTTPOnly)
}

func (h *AuthHandler) clearRefreshTokenCookie(c *gin.Context) {
	cfg := h.cookieConfig
	c.SetSameSite(cfg.SameSite)
	c.SetCookie(cfg.Name, "", -1, cfg.Path, cfg.Domain, cfg.Secure, cfg.HTTPOnly)
}

func (h *AuthHandler) setOIDCStateCookie(c *gin.Context, stateBinding string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oidcStateCookieName, stateBinding, oidcStateCookieMaxAge, oidcStateCookiePath, h.cookieConfig.Domain, h.cookieConfig.Secure, true)
}

func (h *AuthHandler) clearOIDCStateCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oidcStateCookieName, "", -1, oidcStateCookiePath, h.cookieConfig.Domain, h.cookieConfig.Secure, true)
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

func redirectWithLoginSuccess(rawRedirect string) string {
	parsed, err := url.Parse(rawRedirect)
	if err != nil {
		return "/auth/callback?login=success"
	}
	query := parsed.Query()
	query.Set("login", "success")
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
