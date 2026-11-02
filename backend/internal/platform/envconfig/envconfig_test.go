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
	bad := env(map[string]string{"K": "nope"})

	assert.Equal(t, 7, envconfig.Int(env(nil), "K", 7))
	assert.Equal(t, 7, envconfig.Int(bad, "K", 7))
	assert.Equal(t, 3, envconfig.Int(env(map[string]string{"K": "3"}), "K", 7))

	assert.Equal(t, int64(7), envconfig.Int64(env(nil), "K", 7))
	assert.Equal(t, int64(7), envconfig.Int64(bad, "K", 7))
	assert.Equal(t, int64(1<<40), envconfig.Int64(env(map[string]string{"K": "1099511627776"}), "K", 7))

	assert.Equal(t, 1.5, envconfig.Float64(env(nil), "K", 1.5))
	assert.Equal(t, 1.5, envconfig.Float64(bad, "K", 1.5))
	assert.Equal(t, 2.25, envconfig.Float64(env(map[string]string{"K": "2.25"}), "K", 1.5))

	assert.True(t, envconfig.Bool(env(nil), "K", true))
	assert.True(t, envconfig.Bool(bad, "K", true))
	assert.False(t, envconfig.Bool(env(map[string]string{"K": "false"}), "K", true))
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
	assert.Equal(t, "invalid bootstrap configuration", msg)
	assert.Equal(t, []any{"error", other}, args)
}
