package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

func TestAuthProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAuthHandler(&authServiceFake{
		authConfig: model.AuthConfig{
			LocalLoginEnabled:    true,
			LocalRegisterEnabled: false,
			OIDCProviders:        []model.OIDCProvider{{Name: "keycloak"}},
		},
	}, CookieConfig{Name: "refresh_token"}, testRedirectConfig())

	router := gin.New()
	router.GET("/providers", handler.Providers)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/providers", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, expected := range []string{`"local_login_enabled":true`, `"local_register_enabled":false`, `"name":"keycloak"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("response missing %s: %s", expected, body)
		}
	}
}

func TestStartKeycloakLoginRedirects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAuthHandler(&authServiceFake{authStart: model.OIDCLoginStartResult{AuthURL: "https://keycloak.example/auth", StateBinding: "state-binding"}}, CookieConfig{Name: "refresh_token"}, testRedirectConfig())

	router := gin.New()
	router.GET("/start", handler.StartKeycloakLogin)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/start?redirect_after=http://frontend.example/auth/callback", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if location := rec.Header().Get("Location"); location != "https://keycloak.example/auth" {
		t.Fatalf("Location = %q", location)
	}
	if cookie := findCookie(rec.Result(), oidcStateCookieName); cookie == nil || cookie.Value != "state-binding" || !cookie.HttpOnly {
		t.Fatalf("oidc state cookie = %+v", cookie)
	}
}

func TestStartKeycloakLoginAcceptsRelativeCallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := &authServiceFake{authStart: model.OIDCLoginStartResult{AuthURL: "https://keycloak.example/auth", StateBinding: "state-binding"}}
	handler := NewAuthHandler(auth, CookieConfig{Name: "refresh_token"}, testRedirectConfig())

	router := gin.New()
	router.GET("/start", handler.StartKeycloakLogin)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/start?redirect_after=/auth/callback", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if auth.redirectAfter != "/auth/callback" {
		t.Fatalf("redirectAfter = %q", auth.redirectAfter)
	}
}

func TestStartKeycloakLoginRejectsUnsafeRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []string{
		"https://evil.example/auth/callback",
		"//evil.example/auth/callback",
		"http://frontend.example/wrong",
		"javascript:alert(1)",
	}

	for _, redirectAfter := range tests {
		t.Run(redirectAfter, func(t *testing.T) {
			handler := NewAuthHandler(&authServiceFake{authStart: model.OIDCLoginStartResult{AuthURL: "https://keycloak.example/auth", StateBinding: "state-binding"}}, CookieConfig{Name: "refresh_token"}, testRedirectConfig())
			router := gin.New()
			router.GET("/start", handler.StartKeycloakLogin)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/start?redirect_after="+redirectAfter, nil)
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestCompleteKeycloakCallbackSetsCookieAndRedirects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := &authServiceFake{
		callback: model.OIDCCallbackResult{
			Tokens:        model.Tokens{AccessToken: "access-token", RefreshToken: "refresh-token"},
			RedirectAfter: "http://frontend.example/auth/callback",
		},
	}
	handler := NewAuthHandler(auth, CookieConfig{Name: "refresh_token", Path: "/", MaxAge: 3600, HTTPOnly: true}, testRedirectConfig())

	router := gin.New()
	router.GET("/callback", handler.CompleteKeycloakCallback)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/callback?code=code&state=state", nil)
	req.AddCookie(&http.Cookie{Name: oidcStateCookieName, Value: "state-binding"})
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if location := rec.Header().Get("Location"); location != "http://frontend.example/auth/callback?login=success" {
		t.Fatalf("Location = %q", location)
	}
	if strings.Contains(rec.Header().Get("Location"), "access-token") {
		t.Fatalf("Location leaked access token: %q", rec.Header().Get("Location"))
	}
	if auth.callbackStateBinding != "state-binding" {
		t.Fatalf("stateBinding = %q, want state-binding", auth.callbackStateBinding)
	}
	if cookie := findCookie(rec.Result(), "refresh_token"); cookie == nil || cookie.Value != "refresh-token" || !cookie.HttpOnly {
		t.Fatalf("refresh cookie = %+v", cookie)
	}
	if cookie := findCookie(rec.Result(), oidcStateCookieName); cookie == nil || cookie.MaxAge != -1 {
		t.Fatalf("oidc state clear cookie = %+v", cookie)
	}
}

func TestCompleteKeycloakCallbackRejectsMissingStateCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := &authServiceFake{
		callback: model.OIDCCallbackResult{
			Tokens:        model.Tokens{AccessToken: "access-token", RefreshToken: "refresh-token"},
			RedirectAfter: "http://frontend.example/auth/callback",
		},
	}
	handler := NewAuthHandler(auth, CookieConfig{Name: "refresh_token", Path: "/", MaxAge: 3600, HTTPOnly: true}, testRedirectConfig())

	router := gin.New()
	router.GET("/callback", handler.CompleteKeycloakCallback)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/callback?code=code&state=state", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if auth.callbackCalled {
		t.Fatal("CompleteOIDCCallback was called without oidc state cookie")
	}
	if cookie := findCookie(rec.Result(), "refresh_token"); cookie != nil {
		t.Fatalf("unexpected refresh cookie = %+v", cookie)
	}
}

type authServiceFake struct {
	authConfig           model.AuthConfig
	authStart            model.OIDCLoginStartResult
	callback             model.OIDCCallbackResult
	redirectAfter        string
	callbackStateBinding string
	callbackCalled       bool
}

func (f *authServiceFake) Register(ctx context.Context, username, email, password string) (string, error) {
	return "", nil
}

func (f *authServiceFake) Login(ctx context.Context, username, password string) (model.Tokens, error) {
	return model.Tokens{}, nil
}

func (f *authServiceFake) Refresh(ctx context.Context, refreshToken string) (model.Tokens, error) {
	return model.Tokens{}, nil
}

func (f *authServiceFake) GetAuthConfig(ctx context.Context) (model.AuthConfig, error) {
	return f.authConfig, nil
}

func (f *authServiceFake) StartOIDCLogin(ctx context.Context, provider, redirectAfter string) (model.OIDCLoginStartResult, error) {
	f.redirectAfter = redirectAfter
	return f.authStart, nil
}

func (f *authServiceFake) CompleteOIDCCallback(ctx context.Context, provider, code, state, stateBinding string) (model.OIDCCallbackResult, error) {
	f.callbackCalled = true
	f.callbackStateBinding = stateBinding
	return f.callback, nil
}

func testRedirectConfig() AuthRedirectConfig {
	return AuthRedirectConfig{
		AllowedOrigins: []string{"http://frontend.example"},
		CallbackPath:   "/auth/callback",
	}
}

func findCookie(resp *http.Response, name string) *http.Cookie {
	for _, cookie := range resp.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
