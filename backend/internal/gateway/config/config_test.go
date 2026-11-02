package config_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/config"
	gatewayhttp "github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/http"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const secret = "s3cret-value"

func env(values map[string]string) envconfig.Lookup {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func requiredEnv() map[string]string {
	return map[string]string{
		"SSO_GRPC_TARGET":        "sso:1",
		"CORE_GRPC_TARGET":       "core:2",
		"CORE_HTTP_TARGET":       "http://core:3",
		"TELEMETRY_HTTP_TARGET":  "http://telemetry:4",
		"INTERNAL_SERVICE_TOKEN": secret,
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load(env(requiredEnv()))

	require.NoError(t, err)
	assert.Equal(t, 8081, cfg.Port)
	assert.Equal(t, "sso:1", cfg.SSOTarget)
	assert.Equal(t, secret, cfg.InternalToken)
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
	assert.Equal(t, gatewayhttp.DefaultConfig().CORSAllowedOrigins, cfg.Router.CORSAllowedOrigins)
	assert.Equal(t, gatewayhttp.DefaultConfig().AuthRateLimit, cfg.Router.AuthRateLimit)
	assert.Empty(t, cfg.Router.TrustedProxies, "no proxy is trusted by default")
}

func TestLoadOverrides(t *testing.T) {
	values := requiredEnv()
	values["CORS_ALLOWED_ORIGINS"] = "https://a.test, https://b.test"
	values["GATEWAY_AUTH_RATE_LIMIT_REQUESTS"] = "9"
	values["GATEWAY_AUTH_RATE_LIMIT_WINDOW_SECONDS"] = "-1"
	values["COOKIE_SAMESITE"] = " Strict "
	values["COOKIE_NAME"] = ""
	values["GATEWAY_GRPC_TLS_ENABLED"] = "true"
	values["GATEWAY_GRPC_TLS_CA_FILE"] = "/ca.pem"
	values["APP_ENV"] = "production"

	cfg, err := config.Load(env(values))

	require.NoError(t, err)
	assert.Equal(t, []string{"https://a.test", "https://b.test"}, cfg.Router.CORSAllowedOrigins)
	assert.Equal(t, cfg.Router.CORSAllowedOrigins, cfg.Router.AuthRedirect.AllowedOrigins,
		"redirect origins follow CORS origins unless set")
	assert.Equal(t, 9, cfg.Router.AuthRateLimit.Requests)
	assert.Equal(t, gatewayhttp.DefaultConfig().AuthRateLimit.Window, cfg.Router.AuthRateLimit.Window)
	assert.Equal(t, http.SameSiteStrictMode, cfg.Cookie.SameSite)
	assert.Equal(t, "refresh_token", cfg.Cookie.Name, "an empty string keeps its default")
	assert.Equal(t, config.GRPCTLS{Enabled: true, CAFile: "/ca.pem"}, cfg.GRPC.TLS)
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
		cfg, err := config.Load(env(values))
		require.NoError(t, err)
		assert.Equal(t, want, cfg.Cookie.SameSite, raw)
	}
}

func TestLoadKeepsEmptyTrustedProxyEntriesForRouterValidation(t *testing.T) {
	values := requiredEnv()
	values["GATEWAY_TRUSTED_PROXIES"] = "10.0.0.1, ,10.0.0.2"

	cfg, err := config.Load(env(values))

	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.1", "", "10.0.0.2"}, cfg.Router.TrustedProxies)
}

func TestLoadRequiresSettingsWithoutEchoingValues(t *testing.T) {
	for key := range requiredEnv() {
		t.Run(key, func(t *testing.T) {
			values := requiredEnv()
			values[key] = ""

			_, err := config.Load(env(values))

			var missing *envconfig.MissingError
			require.ErrorAs(t, err, &missing)
			assert.Equal(t, key, missing.Key)
			assert.NotContains(t, err.Error(), secret)
		})
	}
}
