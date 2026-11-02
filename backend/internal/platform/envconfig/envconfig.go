// Package envconfig reads bootstrap settings through an injected lookup, so
// parsing, defaults and validation are testable without a process environment.
//
// Parsing never reports an invalid optional value: it falls back to the default.
// Required settings report only the setting's name, never a value.
package envconfig

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// Lookup matches os.LookupEnv.
type Lookup func(string) (string, bool)

// String returns the value when the key is set, even when it is empty.
func String(lookup Lookup, key, fallback string) string {
	if value, ok := lookup(key); ok {
		return value
	}
	return fallback
}

// NonEmpty treats an empty value the same as an unset key.
func NonEmpty(lookup Lookup, key, fallback string) string {
	if value, _ := lookup(key); value != "" {
		return value
	}
	return fallback
}

// Int returns fallback when the key is unset or its value is not an int.
func Int(lookup Lookup, key string, fallback int) int {
	return parsed(lookup, key, fallback, strconv.Atoi)
}

// Int64 returns fallback when the key is unset or its value is not an int64.
func Int64(lookup Lookup, key string, fallback int64) int64 {
	return parsed(lookup, key, fallback, func(raw string) (int64, error) {
		return strconv.ParseInt(raw, 10, 64)
	})
}

// Float64 returns fallback when the key is unset or its value is not a float64.
func Float64(lookup Lookup, key string, fallback float64) float64 {
	return parsed(lookup, key, fallback, func(raw string) (float64, error) {
		return strconv.ParseFloat(raw, 64)
	})
}

// Bool returns fallback when the key is unset or its value is not a bool.
func Bool(lookup Lookup, key string, fallback bool) bool {
	return parsed(lookup, key, fallback, strconv.ParseBool)
}

// DurationSeconds reads a whole number of seconds. Unset, invalid and negative
// values return fallback.
func DurationSeconds(lookup Lookup, key string, fallback time.Duration) time.Duration {
	seconds := Int(lookup, key, -1)
	if seconds < 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

// CSV splits a comma-separated value, trimming entries and dropping blank ones.
// An unset or empty value returns fallback; a value with only blank entries
// returns an empty slice.
func CSV(lookup Lookup, key string, fallback []string) []string {
	raw, _ := lookup(key)
	if raw == "" {
		return fallback
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	return values
}

// NonEmptyCSV is CSV that also returns fallback when no entry remains.
func NonEmptyCSV(lookup Lookup, key string, fallback []string) []string {
	if values := CSV(lookup, key, nil); len(values) > 0 {
		return values
	}
	return fallback
}

// Trimmed returns a lookup whose values have surrounding whitespace removed.
func Trimmed(lookup Lookup) Lookup {
	return func(key string) (string, bool) {
		value, ok := lookup(key)
		return strings.TrimSpace(value), ok
	}
}

func parsed[T any](lookup Lookup, key string, fallback T, parse func(string) (T, error)) T {
	raw, ok := lookup(key)
	if !ok {
		return fallback
	}
	value, err := parse(raw)
	if err != nil {
		return fallback
	}
	return value
}

// MissingError names a required setting that is unset or empty.
type MissingError struct {
	Key string
}

func (e *MissingError) Error() string {
	return "required environment variable is not set: " + e.Key
}

// Required reads required settings and remembers the first one missing, so a
// Load can read them in order and check once.
type Required struct {
	lookup Lookup
	err    error
}

// NewRequired returns a Required reading from lookup.
func NewRequired(lookup Lookup) *Required {
	return &Required{lookup: lookup}
}

// Get returns the setting's value, or records it as missing when it is empty.
func (r *Required) Get(key string) string {
	value, _ := r.lookup(key)
	if value == "" && r.err == nil {
		r.err = &MissingError{Key: key}
	}
	return value
}

// Err returns the first missing setting, if any.
func (r *Required) Err() error {
	return r.err
}

// LogEntry returns the message and attributes a main logs for a Load failure.
func LogEntry(err error) (string, []any) {
	var missing *MissingError
	if errors.As(err, &missing) {
		return "required environment variable is not set", []any{"env_var", missing.Key}
	}
	return "invalid bootstrap configuration", []any{"error", err}
}
