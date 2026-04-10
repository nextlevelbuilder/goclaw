package providers

// schema_normalize.go — entry point + ref resolution, null stripping, const/type/union transforms.
// Union flattening + key stripping live in schema_transforms.go.
// Shared helpers live in schema_helpers.go.

import "maps"

// maxSchemaDepth prevents stack overflow from malicious deeply-nested schemas.
const maxSchemaDepth = 64

// NormalizeSchema applies provider-specific normalization to a tool's JSON Schema.
// This is the single entry point — all providers should call this (directly or
// via CleanToolSchemas / CleanSchemaForProvider wrappers).
func NormalizeSchema(providerName string, schema map[string]any) map[string]any {
	return normalizeWithProfile(profileForProvider(providerName), schema)
}

// normalizeWithProfile applies normalization using a pre-resolved profile.
// Used by CleanToolSchemas to pass per-tool profile overrides.
func normalizeWithProfile(profile SchemaProfile, schema map[string]any) map[string]any {
	if schema == nil {
		return nil
	}
	result := copySchema(schema)

	if profile.ResolveRefs {
		defs := collectDefs(result)
		result = resolveRefs(result, defs, nil, 0)
	}
	if profile.StripNullType {
		result = stripNullVariants(result, 0)
	}
	if profile.FlattenUnions {
		result = flattenUnions(result, 0)
	}
	if profile.ConvertConst {
		result = convertConst(result, 0)
	}
	if profile.InjectObjectType {
		result = injectObjectType(result, 0)
	}
	if profile.RemoveTypeOnUnion {
		result = removeTypeOnUnion(result, 0)
	}
	if len(profile.StripKeys) > 0 {
		result = stripKeys(result, profile.StripKeys, 0)
	}
	if profile.StrictToolMode {
		result = applyStrictMode(result, 0)
	}
	return result
}

// ---------------------------------------------------------------------------
// $ref resolution
// ---------------------------------------------------------------------------

// collectDefs extracts "$defs" and "definitions" into a flat lookup map.
func collectDefs(schema map[string]any) map[string]any {
	defs := make(map[string]any)
	for _, key := range []string{"$defs", "definitions"} {
		if block, ok := schema[key].(map[string]any); ok {
			maps.Copy(defs, block)
		}
	}
	return defs
}

// resolveRefs inlines local $ref pointers from the defs map.
// visited tracks ref paths to break circular references.
func resolveRefs(schema map[string]any, defs map[string]any, visited map[string]bool, depth int) map[string]any {
	if schema == nil || depth > maxSchemaDepth {
		return schema
	}
	if ref, ok := schema["$ref"].(string); ok {
		if visited[ref] {
			return map[string]any{"type": "object", "description": "circular reference"}
		}
		name := refName(ref)
		if resolved, ok := defs[name]; ok {
			if m, ok := resolved.(map[string]any); ok {
				next := copyVisited(visited)
				next[ref] = true
				out := resolveRefs(copySchema(m), defs, next, depth+1)
				copyMeta(schema, out) // preserve parent description/title
				return out
			}
		}
		// Unresolvable ref — keep metadata only.
		out := make(map[string]any)
		copyMeta(schema, out)
		return out
	}
	return walkSchema(schema, func(child map[string]any) map[string]any {
		return resolveRefs(child, defs, visited, depth+1)
	})
}

// ---------------------------------------------------------------------------
// Null variant stripping
// ---------------------------------------------------------------------------

// stripNullVariants simplifies anyOf/oneOf:[T, null] → T (recursive).
func stripNullVariants(schema map[string]any, depth int) map[string]any {
	if schema == nil || depth > maxSchemaDepth {
		return schema
	}
	for _, key := range []string{"anyOf", "oneOf"} {
		variants, ok := schema[key].([]any)
		if !ok || len(variants) == 0 {
			continue
		}
		nonNull := make([]any, 0, len(variants))
		for _, v := range variants {
			if m, ok := v.(map[string]any); ok && isNullSchema(m) {
				continue
			}
			nonNull = append(nonNull, v)
		}
		if len(nonNull) == len(variants) {
			continue // nothing stripped
		}
		if len(nonNull) == 1 {
			if m, ok := nonNull[0].(map[string]any); ok {
				// Unwrap single non-null variant, preserving parent metadata.
				merged := copySchema(m)
				copyMeta(schema, merged)
				return stripNullVariants(merged, depth+1)
			}
		}
		schema[key] = nonNull
	}
	// Handle type:["string","null"] → type:"string"
	if typeArr, ok := schema["type"].([]any); ok {
		filtered := make([]any, 0, len(typeArr))
		for _, t := range typeArr {
			if s, ok := t.(string); ok && s == "null" {
				continue
			}
			filtered = append(filtered, t)
		}
		if len(filtered) == 1 {
			schema["type"] = filtered[0]
		} else if len(filtered) != len(typeArr) {
			schema["type"] = filtered
		}
	}
	return walkSchema(schema, func(child map[string]any) map[string]any {
		return stripNullVariants(child, depth+1)
	})
}
