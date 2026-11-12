package config_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/envtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const secret = "s3cret-value"

func requiredEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":               "postgres://u:" + secret + "@db/core",
		"BASE_DOMAIN":                "example.test",
		"RABBITMQ_URL":               "amqp://u:" + secret + "@mq",
		"SSO_GRPC_TARGET":            "sso:50051",
		"CORE_SERVICE_TOKEN":         "core-" + secret,
		"GATEWAY_SERVICE_TOKEN":      "gateway-" + secret,
		"BUILDER_SERVICE_TOKEN":      "builder-" + secret,
		"REGISTRY_URL":               "registry:5000",
		"REGISTRY_BACKEND_URL":       "https://registry-backend:5000",
		"REGISTRY_CA_FILE":           "/ca",
		"REGISTRY_TLS_CERT_FILE":     "/cert",
		"REGISTRY_TLS_KEY_FILE":      "/key",
		"REGISTRY_SIGNING_CERT_FILE": "/signing-cert",
		"REGISTRY_SIGNING_KEY_FILE":  "/signing-key",
		"REGISTRY_TOKEN_ISSUER":      "dcm",
		"OBJECT_STORAGE_ENDPOINT":    "minio:9000",
		"OBJECT_STORAGE_ACCESS_KEY":  secret,
		"OBJECT_STORAGE_SECRET_KEY":  secret,
	}
}

