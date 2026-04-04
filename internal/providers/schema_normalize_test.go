package providers

import (
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// $ref resolution
// ---------------------------------------------------------------------------

func TestResolveRefs_Simple(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"addr": map[string]any{"$ref": "#/$defs/Address"},
		},
		"$defs": map[string]any{
			"Address": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"street": map[string]any{"type": "string"},
				},
			},
		},
	}
	result := NormalizeSchema("openai", schema)
	addr := prop(result, "addr")
	if addr == nil {
		t.Fatal("expected addr property")
	}
	if _, ok := addr["$ref"]; ok {
		t.Error("$ref should be resolved")
	}
	if prop(addr, "street") == nil {
		t.Error("expected inlined street property from resolved ref")
	}
}

func TestResolveRefs_Circular(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"node": map[string]any{"$ref": "#/$defs/Node"},
		},
		"$defs": map[string]any{
			"Node": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"child": map[string]any{"$ref": "#/$defs/Node"},
				},
			},
		},
	}
	// Use "anthropic" to test ref resolution in isolation (no strict mode transform).
	result := NormalizeSchema("anthropic", schema)
	node := prop(result, "node")
	if node == nil {
		t.Fatal("expected node property")
	}
	// Circular child should be a stub, not infinite.
	child := prop(node, "child")
	if child == nil {
		t.Fatal("expected child property (circular stub)")
	}
	if child["type"] != "object" {
		t.Error("circular stub should have type:object")
	}
}

func TestResolveRefs_LegacyDefinitions(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"item": map[string]any{"$ref": "#/definitions/Item"},
		},
		"definitions": map[string]any{
			"Item": map[string]any{"type": "string"},
		},
	}
	// Use "anthropic" to test ref resolution in isolation (no strict mode transform).
	result := NormalizeSchema("anthropic", schema)
	item := prop(result, "item")
	if item == nil || item["type"] != "string" {
		t.Error("expected definitions/ ref resolved to string type")
	}
}

// ---------------------------------------------------------------------------
// Null variant stripping
// ---------------------------------------------------------------------------

func TestStripNullVariants_AnyOf(t *testing.T) {
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "null"},
		},
	}
	// Gemini profile strips null variants.
	result := NormalizeSchema("gemini", schema)
	if _, ok := result["anyOf"]; ok {
		t.Error("expected anyOf removed after null stripping + flattening")
	}
	if result["type"] != "string" {
		t.Errorf("expected type:string, got %v", result["type"])
	}
}

func TestStripNullVariants_TypeArray(t *testing.T) {
	schema := map[string]any{
		"type": []any{"string", "null"},
	}
	result := NormalizeSchema("gemini", schema)
	if result["type"] != "string" {
		t.Errorf("expected type:string, got %v", result["type"])
	}
}

// ---------------------------------------------------------------------------
// Union flattening
// ---------------------------------------------------------------------------

func TestFlattenUnions_ObjectMerge(t *testing.T) {
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{
				"type":       "object",
				"properties": map[string]any{"action": map[string]any{"const": "create"}, "name": map[string]any{"type": "string"}},
				"required":   []any{"action", "name"},
			},
			map[string]any{
				"type":       "object",
				"properties": map[string]any{"action": map[string]any{"const": "delete"}, "id": map[string]any{"type": "number"}},
				"required":   []any{"action", "id"},
			},
		},
	}
	result := NormalizeSchema("openai", schema)
	if result["type"] != "object" {
		t.Errorf("expected type:object, got %v", result["type"])
	}
	props, _ := result["properties"].(map[string]any)
	if props == nil {
		t.Fatal("expected merged properties")
	}
	for _, key := range []string{"action", "name", "id"} {
		if props[key] == nil {
			t.Errorf("expected property %q in merged result", key)
		}
	}
	req, _ := result["required"].([]any)
	// "action" is required in both variants → should be in intersection.
	found := false
	for _, r := range req {
		if r == "action" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'action' in required (intersection of both variants)")
	}
}

func TestFlattenUnions_LiteralConstNoType(t *testing.T) {
	// M4: variants with const but no explicit type — type should be inferred.
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"const": "x"},
			map[string]any{"const": "y"},
		},
	}
	result := NormalizeSchema("openai", schema)
	if result["type"] != "string" {
		t.Errorf("expected inferred type:string, got %v", result["type"])
	}
	enum, ok := result["enum"].([]any)
	if !ok || len(enum) != 2 {
		t.Fatalf("expected enum with 2 values, got %v", result["enum"])
	}
}

