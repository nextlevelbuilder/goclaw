package providers

import "testing"

// Native tools from another provider can reach Anthropic through model
// fallback. Anthropic has no equivalent, so they are dropped from the request.
func TestAnthropicRequestSkipsToolsWithoutFunction(t *testing.T) {
	p := NewAnthropicProvider("test-key")

	t.Run("keeps function tools", func(t *testing.T) {
		body := p.buildRequestBody("claude-test", ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			Tools: []ToolDefinition{
				{Type: "function", Function: &ToolFunctionSchema{Name: "datetime"}},
				{Type: "image_generation"},
			},
		}, false)
		tools, ok := body["tools"].([]map[string]any)
		if !ok || len(tools) != 1 || tools[0]["name"] != "datetime" {
			t.Fatalf("tools = %v, want only datetime", body["tools"])
		}
	})

	t.Run("omits tools when none remain", func(t *testing.T) {
		body := p.buildRequestBody("claude-test", ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			Tools:    []ToolDefinition{{Type: "image_generation"}},
		}, false)
		if _, ok := body["tools"]; ok {
			t.Fatalf("tools = %v, want no tools key", body["tools"])
		}
	})
}
