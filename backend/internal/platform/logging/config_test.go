package logging_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/stretchr/testify/assert"
)

func TestConfigFromEnvDefaultsAndOverrides(t *testing.T) {
	assert.Equal(t, logging.Config{Level: "info", Format: "json", Env: "production"},
		logging.ConfigFromEnv(func(string) (string, bool) { return "", false }))
	assert.Equal(t, logging.Config{Level: "info", Format: "json", Env: "production"},
		logging.ConfigFromEnv(func(string) (string, bool) { return "", true }), "empty values count as unset")

	values := map[string]string{"LOG_LEVEL": "debug", "LOG_FORMAT": "text", "APP_ENV": "dev"}
	assert.Equal(t, logging.Config{Level: "debug", Format: "text", Env: "dev"},
		logging.ConfigFromEnv(func(key string) (string, bool) {
			value, ok := values[key]
			return value, ok
		}))
}
