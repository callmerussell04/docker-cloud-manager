package envconfig_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func env(values map[string]string) envconfig.Lookup {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func TestStringKeepsAValueSetToEmpty(t *testing.T) {
	assert.Equal(t, "fallback", envconfig.String(env(nil), "K", "fallback"))
	assert.Equal(t, "", envconfig.String(env(map[string]string{"K": ""}), "K", "fallback"))
	assert.Equal(t, " v ", envconfig.String(env(map[string]string{"K": " v "}), "K", "fallback"))
}

func TestNonEmptyTreatsEmptyAsUnset(t *testing.T) {
	assert.Equal(t, "fallback", envconfig.NonEmpty(env(nil), "K", "fallback"))
	assert.Equal(t, "fallback", envconfig.NonEmpty(env(map[string]string{"K": ""}), "K", "fallback"))
	assert.Equal(t, "v", envconfig.NonEmpty(env(map[string]string{"K": "v"}), "K", "fallback"))
}

func TestNumbersAndBoolsFallBackWhenUnsetOrInvalid(t *testing.T) {
	tests := []struct {
		name     string
		read     func(envconfig.Lookup) any
		valid    string
		want     any
		fallback any
	}{
		{"Int", func(l envconfig.Lookup) any { return envconfig.Int(l, "K", 7) }, "3", 3, 7},
		{"Int64", func(l envconfig.Lookup) any { return envconfig.Int64(l, "K", 7) }, "1099511627776", int64(1 << 40), int64(7)},
		{"Float64", func(l envconfig.Lookup) any { return envconfig.Float64(l, "K", 1.5) }, "2.25", 2.25, 1.5},
		{"Bool", func(l envconfig.Lookup) any { return envconfig.Bool(l, "K", true) }, "false", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.fallback, tt.read(env(nil)))
			assert.Equal(t, tt.fallback, tt.read(env(map[string]string{"K": "nope"})))
			assert.Equal(t, tt.want, tt.read(env(map[string]string{"K": tt.valid})))
		})
	}
}

func TestDurationSecondsRejectsNegativeValues(t *testing.T) {
	assert.Equal(t, time.Minute, envconfig.DurationSeconds(env(nil), "K", time.Minute))
	assert.Equal(t, time.Minute, envconfig.DurationSeconds(env(map[string]string{"K": "-1"}), "K", time.Minute))
	assert.Equal(t, time.Minute, envconfig.DurationSeconds(env(map[string]string{"K": "1m"}), "K", time.Minute))
	assert.Equal(t, time.Duration(0), envconfig.DurationSeconds(env(map[string]string{"K": "0"}), "K", time.Minute))
	assert.Equal(t, 5*time.Second, envconfig.DurationSeconds(env(map[string]string{"K": "5"}), "K", time.Minute))
}

func TestCSVTrimsAndDropsBlankEntries(t *testing.T) {
	fallback := []string{"d"}

	assert.Equal(t, fallback, envconfig.CSV(env(nil), "K", fallback))
	assert.Equal(t, fallback, envconfig.CSV(env(map[string]string{"K": ""}), "K", fallback))
	assert.Equal(t, []string{"a", "b"}, envconfig.CSV(env(map[string]string{"K": " a, ,b "}), "K", fallback))
	assert.Equal(t, []string{}, envconfig.CSV(env(map[string]string{"K": " , "}), "K", fallback))
}

func TestNonEmptyCSVFallsBackWhenNoEntryRemains(t *testing.T) {
	fallback := []string{"d"}

	assert.Equal(t, fallback, envconfig.NonEmptyCSV(env(map[string]string{"K": " , "}), "K", fallback))
	assert.Equal(t, fallback, envconfig.NonEmptyCSV(env(map[string]string{"K": "   "}), "K", fallback))
	assert.Equal(t, []string{"a"}, envconfig.NonEmptyCSV(env(map[string]string{"K": " a "}), "K", fallback))
}

func TestTrimmedTrimsEveryValueItReturns(t *testing.T) {
	lookup := envconfig.Trimmed(env(map[string]string{"K": "  true "}))

	assert.True(t, envconfig.Bool(lookup, "K", false))
	_, ok := lookup("UNSET")
	assert.False(t, ok)
}

func TestRequiredReportsTheFirstMissingSettingWithoutValues(t *testing.T) {
	required := envconfig.NewRequired(env(map[string]string{"SET": "s3cret-value", "EMPTY": ""}))

	assert.Equal(t, "s3cret-value", required.Get("SET"))
	assert.Equal(t, "", required.Get("EMPTY"))
	assert.Equal(t, "", required.Get("UNSET"))

	err := required.Err()
	var missing *envconfig.MissingError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "EMPTY", missing.Key)
	assert.NotContains(t, err.Error(), "s3cret-value")
}

func TestRequiredHasNoErrorWhenAllSettingsAreSet(t *testing.T) {
	required := envconfig.NewRequired(env(map[string]string{"SET": "v"}))

	assert.Equal(t, "v", required.Get("SET"))
	assert.NoError(t, required.Err())
}

func TestLogEntryKeepsTheRequiredSettingLogShape(t *testing.T) {
	msg, args := envconfig.LogEntry(fmt.Errorf("load: %w", &envconfig.MissingError{Key: "JWT_SECRET"}))
	assert.Equal(t, "required environment variable is not set", msg)
	assert.Equal(t, []any{"env_var", "JWT_SECRET"}, args)

	other := errors.New("other")
	msg, args = envconfig.LogEntry(other)
	assert.Equal(t, "other", msg)
	assert.Empty(t, args)
}

func TestDistinctNamesTheFirstSharedValueWithoutIt(t *testing.T) {
	require.NoError(t, envconfig.Distinct(
		envconfig.Setting{Key: "A_TOKEN", Value: "one"},
		envconfig.Setting{Key: "B_TOKEN", Value: "two"},
	))

	err := envconfig.Distinct(
		envconfig.Setting{Key: "A_TOKEN", Value: "one"},
		envconfig.Setting{Key: "B_TOKEN", Value: "two"},
		envconfig.Setting{Key: "C_TOKEN", Value: "one"},
	)
	var duplicate *envconfig.DuplicateError
	require.ErrorAs(t, err, &duplicate)
	assert.Equal(t, envconfig.DuplicateError{Key: "A_TOKEN", OtherKey: "C_TOKEN"}, *duplicate)
	assert.NotContains(t, err.Error(), "one")

	msg, args := envconfig.LogEntry(fmt.Errorf("load: %w", err))
	assert.Equal(t, "environment variables must hold different values", msg)
	assert.Equal(t, []any{"env_vars", []string{"A_TOKEN", "C_TOKEN"}}, args)
}
