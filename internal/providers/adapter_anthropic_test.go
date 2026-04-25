package providers

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAnthropicAdapterCapabilities(t *testing.T) {
	adapter, err := NewAnthropicAdapter(ProviderConfig{APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	caps := adapter.Capabilities()

	if !caps.Streaming {
		t.Error("expected Streaming=true")
	}
	if !caps.StreamWithTools {
		t.Error("expected StreamWithTools=true")
	}
	if !caps.Thinking {
		t.Error("expected Thinking=true")
	}
	if !caps.Vision {
		t.Error("expected Vision=true")
	}
	if !caps.CacheControl {
		t.Error("expected CacheControl=true")
	}
	if caps.MaxContextWindow != 200_000 {
		t.Errorf("MaxContextWindow = %d, want 200000", caps.MaxContextWindow)
	}
	if caps.TokenizerID != "cl100k_base" {
		t.Errorf("TokenizerID = %q, want cl100k_base", caps.TokenizerID)
	}
	if adapter.Name() != "anthropic" {
		t.Errorf("Name() = %q, want anthropic", adapter.Name())
	}
}

func TestAnthropicAdapterToRequest_Basic(t *testing.T) {
	adapter, _ := NewAnthropicAdapter(ProviderConfig{APIKey: "sk-test"})

	req := ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
	}
	data, headers, err := adapter.ToRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	// Verify headers
	assertHeader(t, headers, "x-api-key", "sk-test")
	assertHeader(t, headers, "anthropic-version", anthropicAPIVersion)
	assertHeader(t, headers, "Content-Type", "application/json")

	// Verify body structure
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	if body["stream"] != true {
		t.Error("expected stream=true by default")
	}
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Fatal("expected messages array")
	}
	sysBlocks, ok := body["system"].([]any)
	if !ok || len(sysBlocks) == 0 {
		t.Fatal("expected system blocks")
	}
}

func TestAnthropicAdapterToRequest_CacheControl(t *testing.T) {
	adapter, _ := NewAnthropicAdapter(ProviderConfig{APIKey: "sk-test"})

	req := ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "System prompt"},
			{Role: "user", Content: "Hello"},
		},
		Tools: []ToolDefinition{
			{Type: "function", Function: &ToolFunctionSchema{Name: "tool1", Description: "desc1", Parameters: map[string]any{"type": "object"}}},
			{Type: "function", Function: &ToolFunctionSchema{Name: "tool2", Description: "desc2", Parameters: map[string]any{"type": "object"}}},
		},
	}
	data, _, err := adapter.ToRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	var body map[string]any
	json.Unmarshal(data, &body)

	// Last system block should have cache_control
	sysBlocks := body["system"].([]any)
	lastSys := sysBlocks[len(sysBlocks)-1].(map[string]any)
	if _, ok := lastSys["cache_control"]; !ok {
		t.Error("last system block missing cache_control")
	}

	// Last tool should have cache_control
	tools := body["tools"].([]any)
	lastTool := tools[len(tools)-1].(map[string]any)
	if _, ok := lastTool["cache_control"]; !ok {
		t.Error("last tool missing cache_control")
	}
}

func TestAnthropicAdapterToRequest_Thinking(t *testing.T) {
	adapter, _ := NewAnthropicAdapter(ProviderConfig{APIKey: "sk-test"})

	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "Think about this"}},
		Options:  map[string]any{OptThinkingLevel: "high"},
	}
	data, headers, err := adapter.ToRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	// Should have thinking beta header
	if headers.Get("anthropic-beta") != "interleaved-thinking-2025-05-14" {
		t.Error("missing anthropic-beta header for thinking")
	}

	var body map[string]any
	json.Unmarshal(data, &body)

	thinking, ok := body["thinking"].(map[string]any)
	if !ok {
		t.Fatal("expected thinking config in body")
	}
	if thinking["type"] != "enabled" {
		t.Error("thinking type should be 'enabled'")
	}
	budget, _ := thinking["budget_tokens"].(float64)
	if int(budget) != 32000 {
		t.Errorf("thinking budget = %v, want 32000 for high level", budget)
	}

	// Temperature should be removed when thinking is enabled
	if _, hasTemp := body["temperature"]; hasTemp {
		t.Error("temperature should be removed when thinking is enabled")
	}
}

