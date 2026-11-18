package handler_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	gatewayhandlermocks "github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/mocks/gateway/http/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAuthHandlerRegisterLoginRefreshLogoutAndProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := gatewayhandlermocks.NewMockAuthService(t)
	h := handler.NewAuthHandler(auth, anyAudit(t), handler.CookieConfig{Name: "refresh_token", Path: "/", HTTPOnly: true}, testRedirectConfig())
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

	auth.EXPECT().Login(mock.Anything, "alice", "secret123").Return(model.SignIn{Tokens: model.Tokens{AccessToken: "access", RefreshToken: "refresh", RefreshExpiresAt: time.Now().Add(time.Hour)}}, nil)
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
		OIDCProviders:        []model.OIDCProvider{{Name: "keycloak", Available: false}},
	}, nil)
	resp = perform(router, http.MethodGet, "/providers", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"local_login_enabled":true`)
	require.Contains(t, resp.Body.String(), `"oidc_providers":[{"available":false,"name":"keycloak"}]`)

	auth.EXPECT().Logout(mock.Anything, "", "http://frontend.example/login").Return("", nil)
	resp = perform(router, http.MethodPost, "/logout", ``, nil)
	require.Equal(t, http.StatusOK, resp.Code)
	requireCookieMaxAge(t, resp.Result(), "refresh_token", -1)
}

func TestAuthHandlerValidationAndErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := gatewayhandlermocks.NewMockAuthService(t)
	h := handler.NewAuthHandler(auth, anyAudit(t), handler.CookieConfig{Name: "refresh_token"}, testRedirectConfig())
	router := gin.New()
	router.POST("/register", h.Register)
	router.POST("/login", h.Login)
	router.POST("/refresh", h.Refresh)

	resp := perform(router, http.MethodPost, "/register", `{"username":"bad name","email":"bad@example.com","password":"secret"}`, nil)
	require.Equal(t, http.StatusBadRequest, resp.Code)

	auth.EXPECT().Login(mock.Anything, "alice", "wrong").Return(model.SignIn{}, apperrors.ErrInvalidCredentials)
	resp = perform(router, http.MethodPost, "/login", `{"username":"alice","password":"wrong"}`, nil)
	require.Equal(t, http.StatusUnauthorized, resp.Code)

	resp = perform(router, http.MethodPost, "/refresh", ``, nil)
	require.Equal(t, http.StatusUnauthorized, resp.Code)
}

const (
	callbackTarget = "http://frontend.example/auth/callback"
	defaultFailure = "/auth/callback?login=error&reason="
	targetFailure  = callbackTarget + "?login=error&reason="
)

// testState returns a well-formed attempt state (43 base64url characters)
// and the name of its browser-binding cookie: oidc_state_ and the first 16 hex
// characters of the SHA-256 of the state.
func testState(seed string) (string, string) {
	raw := sha256.Sum256([]byte(seed))
	state := base64.RawURLEncoding.EncodeToString(raw[:])
	sum := sha256.Sum256([]byte(state))
	return state, "oidc_state_" + hex.EncodeToString(sum[:])[:16]
}

func oidcRouter(t *testing.T, cookies handler.CookieConfig) (*gatewayhandlermocks.MockAuthService, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	auth := gatewayhandlermocks.NewMockAuthService(t)
	h := handler.NewAuthHandler(auth, anyAudit(t), cookies, testRedirectConfig())
	router := gin.New()
	router.GET("/start", h.StartKeycloakLogin)
	router.GET("/callback", h.CompleteKeycloakCallback)
	return auth, router
}

func expectKeycloakEnabled(auth *gatewayhandlermocks.MockAuthService) {
	auth.EXPECT().GetAuthConfig(mock.Anything).Return(model.AuthConfig{OIDCProviders: []model.OIDCProvider{{Name: "keycloak", Available: true}}}, nil).Maybe()
}

// requireLoginRedirect checks a navigation answer: a redirect to location,
// never a JSON body, and never cached.
func requireLoginRedirect(t *testing.T, resp *httptest.ResponseRecorder, location string) {
	t.Helper()
	require.Equal(t, http.StatusFound, resp.Code)
	require.Equal(t, location, resp.Header().Get("Location"))
	require.NotContains(t, resp.Header().Get("Content-Type"), "json")
	require.Equal(t, "no-store", resp.Header().Get("Cache-Control"))
}

func TestAuthHandlerOIDCFlowBindsEachAttemptToItsOwnCookie(t *testing.T) {
	auth, router := oidcRouter(t, handler.CookieConfig{Name: "refresh_token", Path: "/", HTTPOnly: true, Secure: true, Domain: "example.com"})
	state, cookieName := testState("first")

	auth.EXPECT().
		StartOIDCLogin(mock.Anything, "keycloak", callbackTarget).
		Return(model.OIDCLoginStartResult{AuthURL: "https://keycloak.example/auth", State: state, StateBinding: "state-binding", ExpiresAt: time.Now().Add(30 * time.Minute)}, nil)
	resp := perform(router, http.MethodGet, "/start?redirect_after="+callbackTarget, ``, nil)
	require.Equal(t, http.StatusFound, resp.Code)
	require.Equal(t, "https://keycloak.example/auth", resp.Header().Get("Location"))
	stateCookie := requireCookie(t, resp.Result(), cookieName, "state-binding", true)
	require.Equal(t, "/api/v1/auth/oidc/keycloak/callback", stateCookie.Path)
	require.Equal(t, http.SameSiteLaxMode, stateCookie.SameSite)
	require.True(t, stateCookie.Secure)
	require.Equal(t, "example.com", stateCookie.Domain)
	require.InDelta(t, 30*60, stateCookie.MaxAge, 2, "the cookie lives as long as the attempt SSO started")

	auth.EXPECT().
		CompleteOIDCCallback(mock.Anything, "keycloak", "code", state, "state-binding").
		Return(model.OIDCCallbackResult{Outcome: model.OIDCCallbackSuccess, Tokens: model.Tokens{AccessToken: "access", RefreshToken: "refresh", RefreshExpiresAt: time.Now().Add(time.Hour)}, RedirectAfter: callbackTarget}, nil)
	resp = perform(router, http.MethodGet, "/callback?code=code&state="+state, ``, []*http.Cookie{{Name: cookieName, Value: "state-binding"}})
	requireLoginRedirect(t, resp, callbackTarget+"?login=success")
	require.NotContains(t, resp.Header().Get("Location"), "access")
	requireCookie(t, resp.Result(), "refresh_token", "refresh", true)
	requireCookieMaxAge(t, resp.Result(), cookieName, -1)
}

// Two sign-in tabs in one browser keep separate cookies, and each callback
// sends SSO the binding of its own attempt.
func TestAuthHandlerOIDCTwoAttemptsInOneBrowserBothComplete(t *testing.T) {
	auth, router := oidcRouter(t, handler.CookieConfig{Name: "refresh_token", Path: "/", HTTPOnly: true})
	first, firstCookie := testState("first")
	second, secondCookie := testState("second")
	require.NotEqual(t, firstCookie, secondCookie)
	both := []*http.Cookie{{Name: firstCookie, Value: "first-binding"}, {Name: secondCookie, Value: "second-binding"}}

	for _, attempt := range []struct{ state, binding, cookie string }{{second, "second-binding", secondCookie}, {first, "first-binding", firstCookie}} {
		auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", "code", attempt.state, attempt.binding).
			Return(model.OIDCCallbackResult{Outcome: model.OIDCCallbackSuccess, Tokens: model.Tokens{AccessToken: "access", RefreshToken: "refresh", RefreshExpiresAt: time.Now().Add(time.Hour)}}, nil).Once()
		resp := perform(router, http.MethodGet, "/callback?code=code&state="+attempt.state, ``, both)
		requireLoginRedirect(t, resp, "/auth/callback?login=success")
		requireCookieMaxAge(t, resp.Result(), attempt.cookie, -1)
		for _, cookie := range resp.Result().Cookies() {
			require.False(t, cookie.Name != attempt.cookie && strings.HasPrefix(cookie.Name, "oidc_state"), "the other attempt's cookie is kept")
		}
	}
}

// With a provider enabled every callback failure is a redirect to the SPA
// callback page with a reason code: to the attempt's target when SSO consumed
// the attempt, to the default callback path otherwise. The cookie of a
// well-formed state is cleared on every outcome.
func TestAuthHandlerOIDCCallbackRedirectsEveryFailureWithItsReason(t *testing.T) {
	state, cookieName := testState("attempt")
	withCookie := []*http.Cookie{{Name: cookieName, Value: "binding"}}
	outcome := func(outcome model.OIDCCallbackOutcome, target string) func(*gatewayhandlermocks.MockAuthService) {
		return func(auth *gatewayhandlermocks.MockAuthService) {
			auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", "code", state, "binding").Return(model.OIDCCallbackResult{Outcome: outcome, RedirectAfter: target}, nil)
		}
	}
	failure := func(err error) func(*gatewayhandlermocks.MockAuthService) {
		return func(auth *gatewayhandlermocks.MockAuthService) {
			auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", "code", state, "binding").Return(model.OIDCCallbackResult{}, err)
		}
	}
	for name, tc := range map[string]struct {
		query    string
		cookies  []*http.Cookie
		sso      func(*gatewayhandlermocks.MockAuthService)
		location string
		cleared  bool
	}{
		"cancelled at the provider":             {query: "error=access_denied&state=" + state, cookies: withCookie, location: defaultFailure + "access_denied", cleared: true},
		"another provider error":                {query: "error=server_error&state=" + state, cookies: withCookie, location: defaultFailure + "provider_error", cleared: true},
		"a provider error without state":        {query: "error=access_denied", location: defaultFailure + "access_denied"},
		"no binding cookie":                     {query: "code=code&state=" + state, location: defaultFailure + "state_invalid", cleared: true},
		"no code":                               {query: "state=" + state, cookies: withCookie, location: defaultFailure + "state_invalid", cleared: true},
		"no state":                              {query: "code=code", cookies: withCookie, location: defaultFailure + "state_invalid"},
		"a malformed state is not sent to SSO":  {query: "code=code&state=short", cookies: withCookie, location: defaultFailure + "state_invalid"},
		"an unknown, expired or used attempt":   {query: "code=code&state=" + state, cookies: withCookie, sso: outcome(model.OIDCCallbackInvalidAttempt, ""), location: defaultFailure + "state_invalid", cleared: true},
		"an identity the provider rejected":     {query: "code=code&state=" + state, cookies: withCookie, sso: outcome(model.OIDCCallbackIdentityRejected, callbackTarget), location: targetFailure + "identity_rejected", cleared: true},
		"a deactivated account":                 {query: "code=code&state=" + state, cookies: withCookie, sso: outcome(model.OIDCCallbackAccountDeactivated, callbackTarget), location: targetFailure + "account_deactivated", cleared: true},
		"an account conflict":                   {query: "code=code&state=" + state, cookies: withCookie, sso: outcome(model.OIDCCallbackAccountConflict, callbackTarget), location: targetFailure + "account_conflict", cleared: true},
		"an unexpected outcome":                 {query: "code=code&state=" + state, cookies: withCookie, sso: outcome("", callbackTarget), location: targetFailure + "login_failed", cleared: true},
		"an unsafe target from SSO":             {query: "code=code&state=" + state, cookies: withCookie, sso: outcome(model.OIDCCallbackIdentityRejected, "https://evil.example/auth/callback"), location: defaultFailure + "identity_rejected", cleared: true},
		"the provider or SSO unavailable":       {query: "code=code&state=" + state, cookies: withCookie, sso: failure(apperrors.ErrUnavailable), location: defaultFailure + "provider_unavailable", cleared: true},
		"the provider or SSO timing out":        {query: "code=code&state=" + state, cookies: withCookie, sso: failure(apperrors.ErrTimeout), location: defaultFailure + "provider_unavailable", cleared: true},
		"any other SSO failure":                 {query: "code=code&state=" + state, cookies: withCookie, sso: failure(apperrors.ErrInternal), location: defaultFailure + "login_failed", cleared: true},
		"a success SSO answered without tokens": {query: "code=code&state=" + state, cookies: withCookie, sso: outcome(model.OIDCCallbackSuccess, callbackTarget), location: targetFailure + "login_failed", cleared: true},
	} {
		t.Run(name, func(t *testing.T) {
			auth, router := oidcRouter(t, handler.CookieConfig{Name: "refresh_token", HTTPOnly: true})
			expectKeycloakEnabled(auth)
			if tc.sso != nil {
				tc.sso(auth)
			}
			resp := perform(router, http.MethodGet, "/callback?"+tc.query, ``, tc.cookies)
			requireLoginRedirect(t, resp, tc.location)
			if tc.cleared {
				requireCookieMaxAge(t, resp.Result(), cookieName, -1)
			}
			for _, cookie := range resp.Result().Cookies() {
				require.NotEqual(t, "refresh_token", cookie.Name, "a failed sign-in sets no session")
			}
		})
	}
}

// A callback Gateway does not forward still asks SSO whether the provider is
// enabled, and reports SSO being unreachable as the provider being unavailable.
func TestAuthHandlerOIDCCallbackChecksTheProviderBeforeRefusingLocally(t *testing.T) {
	state, cookieName := testState("attempt")
	t.Run("provider list unreachable", func(t *testing.T) {
		auth, router := oidcRouter(t, handler.CookieConfig{Name: "refresh_token"})
		auth.EXPECT().GetAuthConfig(mock.Anything).Return(model.AuthConfig{}, apperrors.ErrUnavailable)
		resp := perform(router, http.MethodGet, "/callback?error=access_denied&state="+state, ``, nil)
		requireLoginRedirect(t, resp, defaultFailure+"provider_unavailable")
		requireCookieMaxAge(t, resp.Result(), cookieName, -1)
	})
	t.Run("provider enabled but not yet available", func(t *testing.T) {
		auth, router := oidcRouter(t, handler.CookieConfig{Name: "refresh_token"})
		auth.EXPECT().GetAuthConfig(mock.Anything).Return(model.AuthConfig{OIDCProviders: []model.OIDCProvider{{Name: "keycloak", Available: false}}}, nil)
		resp := perform(router, http.MethodGet, "/callback?error=access_denied", ``, nil)
		requireLoginRedirect(t, resp, defaultFailure+"access_denied")
	})
}

func TestAuthHandlerOIDCStartRedirectsFailuresWithTheirReason(t *testing.T) {
	state, cookieName := testState("attempt")
	for name, tc := range map[string]struct {
		redirectAfter string
		result        model.OIDCLoginStartResult
		err           error
		location      string
	}{
		"an unsafe target":            {redirectAfter: "https://evil.example/auth/callback", location: defaultFailure + "login_failed"},
		"a protocol-relative target":  {redirectAfter: "//evil.example/auth/callback", location: defaultFailure + "login_failed"},
		"a target off the callback":   {redirectAfter: "http://frontend.example/wrong", location: defaultFailure + "login_failed"},
		"a script target":             {redirectAfter: "javascript:alert(1)", location: defaultFailure + "login_failed"},
		"the provider unavailable":    {redirectAfter: callbackTarget, err: apperrors.ErrUnavailable, location: targetFailure + "provider_unavailable"},
		"SSO timing out":              {redirectAfter: callbackTarget, err: apperrors.ErrTimeout, location: targetFailure + "provider_unavailable"},
		"any other SSO failure":       {redirectAfter: callbackTarget, err: apperrors.ErrInternal, location: targetFailure + "login_failed"},
		"a malformed state from SSO":  {redirectAfter: callbackTarget, result: model.OIDCLoginStartResult{AuthURL: "https://keycloak.example/auth", State: "short", StateBinding: "binding", ExpiresAt: time.Now().Add(time.Hour)}, location: targetFailure + "login_failed"},
		"an expired attempt from SSO": {redirectAfter: callbackTarget, result: model.OIDCLoginStartResult{AuthURL: "https://keycloak.example/auth", State: state, StateBinding: "binding", ExpiresAt: time.Now().Add(-time.Second)}, location: targetFailure + "login_failed"},
	} {
		t.Run(name, func(t *testing.T) {
			auth, router := oidcRouter(t, handler.CookieConfig{Name: "refresh_token"})
			if tc.redirectAfter == callbackTarget {
				auth.EXPECT().StartOIDCLogin(mock.Anything, "keycloak", callbackTarget).Return(tc.result, tc.err)
			} else {
				expectKeycloakEnabled(auth)
			}
			resp := perform(router, http.MethodGet, "/start?redirect_after="+tc.redirectAfter, ``, nil)
			requireLoginRedirect(t, resp, tc.location)
			for _, cookie := range resp.Result().Cookies() {
				require.NotEqual(t, cookieName, cookie.Name)
			}
		})
	}
}

// With OIDC off the provider routes answer not found, whether or not the
// navigation reaches SSO's start or callback.
func TestAuthHandlerOIDCRoutesAreNotFoundWhenTheProviderIsDisabled(t *testing.T) {
	state, cookieName := testState("attempt")
	auth, router := oidcRouter(t, handler.CookieConfig{Name: "refresh_token"})
	auth.EXPECT().GetAuthConfig(mock.Anything).Return(model.AuthConfig{OIDCProviders: []model.OIDCProvider{}}, nil)
	auth.EXPECT().StartOIDCLogin(mock.Anything, "keycloak", callbackTarget).Return(model.OIDCLoginStartResult{}, apperrors.ErrNotFound)
	auth.EXPECT().CompleteOIDCCallback(mock.Anything, "keycloak", "code", state, "binding").Return(model.OIDCCallbackResult{}, apperrors.ErrNotFound)

	require.Equal(t, http.StatusNotFound, perform(router, http.MethodGet, "/start?redirect_after="+callbackTarget, ``, nil).Code)
	require.Equal(t, http.StatusNotFound, perform(router, http.MethodGet, "/start?redirect_after=https://evil.example/auth/callback", ``, nil).Code)
	require.Equal(t, http.StatusNotFound, perform(router, http.MethodGet, "/callback?error=access_denied", ``, nil).Code)
	require.Equal(t, http.StatusNotFound, perform(router, http.MethodGet, "/callback?code=code&state=short", ``, nil).Code)
	resp := perform(router, http.MethodGet, "/callback?code=code&state="+state, ``, []*http.Cookie{{Name: cookieName, Value: "binding"}})
	require.Equal(t, http.StatusNotFound, resp.Code)
	requireCookieMaxAge(t, resp.Result(), cookieName, -1)
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
				h := handler.NewAuthHandler(auth, anyAudit(t), handler.CookieConfig{Name: "refresh_token", Path: "/api/v1/auth", Domain: "example.com", HTTPOnly: true, Secure: true}, testRedirectConfig())
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
					auth.EXPECT().Logout(mock.Anything, "secret", "http://frontend.example/login").Return("", err)
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
				h := handler.NewAuthHandler(auth, anyAudit(t), handler.CookieConfig{}, testRedirectConfig())
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
			h := handler.NewAuthHandler(auth, anyAudit(t), handler.CookieConfig{}, testRedirectConfig())
			router := gin.New()
			router.POST("/logout", h.Logout)
			if test.allowed {
				auth.EXPECT().Logout(mock.Anything, "", "http://frontend.example/login").Return("", nil)
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

// anyAudit accepts every audit event, for tests that do not observe audit.
func anyAudit(t *testing.T) *gatewayhandlermocks.MockAuditRecorder {
	t.Helper()
	recorder, _ := expectAudit(t, nil)
	return recorder
}
