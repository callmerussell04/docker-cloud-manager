package config_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/envtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const secret = "s3cret-value"

func requiredEnv() map[string]string {
	return map[string]string{
		"SSO_GRPC_TARGET":       "sso:1",
		"CORE_GRPC_TARGET":      "core:2",
		"CORE_HTTP_TARGET":      "http://core:3",
		"GATEWAY_SERVICE_TOKEN": secret,
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load(envtest.Lookup(requiredEnv()))

	require.NoError(t, err)
	assert.Equal(t, 8081, cfg.Port)
	assert.Equal(t, "sso:1", cfg.SSOTarget)
	assert.Equal(t, secret, cfg.ServiceToken)
	assert.Equal(t, config.HTTPServer{
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       360 * time.Second,
		WriteTimeout:      360 * time.Second,
		IdleTimeout:       120 * time.Second,
		ShutdownTimeout:   15 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}, cfg.HTTP)
	assert.Equal(t, "refresh_token", cfg.Cookie.Name)
	assert.Equal(t, "/", cfg.Cookie.Path)
	assert.Empty(t, cfg.Cookie.Domain)
	assert.False(t, cfg.Cookie.Secure)
	assert.True(t, cfg.Cookie.HTTPOnly)
	assert.Equal(t, http.SameSiteLaxMode, cfg.Cookie.SameSite)
	assert.Equal(t, config.Proxy{
		DialTimeout:           10 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
	}, cfg.Proxy)
	assert.Equal(t, config.GRPC{RequestTimeout: 300 * time.Second, MinConnectTimeout: 5 * time.Second}, cfg.GRPC)
	assert.Equal(t, 3*time.Second, cfg.Readiness.Timeout)
	assert.Equal(t, 60*time.Second, cfg.TelemetryTicketTTL)
	assert.Equal(t, config.DefaultRouter().CORSAllowedOrigins, cfg.Router.CORSAllowedOrigins)
	assert.Equal(t, config.DefaultRouter().AuthRateLimit, cfg.Router.AuthRateLimit)
	assert.Empty(t, cfg.Router.TrustedProxies, "no proxy is trusted by default")
}

func TestLoadOverrides(t *testing.T) {
	values := requiredEnv()
	values["CORS_ALLOWED_ORIGINS"] = "https://a.test, https://b.test"
	values["GATEWAY_AUTH_RATE_LIMIT_REQUESTS"] = "9"
	values["GATEWAY_AUTH_RATE_LIMIT_WINDOW_SECONDS"] = "-1"
	values["COOKIE_SAMESITE"] = " Strict "
	values["COOKIE_NAME"] = ""
	values["APP_ENV"] = "production"

	cfg, err := config.Load(envtest.Lookup(values))

	require.NoError(t, err)
	assert.Equal(t, []string{"https://a.test", "https://b.test"}, cfg.Router.CORSAllowedOrigins)
	assert.Equal(t, cfg.Router.CORSAllowedOrigins, cfg.Router.AuthRedirect.AllowedOrigins,
		"redirect origins follow CORS origins unless set")
	assert.Equal(t, 9, cfg.Router.AuthRateLimit.Requests)
	assert.Equal(t, config.DefaultRouter().AuthRateLimit.Window, cfg.Router.AuthRateLimit.Window)
	assert.Equal(t, http.SameSiteStrictMode, cfg.Cookie.SameSite)
	assert.Equal(t, "refresh_token", cfg.Cookie.Name, "an empty string keeps its default")
	assert.Equal(t, "production", cfg.AppEnv)
}

func TestLoadCookieSameSite(t *testing.T) {
	for raw, want := range map[string]http.SameSite{
		"":       http.SameSiteLaxMode,
		"lax":    http.SameSiteLaxMode,
		"strict": http.SameSiteStrictMode,
		"NONE":   http.SameSiteNoneMode,
		"bogus":  http.SameSiteLaxMode,
	} {
		values := requiredEnv()
		values["COOKIE_SAMESITE"] = raw
		cfg, err := config.Load(envtest.Lookup(values))
		require.NoError(t, err)
		assert.Equal(t, want, cfg.Cookie.SameSite, raw)
	}
}

func TestLoadKeepsEmptyTrustedProxyEntriesForRouterValidation(t *testing.T) {
	values := requiredEnv()
	values["GATEWAY_TRUSTED_PROXIES"] = "10.0.0.1, ,10.0.0.2"

	cfg, err := config.Load(envtest.Lookup(values))

	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.1", "", "10.0.0.2"}, cfg.Router.TrustedProxies)
}

func TestLoadReportsRequiredSettingsInOrderWithoutValues(t *testing.T) {
	envtest.AssertRequired(t, config.Load, requiredEnv(), []string{
		"SSO_GRPC_TARGET",
		"CORE_GRPC_TARGET",
		"CORE_HTTP_TARGET",
		"GATEWAY_SERVICE_TOKEN",
	})
}

func TestInsecureCookieInProduction(t *testing.T) {
	tests := []struct {
		appEnv string
		secure bool
		want   bool
	}{
		{"production", false, true},
		{"production", true, false},
		{"", false, false},
		{"dev", false, false},
	}
	for _, tt := range tests {
		cfg := config.Bootstrap{AppEnv: tt.appEnv, Cookie: config.Cookie{Secure: tt.secure}}
		assert.Equal(t, tt.want, cfg.InsecureCookieInProduction(), "%q secure=%v", tt.appEnv, tt.secure)
	}
}
