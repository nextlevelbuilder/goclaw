package providers

import (
	"testing"
)

// TestBuildToolsPayload_NativeOnly verifies that a single native "image_generation" tool
// is serialized as bare {"type":"image_generation"} without a "function" wrapper.
func TestBuildToolsPayload_NativeOnly(t *testing.T) {
	tools := []ToolDefinition{
		{Type: "image_generation"},
	}
	got := buildToolsPayload("openai", tools)
	if len(got) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(got))
	}
	if got[0]["type"] != "image_generation" {
		t.Errorf("type = %q, want image_generation", got[0]["type"])
	}
	if _, hasFunc := got[0]["function"]; hasFunc {
		t.Error("native tool must not have 'function' field")
	}
	if len(got[0]) != 1 {
		t.Errorf("native tool payload should have exactly 1 key, got %d: %v", len(got[0]), got[0])
	}
}

// TestBuildToolsPayload_MixedOrder verifies that mixed function + native tools preserve
// insertion order and each is serialized correctly.
func TestBuildToolsPayload_MixedOrder(t *testing.T) {
	tools := []ToolDefinition{
		{
			Type: "function",
			Function: &ToolFunctionSchema{
				Name:        "read_file",
				Description: "read a file",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string"},
					},
					"required": []string{"path"},
				},
			},
		},
		{Type: "image_generation"},
		{
			Type: "function",
			Function: &ToolFunctionSchema{
				Name:        "write_file",
				Description: "write a file",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":    map[string]any{"type": "string"},
						"content": map[string]any{"type": "string"},
					},
					"required": []string{"path", "content"},
				},
			},
		},
	}

	got := buildToolsPayload("openrouter", tools)
	if len(got) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(got))
	}

	// Position 0: function tool
	if got[0]["type"] != "function" {
		t.Errorf("got[0] type = %q, want function", got[0]["type"])
	}
	fn0, ok := got[0]["function"].(map[string]any)
	if !ok {
		t.Fatalf("got[0]['function'] is not map[string]any: %T", got[0]["function"])
	}
	if fn0["name"] != "read_file" {
		t.Errorf("got[0] function.name = %q, want read_file", fn0["name"])
	}

	// Position 1: native tool
	if got[1]["type"] != "image_generation" {
		t.Errorf("got[1] type = %q, want image_generation", got[1]["type"])
	}
	if _, hasFunc := got[1]["function"]; hasFunc {
		t.Error("native tool at position 1 must not have 'function' field")
	}

	// Position 2: function tool
	if got[2]["type"] != "function" {
		t.Errorf("got[2] type = %q, want function", got[2]["type"])
	}
	fn2, ok := got[2]["function"].(map[string]any)
	if !ok {
		t.Fatalf("got[2]['function'] is not map[string]any: %T", got[2]["function"])
	}
	if fn2["name"] != "write_file" {
		t.Errorf("got[2] function.name = %q, want write_file", fn2["name"])
	}
}

// TestBuildToolsPayload_FunctionToolByteIdentical verifies existing function-only paths
// produce the expected function wrapper with name, description, and parameters fields.
func TestBuildToolsPayload_FunctionToolByteIdentical(t *testing.T) {
	tools := []ToolDefinition{
		{
			Type: "function",
			Function: &ToolFunctionSchema{
				Name:        "get_weather",
				Description: "Get current weather",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []string{"city"},
				},
			},
		},
	}

	got := buildToolsPayload("openrouter", tools)
	if len(got) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(got))
	}
	if got[0]["type"] != "function" {
		t.Errorf("type = %q, want function", got[0]["type"])
	}
	fn, ok := got[0]["function"].(map[string]any)
	if !ok {
		t.Fatalf("'function' is not map[string]any: %T", got[0]["function"])
	}
	if fn["name"] != "get_weather" {
		t.Errorf("name = %q, want get_weather", fn["name"])
	}
	if fn["description"] != "Get current weather" {
		t.Errorf("description = %q, want 'Get current weather'", fn["description"])
	}
	if fn["parameters"] == nil {
		t.Error("parameters should not be nil")
	}
}

// TestBuildToolsPayload_NilFunctionSkipped verifies a malformed function tool
// (Type="function" with nil Function) is silently skipped.
func TestBuildToolsPayload_NilFunctionSkipped(t *testing.T) {
	tools := []ToolDefinition{
		{Type: "function", Function: nil}, // malformed
		{Type: "image_generation"},
	}
	got := buildToolsPayload("openai", tools)
	if len(got) != 1 {
		t.Fatalf("expected 1 tool (malformed skipped), got %d", len(got))
	}
	if got[0]["type"] != "image_generation" {
		t.Errorf("expected image_generation tool, got type=%q", got[0]["type"])
	}
}

// TestBuildRequestBody_NativeToolInBody verifies that buildRequestBody emits
// a native tool correctly in the request body via the tools key.
func TestBuildRequestBody_NativeToolInBody(t *testing.T) {
	p := NewOpenAIProvider("openai", "sk-test", "https://api.openai.com/v1", "gpt-4o")
	req := ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hello"}},
		Tools: []ToolDefinition{
			{Type: "image_generation"},
		},
	}
	body := p.buildRequestBody("gpt-4o", req, false)
	rawTools, ok := body["tools"]
	if !ok {
		t.Fatal("body missing 'tools' key")
	}
	tools, ok := rawTools.([]map[string]any)
	if !ok {
		t.Fatalf("tools is not []map[string]any: %T", rawTools)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0]["type"] != "image_generation" {
		t.Errorf("type = %q, want image_generation", tools[0]["type"])
	}
	if _, hasFunc := tools[0]["function"]; hasFunc {
		t.Error("native tool must not have 'function' field in request body")
	}
}
