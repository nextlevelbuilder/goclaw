package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newMiniMaxTestServer sets up a mock SSE server for MiniMax Chat/ChatStream calls
// and returns both the server and a pointer that will hold the last captured request body.
func newMiniMaxTestServer(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	captured := &map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(captured); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		// stream=false path (Chat): return JSON
		if v, _ := (*captured)["stream"].(bool); !v {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
			return
		}
		// stream=true path (ChatStream): return SSE
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	return server, captured
}

// callMiniMaxChat sends req through Chat and returns the captured body.
func callMiniMaxChat(t *testing.T, req ChatRequest) map[string]any {
	t.Helper()
	server, captured := newMiniMaxTestServer(t)
	p := NewMiniMaxProvider("minimax-test", "test-key", server.URL, "")
	p.retryConfig.Attempts = 1
	p.Chat(context.Background(), req) //nolint:errcheck
	return *captured
}

// callMiniMaxStream sends req through ChatStream and returns the captured body.
func callMiniMaxStream(t *testing.T, req ChatRequest) map[string]any {
	t.Helper()
	server, captured := newMiniMaxTestServer(t)
	p := NewMiniMaxProvider("minimax-test", "test-key", server.URL, "")
	p.retryConfig.Attempts = 1
	p.ChatStream(context.Background(), req, nil) //nolint:errcheck
	return *captured
}

// TestMiniMaxDefaultModel verifies the provider defaults to MiniMax-M2.7.
func TestMiniMaxDefaultModel(t *testing.T) {
	p := NewMiniMaxProvider("minimax", "key", "", "")
	if p.DefaultModel() != "MiniMax-M2.7" {
		t.Errorf("DefaultModel() = %q, want %q", p.DefaultModel(), "MiniMax-M2.7")
	}
}

// TestMiniMaxDefaultAPIBase verifies the default API base URL.
func TestMiniMaxDefaultAPIBase(t *testing.T) {
	p := NewMiniMaxProvider("minimax", "key", "", "")
	if p.APIBase() != "https://api.minimax.io/v1" {
		t.Errorf("APIBase() = %q, want %q", p.APIBase(), "https://api.minimax.io/v1")
	}
}

// TestMiniMaxProviderType verifies providerType is set to minimax_native.
func TestMiniMaxProviderType(t *testing.T) {
	p := NewMiniMaxProvider("minimax", "key", "", "")
	if p.ProviderType() != "minimax_native" {
		t.Errorf("ProviderType() = %q, want %q", p.ProviderType(), "minimax_native")
	}
}

// TestMiniMaxChatPath verifies the standard /chat/completions path is used.
func TestMiniMaxChatPath(t *testing.T) {
	p := NewMiniMaxProvider("minimax", "key", "", "")
	if p.chatPath != "/chat/completions" {
		t.Errorf("chatPath = %q, want %q", p.chatPath, "/chat/completions")
	}
}

// TestMiniMaxSupportsThinking verifies thinking support.
func TestMiniMaxSupportsThinking(t *testing.T) {
	p := NewMiniMaxProvider("minimax", "key", "", "")
	if !p.SupportsThinking() {
		t.Error("SupportsThinking() = false, want true")
	}
}

// TestMiniMaxName verifies the Name() method returns the configured name.
func TestMiniMaxName(t *testing.T) {
	p := NewMiniMaxProvider("my-minimax", "key", "", "")
	if p.Name() != "my-minimax" {
		t.Errorf("Name() = %q, want %q", p.Name(), "my-minimax")
	}
}

// TestMiniMaxTempClamped_ZeroRemoved verifies temperature=0 is removed from the request
// (MiniMax rejects 0; server uses its default instead).
func TestMiniMaxTempClamped_ZeroRemoved(t *testing.T) {
	body := callMiniMaxChat(t, ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptTemperature: 0.0},
	})
	if _, has := body["temperature"]; has {
		t.Errorf("temperature should be removed for value 0, got: %v", body["temperature"])
	}
}

// TestMiniMaxTempClamped_NegativeRemoved verifies negative temperature is removed.
func TestMiniMaxTempClamped_NegativeRemoved(t *testing.T) {
	body := callMiniMaxChat(t, ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptTemperature: -0.5},
	})
	if _, has := body["temperature"]; has {
		t.Errorf("temperature should be removed for negative value, got: %v", body["temperature"])
	}
}

