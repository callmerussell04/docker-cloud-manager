package main

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/app"
	gatewayhttp "github.com/callmerussell04/docker-cloud-manager/internal/gateway/http"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/handler"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/http/middleware"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
)

func main() {
	logger := logging.NewLogger("gateway", logging.ConfigFromEnv())
	slog.SetDefault(logger)

	cfg := app.Config{
		Port:                intEnv("GATEWAY_PORT", 8081),
		SSOTarget:           requiredEnv(logger, "SSO_GRPC_TARGET"),
		CoreTarget:          requiredEnv(logger, "CORE_GRPC_TARGET"),
		CoreHTTPTarget:      requiredEnv(logger, "CORE_HTTP_TARGET"),
		TelemetryHTTPTarget: requiredEnv(logger, "TELEMETRY_HTTP_TARGET"),
		InternalToken:       requiredEnv(logger, "INTERNAL_SERVICE_TOKEN"),
		HTTP: app.HTTPServerConfig{
			ReadHeaderTimeout: durationSecondsEnv("GATEWAY_READ_HEADER_TIMEOUT_SECONDS", 5*time.Second),
			ReadTimeout:       durationSecondsEnv("GATEWAY_READ_TIMEOUT_SECONDS", 360*time.Second),
			WriteTimeout:      durationSecondsEnv("GATEWAY_WRITE_TIMEOUT_SECONDS", 360*time.Second),
			IdleTimeout:       durationSecondsEnv("GATEWAY_IDLE_TIMEOUT_SECONDS", 120*time.Second),
			ShutdownTimeout:   durationSecondsEnv("GATEWAY_SHUTDOWN_TIMEOUT_SECONDS", 15*time.Second),
			MaxHeaderBytes:    intEnv("GATEWAY_MAX_HEADER_BYTES", 1<<20),
		},
		Router: routerConfigFromEnv(),
		Cookie: handler.CookieConfig{
			Name:     getenv("COOKIE_NAME", "refresh_token"),
			Path:     getenv("COOKIE_PATH", "/"),
			Domain:   os.Getenv("COOKIE_DOMAIN"),
			MaxAge:   intEnv("COOKIE_MAX_AGE_SECONDS", 30*24*3600),
			Secure:   boolEnv("COOKIE_SECURE", false),
			HTTPOnly: boolEnv("COOKIE_HTTP_ONLY", true),
			SameSite: sameSiteEnv("COOKIE_SAMESITE", http.SameSiteLaxMode),
		},
		Proxy: app.ProxyConfig{
			DialTimeout:           durationSecondsEnv("GATEWAY_PROXY_DIAL_TIMEOUT_SECONDS", 10*time.Second),
			TLSHandshakeTimeout:   durationSecondsEnv("GATEWAY_PROXY_TLS_HANDSHAKE_TIMEOUT_SECONDS", 10*time.Second),
			ResponseHeaderTimeout: durationSecondsEnv("GATEWAY_PROXY_RESPONSE_HEADER_TIMEOUT_SECONDS", 60*time.Second),
			IdleConnTimeout:       durationSecondsEnv("GATEWAY_PROXY_IDLE_CONN_TIMEOUT_SECONDS", 90*time.Second),
			ExpectContinueTimeout: durationSecondsEnv("GATEWAY_PROXY_EXPECT_CONTINUE_TIMEOUT_SECONDS", time.Second),
			MaxIdleConns:          intEnv("GATEWAY_PROXY_MAX_IDLE_CONNS", 100),
			MaxIdleConnsPerHost:   intEnv("GATEWAY_PROXY_MAX_IDLE_CONNS_PER_HOST", 20),
		},
		GRPC: app.GRPCConfig{
			RequestTimeout:    durationSecondsEnv("GATEWAY_GRPC_REQUEST_TIMEOUT_SECONDS", 300*time.Second),
			MinConnectTimeout: durationSecondsEnv("GATEWAY_GRPC_MIN_CONNECT_TIMEOUT_SECONDS", 5*time.Second),
			TLS: app.GRPCTLSConfig{
				Enabled:    boolEnv("GATEWAY_GRPC_TLS_ENABLED", false),
				CAFile:     os.Getenv("GATEWAY_GRPC_TLS_CA_FILE"),
				CertFile:   os.Getenv("GATEWAY_GRPC_TLS_CERT_FILE"),
				KeyFile:    os.Getenv("GATEWAY_GRPC_TLS_KEY_FILE"),
				ServerName: os.Getenv("GATEWAY_GRPC_TLS_SERVER_NAME"),
			},
		},
		Readiness: app.ReadinessConfig{
			Timeout: durationSecondsEnv("GATEWAY_READINESS_TIMEOUT_SECONDS", 3*time.Second),
		},
		TelemetryTicketTTL: durationSecondsEnv("GATEWAY_TELEMETRY_TICKET_TTL_SECONDS", 60*time.Second),
	}

	if os.Getenv("APP_ENV") == "production" && !cfg.Cookie.Secure {
		logger.Warn("refresh token cookie is not secure in production environment")
	}

	application, err := app.New(cfg, logger)
	if err != nil {
		fatal(logger, "failed to initialize gateway app", "error", err)
	}

	if err := application.Run(); err != nil {
		fatal(logger, "gateway server failed", "error", err)
	}
}

func routerConfigFromEnv() gatewayhttp.Config {
	cfg := gatewayhttp.DefaultConfig()
	cfg.CORSAllowedOrigins = csvEnv("CORS_ALLOWED_ORIGINS", cfg.CORSAllowedOrigins)
	cfg.AuthRedirect.AllowedOrigins = csvEnv("GATEWAY_AUTH_REDIRECT_ALLOWED_ORIGINS", cfg.CORSAllowedOrigins)
	cfg.AuthRedirect.CallbackPath = getenv("GATEWAY_AUTH_CALLBACK_PATH", cfg.AuthRedirect.CallbackPath)
	cfg.TrustedProxies = csvEnv("GATEWAY_TRUSTED_PROXIES", nil)
	cfg.MaxJSONBodyBytes = int64Env("GATEWAY_MAX_JSON_BODY_BYTES", cfg.MaxJSONBodyBytes)
	cfg.AuthRateLimit = middleware.RateLimitConfig{
		Requests: intEnv("GATEWAY_AUTH_RATE_LIMIT_REQUESTS", cfg.AuthRateLimit.Requests),
		Window:   durationSecondsEnv("GATEWAY_AUTH_RATE_LIMIT_WINDOW_SECONDS", cfg.AuthRateLimit.Window),
	}
	cfg.UploadRateLimit = middleware.RateLimitConfig{
		Requests: intEnv("GATEWAY_UPLOAD_RATE_LIMIT_REQUESTS", cfg.UploadRateLimit.Requests),
		Window:   durationSecondsEnv("GATEWAY_UPLOAD_RATE_LIMIT_WINDOW_SECONDS", cfg.UploadRateLimit.Window),
	}
	return cfg
}

func requiredEnv(logger *slog.Logger, key string) string {
	value := os.Getenv(key)
	if value == "" {
		fatal(logger, "required environment variable is not set", "env_var", key)
	}
	return value
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func intEnv(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func int64Env(key string, fallback int64) int64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

func boolEnv(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func durationSecondsEnv(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return time.Duration(value) * time.Second
}

func csvEnv(key string, fallback []string) []string {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func sameSiteEnv(key string, fallback http.SameSite) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	case "lax", "":
		return fallback
	default:
		return fallback
	}
}

func fatal(logger *slog.Logger, msg string, args ...any) {
	logger.Error(msg, args...)
	os.Exit(1)
}
