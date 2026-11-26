package testenv_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/testenv"
)

// recorder captures how a gate ends a test without stopping the caller.
type recorder struct {
	testing.TB
	skipped, failed bool
	message         string
}

func (r *recorder) Helper() {}

func (r *recorder) Skip(args ...any) {
	r.skipped = true
	r.message = args[0].(string)
}

func (r *recorder) Fatal(args ...any) {
	r.failed = true
	r.message = args[0].(string)
}

func TestLookup(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		value       string
		require     string
		wantValue   string
		wantSkipped bool
		wantFailed  bool
	}{
		{name: "set value is returned", key: "CORE_TEST_DATABASE_URL", value: " postgres://db ", wantValue: "postgres://db"},
		{name: "missing value skips", key: "CORE_TEST_DATABASE_URL", wantSkipped: true},
		{name: "blank value skips", key: "CORE_TEST_DATABASE_URL", value: "  ", wantSkipped: true},
		{name: "missing value fails when integration is required", key: "CORE_TEST_DATABASE_URL", require: "1", wantFailed: true},
		{name: "require flag other than 1 still skips", key: "CORE_TEST_DATABASE_URL", require: "true", wantSkipped: true},
		{name: "exempt gate skips when integration is required", key: "CORE_TEST_NETWORK_POLICY", require: "1", wantSkipped: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			t.Setenv(testenv.RequireIntegration, tt.require)
			rec := &recorder{TB: t}

			got := testenv.Lookup(rec, tt.key)

			assert.Equal(t, tt.wantValue, got)
			assert.Equal(t, tt.wantSkipped, rec.skipped, "skipped")
			assert.Equal(t, tt.wantFailed, rec.failed, "failed")
			if tt.wantSkipped || tt.wantFailed {
				assert.Contains(t, rec.message, tt.key)
			}
		})
	}
}

func TestEnabled(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		value       string
		require     string
		wantSkipped bool
		wantFailed  bool
	}{
		{name: "enabled gate runs", key: "CORE_TEST_DOCKER", value: "1"},
		{name: "disabled gate skips", key: "CORE_TEST_DOCKER", value: "0", wantSkipped: true},
		{name: "unset gate skips", key: "CORE_TEST_DOCKER", wantSkipped: true},
		{name: "disabled gate fails when integration is required", key: "CORE_TEST_DOCKER", value: "0", require: "1", wantFailed: true},
		{name: "network policy is exempt", key: "CORE_TEST_NETWORK_POLICY", require: "1", wantSkipped: true},
		{name: "volume quota host is exempt", key: "DCM_TEST_VOLUME_QUOTA_DOCKER", require: "1", wantSkipped: true},
		{name: "enabled exempt gate runs", key: "CORE_TEST_NETWORK_POLICY", value: "1", require: "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			t.Setenv(testenv.RequireIntegration, tt.require)
			rec := &recorder{TB: t}

			testenv.Enabled(rec, tt.key)

			assert.Equal(t, tt.wantSkipped, rec.skipped, "skipped")
			assert.Equal(t, tt.wantFailed, rec.failed, "failed")
		})
	}
}
