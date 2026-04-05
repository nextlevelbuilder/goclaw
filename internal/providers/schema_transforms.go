package providers

import "slices"

// ---------------------------------------------------------------------------
// Union flattening
// ---------------------------------------------------------------------------

// flattenUnions merges anyOf/oneOf object variants into a single object schema.
func flattenUnions(schema map[string]any, depth int) map[string]any {
	if schema == nil || depth > maxSchemaDepth {
		return schema
	}
	for _, key := range []string{"anyOf", "oneOf"} {
		variants, ok := schema[key].([]any)
		if !ok || len(variants) == 0 {
			continue
		}
		// Try literal flatten first: [{const:"a"},{const:"b"}] → {enum:["a","b"]}
		if flattened, ok := flattenLiterals(variants); ok {
			merged := make(map[string]any)
			copyMeta(schema, merged)
			for k, v := range flattened {
				merged[k] = v
			}
			return flattenUnions(merged, depth+1)
		}
		// Try object merge: all variants are objects → merge properties.
		if merged, ok := mergeObjectVariants(variants); ok {
			result := make(map[string]any)
			copyMeta(schema, result)
			for k, v := range merged {
				result[k] = v
			}
			return flattenUnions(result, depth+1)
		}
	}
	return walkSchema(schema, func(child map[string]any) map[string]any {
		return flattenUnions(child, depth+1)
	})
}

// flattenLiterals converts [{const:"a"},{const:"b"}] → {type:T, enum:[a,b]}.
func flattenLiterals(variants []any) (map[string]any, bool) {
	var values []any
	var commonType string
	for _, v := range variants {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		var val any
		if c, ok := m["const"]; ok {
			val = c
		} else if e, ok := m["enum"].([]any); ok && len(e) == 1 {
			val = e[0]
		} else {
			return nil, false
		}
		t, _ := m["type"].(string)
		if t == "" {
			t = inferType(val) // infer from const value when type is omitted
			if t == "" {
				return nil, false
			}
		}
		if commonType == "" {
			commonType = t
		} else if commonType != t {
			return nil, false
		}
		values = append(values, val)
	}
	if len(values) == 0 {
		return nil, false
	}
	return map[string]any{"type": commonType, "enum": values}, true
}

// mergeObjectVariants merges object-typed variants: union properties, intersect required.
// NOTE: For duplicate property keys across variants, the first definition wins.
// This is lossy for discriminated unions (e.g. action:{const:"create"} vs action:{const:"delete"}),
// but acceptable because flattenLiterals handles the const-enum case first.
func mergeObjectVariants(variants []any) (map[string]any, bool) {
	mergedProps := make(map[string]any)
	requiredCounts := make(map[string]int)
	objectCount := 0

	for _, v := range variants {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		props, ok := m["properties"].(map[string]any)
		if !ok {
			return nil, false
		}
		objectCount++
		for k, val := range props {
			if _, exists := mergedProps[k]; !exists {
				mergedProps[k] = val
			}
		}
		if req, ok := m["required"].([]any); ok {
			for _, r := range req {
				if s, ok := r.(string); ok {
					requiredCounts[s]++
				}
			}
		}
	}
	if objectCount == 0 {
		return nil, false
	}
	// required = fields present in ALL variants
	var required []any
	for name, count := range requiredCounts {
		if count == objectCount {
			required = append(required, name)
		}
	}
	result := map[string]any{
		"type":       "object",
		"properties": mergedProps,
	}
	if len(required) > 0 {
		result["required"] = required
	}
	return result, true
}

// ---------------------------------------------------------------------------
// Const → enum conversion
// ---------------------------------------------------------------------------

func convertConst(schema map[string]any, depth int) map[string]any {
	if schema == nil || depth > maxSchemaDepth {
		return schema
	}
	if val, ok := schema["const"]; ok {
		schema["enum"] = []any{val}
		delete(schema, "const")
	}
	return walkSchema(schema, func(child map[string]any) map[string]any {
		return convertConst(child, depth+1)
	})
}

// ---------------------------------------------------------------------------
// type:"object" injection
// ---------------------------------------------------------------------------

func injectObjectType(schema map[string]any, depth int) map[string]any {
	if schema == nil || depth > maxSchemaDepth {
		return schema
	}
	if _, hasType := schema["type"]; !hasType {
		_, hasProps := schema["properties"]
		_, hasReq := schema["required"]
		hasUnion := false
		if _, ok := schema["anyOf"]; ok {
			hasUnion = true
		}
		if _, ok := schema["oneOf"]; ok {
			hasUnion = true
		}
		if (hasProps || hasReq) && !hasUnion {
			schema["type"] = "object"
		}
	}
	return walkSchema(schema, func(child map[string]any) map[string]any {
		return injectObjectType(child, depth+1)
	})
}

// ---------------------------------------------------------------------------
// Remove type when anyOf/oneOf present (Gemini conflict)
// ---------------------------------------------------------------------------

func removeTypeOnUnion(schema map[string]any, depth int) map[string]any {
	if schema == nil || depth > maxSchemaDepth {
		return schema
	}
	_, hasAnyOf := schema["anyOf"]
	_, hasOneOf := schema["oneOf"]
	if hasAnyOf || hasOneOf {
		delete(schema, "type")
	}
	return walkSchema(schema, func(child map[string]any) map[string]any {
		return removeTypeOnUnion(child, depth+1)
	})
}

// ---------------------------------------------------------------------------
// Key stripping (evolved from original cleanSchema)
// ---------------------------------------------------------------------------

func stripKeys(schema map[string]any, keys []string, depth int) map[string]any {
	if schema == nil || depth > maxSchemaDepth {
		return schema
	}
	result := make(map[string]any, len(schema))
	for k, v := range schema {
		if slices.Contains(keys, k) {
			continue
		}
		// Strip empty required arrays (Gemini rejects required:[])
		if k == "required" {
			if arr, ok := v.([]any); ok && len(arr) == 0 {
				continue
			}
		}
		switch val := v.(type) {
		case map[string]any:
			result[k] = stripKeys(val, keys, depth+1)
		case []any:
			result[k] = stripKeysSlice(val, keys, depth+1)
		default:
			result[k] = v
		}
	}
	return result
}

func stripKeysSlice(items []any, keys []string, depth int) []any {
	result := make([]any, len(items))
	for i, item := range items {
		if m, ok := item.(map[string]any); ok {
			result[i] = stripKeys(m, keys, depth+1)
		} else {
			result[i] = item
		}
	}
	return result
}
