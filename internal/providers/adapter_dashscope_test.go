package providers

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDashScopeAdapter_Basics verifies:
// - Name() returns "dashscope" (NOT openai — wire format differs conceptually)
// - StreamWithTools is overridden to false (DashScope falls back to non-stream for tool calls)
// - Other capabilities inherit from OpenAI
func TestDashScopeAdapter_Basics(t *testing.T) {
	a, err := NewDashScopeAdapter(ProviderConfig{APIKey: "sk-ds"})
	if err != nil {
		t.Fatalf("NewDashScopeAdapter error: %v", err)
	}
	if a.Name() != "dashscope" {
		t.Errorf("Name() = %q, want dashscope", a.Name())
	}
	caps := a.Capabilities()
	if caps.StreamWithTools {
		t.Error("DashScope must NOT support StreamWithTools (non-stream fallback on tool calls)")
	}
	if !caps.Streaming {
		t.Error("expected Streaming=true (inherited)")
	}
	if !caps.ToolCalling {
		t.Error("expected ToolCalling=true (inherited)")
	}
}

// TestDashScopeAdapter_DefaultsApplied verifies BaseURL and Model defaults
// are injected when the caller leaves them empty.
func TestDashScopeAdapter_DefaultsApplied(t *testing.T) {
	a, err := NewDashScopeAdapter(ProviderConfig{APIKey: "sk-ds"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	body, _, err := a.ToRequest(ChatRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("ToRequest: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	model, _ := m["model"].(string)
	if model == "" {
		t.Fatal("model missing from request body")
	}
	// Default model constant is dashscopeDefaultModel — confirm it flowed through.
	if model != dashscopeDefaultModel {
		t.Errorf("model = %q, want %q (default)", model, dashscopeDefaultModel)
	}
}

// TestDashScopeAdapter_ToRequestDelegates verifies the adapter produces the
// same wire format as OpenAIAdapter for equivalent input (since DashScope is
// an OpenAI-compatible endpoint).
func TestDashScopeAdapter_ToRequestDelegates(t *testing.T) {
	a, _ := NewDashScopeAdapter(ProviderConfig{APIKey: "sk", Model: "qwen-plus"})
	body, headers, err := a.ToRequest(ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello qwen"}},
	})
	if err != nil {
		t.Fatalf("ToRequest: %v", err)
	}
	if !strings.HasPrefix(headers.Get("Authorization"), "Bearer sk") {
		t.Errorf("Authorization = %q, want Bearer sk...", headers.Get("Authorization"))
	}
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	if m["model"] != "qwen-plus" {
		t.Errorf("model = %v, want qwen-plus", m["model"])
	}
}

// TestDashScopeAdapter_FromResponseDelegates verifies response parsing
// works against the OpenAI-compat shape.
func TestDashScopeAdapter_FromResponseDelegates(t *testing.T) {
	a, _ := NewDashScopeAdapter(ProviderConfig{APIKey: "sk"})
	raw := []byte(`{
		"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}
	}`)
	resp, err := a.FromResponse(raw)
	if err != nil {
		t.Fatalf("FromResponse: %v", err)
	}
	if resp.Content != "hi" {
		t.Errorf("Content = %q, want hi", resp.Content)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 4 {
		t.Errorf("Usage TotalTokens = %v, want 4", resp.Usage)
	}
}

// TestDashScopeAdapter_FromStreamChunkDelegates verifies stream chunk parsing
// reuses OpenAI logic end-to-end.
func TestDashScopeAdapter_FromStreamChunkDelegates(t *testing.T) {
	a, _ := NewDashScopeAdapter(ProviderConfig{APIKey: "sk"})

	sc, err := a.FromStreamChunk([]byte("[DONE]"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sc == nil || !sc.Done {
		t.Errorf("want Done, got %+v", sc)
	}

	sc2, err := a.FromStreamChunk([]byte(`{"choices":[{"delta":{"content":"x"}}]}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sc2 == nil || sc2.Content != "x" {
		t.Errorf("want Content=x, got %+v", sc2)
	}
}

// ensure adapter satisfies the interface at compile time.
var _ ProviderAdapter = (*DashScopeAdapter)(nil)
