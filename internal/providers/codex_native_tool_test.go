package providers

import "testing"

// Only function tools and image_generation are sent to Codex. Any other native
// tool, or a function tool without a definition, is dropped from the request.
func TestCodexRequestSkipsUnsupportedTools(t *testing.T) {
	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, "", "gpt-4o")

	t.Run("keeps supported tools", func(t *testing.T) {
		body := p.buildRequestBody(ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			Tools: []ToolDefinition{
				{Type: "function", Function: &ToolFunctionSchema{Name: "datetime"}},
				{Type: "function"},
				{Type: "web_search"},
				{Type: "image_generation"},
			},
		}, false)
		tools, ok := body["tools"].([]map[string]any)
		if !ok || len(tools) != 2 || tools[0]["name"] != "datetime" || tools[1]["type"] != "image_generation" {
			t.Fatalf("tools = %v, want datetime and image_generation", body["tools"])
		}
	})

	t.Run("omits tools when none remain", func(t *testing.T) {
		body := p.buildRequestBody(ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			Tools:    []ToolDefinition{{Type: "web_search"}},
		}, false)
		if _, ok := body["tools"]; ok {
			t.Fatalf("tools = %v, want no tools key", body["tools"])
		}
	})
}
