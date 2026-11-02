package config_test

import (
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/envtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const secret = "s3cret-value"

func requiredEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":           "postgres://u:" + secret + "@db/sso",
		"JWT_SECRET":             secret,
		"INTERNAL_SERVICE_TOKEN": secret,
		"SSO_ADMIN_PASSWORD":     secret,
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load(envtest.Lookup(requiredEnv()))

	require.NoError(t, err)
	assert.Equal(t, config.Bootstrap{
		Port:           50051,
		DBURL:          "postgres://u:" + secret + "@db/sso",
		JWTSecret:      secret,
		InternalToken:  secret,
		AccessTTL:      15 * time.Minute,
		SessionTTL:     30 * 24 * time.Hour,
		SessionIdleTTL: 7 * 24 * time.Hour,
		BootstrapAdmin: config.BootstrapAdmin{Username: "admin", Email: "admin@example.local", Password: secret},
		Auth:           config.Auth{LocalLoginEnabled: true, LocalRegisterEnabled: true},
		OIDC:           config.OIDC{AdminGroups: []string{}, DefaultRole: "user"},
	}, cfg)
}

func TestLoadTrimsOptionalSettings(t *testing.T) {
	values := requiredEnv()
	values["SSO_GRPC_PORT"] = "6000"
	values["SSO_ADMIN_USERNAME"] = "  root "
	values["SSO_LOCAL_LOGIN_ENABLED"] = " false "
	values["SSO_OIDC_ENABLED"] = "true"
	values["SSO_OIDC_ISSUER_URL"] = " https://idp.test "
	values["SSO_OIDC_ADMIN_GROUPS"] = " a, ,b "
	values["SSO_OIDC_DEFAULT_ROLE"] = "   "
	values["SSO_OIDC_CLIENT_SECRET"] = " kept as is "

	cfg, err := config.Load(envtest.Lookup(values))

	require.NoError(t, err)
	assert.Equal(t, 6000, cfg.Port)
	assert.Equal(t, "root", cfg.BootstrapAdmin.Username)
	assert.False(t, cfg.Auth.LocalLoginEnabled)
	assert.True(t, cfg.OIDC.Enabled)
	assert.Equal(t, "https://idp.test", cfg.OIDC.IssuerURL)
	assert.Equal(t, []string{"a", "b"}, cfg.OIDC.AdminGroups)
	assert.Equal(t, "user", cfg.OIDC.DefaultRole)
	assert.Equal(t, " kept as is ", cfg.OIDC.ClientSecret)
}

func TestLoadReportsRequiredSettingsInOrderWithoutValues(t *testing.T) {
	envtest.AssertRequired(t, config.Load, requiredEnv(), []string{
		"DATABASE_URL",
		"JWT_SECRET",
		"INTERNAL_SERVICE_TOKEN",
		"SSO_ADMIN_PASSWORD",
	})
}