func TestFlattenUnions_LiteralConst(t *testing.T) {
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"const": "a", "type": "string"},
			map[string]any{"const": "b", "type": "string"},
		},
	}
	result := NormalizeSchema("openai", schema)
	if result["type"] != "string" {
		t.Errorf("expected type:string, got %v", result["type"])
	}
	enum, ok := result["enum"].([]any)
	if !ok || len(enum) != 2 {
		t.Fatalf("expected enum with 2 values, got %v", result["enum"])
	}
	if enum[0] != "a" || enum[1] != "b" {
		t.Errorf("expected enum [a,b], got %v", enum)
	}
}

// ---------------------------------------------------------------------------
// Const → enum
// ---------------------------------------------------------------------------

func TestConvertConst(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"mode": map[string]any{"const": "fast"},
		},
	}
	result := NormalizeSchema("gemini", schema)
	mode := prop(result, "mode")
	if mode == nil {
		t.Fatal("expected mode property")
	}
	if _, ok := mode["const"]; ok {
		t.Error("const should be converted to enum")
	}
	enum, ok := mode["enum"].([]any)
	if !ok || len(enum) != 1 || enum[0] != "fast" {
		t.Errorf("expected enum:[fast], got %v", mode["enum"])
	}
}

// ---------------------------------------------------------------------------
// type:"object" injection
// ---------------------------------------------------------------------------

func TestInjectObjectType(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []any{"name"},
	}
	result := NormalizeSchema("openai", schema)
	if result["type"] != "object" {
		t.Errorf("expected type:object injected, got %v", result["type"])
	}
}

func TestInjectObjectType_SkipsWhenTypeExists(t *testing.T) {
	schema := map[string]any{
		"type": "array",
		"properties": map[string]any{
			"x": map[string]any{"type": "string"},
		},
	}
	result := NormalizeSchema("openai", schema)
	if result["type"] != "array" {
		t.Error("should not overwrite existing type")
	}
}

// ---------------------------------------------------------------------------
// Remove type on union (Gemini)
// ---------------------------------------------------------------------------

func TestRemoveTypeOnUnion(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "number"},
		},
	}
	result := NormalizeSchema("gemini", schema)
	if _, ok := result["type"]; ok {
		t.Error("type should be removed when anyOf present (Gemini)")
	}
}

// ---------------------------------------------------------------------------
// Key stripping
// ---------------------------------------------------------------------------

func TestStripKeys_Gemini(t *testing.T) {
	schema := map[string]any{
		"type":    "object",
		"minimum": 1.0,
		"maximum": 10.0,
		"format":  "int32",
		"pattern": "^[a-z]+$",
		"properties": map[string]any{
			"count": map[string]any{
				"type":      "number",
				"minimum":   0.0,
				"maxLength": 100,
			},
		},
	}
	result := NormalizeSchema("gemini", schema)
	for _, key := range []string{"minimum", "maximum", "format", "pattern"} {
		if _, ok := result[key]; ok {
			t.Errorf("expected %q stripped for Gemini", key)
		}
	}
	count := prop(result, "count")
	if count == nil {
		t.Fatal("expected count property")
	}
	if _, ok := count["minimum"]; ok {
		t.Error("expected nested minimum stripped")
	}
	if _, ok := count["maxLength"]; ok {
		t.Error("expected nested maxLength stripped")
	}
}

func TestStripKeys_EmptyRequired(t *testing.T) {
	schema := map[string]any{
		"type":       "object",
		"required":   []any{},
		"properties": map[string]any{},
	}
	result := NormalizeSchema("gemini", schema)
	if _, ok := result["required"]; ok {
		t.Error("expected empty required:[] stripped for Gemini")
	}
}

// ---------------------------------------------------------------------------
// End-to-end per provider
// ---------------------------------------------------------------------------

func TestNormalizeSchema_Anthropic(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{"$ref": "#/$defs/URL"},
		},
		"$defs": map[string]any{"URL": map[string]any{"type": "string", "format": "uri"}},
		"anyOf": []any{map[string]any{"type": "object"}, map[string]any{"type": "null"}},
	}
	result := NormalizeSchema("anthropic", schema)
	// $ref resolved
	url := prop(result, "url")
	if url == nil || url["type"] != "string" {
		t.Error("expected $ref resolved to string type")
	}
	// format preserved (Anthropic supports it)
	if url["format"] != "uri" {
		t.Error("expected format preserved for Anthropic")
	}
	// anyOf preserved (Anthropic handles unions natively)
	if _, ok := result["anyOf"]; !ok {
		t.Error("expected anyOf preserved for Anthropic (no flatten)")
	}
}

