// Package depstest checks that constructors reject missing dependencies.
package depstest

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// RequireRejectsMissing checks that construct rejects a dependency struct in
// which any one required field is missing. required maps each field name of D
// to the dependency name its error reports. For every field a subtest clears
// it to the zero value, and for an interface field a second subtest stores a
// typed nil pointer of the valid value's type; construct must then return a nil
// result and an error containing "<name> is required". valid builds a complete
// dependency struct for each subtest, so mocks bind to that subtest.
func RequireRejectsMissing[D, R any](t *testing.T, valid func(*testing.T) D, construct func(D) (R, error), required map[string]string) {
	t.Helper()
	for _, field := range slices.Sorted(maps.Keys(required)) {
		message := required[field]
		static, ok := reflect.TypeFor[D]().FieldByName(field)
		require.True(t, ok, "%s has no field %s", reflect.TypeFor[D](), field)
		variants := []bool{false}
		if static.Type.Kind() == reflect.Interface {
			variants = append(variants, true)
		}
		for _, typedNil := range variants {
			name := field + "/nil"
			if typedNil {
				name = field + "/typed_nil"
			}
			t.Run(name, func(t *testing.T) {
				deps := valid(t)
				value := reflect.ValueOf(&deps).Elem().FieldByName(field)
				if typedNil {
					nilValue, ok := typedNilFor(value)
					require.True(t, ok, "the valid %s cannot become a typed nil pointer", field)
					value.Set(nilValue)
				} else {
					value.SetZero()
				}
				got, err := construct(deps)
				require.ErrorContains(t, err, message+" is required")
				require.Nil(t, got)
			})
		}
	}
}

// typedNilFor returns a nil pointer of the type an interface field holds,
// or of a pointer to it when the value is not a pointer but its pointer type
// still satisfies the interface.
func typedNilFor(field reflect.Value) (reflect.Value, bool) {
	if field.IsNil() {
		return reflect.Value{}, false
	}
	concrete := field.Elem().Type()
	if concrete.Kind() != reflect.Pointer {
		concrete = reflect.PointerTo(concrete)
	}
	if !concrete.Implements(field.Type()) {
		return reflect.Value{}, false
	}
	return reflect.Zero(concrete), true
}
