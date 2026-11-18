// Package config loads SSO's bootstrap settings.
package config

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
)

// Bootstrap is SSO's process-lifetime configuration.
type Bootstrap struct {
	Port      int
	DBURL     string
	JWTSecret string
	// GatewayToken and CoreToken identify the callers SSO accepts.
	GatewayToken   string
	CoreToken      string
	AccessTTL      time.Duration
	SessionTTL     time.Duration
	SessionIdleTTL time.Duration
	BootstrapAdmin BootstrapAdmin
	Auth           Auth
	OIDC           OIDC
}

// BootstrapAdmin is the administrator created at startup.
type BootstrapAdmin struct {
	Username string
	Email    string
	Password string
}

// Auth switches local login and registration.
type Auth struct {
	LocalLoginEnabled    bool
	LocalRegisterEnabled bool
}

// OIDC configures the external authorization-code provider.
type OIDC struct {
	Enabled      bool
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AdminGroups  []string
}

// ErrNoSignInMethod refuses a configuration in which nobody can sign in.
var ErrNoSignInMethod = errors.New("SSO_LOCAL_LOGIN_ENABLED and SSO_OIDC_ENABLED are both false: no sign-in method is enabled")

// Load reads SSO's settings. Optional settings other than the port and the OIDC
// client secret are trimmed. Its error names the first missing required setting.
// With OIDC on, the issuer URL, client ID, client secret and redirect URL are
// required, and both URLs must be absolute http or https URLs.
func Load(lookup envconfig.Lookup) (Bootstrap, error) {
	required := envconfig.NewRequired(lookup)
	trimmed := envconfig.Trimmed(lookup)
	cfg := Bootstrap{
		Port:           envconfig.Int(lookup, "SSO_GRPC_PORT", 50051),
		DBURL:          required.Get("DATABASE_URL"),
		JWTSecret:      required.Get("JWT_SECRET"),
		GatewayToken:   required.Get("GATEWAY_SERVICE_TOKEN"),
		CoreToken:      required.Get("CORE_SERVICE_TOKEN"),
		AccessTTL:      15 * time.Minute,
		SessionTTL:     30 * 24 * time.Hour,
		SessionIdleTTL: 7 * 24 * time.Hour,
		BootstrapAdmin: BootstrapAdmin{
			Username: envconfig.NonEmpty(trimmed, "SSO_ADMIN_USERNAME", "admin"),
			Email:    envconfig.NonEmpty(trimmed, "SSO_ADMIN_EMAIL", "admin@example.local"),
			Password: required.Get("SSO_ADMIN_PASSWORD"),
		},
		Auth: Auth{
			LocalLoginEnabled:    envconfig.Bool(trimmed, "SSO_LOCAL_LOGIN_ENABLED", true),
			LocalRegisterEnabled: envconfig.Bool(trimmed, "SSO_LOCAL_REGISTER_ENABLED", true),
		},
		OIDC: OIDC{
			Enabled:      envconfig.Bool(trimmed, "SSO_OIDC_ENABLED", false),
			IssuerURL:    envconfig.NonEmpty(trimmed, "SSO_OIDC_ISSUER_URL", ""),
			ClientID:     envconfig.NonEmpty(trimmed, "SSO_OIDC_CLIENT_ID", ""),
			ClientSecret: envconfig.String(lookup, "SSO_OIDC_CLIENT_SECRET", ""),
			RedirectURL:  envconfig.NonEmpty(trimmed, "SSO_OIDC_REDIRECT_URL", ""),
			AdminGroups:  envconfig.CSV(trimmed, "SSO_OIDC_ADMIN_GROUPS", []string{}),
		},
	}
	if err := required.Err(); err != nil {
		return Bootstrap{}, err
	}
	if cfg.OIDC.Enabled {
		if err := checkOIDC(cfg.OIDC); err != nil {
			return Bootstrap{}, err
		}
	} else if !cfg.Auth.LocalLoginEnabled {
		return Bootstrap{}, ErrNoSignInMethod
	}
	if err := envconfig.Distinct(
		envconfig.Setting{Key: "GATEWAY_SERVICE_TOKEN", Value: cfg.GatewayToken},
		envconfig.Setting{Key: "CORE_SERVICE_TOKEN", Value: cfg.CoreToken},
	); err != nil {
		return Bootstrap{}, err
	}
	return cfg, nil
}

func checkOIDC(cfg OIDC) error {
	// The secret is kept as set, but one of only spaces is as good as none.
	for _, setting := range []envconfig.Setting{
		{Key: "SSO_OIDC_ISSUER_URL", Value: cfg.IssuerURL},
		{Key: "SSO_OIDC_CLIENT_ID", Value: cfg.ClientID},
		{Key: "SSO_OIDC_CLIENT_SECRET", Value: strings.TrimSpace(cfg.ClientSecret)},
		{Key: "SSO_OIDC_REDIRECT_URL", Value: cfg.RedirectURL},
	} {
		if setting.Value == "" {
			return &envconfig.MissingError{Key: setting.Key}
		}
	}
	if !absoluteHTTPURL(cfg.IssuerURL) {
		return &envconfig.InvalidError{Key: "SSO_OIDC_ISSUER_URL", Reason: httpURLReason}
	}
	if !absoluteHTTPURL(cfg.RedirectURL) {
		return &envconfig.InvalidError{Key: "SSO_OIDC_REDIRECT_URL", Reason: httpURLReason}
	}
	return nil
}

const httpURLReason = "must be an absolute http or https URL"

// absoluteHTTPURL reports whether value parses as an http or https URL with a
// host. The parse error is dropped because url.Parse quotes the value.
func absoluteHTTPURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}
