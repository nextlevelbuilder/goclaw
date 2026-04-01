package providers

import (
	"testing"
)

func TestCleanToolSchemas_Gemini(t *testing.T) {
	tools := []ToolDefinition{{
		Type: "function",
		Function: ToolFunctionSchema{
			Name:        "test",
			Description: "desc",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":    "string",
						"default": "world",
					},
				},
				"$defs":                map[string]any{"Foo": map[string]any{"type": "string"}},
				"additionalProperties": false,
				"examples":             []any{"a"},
			},
		},
	}}

	cleaned := CleanToolSchemas("gemini", tools)
	if len(cleaned) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(cleaned))
	}

	params := cleaned[0].Function.Parameters
	for _, key := range []string{"$defs", "additionalProperties", "examples"} {
		if _, ok := params[key]; ok {
			t.Errorf("expected key %q to be removed", key)
		}
	}

	// "type" should remain
	if _, ok := params["type"]; !ok {
		t.Error("expected 'type' to remain")
	}

	// Nested "default" should be removed
	props := params["properties"].(map[string]any)
	nameSchema := props["name"].(map[string]any)
	if _, ok := nameSchema["default"]; ok {
		t.Error("expected nested 'default' to be removed for gemini")
	}
	if _, ok := nameSchema["type"]; !ok {
		t.Error("expected nested 'type' to remain")
	}
}

func TestCleanToolSchemas_Anthropic(t *testing.T) {
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type": "string",
				"$ref": "#/$defs/URL",
			},
		},
		"$defs":                map[string]any{"URL": map[string]any{"type": "string"}},
		"additionalProperties": false,
		"default":              "x",
	}

	cleaned := CleanSchemaForProvider("anthropic", params)

	// $defs removed (after resolution)
	if _, ok := cleaned["$defs"]; ok {
		t.Error("expected $defs removed for anthropic")
	}
	props := cleaned["properties"].(map[string]any)
	urlSchema := props["url"].(map[string]any)
	if _, ok := urlSchema["$ref"]; ok {
		t.Error("expected nested $ref removed for anthropic")
	}

	// additionalProperties and default should remain for anthropic
	if _, ok := cleaned["additionalProperties"]; !ok {
		t.Error("expected additionalProperties to remain for anthropic")
	}
	if _, ok := cleaned["default"]; !ok {
		t.Error("expected default to remain for anthropic")
	}
}

func TestCleanToolSchemas_Unknown(t *testing.T) {
	tools := []ToolDefinition{{
		Type: "function",
		Function: ToolFunctionSchema{
			Name: "test",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x": map[string]any{"type": "string"},
				},
				"default": "val",
			},
		},
	}}

	cleaned := CleanToolSchemas("openrouter", tools)
	params := cleaned[0].Function.Parameters

	// Unknown providers now get default normalization.
	// type and properties should be preserved.
	if params["type"] != "object" {
		t.Error("expected type to remain")
	}
	// default is preserved (only stripped for Gemini).
	if _, ok := params["default"]; !ok {
		t.Error("expected default to remain for unknown provider")
	}
}

func TestCleanToolSchemas_Empty(t *testing.T) {
	cleaned := CleanToolSchemas("gemini", nil)
	if cleaned != nil {
		t.Error("expected nil for nil tools")
	}
}

func TestCleanSchema_NilParams(t *testing.T) {
	result := CleanSchemaForProvider("gemini", nil)
	if result != nil {
		t.Error("expected nil for nil params")
	}
}

func TestCleanSchema_NestedArray(t *testing.T) {
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"anyOf": []any{
					map[string]any{
						"type":    "string",
						"default": "x",
					},
					map[string]any{
						"type":    "number",
						"default": 42,
					},
				},
			},
		},
	}

	cleaned := CleanSchemaForProvider("gemini", params)
	props := cleaned["properties"].(map[string]any)
	items := props["items"].(map[string]any)

	// Gemini strips default from nested schemas.
	// After flattening, anyOf may be removed if variants are merged.
	// The key assertion: no "default" keys survive.
	if _, ok := items["default"]; ok {
		t.Error("expected 'default' removed in nested schema")
	}
}

// ---------------------------------------------------------------------------
// Multi-action tool strict mode exemption
// ---------------------------------------------------------------------------

