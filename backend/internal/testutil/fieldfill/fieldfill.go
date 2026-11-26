// Package fieldfill fills a struct's fields with distinct values, so that a
// test of a field-by-field mapping fails for any field the mapping drops.
package fieldfill

import (
	"fmt"
	"reflect"
	"testing"
)

// Distinct sets every exported, non-embedded field of the struct target
// points to: numbers and strings to a value no other field gets, booleans to
// true and string slices to one distinct element. It fails the test on a field
// kind it does not know, so a new kind of field cannot go unfilled.
func Distinct(t testing.TB, target any) {
	t.Helper()
	value := reflect.ValueOf(target).Elem()
	for i := range value.NumField() {
		field := value.Type().Field(i)
		if field.Anonymous || !field.IsExported() {
			continue
		}
		n := i + 1
		switch f := value.Field(i); f.Kind() {
		case reflect.String:
			f.SetString(fmt.Sprintf("value-%d", n))
		case reflect.Int, reflect.Int32, reflect.Int64:
			f.SetInt(int64(n))
		case reflect.Float64:
			f.SetFloat(float64(n) + 0.5)
		case reflect.Bool:
			f.SetBool(true)
		case reflect.Slice:
			if f.Type().Elem().Kind() != reflect.String {
				t.Fatalf("fieldfill: field %s has an unsupported slice type", field.Name)
			}
			f.Set(reflect.ValueOf([]string{fmt.Sprintf("value-%d", n)}))
		default:
			t.Fatalf("fieldfill: field %s has unsupported kind %s", field.Name, f.Kind())
		}
	}
}
