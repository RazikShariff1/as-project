// Package pgtypes converts Go values to and from the Postgres array and JSONB columns the stores use.
package pgtypes

import (
	"encoding/json"

	"github.com/lib/pq"
)

// Strings returns s as a Postgres text[] value, never NULL.
func Strings(s []string) pq.StringArray {
	if s == nil {
		return pq.StringArray{}
	}

	return s
}

// Int64s returns s as a Postgres bigint[] value, never NULL.
func Int64s(s []int64) pq.Int64Array {
	if s == nil {
		return pq.Int64Array{}
	}

	return s
}

// NonNil returns s, or an empty slice when s is nil, so JSON renders [] instead of null.
func NonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}

	return s
}

// EncodeJSON marshals a metadata map for a JSONB column (nil becomes {}).
func EncodeJSON(m map[string]any) ([]byte, error) {
	if m == nil {
		m = map[string]any{}
	}

	return json.Marshal(m)
}

// DecodeJSON unmarshals a JSONB column into a metadata map (never nil).
func DecodeJSON(b []byte) (map[string]any, error) {
	m := map[string]any{}
	if len(b) == 0 {
		return m, nil
	}

	err := json.Unmarshal(b, &m)

	return m, err
}