// TestMiniMaxTempClamped_Above1 verifies temperature > 1.0 is clamped to 1.0.
func TestMiniMaxTempClamped_Above1(t *testing.T) {
	body := callMiniMaxChat(t, ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptTemperature: 1.5},
	})
	temp, ok := body["temperature"].(float64)
	if !ok {
		t.Fatalf("temperature not found in request body")
	}
	if temp != 1.0 {
		t.Errorf("temperature = %v, want 1.0", temp)
	}
}

// TestMiniMaxTempPassthrough_Valid verifies valid temperature (0 < t ≤ 1) is passed as-is.
func TestMiniMaxTempPassthrough_Valid(t *testing.T) {
	body := callMiniMaxChat(t, ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptTemperature: 0.7},
	})
	temp, ok := body["temperature"].(float64)
	if !ok {
		t.Fatalf("temperature not found in request body")
	}
	if temp != 0.7 {
		t.Errorf("temperature = %v, want 0.7", temp)
	}
}

// TestMiniMaxTempPassthrough_ExactlyOne verifies temperature=1.0 is valid and passed as-is.
func TestMiniMaxTempPassthrough_ExactlyOne(t *testing.T) {
	body := callMiniMaxChat(t, ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptTemperature: 1.0},
	})
	temp, ok := body["temperature"].(float64)
	if !ok {
		t.Fatalf("temperature not found in request body")
	}
	if temp != 1.0 {
		t.Errorf("temperature = %v, want 1.0", temp)
	}
}

// TestMiniMaxNoTemperature verifies requests without temperature are unchanged.
func TestMiniMaxNoTemperature(t *testing.T) {
	body := callMiniMaxChat(t, ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptMaxTokens: 100},
	})
	if _, has := body["temperature"]; has {
		t.Error("temperature should not be present when not set in options")
	}
}

// TestMiniMaxStreamTempClamped verifies temperature clamping works for ChatStream too.
func TestMiniMaxStreamTempClamped(t *testing.T) {
	body := callMiniMaxStream(t, ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptTemperature: 2.0},
	})
	temp, ok := body["temperature"].(float64)
	if !ok {
		t.Fatalf("temperature not found in request body")
	}
	if temp != 1.0 {
		t.Errorf("temperature = %v, want 1.0 (clamped from 2.0)", temp)
	}
}

// TestMiniMaxChatResponse verifies a basic Chat call returns correct content.
func TestMiniMaxChatResponse(t *testing.T) {
	server, _ := newMiniMaxTestServer(t)
	p := NewMiniMaxProvider("minimax-test", "test-key", server.URL, "")
	p.retryConfig.Attempts = 1

	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "stop")
	}
}

// TestMiniMaxStreamResponse verifies a basic ChatStream call returns correct content.
func TestMiniMaxStreamResponse(t *testing.T) {
	server, _ := newMiniMaxTestServer(t)
	p := NewMiniMaxProvider("minimax-test", "test-key", server.URL, "")
	p.retryConfig.Attempts = 1

	var chunks []string
	resp, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(chunk StreamChunk) {
		if chunk.Content != "" {
			chunks = append(chunks, chunk.Content)
		}
	})
	if err != nil {
		t.Fatalf("ChatStream() error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok")
	}
	if len(chunks) == 0 {
		t.Error("expected at least one content chunk")
	}
}

// TestMiniMaxCustomAPIBase verifies custom API base is respected.
func TestMiniMaxCustomAPIBase(t *testing.T) {
	p := NewMiniMaxProvider("minimax", "key", "https://custom.api.minimax.io/v1", "")
	if p.APIBase() != "https://custom.api.minimax.io/v1" {
		t.Errorf("APIBase() = %q, want %q", p.APIBase(), "https://custom.api.minimax.io/v1")
	}
}

// TestMiniMaxCustomModel verifies custom default model is respected.
func TestMiniMaxCustomModel(t *testing.T) {
	p := NewMiniMaxProvider("minimax", "key", "", "MiniMax-M2.5")
	if p.DefaultModel() != "MiniMax-M2.5" {
		t.Errorf("DefaultModel() = %q, want %q", p.DefaultModel(), "MiniMax-M2.5")
	}
}

// TestMiniMaxTempClamped_IntegerValue verifies integer temperature values are handled.
func TestMiniMaxTempClamped_IntegerValue(t *testing.T) {
	body := callMiniMaxChat(t, ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptTemperature: 2},
	})
	temp, ok := body["temperature"].(float64)
	if !ok {
		t.Fatalf("temperature not found in request body")
	}
	if temp != 1.0 {
		t.Errorf("temperature = %v, want 1.0 (clamped from int 2)", temp)
	}
}
