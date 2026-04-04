package providers

import "strings"

// walkSchema applies fn to every nested map in known schema fields
// (properties, items, additionalProperties, anyOf, oneOf, allOf, not)
// without touching the current level.
func walkSchema(schema map[string]any, fn func(map[string]any) map[string]any) map[string]any {
	if props, ok := schema["properties"].(map[string]any); ok {
		cleaned := make(map[string]any, len(props))
		for k, v := range props {
			if m, ok := v.(map[string]any); ok {
				cleaned[k] = fn(m)
			} else {
				cleaned[k] = v
			}
		}
		schema["properties"] = cleaned
	}
	if items, ok := schema["items"].(map[string]any); ok {
		schema["items"] = fn(items)
	}
	// additionalProperties can be a schema (map) or boolean — only recurse into maps.
	if ap, ok := schema["additionalProperties"].(map[string]any); ok {
		schema["additionalProperties"] = fn(ap)
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if arr, ok := schema[key].([]any); ok {
			for i, item := range arr {
				if m, ok := item.(map[string]any); ok {
					arr[i] = fn(m)
				}
			}
		}
	}
	// "not" keyword contains a single schema.
	if notSchema, ok := schema["not"].(map[string]any); ok {
		schema["not"] = fn(notSchema)
	}
	return schema
}

// copySchema deep-copies a map[string]any to avoid mutating the original.
func copySchema(schema map[string]any) map[string]any {
	if schema == nil {
		return nil
	}
	result := make(map[string]any, len(schema))
	for k, v := range schema {
		switch val := v.(type) {
		case map[string]any:
			result[k] = copySchema(val)
		case []any:
			cp := make([]any, len(val))
			for i, item := range val {
				if m, ok := item.(map[string]any); ok {
					cp[i] = copySchema(m)
				} else {
					cp[i] = item
				}
			}
			result[k] = cp
		default:
			result[k] = v
		}
	}
	return result
}

func copyVisited(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// copyMeta copies description/title from src to dst if present.
func copyMeta(src, dst map[string]any) {
	for _, key := range []string{"description", "title"} {
		if v, ok := src[key]; ok {
			if _, exists := dst[key]; !exists {
				dst[key] = v
			}
		}
	}
}

// refName extracts the definition name from a $ref path like "#/$defs/Foo".
func refName(ref string) string {
	for _, prefix := range []string{"#/$defs/", "#/definitions/"} {
		if after, ok := strings.CutPrefix(ref, prefix); ok {
			return after
		}
	}
	return ""
}

// isNullSchema returns true for schemas representing the null type.
func isNullSchema(schema map[string]any) bool {
	if t, ok := schema["type"].(string); ok && t == "null" {
		return true
	}
	if c, ok := schema["const"]; ok && c == nil {
		return true
	}
	if e, ok := schema["enum"].([]any); ok && len(e) == 1 && e[0] == nil {
		return true
	}
	return false
}

// inferType returns a JSON Schema type string for a Go value.
// Used when const/enum variants omit the explicit "type" field.
func inferType(val any) string {
	switch val.(type) {
	case string:
		return "string"
	case float64, float32, int, int64:
		return "number"
	case bool:
		return "boolean"
	default:
		return ""
	}
}