func TestLoadBootstrapDefaults(t *testing.T) {
	settings, err := config.Load(envtest.Lookup(requiredEnv()))

	require.NoError(t, err)
	assert.Equal(t, config.Bootstrap{
		Port:          50052,
		HTTPPort:      8083,
		DBURL:         "postgres://u:" + secret + "@db/core",
		RabbitMQURL:   "amqp://u:" + secret + "@mq",
		SSOTarget:     "sso:50051",
		ServiceToken:  "core-" + secret,
		GatewayToken:  "gateway-" + secret,
		BuilderToken:  "builder-" + secret,
		HostDiskPath:  "/",
		ObjectStorage: objectstorage.Config{Endpoint: "minio:9000", Bucket: "dcm-builds", AccessKey: secret, SecretKey: secret},
		ClickHouse:    config.ClickHouse{Database: "dcm_reports"},
		Registry: config.Registry{
			Authority:              "registry:5000",
			BackendURL:             "https://registry-backend:5000",
			CAFile:                 "/ca",
			CertificateFile:        "/cert",
			KeyFile:                "/key",
			SigningCertificateFile: "/signing-cert",
			SigningKeyFile:         "/signing-key",
			Issuer:                 "dcm",
		},
		HTTP: config.HTTPServer{
			ReadHeaderTimeout: 5 * time.Second,
			IdleTimeout:       120 * time.Second,
			ShutdownTimeout:   15 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
		Streams: config.StreamLimits{MaxLogStreams: 100, MaxTerminalSessions: 50},
	}, settings.Bootstrap)
}

func TestLoadRuntimeDefaultsAreValidAndHoldNoBootstrapSecrets(t *testing.T) {
	settings, err := config.Load(envtest.Lookup(requiredEnv()))
	require.NoError(t, err)

	runtime := settings.Runtime
	require.NoError(t, config.ValidateSystemConfig(runtime))
	assert.Equal(t, "example.test", runtime.BaseDomain)
	assert.Equal(t, int64(256<<20), runtime.DefaultMemoryReservation)
	assert.Equal(t, 1.0, runtime.OvercommitFactor)
	assert.Equal(t, 10, runtime.MaxContainersPerUser)
	assert.Equal(t, "https://registry-backend:5000", runtime.RegistryAPIURL)
	assert.Equal(t, []string{"github.com", "gitlab.com", "bitbucket.org"}, runtime.GitAllowedHosts)
	assert.Equal(t, []string{}, runtime.BlockedDomainPrefixPatterns)
	assert.Equal(t, config.DefaultReportsUsageSnapshotIntervalSeconds, runtime.ReportsUsageSnapshotIntervalSeconds)
	assert.False(t, runtime.VolumeSizeEnforcement, "volume sizes are measured unless an administrator turns enforcement on")
	assert.Equal(t, 1.0, runtime.DefaultUserQuotaCPU)
	assert.Equal(t, int64(2048), runtime.DefaultUserQuotaRAMMB)
	assert.Equal(t, int64(5120), runtime.DefaultUserQuotaDiskMB)
	assert.NotContains(t, assertJSON(t, runtime), secret)
}

func TestLoadOverrides(t *testing.T) {
	values := requiredEnv()
	values["CORE_GRPC_PORT"] = "6000"
	values["MAX_CONTAINERS_PER_USER"] = "not-a-number"
	values["OVERCOMMIT_FACTOR"] = "2.5"
	values["GIT_ALLOWED_HOSTS"] = " , "
	values["TELEMETRY_ALLOWED_EXEC_COMMANDS"] = "/bin/sh, /bin/ash"
	values["OBJECT_STORAGE_USE_SSL"] = "true"
	values["CLICKHOUSE_DB"] = ""
	values["TENANT_CGROUP_PARENT"] = "dcm-tenants.slice"
	values["VOLUME_SIZE_ENFORCEMENT"] = "true"
	values["DEFAULT_USER_QUOTA_CPU"] = "2.5"
	values["DEFAULT_USER_QUOTA_RAM_MB"] = "4096"
	values["DEFAULT_USER_QUOTA_DISK_MB"] = "10240"
	values["TELEMETRY_SHUTDOWN_TIMEOUT_SECONDS"] = "7"
	values["TELEMETRY_MAX_GLOBAL_LOG_STREAMS"] = "12"
	values["TELEMETRY_MAX_GLOBAL_TERMINAL_SESSIONS"] = "not-a-number"

	settings, err := config.Load(envtest.Lookup(values))

	require.NoError(t, err)
	assert.Equal(t, 6000, settings.Bootstrap.Port)
	assert.Equal(t, "dcm-tenants.slice", settings.Bootstrap.TenantCgroupParent)
	assert.Equal(t, 7*time.Second, settings.Bootstrap.HTTP.ShutdownTimeout)
	assert.Equal(t, 12, settings.Bootstrap.Streams.MaxLogStreams)
	assert.Equal(t, 50, settings.Bootstrap.Streams.MaxTerminalSessions, "an invalid optional value keeps its default")
	assert.True(t, settings.Bootstrap.ObjectStorage.UseSSL)
	assert.Equal(t, "", settings.Bootstrap.ClickHouse.Database, "a value set to empty replaces the default")
	assert.Equal(t, 10, settings.Runtime.MaxContainersPerUser, "an invalid optional value keeps its default")
	assert.Equal(t, 2.5, settings.Runtime.OvercommitFactor)
	assert.True(t, settings.Runtime.VolumeSizeEnforcement)
	assert.Equal(t, 2.5, settings.Runtime.DefaultUserQuotaCPU)
	assert.Equal(t, int64(4096), settings.Runtime.DefaultUserQuotaRAMMB)
	assert.Equal(t, int64(10240), settings.Runtime.DefaultUserQuotaDiskMB)
	assert.Equal(t, []string{"github.com", "gitlab.com", "bitbucket.org"}, settings.Runtime.GitAllowedHosts)
	assert.Equal(t, []string{"/bin/sh", "/bin/ash"}, settings.Runtime.TelemetryAllowedExecCommands)
}

func TestLoadReportsRequiredSettingsInOrderWithoutValues(t *testing.T) {
	envtest.AssertRequired(t, config.Load, requiredEnv(), []string{
		"DATABASE_URL",
		"BASE_DOMAIN",
		"RABBITMQ_URL",
		"SSO_GRPC_TARGET",
		"CORE_SERVICE_TOKEN",
		"GATEWAY_SERVICE_TOKEN",
		"BUILDER_SERVICE_TOKEN",
		"REGISTRY_URL",
		"REGISTRY_BACKEND_URL",
		"REGISTRY_CA_FILE",
		"REGISTRY_TLS_CERT_FILE",
		"REGISTRY_TLS_KEY_FILE",
		"REGISTRY_SIGNING_CERT_FILE",
		"REGISTRY_SIGNING_KEY_FILE",
		"REGISTRY_TOKEN_ISSUER",
		"OBJECT_STORAGE_ENDPOINT",
		"OBJECT_STORAGE_ACCESS_KEY",
		"OBJECT_STORAGE_SECRET_KEY",
	})
}

func assertJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}

func TestLoadRefusesSharedServiceCredentialsWithoutValues(t *testing.T) {
	envtest.AssertDistinct(t, config.Load, requiredEnv(), []string{"CORE_SERVICE_TOKEN", "GATEWAY_SERVICE_TOKEN", "BUILDER_SERVICE_TOKEN"})
}
