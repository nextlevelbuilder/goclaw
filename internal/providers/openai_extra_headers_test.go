package providers

// Coverage for OpenAIProvider.WithExtraHeaders — the mechanism Kimi Coding
// uses to send a fixed User-Agent on every request.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOpenAIProvider_ExtraHeaders_AppliedOnHTTPRequest verifies that headers
// set via WithExtraHeaders reach the actual outgoing request — not just the
// adapter's header map.
func TestOpenAIProvider_ExtraHeaders_AppliedOnHTTPRequest(t *testing.T) {
	var gotUserAgent, gotXTrace, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		gotXTrace = r.Header.Get("X-Trace-Id")
		gotAuth = r.Header.Get("Authorization")
		// Minimal non-stream response so doRequest returns cleanly.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	p := NewOpenAIProvider("kimi-coding-test", "sk-fake", srv.URL, "kimi-k2-turbo-preview").
		WithExtraHeaders(map[string]string{
			"User-Agent": "claude-code/0.1.0",
			"X-Trace-Id": "abc",
		})

	body, err := p.doRequest(context.Background(), map[string]any{
		"model":    "kimi-k2-turbo-preview",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()

	if gotUserAgent != "claude-code/0.1.0" {
		t.Errorf("User-Agent = %q, want %q", gotUserAgent, "claude-code/0.1.0")
	}
	if gotXTrace != "abc" {
		t.Errorf("X-Trace-Id = %q, want %q", gotXTrace, "abc")
	}
	// Standard Bearer auth must still apply alongside extra headers.
	if gotAuth != "Bearer sk-fake" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer sk-fake")
	}
}

// TestOpenAIAdapter_ExtraHeaders_MirroredInToRequest verifies the adapter path
// emits the same extra headers as the direct doRequest path — important
// because some call sites use adapter.ToRequest to produce headers separately.
func TestOpenAIAdapter_ExtraHeaders_MirroredInToRequest(t *testing.T) {
	p := NewOpenAIProvider("kimi-coding-test", "sk-fake", "https://api.kimi.com/coding/v1", "kimi-k2-turbo-preview").
		WithExtraHeaders(map[string]string{
			"User-Agent": "claude-code/0.1.0",
		})
	a := &OpenAIAdapter{provider: p}

	_, headers, err := a.ToRequest(ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ToRequest: %v", err)
	}
	if got := headers.Get("User-Agent"); got != "claude-code/0.1.0" {
		t.Errorf("adapter User-Agent = %q, want claude-code/0.1.0", got)
	}
}

// TestOpenAIProvider_ExtraHeaders_NoOpWhenEmpty makes sure the
// WithExtraHeaders(nil) / WithExtraHeaders({}) calls leave the provider's
// state alone — protects against accidental nil-map allocations in callers
// that pass through optional config.
func TestOpenAIProvider_ExtraHeaders_NoOpWhenEmpty(t *testing.T) {
	p := NewOpenAIProvider("x", "k", "https://example.com", "m").
		WithExtraHeaders(nil).
		WithExtraHeaders(map[string]string{})

	if got := p.ExtraHeaders(); got != nil {
		t.Errorf("ExtraHeaders after empty calls = %v, want nil", got)
	}
}

// TestKimiCoding_TemperatureSkipped reproduces the upstream rejection
// `invalid temperature: only 1 is allowed for this model`. When the provider
// is kimi_coding, the request body must omit temperature entirely so the
// upstream applies its mandatory default.
func TestKimiCoding_TemperatureSkipped(t *testing.T) {
	p := NewOpenAIProvider("kimi-coding", "sk-fake", "https://api.kimi.com/coding/v1", "kimi-k2-turbo-preview").
		WithProviderType("kimi_coding")

	body := p.buildRequestBody("kimi-k2-turbo-preview", ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptTemperature: 0.7},
	}, true)

	if _, present := body["temperature"]; present {
		t.Errorf("temperature must not be sent to kimi_coding; got body[temperature]=%v", body["temperature"])
	}
}

