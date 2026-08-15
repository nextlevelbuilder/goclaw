package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (t *BridgeTool) normalizeArgsForSchema(args map[string]any) (map[string]any, []string, error) {
	if len(args) == 0 {
		return args, nil, nil
	}

	normalized, coercedPaths, err := normalizeValueForSchema(args, t.inputSchema, "$")
	if err != nil {
		return nil, nil, err
	}
	normalizedArgs, ok := normalized.(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("arguments must be object, got %s", valueKind(normalized))
	}
	return normalizedArgs, coercedPaths, nil
}

func normalizeValueForSchema(value any, schema map[string]any, path string) (any, []string, error) {
	if schema == nil {
		return value, nil, nil
	}

	types := schemaTypeSet(schema)
	expectsContainer := (types["object"] || types["array"]) && !types["string"]
	if s, ok := value.(string); ok && expectsContainer {
		return normalizeStringContainer(s, schema, types, path)
	}

	if obj, ok := value.(map[string]any); ok {
		return normalizeObjectValue(obj, schema, path)
	}
	if arr, ok := value.([]any); ok {
		return normalizeArrayValue(arr, schema, path)
	}
	return value, nil, nil
}

func normalizeStringContainer(raw string, schema map[string]any, types map[string]bool, path string) (any, []string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil, fmt.Errorf("%s must be %s, got empty string", path, expectedContainerLabel(types))
	}

	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, nil, fmt.Errorf("%s must be %s, got JSON string that cannot be parsed: pass a native value instead of a stringified JSON value", path, expectedContainerLabel(types))
	}

	switch v := decoded.(type) {
	case map[string]any:
		if !types["object"] {
			return nil, nil, fmt.Errorf("%s must be %s, got JSON string containing object", path, expectedContainerLabel(types))
		}
		normalized, nestedPaths, err := normalizeObjectValue(v, schema, path)
		if err != nil {
			return nil, nil, err
		}
		return normalized, prependPath(path, nestedPaths), nil
	case []any:
		if !types["array"] {
			return nil, nil, fmt.Errorf("%s must be %s, got JSON string containing array", path, expectedContainerLabel(types))
		}
		normalized, nestedPaths, err := normalizeArrayValue(v, schema, path)
		if err != nil {
			return nil, nil, err
		}
		return normalized, prependPath(path, nestedPaths), nil
	default:
		return nil, nil, fmt.Errorf("%s must be %s, got JSON string containing %s", path, expectedContainerLabel(types), valueKind(decoded))
	}
}

func normalizeObjectValue(obj map[string]any, schema map[string]any, path string) (map[string]any, []string, error) {
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return obj, nil, nil
	}

	normalized := make(map[string]any, len(obj))
	var coercedPaths []string
	for key, value := range obj {
		propSchema, _ := props[key].(map[string]any)
		if propSchema == nil {
			normalized[key] = value
			continue
		}
		nextPath := joinSchemaPath(path, key)
		nextValue, nestedPaths, err := normalizeValueForSchema(value, propSchema, nextPath)
		if err != nil {
			return nil, nil, err
		}
		normalized[key] = nextValue
		coercedPaths = append(coercedPaths, nestedPaths...)
	}
	return normalized, coercedPaths, nil
}

func normalizeArrayValue(arr []any, schema map[string]any, path string) ([]any, []string, error) {
	itemSchema, _ := schema["items"].(map[string]any)
	if itemSchema == nil {
		return arr, nil, nil
	}

	normalized := make([]any, len(arr))
	var coercedPaths []string
	for i, value := range arr {
		nextPath := fmt.Sprintf("%s[%d]", path, i)
		nextValue, nestedPaths, err := normalizeValueForSchema(value, itemSchema, nextPath)
		if err != nil {
			return nil, nil, err
		}
		normalized[i] = nextValue
		coercedPaths = append(coercedPaths, nestedPaths...)
	}
	return normalized, coercedPaths, nil
}