func TestAnthropicAdapterFromResponse_ToolCalls(t *testing.T) {
	adapter, _ := NewAnthropicAdapter(ProviderConfig{APIKey: "sk-test"})

	respJSON := `{
		"content": [
			{"type": "text", "text": "Let me search."},
			{"type": "tool_use", "id": "toolu_01", "name": "web_search", "input": {"query": "test"}}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 100, "output_tokens": 50}
	}`

	resp, err := adapter.FromResponse([]byte(respJSON))
	if err != nil {
		t.Fatal(err)
	}

	if resp.Content != "Let me search." {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want tool_calls", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "web_search" {
		t.Errorf("ToolCall name = %q", resp.ToolCalls[0].Name)
	}
	if resp.Usage.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d", resp.Usage.PromptTokens)
	}
}

func TestAnthropicAdapterFromResponse_ThinkingBlocks(t *testing.T) {
	adapter, _ := NewAnthropicAdapter(ProviderConfig{APIKey: "sk-test"})

	respJSON := `{
		"content": [
			{"type": "thinking", "thinking": "Let me reason about this..."},
			{"type": "text", "text": "The answer is 42."}
		],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 50, "output_tokens": 80}
	}`

	resp, err := adapter.FromResponse([]byte(respJSON))
	if err != nil {
		t.Fatal(err)
	}

	if resp.Thinking != "Let me reason about this..." {
		t.Errorf("Thinking = %q", resp.Thinking)
	}
	if resp.Content != "The answer is 42." {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.Usage.ThinkingTokens == 0 {
		t.Error("expected non-zero ThinkingTokens")
	}
}

func TestAnthropicAdapterFromStreamChunk_TextDelta(t *testing.T) {
	adapter, _ := NewAnthropicAdapter(ProviderConfig{APIKey: "sk-test"})

	chunk := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`
	sc, err := adapter.FromStreamChunk([]byte(chunk))
	if err != nil {
		t.Fatal(err)
	}
	if sc == nil || sc.Content != "Hello" {
		t.Errorf("expected Content='Hello', got %+v", sc)
	}
}

func TestAnthropicAdapterFromStreamChunk_ThinkingDelta(t *testing.T) {
	adapter, _ := NewAnthropicAdapter(ProviderConfig{APIKey: "sk-test"})

	chunk := `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"reasoning..."}}`
	sc, err := adapter.FromStreamChunk([]byte(chunk))
	if err != nil {
		t.Fatal(err)
	}
	if sc == nil || sc.Thinking != "reasoning..." {
		t.Errorf("expected Thinking='reasoning...', got %+v", sc)
	}
}

func TestAnthropicAdapterFromStreamChunk_Skip(t *testing.T) {
	adapter, _ := NewAnthropicAdapter(ProviderConfig{APIKey: "sk-test"})

	// message_start events should be skipped
	chunk := `{"type":"message_start","message":{"usage":{"input_tokens":100}}}`
	sc, err := adapter.FromStreamChunk([]byte(chunk))
	if err != nil {
		t.Fatal(err)
	}
	if sc != nil {
		t.Errorf("expected nil for message_start, got %+v", sc)
	}

	// content_block_start should also be skipped
	chunk = `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`
	sc, err = adapter.FromStreamChunk([]byte(chunk))
	if err != nil {
		t.Fatal(err)
	}
	if sc != nil {
		t.Errorf("expected nil for content_block_start, got %+v", sc)
	}
}

func TestAnthropicAdapterFromStreamChunk_MessageStop(t *testing.T) {
	adapter, _ := NewAnthropicAdapter(ProviderConfig{APIKey: "sk-test"})

	chunk := `{"type":"message_stop"}`
	sc, err := adapter.FromStreamChunk([]byte(chunk))
	if err != nil {
		t.Fatal(err)
	}
	if sc == nil || !sc.Done {
		t.Errorf("expected Done=true for message_stop, got %+v", sc)
	}
}

func assertHeader(t *testing.T, h http.Header, key, want string) {
	t.Helper()
	got := h.Get(key)
	if got != want {
		t.Errorf("header %q = %q, want %q", key, got, want)
	}
}
