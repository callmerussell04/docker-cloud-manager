// Package config loads SSO's bootstrap settings.
package config

import (
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
)

// Bootstrap is SSO's process-lifetime configuration.
type Bootstrap struct {
	Port           int
	DBURL          string
	JWTSecret      string
	InternalToken  string
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
	DefaultRole  string
}

// Load reads SSO's settings. Optional settings other than the port and the OIDC
// client secret are trimmed. Its error names the first missing required setting.
func Load(lookup envconfig.Lookup) (Bootstrap, error) {
	required := envconfig.NewRequired(lookup)
	trimmed := envconfig.Trimmed(lookup)
	cfg := Bootstrap{
		Port:           envconfig.Int(lookup, "SSO_GRPC_PORT", 50051),
		DBURL:          required.Get("DATABASE_URL"),
		JWTSecret:      required.Get("JWT_SECRET"),
		InternalToken:  required.Get("INTERNAL_SERVICE_TOKEN"),
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
			DefaultRole:  envconfig.NonEmpty(trimmed, "SSO_OIDC_DEFAULT_ROLE", "user"),
		},
	}
	if err := required.Err(); err != nil {
		return Bootstrap{}, err
	}
	return cfg, nil
}
