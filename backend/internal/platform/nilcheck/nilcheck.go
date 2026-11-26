// Package nilcheck tells a missing dependency from a supplied one.
package nilcheck

import "reflect"

// IsNil reports whether value is nil: an untyped nil, or a nil pointer, map,
// slice, channel, function or interface stored in an interface. Constructors
// use it to reject a dependency that was not supplied, including a typed nil
// pointer, which compares unequal to nil.
func IsNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
