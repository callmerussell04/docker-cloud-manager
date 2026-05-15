package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	gatewayhandlermocks "github.com/callmerussell04/docker-cloud-manager/tests/mocks/gateway/http/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAuthHandlerRegisterLoginRefreshLogoutAndProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := gatewayhandlermocks.NewMockAuthService(t)
	h := handler.NewAuthHandler(auth, handler.CookieConfig{Name: "refresh_token", Path: "/", MaxAge: 3600, HTTPOnly: true}, testRedirectConfig())
	router := gin.New()
	router.POST("/register", h.Register)
	router.POST("/login", h.Login)
	router.POST("/refresh", h.Refresh)
	router.POST("/logout", h.Logout)
	router.GET("/providers", h.Providers)

	auth.EXPECT().Register(mock.Anything, "alice", "alice@example.com", "secret123").Return("user-id", nil)
	resp := perform(router, http.MethodPost, "/register", `{"username":"alice","email":"alice@example.com","password":"secret123"}`, nil)
	require.Equal(t, http.StatusCreated, resp.Code)
	require.Contains(t, resp.Body.String(), `"user_id":"user-id"`)

	auth.EXPECT().Login(mock.Anything, "alice", "secret123").Return(model.Tokens{AccessToken: "access", RefreshToken: "refresh"}, nil)
	resp = perform(router, http.MethodPost, "/login", `{"username":"alice","password":"secret123"}`, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"access_token":"access"`)
	requireCookie(t, resp.Result(), "refresh_token", "refresh", true)

	auth.EXPECT().Refresh(mock.Anything, "refresh").Return(model.Tokens{AccessToken: "access2", RefreshToken: "refresh2"}, nil)
	resp = perform(router, http.MethodPost, "/refresh", ``, []*http.Cookie{{Name: "refresh_token", Value: "refresh"}})
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"access_token":"access2"`)
	requireCookie(t, resp.Result(), "refresh_token", "refresh2", true)

	auth.EXPECT().GetAuthConfig(mock.Anything).Return(model.AuthConfig{
		LocalLoginEnabled:    true,
		LocalRegisterEnabled: false,
		OIDCProviders:        []model.OIDCProvider{{Name: "keycloak"}},
	}, nil)
	resp = perform(router, http.MethodGet, "/providers", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"local_login_enabled":true`)
	require.Contains(t, resp.Body.String(), `"name":"keycloak"`)

	resp = perform(router, http.MethodPost, "/logout", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	requireCookieMaxAge(t, resp.Result(), "refresh_token", -1)
}

func TestAuthHandlerValidationAndErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := gatewayhandlermocks.NewMockAuthService(t)
	h := handler.NewAuthHandler(auth, handler.CookieConfig{Name: "refresh_token"}, testRedirectConfig())
	router := gin.New()
	router.POST("/register", h.Register)
	router.POST("/login", h.Login)
	router.POST("/refresh", h.Refresh)

	resp := perform(router, http.MethodPost, "/register", `{"username":"bad name","email":"bad@example.com","password":"secret"}`, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)

	auth.EXPECT().Login(mock.Anything, "alice", "wrong").Return(model.Tokens{}, apperrors.ErrInvalidCredentials)
	resp = perform(router, http.MethodPost, "/login", `{"username":"alice","password":"wrong"}`, nil)
	require.Equal(t, http.StatusUnauthorized, resp.Code)

	resp = perform(router, http.MethodPost, "/refresh", ``, nil)
	require.Equal(t, http.StatusUnauthorized, resp.Code)
}

func TestAuthHandlerOIDCFlowUsesHttpOnlyStateCookieAndSafeRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := gatewayhandlermocks.NewMockAuthService(t)
	h := handler.NewAuthHandler(auth, handler.CookieConfig{Name: "refresh_token", Path: "/", MaxAge: 3600, HTTPOnly: true}, testRedirectConfig())
	router := gin.New()
	router.GET("/start", h.StartKeycloakLogin)
	router.GET("/callback", h.CompleteKeycloakCallback)

	auth.EXPECT().
		StartOIDCLogin(mock.Anything, "keycloak", "http://frontend.example/auth/callback").
		Return(model.OIDCLoginStartResult{AuthURL: "https://keycloak.example/auth", StateBinding: "state-binding"}, nil)
	resp := perform(router, http.MethodGet, "/start?redirect_after=http://frontend.example/auth/callback", ``, nil)
	require.Equal(t, http.StatusFound, resp.Code)
	require.Equal(t, "https://keycloak.example/auth", resp.Header().Get("Location"))
	stateCookie := requireCookie(t, resp.Result(), "oidc_state", "state-binding", true)
	require.Equal(t, "/api/v1/auth/oidc/keycloak/callback", stateCookie.Path)

	auth.EXPECT().
		CompleteOIDCCallback(mock.Anything, "keycloak", "code", "state", "state-binding").
		Return(model.OIDCCallbackResult{Tokens: model.Tokens{AccessToken: "access", RefreshToken: "refresh"}, RedirectAfter: "http://frontend.example/auth/callback"}, nil)
	resp = perform(router, http.MethodGet, "/callback?code=code&state=state", ``, []*http.Cookie{{Name: "oidc_state", Value: "state-binding"}})
	require.Equal(t, http.StatusFound, resp.Code)
	require.Equal(t, "http://frontend.example/auth/callback?login=success", resp.Header().Get("Location"))
	require.NotContains(t, resp.Header().Get("Location"), "access")
	requireCookie(t, resp.Result(), "refresh_token", "refresh", true)
	requireCookieMaxAge(t, resp.Result(), "oidc_state", -1)
}

func TestAuthHandlerOIDCRejectsUnsafeRedirectAndMissingState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := gatewayhandlermocks.NewMockAuthService(t)
	h := handler.NewAuthHandler(auth, handler.CookieConfig{Name: "refresh_token"}, testRedirectConfig())
	router := gin.New()
	router.GET("/start", h.StartKeycloakLogin)
	router.GET("/callback", h.CompleteKeycloakCallback)

	for _, redirectAfter := range []string{"https://evil.example/auth/callback", "//evil.example/auth/callback", "http://frontend.example/wrong", "javascript:alert(1)"} {
		resp := perform(router, http.MethodGet, "/start?redirect_after="+redirectAfter, ``, nil)
		require.Equal(t, http.StatusBadRequest, resp.Code)
	}

	resp := perform(router, http.MethodGet, "/callback?code=code&state=state", ``, nil)
	require.Equal(t, http.StatusUnauthorized, resp.Code)
}

func testRedirectConfig() handler.AuthRedirectConfig {
	return handler.AuthRedirectConfig{
		AllowedOrigins: []string{"http://frontend.example"},
		CallbackPath:   "/auth/callback",
	}
}

func perform(router http.Handler, method, path, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func requireCookie(t *testing.T, resp *http.Response, name, value string, httpOnly bool) *http.Cookie {
	t.Helper()
	for _, cookie := range resp.Cookies() {
		if cookie.Name == name && cookie.Value == value {
			require.Equal(t, httpOnly, cookie.HttpOnly)
			return cookie
		}
	}
	t.Fatalf("cookie %s=%s not found: %+v", name, value, resp.Cookies())
	return nil
}

func requireCookieMaxAge(t *testing.T, resp *http.Response, name string, maxAge int) {
	t.Helper()
	for _, cookie := range resp.Cookies() {
		if cookie.Name == name && cookie.MaxAge == maxAge {
			return
		}
	}
	t.Fatalf("cookie %s with max-age %d not found: %+v", name, maxAge, resp.Cookies())
}

var _ handler.AuthService = (*gatewayhandlermocks.MockAuthService)(nil)
