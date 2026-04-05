package providers

// schema_strict.go — OpenAI strict tool mode transform.
// Converts optional properties to nullable unions, requires all properties,
// and sets additionalProperties:false so constrained decoding can enforce schema compliance.

// applyStrictMode transforms a tool's JSON Schema for OpenAI strict mode.
// - Optional properties (not in "required") get their type changed to ["<type>", "null"]
// - ALL property names are added to "required"
// - "additionalProperties": false is set on object schemas
// This is applied recursively to nested object schemas.
func applyStrictMode(schema map[string]any, depth int) map[string]any {
	if schema == nil || depth > maxSchemaDepth {
		return schema
	}

	typ, _ := schema["type"].(string)
	props, hasProps := schema["properties"].(map[string]any)

	if typ != "object" || !hasProps {
		return schema
	}

	// Build the set of currently required properties.
	reqSet := make(map[string]bool)
	if reqArr, ok := schema["required"].([]any); ok {
		for _, r := range reqArr {
			if s, ok := r.(string); ok {
				reqSet[s] = true
			}
		}
	}
	if reqArr, ok := schema["required"].([]string); ok {
		for _, s := range reqArr {
			reqSet[s] = true
		}
	}

	// Collect all property names for the new required array.
	allRequired := make([]any, 0, len(props))

	for name, prop := range props {
		allRequired = append(allRequired, name)

		pm, ok := prop.(map[string]any)
		if !ok {
			continue
		}

		// Recurse into nested objects first.
		pm = applyStrictMode(pm, depth+1)
		props[name] = pm

		// Recurse into array items.
		if items, ok := pm["items"].(map[string]any); ok {
			pm["items"] = applyStrictMode(items, depth+1)
		}

		// Already required — no need to make nullable.
		if reqSet[name] {
			continue
		}

		// Make optional property nullable: type:"string" → type:["string","null"]
		makeNullable(pm)
	}

	schema["properties"] = props
	schema["required"] = allRequired
	schema["additionalProperties"] = false

	return schema
}

// IsMultiActionSchema detects tool schemas using the multi-action pattern:
// a top-level "action" property with an "enum" list of 2+ values.
// These tools have many optional params shared across actions;
// strict mode forces models to send all params for every call.
func IsMultiActionSchema(schema map[string]any) bool {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return false
	}
	actionProp, ok := props["action"].(map[string]any)
	if !ok {
		return false
	}
	enumVal, ok := actionProp["enum"]
	if !ok {
		return false
	}
	// Only exempt if 2+ actions — single-action tools benefit from strict.
	switch e := enumVal.(type) {
	case []string:
		return len(e) >= 2
	case []any:
		return len(e) >= 2
	}
	return false
}

// makeNullable converts a property schema to accept null values.
// - Simple type: "string" → ["string", "null"]
// - Type array: ["string", "integer"] → ["string", "integer", "null"]
// - anyOf/oneOf: appends {"type":"null"} variant
// - No type: adds type:["null"] (fallback)
func makeNullable(schema map[string]any) {
	// Already has null variant in anyOf/oneOf — skip.
	for _, key := range []string{"anyOf", "oneOf"} {
		if variants, ok := schema[key].([]any); ok {
			for _, v := range variants {
				if m, ok := v.(map[string]any); ok && isNullSchema(m) {
					return // already nullable
				}
			}
			// Append null variant.
			schema[key] = append(variants, map[string]any{"type": "null"})
			return
		}
	}

	typ, hasType := schema["type"]

	switch t := typ.(type) {
	case string:
		if t == "null" {
			return // already null
		}
		schema["type"] = []any{t, "null"}
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok && s == "null" {
				return // already has null
			}
		}
		schema["type"] = append(t, "null")
	default:
		if !hasType {
			// No type field — add nullable object as fallback.
			schema["type"] = []any{"object", "null"}
		}
	}
}