func TestNormalizeSchema_Codex(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	result := NormalizeSchema("codex", schema)
	if result["type"] != "object" {
		t.Error("expected type:object injected for Codex")
	}
}

func TestNormalizeSchema_XAI(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":      "string",
				"minLength": 1,
				"maxLength": 500,
			},
		},
	}
	result := NormalizeSchema("xai", schema)
	query := prop(result, "query")
	if query == nil {
		t.Fatal("expected query property")
	}
	if _, ok := query["minLength"]; ok {
		t.Error("expected minLength stripped for xAI")
	}
	if _, ok := query["maxLength"]; ok {
		t.Error("expected maxLength stripped for xAI")
	}
	if query["type"] != "string" {
		t.Error("expected type preserved")
	}
}

func TestNormalizeSchema_Nil(t *testing.T) {
	if result := NormalizeSchema("openai", nil); result != nil {
		t.Error("expected nil for nil schema")
	}
}

func TestNormalizeSchema_DoesNotMutateOriginal(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"x": map[string]any{"$ref": "#/$defs/X"},
		},
		"$defs": map[string]any{"X": map[string]any{"type": "string"}},
	}
	original, _ := json.Marshal(schema)
	_ = NormalizeSchema("openai", schema)
	after, _ := json.Marshal(schema)
	if string(original) != string(after) {
		t.Error("NormalizeSchema should not mutate the original schema")
	}
}

// ---------------------------------------------------------------------------
// Strict tool mode
// ---------------------------------------------------------------------------

func TestApplyStrictMode_Basic(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":     map[string]any{"type": "string", "description": "The URL"},
			"api_key": map[string]any{"type": "string", "description": "API key"},
			"timeout": map[string]any{"type": "number", "description": "Timeout"},
		},
		"required": []any{"url"},
	}
	result := NormalizeSchema("openai", schema)

	// All properties should be required.
	reqArr, _ := result["required"].([]any)
	reqSet := make(map[string]bool, len(reqArr))
	for _, r := range reqArr {
		reqSet[r.(string)] = true
	}
	for _, name := range []string{"url", "api_key", "timeout"} {
		if !reqSet[name] {
			t.Errorf("expected %q in required array", name)
		}
	}

	// additionalProperties should be false.
	if result["additionalProperties"] != false {
		t.Error("expected additionalProperties:false")
	}

	// Required param 'url' should keep original type.
	urlProp := prop(result, "url")
	if urlProp["type"] != "string" {
		t.Errorf("expected url type:string, got %v", urlProp["type"])
	}

	// Optional params should be nullable.
	apiKeyProp := prop(result, "api_key")
	apiKeyType, ok := apiKeyProp["type"].([]any)
	if !ok {
		t.Fatalf("expected api_key type to be array, got %T: %v", apiKeyProp["type"], apiKeyProp["type"])
	}
	hasNull := false
	for _, v := range apiKeyType {
		if v == "null" {
			hasNull = true
		}
	}
	if !hasNull {
		t.Error("expected api_key type to include 'null'")
	}

	timeoutProp := prop(result, "timeout")
	timeoutType, ok := timeoutProp["type"].([]any)
	if !ok {
		t.Fatalf("expected timeout type to be array, got %T", timeoutProp["type"])
	}
	hasNull = false
	for _, v := range timeoutType {
		if v == "null" {
			hasNull = true
		}
	}
	if !hasNull {
		t.Error("expected timeout type to include 'null'")
	}
}

func TestApplyStrictMode_NestedObject(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"config": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": map[string]any{"type": "string"},
				},
			},
		},
		"required": []any{},
	}
	result := NormalizeSchema("openai", schema)

	// Nested object should also have additionalProperties:false.
	config := prop(result, "config")
	if config == nil {
		t.Fatal("expected config property")
	}
	if config["additionalProperties"] != false {
		t.Error("expected nested object to have additionalProperties:false")
	}
}

func TestApplyStrictMode_SkipsAnthropic(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":    map[string]any{"type": "string"},
			"debug":  map[string]any{"type": "boolean"},
		},
		"required": []any{"url"},
	}
	result := NormalizeSchema("anthropic", schema)

	// Anthropic should NOT get strict mode transforms.
	if result["additionalProperties"] == false {
		t.Error("Anthropic should not have additionalProperties:false")
	}
	debugProp := prop(result, "debug")
	if debugProp["type"] != "boolean" {
		t.Error("Anthropic should keep original type (no nullable transform)")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func prop(schema map[string]any, name string) map[string]any {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}
	m, _ := props[name].(map[string]any)
	return m
}
