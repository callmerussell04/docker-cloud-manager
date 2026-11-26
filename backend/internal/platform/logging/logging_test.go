package logging_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/logging"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/envtest"
	"github.com/stretchr/testify/assert"
)

func TestConfigFromEnv(t *testing.T) {
	defaults := logging.Config{Level: "info", Format: "json", Env: "production"}
	tests := []struct {
		name   string
		values map[string]string
		want   logging.Config
	}{
		{"unset", nil, defaults},
		{"empty counts as unset", map[string]string{"LOG_LEVEL": "", "LOG_FORMAT": "", "APP_ENV": ""}, defaults},
		{"overrides", map[string]string{"LOG_LEVEL": "debug", "LOG_FORMAT": "text", "APP_ENV": "dev"}, logging.Config{Level: "debug", Format: "text", Env: "dev"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, logging.ConfigFromEnv(envtest.Lookup(tt.values)))
		})
	}
}
