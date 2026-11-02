package config_test

import (
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/telemetry/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/envtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const secret = "s3cret-value"

func requiredEnv() map[string]string {
	return map[string]string{"CORE_GRPC_TARGET": "core:50052", "INTERNAL_SERVICE_TOKEN": secret}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load(envtest.Lookup(requiredEnv()))

	require.NoError(t, err)
	assert.Equal(t, config.Bootstrap{
		Port:          8084,
		CoreTarget:    "core:50052",
		InternalToken: secret,
		MaxLogStreams: 100,
		MaxTerminals:  50,
		HTTP: config.HTTPServer{
			ReadHeaderTimeout: 5 * time.Second,
			IdleTimeout:       120 * time.Second,
			ShutdownTimeout:   15 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
		GRPC: config.GRPC{RequestTimeout: 10 * time.Second, MinConnectTimeout: 5 * time.Second},
	}, cfg)
}

func TestLoadOverrides(t *testing.T) {
	values := requiredEnv()
	values["TELEMETRY_PORT"] = "9000"
	values["TELEMETRY_GRPC_REQUEST_TIMEOUT_SECONDS"] = "7"
	values["TELEMETRY_MAX_GLOBAL_TERMINAL_SESSIONS"] = "not-a-number"

	cfg, err := config.Load(envtest.Lookup(values))

	require.NoError(t, err)
	assert.Equal(t, 9000, cfg.Port)
	assert.Equal(t, 7*time.Second, cfg.GRPC.RequestTimeout)
	assert.Equal(t, 50, cfg.MaxTerminals, "an invalid optional value keeps its default")
}

func TestLoadReportsRequiredSettingsInOrderWithoutValues(t *testing.T) {
	envtest.AssertRequired(t, config.Load, requiredEnv(), []string{
		"CORE_GRPC_TARGET",
		"INTERNAL_SERVICE_TOKEN",
	})
}
