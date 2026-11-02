// Package config loads Gateway's bootstrap settings.
package config

import (
	"net/http"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
)

// Bootstrap is Gateway's process-lifetime configuration.
type Bootstrap struct {
	Port                int
	SSOTarget           string
	CoreTarget          string
	CoreHTTPTarget      string
	TelemetryHTTPTarget string
	InternalToken       string
	// AppEnv is the raw APP_ENV value; unlike the logging default it is empty when unset.
	AppEnv string

	HTTP      HTTPServer
	Router    Router
	Cookie    Cookie
	Proxy     Proxy
	GRPC      GRPC
	Readiness Readiness

	TelemetryTicketTTL time.Duration
}

// InsecureCookieInProduction reports a refresh cookie sent without Secure in production.
func (b Bootstrap) InsecureCookieInProduction() bool {
	return b.AppEnv == "production" && !b.Cookie.Secure
}

// Router configures Gateway's HTTP router.
type Router struct {
	CORSAllowedOrigins []string
	TrustedProxies     []string
	MaxJSONBodyBytes   int64
	AuthRateLimit      RateLimit
	UploadRateLimit    RateLimit
	AuthRedirect       AuthRedirect
}

// RateLimit allows Requests per client within Window.
type RateLimit struct {
	Requests int
	Window   time.Duration
}

// AuthRedirect configures where the browser may return after external login.
type AuthRedirect struct {
	AllowedOrigins       []string
	CookieAllowedOrigins []string
	CallbackPath         string
}

// Cookie configures the refresh token cookie.
type Cookie struct {
	Name     string
	Path     string
	Domain   string
	Secure   bool
	HTTPOnly bool
	SameSite http.SameSite
}

// DefaultRouter returns the router settings used when no override is set.
func DefaultRouter() Router {
	return Router{
		CORSAllowedOrigins: []string{"http://localhost", "http://localhost:3000", "http://localhost:5173"},
		MaxJSONBodyBytes:   1 << 20,
		AuthRateLimit:      RateLimit{Requests: 20, Window: time.Minute},
		UploadRateLimit:    RateLimit{Requests: 10, Window: time.Minute},
		AuthRedirect: AuthRedirect{
			AllowedOrigins: []string{"http://localhost", "http://localhost:3000", "http://localhost:5173"},
			CallbackPath:   "/auth/callback",
		},
	}
}

// HTTPServer configures Gateway's public HTTP server.
type HTTPServer struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
}

// Proxy configures the transport for proxied Core and Telemetry requests.
type Proxy struct {
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	IdleConnTimeout       time.Duration
	ExpectContinueTimeout time.Duration
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
}

// GRPC configures the client connections to SSO and Core.
type GRPC struct {
	RequestTimeout    time.Duration
	MinConnectTimeout time.Duration
	TLS               GRPCTLS
}

// GRPCTLS configures optional TLS or mTLS on those connections.
type GRPCTLS struct {
	Enabled    bool
	CAFile     string
	CertFile   string
	KeyFile    string
	ServerName string
}

// Readiness bounds the upstream checks behind /health/ready.
type Readiness struct {
	Timeout time.Duration
}

