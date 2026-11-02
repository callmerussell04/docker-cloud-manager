// Package envtest holds shared checks for config.Load functions.
package envtest

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/envconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Lookup returns a fake environment holding values.
func Lookup(values map[string]string) envconfig.Lookup {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

// AssertRequired checks that load reports each of order, the required settings
// in the order they are read, when it is unset, empty, or the first still
// missing, and that the errors echo no value from values.
func AssertRequired[T any](t *testing.T, load func(envconfig.Lookup) (T, error), values map[string]string, order []string) {
	t.Helper()
	require.Len(t, order, len(values), "order lists every required setting")

	assertMissing := func(t *testing.T, env map[string]string, key string) {
		t.Helper()
		_, err := load(Lookup(env))
		var missing *envconfig.MissingError
		require.ErrorAs(t, err, &missing)
		assert.Equal(t, key, missing.Key)
		for _, value := range values {
			assert.NotContains(t, err.Error(), value)
		}
	}

	present := map[string]string{}
	for _, key := range order {
		t.Run(key, func(t *testing.T) {
			assertMissing(t, present, key)

			unset := copied(values)
			delete(unset, key)
			assertMissing(t, unset, key)

			empty := copied(values)
			empty[key] = ""
			assertMissing(t, empty, key)
		})
		present[key] = values[key]
	}
	_, err := load(Lookup(values))
	require.NoError(t, err)
}

func copied(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
