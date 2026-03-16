package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newDashScopeTestServer sets up a mock SSE server for ChatStream calls and
// returns both the server and a pointer that will hold the last captured request body.
func newDashScopeTestServer(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	captured := &map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(captured); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		// stream=false path (Chat): return JSON
		if v, _ := (*captured)["stream"].(bool); !v {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
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

// callDashScopeStream sends req through ChatStream and returns the captured body.
func callDashScopeStream(t *testing.T, req ChatRequest) map[string]any {
	t.Helper()
	server, captured := newDashScopeTestServer(t)
	p := NewDashScopeProvider("test-key", server.URL, "")
	p.retryConfig.Attempts = 1
	p.ChatStream(context.Background(), req, nil) //nolint:errcheck
	return *captured
}

// TestDashScopeModelSupportsThinking verifies the whitelist is correct.
func TestDashScopeModelSupportsThinking(t *testing.T) {
	p := NewDashScopeProvider("key", "", "")

	tests := []struct {
		model string
		want  bool
	}{
		{"qwen3.5-plus", true},
		{"qwen3.5-turbo", true},
		{"qwen3-max", true},
		{"qwen3-235b-a22b", true},
		{"qwen3-32b", true},
		{"qwen3-14b", true},
		{"qwen3-8b", true},
		// Models that do NOT support thinking:
		{"qwen3-plus", false},
		{"qwen3-turbo", false},
		{"qwen2-72b-instruct", false},
	}

	for _, tt := range tests {
		got := p.ModelSupportsThinking(tt.model)
		if got != tt.want {
			t.Errorf("ModelSupportsThinking(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

// TestDashScopeThinkingInjected_WhenModelSupports verifies enable_thinking IS sent
// for a model on the whitelist (qwen3.5-plus) when thinking_level is set.
// ChatStream is used because that's the path where thinking params are injected.
func TestDashScopeThinkingInjected_WhenModelSupports(t *testing.T) {
	body := callDashScopeStream(t, ChatRequest{
		Model:    "qwen3.5-plus",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptThinkingLevel: "medium"},
	})

	if body["enable_thinking"] != true {
		t.Errorf("enable_thinking = %v, want true for qwen3.5-plus with thinking_level=medium", body["enable_thinking"])
	}
	budget, _ := body["thinking_budget"].(float64)
	if budget <= 0 {
		t.Errorf("thinking_budget = %v, want > 0", body["thinking_budget"])
	}
	if _, hasThinkingLevel := body["thinking_level"]; hasThinkingLevel {
		t.Error("thinking_level should be removed from body (converted to enable_thinking)")
	}
}

// TestDashScopeThinkingNotInjected_WhenModelDoesNotSupport verifies enable_thinking
// is NOT sent for qwen3-plus (not on whitelist), even when thinking_level is set.
// This is the root cause fix: previously this caused "model 'qwen3-plus' is not supported".
func TestDashScopeThinkingNotInjected_WhenModelDoesNotSupport(t *testing.T) {
	body := callDashScopeStream(t, ChatRequest{
		Model:    "qwen3-plus",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptThinkingLevel: "medium"},
	})

	if _, has := body["enable_thinking"]; has {
		t.Errorf("enable_thinking should NOT be sent for qwen3-plus, got: %v", body["enable_thinking"])
	}
	if _, has := body["thinking_budget"]; has {
		t.Errorf("thinking_budget should NOT be sent for qwen3-plus, got: %v", body["thinking_budget"])
	}
}

// TestDashScopeNoThinkingLevel verifies that without thinking_level,
// enable_thinking is never injected regardless of model.
func TestDashScopeNoThinkingLevel(t *testing.T) {
	body := callDashScopeStream(t, ChatRequest{
		Model:    "qwen3-max",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptMaxTokens: 1},
	})

	if _, has := body["enable_thinking"]; has {
		t.Error("enable_thinking should NOT be sent without thinking_level")
	}
}

// TestDashScopeThinkingBudgetValues verifies thinking_budget level mapping.
func TestDashScopeThinkingBudgetValues(t *testing.T) {
	tests := []struct {
		level string
		want  int
	}{
		{"low", 4096},
		{"medium", 16384},
		{"high", 32768},
		{"unknown", 16384}, // default
	}
	for _, tt := range tests {
		got := dashscopeThinkingBudget(tt.level)
		if got != tt.want {
			t.Errorf("dashscopeThinkingBudget(%q) = %d, want %d", tt.level, got, tt.want)
		}
	}
}
