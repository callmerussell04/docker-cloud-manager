package config_test

import (
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/sso/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/envtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const secret = "s3cret-value"

func requiredEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":          "postgres://u:" + secret + "@db/sso",
		"JWT_SECRET":            secret,
		"GATEWAY_SERVICE_TOKEN": "gateway-" + secret,
		"CORE_SERVICE_TOKEN":    "core-" + secret,
		"SSO_ADMIN_PASSWORD":    secret,
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load(envtest.Lookup(requiredEnv()))

	require.NoError(t, err)
	assert.Equal(t, config.Bootstrap{
		Port:           50051,
		DBURL:          "postgres://u:" + secret + "@db/sso",
		JWTSecret:      secret,
		GatewayToken:   "gateway-" + secret,
		CoreToken:      "core-" + secret,
		AccessTTL:      15 * time.Minute,
		SessionTTL:     30 * 24 * time.Hour,
		SessionIdleTTL: 7 * 24 * time.Hour,
		BootstrapAdmin: config.BootstrapAdmin{Username: "admin", Email: "admin@example.local", Password: secret},
		Auth:           config.Auth{LocalLoginEnabled: true, LocalRegisterEnabled: true},
		OIDC:           config.OIDC{AdminGroups: []string{}},
	}, cfg)
}

func TestLoadTrimsOptionalSettings(t *testing.T) {
	values := requiredEnv()
	values["SSO_GRPC_PORT"] = "6000"
	values["SSO_ADMIN_USERNAME"] = "  root "
	values["SSO_LOCAL_LOGIN_ENABLED"] = " false "
	values["SSO_OIDC_ENABLED"] = "true"
	values["SSO_OIDC_ISSUER_URL"] = " https://idp.test "
	values["SSO_OIDC_CLIENT_ID"] = " dcm "
	values["SSO_OIDC_REDIRECT_URL"] = " https://dcm.test/api/v1/auth/oidc/keycloak/callback "
	values["SSO_OIDC_ADMIN_GROUPS"] = " a, ,b "
	values["SSO_OIDC_CLIENT_SECRET"] = " kept as is "

	cfg, err := config.Load(envtest.Lookup(values))

	require.NoError(t, err)
	assert.Equal(t, 6000, cfg.Port)
	assert.Equal(t, "root", cfg.BootstrapAdmin.Username)
	assert.False(t, cfg.Auth.LocalLoginEnabled)
	assert.True(t, cfg.OIDC.Enabled)
	assert.Equal(t, "https://idp.test", cfg.OIDC.IssuerURL)
	assert.Equal(t, "dcm", cfg.OIDC.ClientID)
	assert.Equal(t, "https://dcm.test/api/v1/auth/oidc/keycloak/callback", cfg.OIDC.RedirectURL)
	assert.Equal(t, []string{"a", "b"}, cfg.OIDC.AdminGroups)
	assert.Equal(t, " kept as is ", cfg.OIDC.ClientSecret)
}

func TestLoadReportsRequiredSettingsInOrderWithoutValues(t *testing.T) {
	envtest.AssertRequired(t, config.Load, requiredEnv(), []string{
		"DATABASE_URL",
		"JWT_SECRET",
		"GATEWAY_SERVICE_TOKEN",
		"CORE_SERVICE_TOKEN",
		"SSO_ADMIN_PASSWORD",
	})
}

func TestLoadRefusesSharedServiceCredentialsWithoutValues(t *testing.T) {
	envtest.AssertDistinct(t, config.Load, requiredEnv(), []string{"GATEWAY_SERVICE_TOKEN", "CORE_SERVICE_TOKEN"})
}

func oidcEnv() map[string]string {
	values := requiredEnv()
	values["SSO_OIDC_ENABLED"] = "true"
	values["SSO_OIDC_ISSUER_URL"] = "https://idp.test/realms/" + secret
	values["SSO_OIDC_CLIENT_ID"] = "client-" + secret
	values["SSO_OIDC_CLIENT_SECRET"] = "oidc-" + secret
	values["SSO_OIDC_REDIRECT_URL"] = "https://dcm.test/callback?" + secret
	return values
}

func TestLoadRequiresEveryOIDCSettingWhenOIDCIsOn(t *testing.T) {
	for _, key := range []string{"SSO_OIDC_ISSUER_URL", "SSO_OIDC_CLIENT_ID", "SSO_OIDC_CLIENT_SECRET", "SSO_OIDC_REDIRECT_URL"} {
		t.Run(key, func(t *testing.T) {
			for _, value := range []string{"", "   "} {
				values := oidcEnv()
				values[key] = value
				if value == "" {
					delete(values, key)
				}
				_, err := config.Load(envtest.Lookup(values))
				var missing *envconfig.MissingError
				require.ErrorAs(t, err, &missing)
				assert.Equal(t, key, missing.Key)
				assertNoValue(t, err, oidcEnv())
			}
		})
	}
}

func TestLoadIgnoresOIDCSettingsWhenOIDCIsOff(t *testing.T) {
	cfg, err := config.Load(envtest.Lookup(requiredEnv()))

	require.NoError(t, err)
	assert.False(t, cfg.OIDC.Enabled)
}

func TestLoadRefusesMalformedOIDCURLsWithoutValues(t *testing.T) {
	for _, key := range []string{"SSO_OIDC_ISSUER_URL", "SSO_OIDC_REDIRECT_URL"} {
		for _, value := range []string{"idp.test/" + secret, "/relative/" + secret, "ftp://idp.test/" + secret, "https://" + secret + "%zz", "https:///" + secret} {
			t.Run(key+" "+value, func(t *testing.T) {
				values := oidcEnv()
				values[key] = value
				_, err := config.Load(envtest.Lookup(values))
				var invalid *envconfig.InvalidError
				require.ErrorAs(t, err, &invalid)
				assert.Equal(t, key, invalid.Key)
				assertNoValue(t, err, values)
			})
		}
	}
}

func TestLoadRefusesNoSignInMethodNamingBothSettings(t *testing.T) {
	values := requiredEnv()
	values["SSO_LOCAL_LOGIN_ENABLED"] = "false"

	_, err := config.Load(envtest.Lookup(values))

	require.ErrorIs(t, err, config.ErrNoSignInMethod)
	assert.Contains(t, err.Error(), "SSO_LOCAL_LOGIN_ENABLED")
	assert.Contains(t, err.Error(), "SSO_OIDC_ENABLED")
	assertNoValue(t, err, values)

	values = oidcEnv()
	values["SSO_LOCAL_LOGIN_ENABLED"] = "false"
	_, err = config.Load(envtest.Lookup(values))
	require.NoError(t, err)
}

func assertNoValue(t *testing.T, err error, values map[string]string) {
	t.Helper()
	assert.NotContains(t, err.Error(), secret)
	for _, value := range values {
		// Short values such as "true" are setting values, not secrets, and may appear in any text.
		if len(value) > 5 {
			assert.NotContains(t, err.Error(), value)
		}
	}
}