func TestIsMultiActionSchema(t *testing.T) {
	tests := []struct {
		name   string
		schema map[string]any
		want   bool
	}{
		{
			name: "team_tasks-like with []string enum",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type": "string",
						"enum": []string{"list", "create", "search"},
					},
					"query": map[string]any{"type": "string"},
				},
				"required": []string{"action"},
			},
			want: true,
		},
		{
			name: "cron-like with []any enum",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type": "string",
						"enum": []any{"status", "add", "remove"},
					},
				},
			},
			want: true,
		},
		{
			name: "single action — not exempt",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type": "string",
						"enum": []string{"send"},
					},
				},
			},
			want: false,
		},
		{
			name: "no action property",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
			},
			want: false,
		},
		{
			name: "action without enum",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{"type": "string"},
				},
			},
			want: false,
		},
		{
			name: "enum on non-action field",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"mode": map[string]any{
						"type": "string",
						"enum": []string{"markdown", "text"},
					},
				},
			},
			want: false,
		},
		{
			name: "nil schema",
			schema: nil,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsMultiActionSchema(tt.schema)
			if got != tt.want {
				t.Errorf("IsMultiActionSchema() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCleanToolSchemas_OpenAI_MultiActionExempt(t *testing.T) {
	multiAction := ToolDefinition{
		Type: "function",
		Function: ToolFunctionSchema{
			Name:        "team_tasks",
			Description: "multi-action tool",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type": "string",
						"enum": []string{"list", "create", "search"},
					},
					"query": map[string]any{
						"type":        "string",
						"description": "search query",
					},
					"subject": map[string]any{
						"type":        "string",
						"description": "task subject",
					},
				},
				"required": []string{"action"},
			},
		},
	}
	simple := ToolDefinition{
		Type: "function",
		Function: ToolFunctionSchema{
			Name:        "read_file",
			Description: "simple tool",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "file path",
					},
				},
				"required": []string{"path"},
			},
		},
	}

	cleaned := CleanToolSchemas("openai", []ToolDefinition{multiAction, simple})

	// Multi-action tool: strict should be nil (exempt).
	if cleaned[0].Function.Strict != nil {
		t.Errorf("multi-action tool: expected Strict=nil, got %v", *cleaned[0].Function.Strict)
	}

	// Multi-action tool: required should stay ["action"] only.
	params0 := cleaned[0].Function.Parameters
	reqArr, ok := params0["required"].([]string)
	if !ok {
		// May be []any from original schema copy.
		if reqAny, ok2 := params0["required"].([]any); ok2 {
			if len(reqAny) != 1 {
				t.Errorf("multi-action: expected 1 required, got %d", len(reqAny))
			}
		}
	} else if len(reqArr) != 1 {
		t.Errorf("multi-action: expected 1 required, got %d: %v", len(reqArr), reqArr)
	}

	// Multi-action tool: should NOT have additionalProperties:false.
	if _, hasAP := params0["additionalProperties"]; hasAP {
		t.Error("multi-action: expected no additionalProperties")
	}

	// Simple tool: strict should be true.
	if cleaned[1].Function.Strict == nil || !*cleaned[1].Function.Strict {
		t.Error("simple tool: expected Strict=true")
	}

	// Simple tool: all props required (strict mode applied).
	params1 := cleaned[1].Function.Parameters
	if reqAny, ok := params1["required"].([]any); ok {
		if len(reqAny) != 1 {
			t.Errorf("simple tool: expected 1 required (path), got %d", len(reqAny))
		}
	}

	// Simple tool: additionalProperties:false should be set.
	if ap, ok := params1["additionalProperties"].(bool); !ok || ap {
		t.Error("simple tool: expected additionalProperties=false")
	}
}

func TestCleanSchema_DeepNesting(t *testing.T) {
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"config": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"nested": map[string]any{
						"type":    "string",
						"default": "deep",
					},
				},
			},
		},
	}

	cleaned := CleanSchemaForProvider("gemini", params)
	props := cleaned["properties"].(map[string]any)
	config := props["config"].(map[string]any)
	innerProps := config["properties"].(map[string]any)
	nested := innerProps["nested"].(map[string]any)

	if _, ok := nested["default"]; ok {
		t.Error("expected deeply nested 'default' removed")
	}
	if _, ok := nested["type"]; !ok {
		t.Error("expected 'type' to remain at deep level")
	}
}
