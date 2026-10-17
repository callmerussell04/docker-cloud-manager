package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	h := handler.NewAuthHandler(auth, handler.CookieConfig{Name: "refresh_token", Path: "/", HTTPOnly: true}, testRedirectConfig())
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

	auth.EXPECT().Login(mock.Anything, "alice", "secret123").Return(model.Tokens{AccessToken: "access", RefreshToken: "refresh", RefreshExpiresAt: time.Now().Add(time.Hour)}, nil)
	resp = perform(router, http.MethodPost, "/login", `{"username":"alice","password":"secret123"}`, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"access_token":"access"`)
	requireCookie(t, resp.Result(), "refresh_token", "refresh", true)

	auth.EXPECT().Refresh(mock.Anything, "refresh").Return(model.Tokens{AccessToken: "access2", RefreshToken: "refresh2", RefreshExpiresAt: time.Now().Add(time.Hour)}, nil)
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

	auth.EXPECT().Logout(mock.Anything, "").Return(nil)
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
	h := handler.NewAuthHandler(auth, handler.CookieConfig{Name: "refresh_token", Path: "/", HTTPOnly: true}, testRedirectConfig())
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
		Return(model.OIDCCallbackResult{Tokens: model.Tokens{AccessToken: "access", RefreshToken: "refresh", RefreshExpiresAt: time.Now().Add(time.Hour)}, RedirectAfter: "http://frontend.example/auth/callback"}, nil)
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
		AllowedOrigins:       []string{"http://frontend.example"},
		CookieAllowedOrigins: []string{"http://frontend.example"},
		CallbackPath:         "/auth/callback",
	}
}

func perform(router http.Handler, method, path, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Origin", "http://frontend.example")
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

func TestAuthHandlerLogoutRevocationAndRefreshFailureCookies(t *testing.T) {
	for _, action := range []string{"refresh", "logout"} {
		for _, kind := range []string{"success", "invalid", "unavailable", "timeout"} {
			t.Run(action+"/"+kind, func(t *testing.T) {
				auth := gatewayhandlermocks.NewMockAuthService(t)
				h := handler.NewAuthHandler(auth, handler.CookieConfig{Name: "refresh_token", Path: "/api/v1/auth", Domain: "example.com", HTTPOnly: true, Secure: true}, testRedirectConfig())
				router := gin.New()
				router.POST("/refresh", h.Refresh)
				router.POST("/logout", h.Logout)
				var err error
				statusCode := http.StatusOK
				switch kind {
				case "invalid":
					err, statusCode = apperrors.ErrInvalidToken, http.StatusUnauthorized
				case "unavailable":
					err, statusCode = apperrors.ErrUnavailable, http.StatusServiceUnavailable
				case "timeout":
					err, statusCode = apperrors.ErrTimeout, http.StatusGatewayTimeout
				}
				if action == "refresh" {
					auth.EXPECT().Refresh(mock.Anything, "secret").Return(model.Tokens{AccessToken: "access", RefreshToken: "next", RefreshExpiresAt: time.Now().Add(90 * time.Second)}, err)
				} else {
					auth.EXPECT().Logout(mock.Anything, "secret").Return(err)
				}
				resp := perform(router, http.MethodPost, "/"+action, "", []*http.Cookie{{Name: "refresh_token", Value: "secret"}})
				require.Equal(t, statusCode, resp.Code)
				if kind == "unavailable" || kind == "timeout" || (action == "logout" && kind == "invalid") {
					require.Empty(t, resp.Result().Cookies())
				} else if action == "logout" || kind == "invalid" {
					requireCookieMaxAge(t, resp.Result(), "refresh_token", -1)
				} else {
					cookie := requireCookie(t, resp.Result(), "refresh_token", "next", true)
					require.InDelta(t, 89, cookie.MaxAge, 1)
					require.Equal(t, "/api/v1/auth", cookie.Path)
					require.Equal(t, "example.com", cookie.Domain)
					require.True(t, cookie.Secure)
					require.NotContains(t, resp.Body.String(), "next")
				}
				require.Equal(t, "no-store", resp.Header().Get("Cache-Control"))
			})
		}
	}
}

func TestAuthHandlerCookieRequestsRequireTrustedBrowserOrigin(t *testing.T) {
	for _, action := range []string{"refresh", "logout"} {
		for _, origin := range []string{"", "null", "http://evil.example", "http://frontend.example.evil", "http://frontend.example/path"} {
			t.Run(action+"/"+origin, func(t *testing.T) {
				auth := gatewayhandlermocks.NewMockAuthService(t)
				h := handler.NewAuthHandler(auth, handler.CookieConfig{}, testRedirectConfig())
				router := gin.New()
				router.POST("/refresh", h.Refresh)
				router.POST("/logout", h.Logout)
				req := httptest.NewRequest(http.MethodPost, "/"+action, nil)
				req.Header.Set("Origin", origin)
				req.Header.Set("Referer", "http://evil.example/page")
				req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "secret"})
				resp := httptest.NewRecorder()
				router.ServeHTTP(resp, req)
				require.Equal(t, http.StatusForbidden, resp.Code)
				require.Empty(t, resp.Result().Cookies())
			})
		}
	}
}

func TestCookieOriginFallsBackToRefererOnlyWhenOriginIsAbsent(t *testing.T) {
	for _, test := range []struct {
		name, origin, referer string
		allowed               bool
	}{
		{"trusted-origin", "http://frontend.example", "", true},
		{"trusted-referer", "", "http://frontend.example/page?from=app", true},
		{"origin-takes-precedence", "http://evil.example", "http://frontend.example/page", false},
		{"null-origin-is-not-absent", "null", "http://frontend.example/page", false},
		{"referer-userinfo", "", "http://user@frontend.example/page", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			auth := gatewayhandlermocks.NewMockAuthService(t)
			h := handler.NewAuthHandler(auth, handler.CookieConfig{}, testRedirectConfig())
			router := gin.New()
			router.POST("/logout", h.Logout)
			if test.allowed {
				auth.EXPECT().Logout(mock.Anything, "").Return(nil)
			}
			req := httptest.NewRequest(http.MethodPost, "/logout", nil)
			if test.origin != "" {
				req.Header.Set("Origin", test.origin)
			}
			if test.referer != "" {
				req.Header.Set("Referer", test.referer)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if test.allowed {
				require.Equal(t, http.StatusOK, response.Code)
			} else {
				require.Equal(t, http.StatusForbidden, response.Code)
				require.Empty(t, response.Result().Cookies())
			}
		})
	}
}
