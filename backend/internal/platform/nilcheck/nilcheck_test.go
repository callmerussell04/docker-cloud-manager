package nilcheck_test

import (
	"io"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/nilcheck"
	"github.com/stretchr/testify/require"
)

type store struct{}

func (*store) Read([]byte) (int, error) { return 0, io.EOF }

func TestIsNil(t *testing.T) {
	var (
		typedNilPointer *store
		typedNilReader  io.Reader = typedNilPointer
		nilMap          map[string]int
		nilSlice        []int
		nilChannel      chan int
		nilFunc         func()
	)
	for name, value := range map[string]any{
		"untyped nil":                   nil,
		"typed nil pointer":             typedNilPointer,
		"typed nil pointer in a reader": typedNilReader,
		"nil map":                       nilMap,
		"nil slice":                     nilSlice,
		"nil channel":                   nilChannel,
		"nil function":                  nilFunc,
	} {
		t.Run(name, func(t *testing.T) {
			require.True(t, nilcheck.IsNil(value))
		})
	}
	for name, value := range map[string]any{
		"pointer":      &store{},
		"reader":       io.Reader(&store{}),
		"struct value": store{},
		"zero int":     0,
		"empty string": "",
		"empty map":    map[string]int{},
		"function":     func() {},
	} {
		t.Run(name, func(t *testing.T) {
			require.False(t, nilcheck.IsNil(value))
		})
	}
}