// Load reads Gateway's settings. Its error names the first missing required setting.
func Load(lookup envconfig.Lookup) (Bootstrap, error) {
	required := envconfig.NewRequired(lookup)
	cfg := Bootstrap{
		Port:                envconfig.Int(lookup, "GATEWAY_PORT", 8081),
		SSOTarget:           required.Get("SSO_GRPC_TARGET"),
		CoreTarget:          required.Get("CORE_GRPC_TARGET"),
		CoreHTTPTarget:      required.Get("CORE_HTTP_TARGET"),
		TelemetryHTTPTarget: required.Get("TELEMETRY_HTTP_TARGET"),
		InternalToken:       required.Get("INTERNAL_SERVICE_TOKEN"),
		AppEnv:              envconfig.String(lookup, "APP_ENV", ""),
		HTTP: HTTPServer{
			ReadHeaderTimeout: envconfig.DurationSeconds(lookup, "GATEWAY_READ_HEADER_TIMEOUT_SECONDS", 5*time.Second),
			ReadTimeout:       envconfig.DurationSeconds(lookup, "GATEWAY_READ_TIMEOUT_SECONDS", 360*time.Second),
			WriteTimeout:      envconfig.DurationSeconds(lookup, "GATEWAY_WRITE_TIMEOUT_SECONDS", 360*time.Second),
			IdleTimeout:       envconfig.DurationSeconds(lookup, "GATEWAY_IDLE_TIMEOUT_SECONDS", 120*time.Second),
			ShutdownTimeout:   envconfig.DurationSeconds(lookup, "GATEWAY_SHUTDOWN_TIMEOUT_SECONDS", 15*time.Second),
			MaxHeaderBytes:    envconfig.Int(lookup, "GATEWAY_MAX_HEADER_BYTES", 1<<20),
		},
		Router: loadRouter(lookup),
		Cookie: Cookie{
			Name:     envconfig.NonEmpty(lookup, "COOKIE_NAME", "refresh_token"),
			Path:     envconfig.NonEmpty(lookup, "COOKIE_PATH", "/"),
			Domain:   envconfig.String(lookup, "COOKIE_DOMAIN", ""),
			Secure:   envconfig.Bool(lookup, "COOKIE_SECURE", false),
			HTTPOnly: envconfig.Bool(lookup, "COOKIE_HTTP_ONLY", true),
			SameSite: loadSameSite(lookup),
		},
		Proxy: Proxy{
			DialTimeout:           envconfig.DurationSeconds(lookup, "GATEWAY_PROXY_DIAL_TIMEOUT_SECONDS", 10*time.Second),
			TLSHandshakeTimeout:   envconfig.DurationSeconds(lookup, "GATEWAY_PROXY_TLS_HANDSHAKE_TIMEOUT_SECONDS", 10*time.Second),
			ResponseHeaderTimeout: envconfig.DurationSeconds(lookup, "GATEWAY_PROXY_RESPONSE_HEADER_TIMEOUT_SECONDS", 60*time.Second),
			IdleConnTimeout:       envconfig.DurationSeconds(lookup, "GATEWAY_PROXY_IDLE_CONN_TIMEOUT_SECONDS", 90*time.Second),
			ExpectContinueTimeout: envconfig.DurationSeconds(lookup, "GATEWAY_PROXY_EXPECT_CONTINUE_TIMEOUT_SECONDS", time.Second),
			MaxIdleConns:          envconfig.Int(lookup, "GATEWAY_PROXY_MAX_IDLE_CONNS", 100),
			MaxIdleConnsPerHost:   envconfig.Int(lookup, "GATEWAY_PROXY_MAX_IDLE_CONNS_PER_HOST", 20),
		},
		GRPC: GRPC{
			RequestTimeout:    envconfig.DurationSeconds(lookup, "GATEWAY_GRPC_REQUEST_TIMEOUT_SECONDS", 300*time.Second),
			MinConnectTimeout: envconfig.DurationSeconds(lookup, "GATEWAY_GRPC_MIN_CONNECT_TIMEOUT_SECONDS", 5*time.Second),
			TLS: GRPCTLS{
				Enabled:    envconfig.Bool(lookup, "GATEWAY_GRPC_TLS_ENABLED", false),
				CAFile:     envconfig.String(lookup, "GATEWAY_GRPC_TLS_CA_FILE", ""),
				CertFile:   envconfig.String(lookup, "GATEWAY_GRPC_TLS_CERT_FILE", ""),
				KeyFile:    envconfig.String(lookup, "GATEWAY_GRPC_TLS_KEY_FILE", ""),
				ServerName: envconfig.String(lookup, "GATEWAY_GRPC_TLS_SERVER_NAME", ""),
			},
		},
		Readiness: Readiness{
			Timeout: envconfig.DurationSeconds(lookup, "GATEWAY_READINESS_TIMEOUT_SECONDS", 3*time.Second),
		},
		TelemetryTicketTTL: envconfig.DurationSeconds(lookup, "GATEWAY_TELEMETRY_TICKET_TTL_SECONDS", 60*time.Second),
	}
	if err := required.Err(); err != nil {
		return Bootstrap{}, err
	}
	return cfg, nil
}

func loadRouter(lookup envconfig.Lookup) Router {
	cfg := DefaultRouter()
	cfg.CORSAllowedOrigins = envconfig.CSV(lookup, "CORS_ALLOWED_ORIGINS", cfg.CORSAllowedOrigins)
	cfg.AuthRedirect.AllowedOrigins = envconfig.CSV(lookup, "GATEWAY_AUTH_REDIRECT_ALLOWED_ORIGINS", cfg.CORSAllowedOrigins)
	cfg.AuthRedirect.CallbackPath = envconfig.NonEmpty(lookup, "GATEWAY_AUTH_CALLBACK_PATH", cfg.AuthRedirect.CallbackPath)
	// Preserve empty CSV entries so malformed proxy lists fail router validation.
	if raw, _ := lookup("GATEWAY_TRUSTED_PROXIES"); raw != "" {
		cfg.TrustedProxies = strings.Split(raw, ",")
		for i := range cfg.TrustedProxies {
			cfg.TrustedProxies[i] = strings.TrimSpace(cfg.TrustedProxies[i])
		}
	}
	cfg.MaxJSONBodyBytes = envconfig.Int64(lookup, "GATEWAY_MAX_JSON_BODY_BYTES", cfg.MaxJSONBodyBytes)
	cfg.AuthRateLimit = RateLimit{
		Requests: envconfig.Int(lookup, "GATEWAY_AUTH_RATE_LIMIT_REQUESTS", cfg.AuthRateLimit.Requests),
		Window:   envconfig.DurationSeconds(lookup, "GATEWAY_AUTH_RATE_LIMIT_WINDOW_SECONDS", cfg.AuthRateLimit.Window),
	}
	cfg.UploadRateLimit = RateLimit{
		Requests: envconfig.Int(lookup, "GATEWAY_UPLOAD_RATE_LIMIT_REQUESTS", cfg.UploadRateLimit.Requests),
		Window:   envconfig.DurationSeconds(lookup, "GATEWAY_UPLOAD_RATE_LIMIT_WINDOW_SECONDS", cfg.UploadRateLimit.Window),
	}
	return cfg
}

func loadSameSite(lookup envconfig.Lookup) http.SameSite {
	raw, _ := lookup("COOKIE_SAMESITE")
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}
