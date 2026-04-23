package audio

import (
	"fmt"
	"maps"
)

// ErrTTSParamOutOfRange is returned when a numeric param value is outside the
// declared Min/Max bounds.
type ErrTTSParamOutOfRange struct {
	Key string
	Val any
	Min any
	Max any
}

func (e ErrTTSParamOutOfRange) Error() string {
	return fmt.Sprintf("TTS param %q value %v is out of range [%v, %v]", e.Key, e.Val, e.Min, e.Max)
}

// ErrTTSParamUnknownKey is returned when a user-supplied key is not present in
// the capability schema for the provider.
type ErrTTSParamUnknownKey struct {
	Key string
}

func (e ErrTTSParamUnknownKey) Error() string {
	return fmt.Sprintf("TTS param %q is not supported by this provider", e.Key)
}

// ValidateParams checks all keys in values against the capability schema:
//   - Unknown keys return ErrTTSParamUnknownKey.
//   - Numeric values outside declared Min/Max return ErrTTSParamOutOfRange.
//   - String values not in the Enum list (when non-empty) return an out-of-range error.
//
// Empty values map: no-op returns nil.
// Empty schema: every key is unknown — callers that don't have a schema may pass nil schema to skip.
// Only the first validation error is returned (fail-fast).
func ValidateParams(schema []ParamSchema, values map[string]any) error {
	if len(values) == 0 {
		return nil
	}

	// Build a flat key → schema index map for O(1) lookup.
	// ParamSchema keys use dot-notation (e.g. "voice_settings.stability"), but
	// values arriving from the HTTP layer use the same dot-notation in a nested map.
	// We flatten the incoming map to dot-notation keys for matching.
	flat := flattenMap(values, "")

	// Index schema by key for fast lookup.
	schemaIdx := make(map[string]ParamSchema, len(schema))
	for _, s := range schema {
		schemaIdx[s.Key] = s
	}

	for flatKey, val := range flat {
		s, ok := schemaIdx[flatKey]
		if !ok {
			return ErrTTSParamUnknownKey{Key: flatKey}
		}

		// Range / number / integer: enforce Min/Max when declared.
		if s.Min != nil || s.Max != nil {
			fval, ok := toFloat64(val)
			if ok {
				if s.Min != nil && fval < *s.Min {
					return ErrTTSParamOutOfRange{Key: flatKey, Val: val, Min: *s.Min, Max: maxOrInf(s.Max)}
				}
				if s.Max != nil && fval > *s.Max {
					return ErrTTSParamOutOfRange{Key: flatKey, Val: val, Min: minOrNegInf(s.Min), Max: *s.Max}
				}
			}
		}

		// Enum: validate against declared options (skip empty value — means "use default").
		if len(s.Enum) > 0 {
			if sv, ok := val.(string); ok && sv != "" {
				if !enumContains(s.Enum, sv) {
					return ErrTTSParamOutOfRange{Key: flatKey, Val: val, Min: enumFirst(s.Enum), Max: enumLast(s.Enum)}
				}
			}
		}
	}
	return nil
}

// flattenMap converts a nested map[string]any into a flat map with dot-separated keys.
// prefix is the parent key path (empty string for the root call).
func flattenMap(m map[string]any, prefix string) map[string]any {
	out := make(map[string]any)
	for k, v := range m {
		fullKey := k
		if prefix != "" {
			fullKey = prefix + "." + k
		}
		if nested, ok := v.(map[string]any); ok {
			maps.Copy(out, flattenMap(nested, fullKey))
		} else {
			out[fullKey] = v
		}
	}
	return out
}

// toFloat64 converts common numeric JSON types to float64.
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func enumContains(opts []EnumOption, val string) bool {
	for _, o := range opts {
		if o.Value == val {
			return true
		}
	}
	return false
}

func enumFirst(opts []EnumOption) any {
	if len(opts) == 0 {
		return nil
	}
	return opts[0].Value
}

func enumLast(opts []EnumOption) any {
	if len(opts) == 0 {
		return nil
	}
	return opts[len(opts)-1].Value
}

func maxOrInf(p *float64) any {
	if p == nil {
		return "+∞"
	}
	return *p
}

func minOrNegInf(p *float64) any {
	if p == nil {
		return "-∞"
	}
	return *p
}