// TestKimiCoding_TemperatureSentForOtherProviders is the negative control —
// without provider_type=kimi_coding, a temperature option still flows through.
func TestKimiCoding_TemperatureSentForOtherProviders(t *testing.T) {
	p := NewOpenAIProvider("openai", "sk-fake", "https://api.openai.com/v1", "gpt-4o-mini")

	body := p.buildRequestBody("gpt-4o-mini", ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptTemperature: 0.7},
	}, true)

	got, ok := body["temperature"]
	if !ok {
		t.Fatal("temperature must be sent for non-kimi providers")
	}
	if got != 0.7 {
		t.Errorf("temperature = %v, want 0.7", got)
	}
}

// TestKimiCoding_ReasoningContentAlwaysPresentOnAssistant reproduces upstream
// "thinking is enabled but reasoning_content is missing in assistant tool call
// message" — when an assistant message has tool_calls but no captured Thinking,
// kimi_coding must still carry reasoning_content (empty string is fine).
func TestKimiCoding_ReasoningContentAlwaysPresentOnAssistant(t *testing.T) {
	p := NewOpenAIProvider("kimi-coding", "sk", "https://api.kimi.com/coding/v1", "kimi-k2-turbo-preview").
		WithProviderType("kimi_coding")

	body := p.buildRequestBody("kimi-k2-turbo-preview", ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "list pods"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "exec", Arguments: map[string]any{"cmd": "kubectl get pods"}}}},
			{Role: "tool", Content: "...", ToolCallID: "call_1"},
		},
	}, true)

	msgs, ok := body["messages"].([]map[string]any)
	if !ok {
		t.Fatalf("messages not []map[string]any: %T", body["messages"])
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	// Assistant tool-call message must carry reasoning_content key.
	assistant := msgs[1]
	rc, present := assistant["reasoning_content"]
	if !present {
		t.Fatalf("kimi_coding assistant tool-call message must include reasoning_content key; got %v", assistant)
	}
	if rc != "" {
		t.Errorf("reasoning_content = %q, want empty string when Thinking unset", rc)
	}
}

// TestKimiCoding_ReasoningContentPreservedWhenSet ensures the empty-string
// fallback doesn't clobber real captured thinking content.
// (Use a non-trailing assistant message — buildRequestBody strips trailing
// assistant prefills as a safety net for proxy providers.)
func TestKimiCoding_ReasoningContentPreservedWhenSet(t *testing.T) {
	p := NewOpenAIProvider("kimi-coding", "sk", "https://api.kimi.com/coding/v1", "kimi-k2-turbo-preview").
		WithProviderType("kimi_coding")

	body := p.buildRequestBody("kimi-k2-turbo-preview", ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello", Thinking: "the user said hi"},
			{Role: "user", Content: "more"},
		},
	}, true)

	msgs := body["messages"].([]map[string]any)
	if got := msgs[1]["reasoning_content"]; got != "the user said hi" {
		t.Errorf("reasoning_content = %q, want %q", got, "the user said hi")
	}
}

// TestNonKimi_ReasoningContentNotAddedWhenEmpty is the negative control — for
// other providers in the allowlist (e.g. deepseek), an empty Thinking must NOT
// inject an empty reasoning_content key, preserving today's behavior.
func TestNonKimi_ReasoningContentNotAddedWhenEmpty(t *testing.T) {
	p := NewOpenAIProvider("deepseek", "sk", "https://api.deepseek.com/v1", "deepseek-chat")

	body := p.buildRequestBody("deepseek-chat", ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "exec", Arguments: map[string]any{}}}},
			{Role: "tool", Content: "...", ToolCallID: "call_1"},
		},
	}, true)

	msgs := body["messages"].([]map[string]any)
	if _, present := msgs[1]["reasoning_content"]; present {
		t.Error("non-kimi providers must not inject empty reasoning_content; key should be absent")
	}
}
