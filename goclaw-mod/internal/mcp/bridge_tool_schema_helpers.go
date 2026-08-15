package mcp

import "fmt"

func schemaTypeSet(schema map[string]any) map[string]bool {
	types := make(map[string]bool)
	addSchemaTypes(schema, types)
	if len(types) == 0 {
		if _, ok := schema["properties"]; ok {
			types["object"] = true
		}
		if _, ok := schema["items"]; ok {
			types["array"] = true
		}
	}
	return types
}

func addSchemaTypes(schema map[string]any, types map[string]bool) {
	addTypeValue(schema["type"], types)
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		alternatives, _ := schema[key].([]any)
		for _, alternative := range alternatives {
			altSchema, _ := alternative.(map[string]any)
			if altSchema != nil {
				addSchemaTypes(altSchema, types)
			}
		}
	}
}

func addTypeValue(value any, types map[string]bool) {
	switch v := value.(type) {
	case string:
		if v != "" {
			types[v] = true
		}
	case []any:
		for _, item := range v {
			addTypeValue(item, types)
		}
	case []string:
		for _, item := range v {
			addTypeValue(item, types)
		}
	}
}

func expectedContainerLabel(types map[string]bool) string {
	switch {
	case types["object"] && types["array"]:
		return "object or array"
	case types["object"]:
		return "object"
	case types["array"]:
		return "array"
	default:
		return "non-string JSON container"
	}
}

func prependPath(path string, nestedPaths []string) []string {
	if len(nestedPaths) > 0 {
		return append([]string{path}, nestedPaths...)
	}
	return []string{path}
}

func joinSchemaPath(path, key string) string {
	if path == "" || path == "$" {
		return "$." + key
	}
	return path + "." + key
}

func valueKind(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "number"
	default:
		return fmt.Sprintf("%T", value)
	}
}
