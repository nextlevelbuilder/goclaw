package providers

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestOpenAIAdapter_Basics verifies Name() and Capabilities() delegate to the
// wrapped OpenAIProvider rather than returning zero values.
func TestOpenAIAdapter_Basics(t *testing.T) {
	a, err := NewOpenAIAdapter(ProviderConfig{
		APIKey:  "sk-fake",
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("NewOpenAIAdapter error: %v", err)
	}
	if a.Name() != "openai" {
		t.Errorf("Name() = %q, want openai", a.Name())
	}
	caps := a.Capabilities()
	if !caps.Streaming {
		t.Error("expected Streaming=true")
	}
	if !caps.ToolCalling {
		t.Error("expected ToolCalling=true")
	}
	if caps.TokenizerID == "" {
		t.Error("expected non-empty TokenizerID")
	}
}

// TestOpenAIAdapter_ToRequest_DefaultStreamAndAuth verifies defaults:
// - stream=true (when not set in Options)
// - Authorization "Bearer <key>" for non-Azure endpoints
// - Content-Type header present
// - resolved model travels into the body
func TestOpenAIAdapter_ToRequest_DefaultStreamAndAuth(t *testing.T) {
	a, _ := NewOpenAIAdapter(ProviderConfig{
		APIKey:  "sk-abc",
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4o-mini",
	})

	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}
	body, headers, err := a.ToRequest(req)
	if err != nil {
		t.Fatalf("ToRequest error: %v", err)
	}

	if got := headers.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := headers.Get("Authorization"); got != "Bearer sk-abc" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer sk-abc")
	}
	if got := headers.Get("api-key"); got != "" {
		t.Errorf("api-key should be empty for non-Azure, got %q", got)
	}

	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	if m["stream"] != true {
		t.Errorf("stream = %v, want true", m["stream"])
	}
	if m["model"] == "" || m["model"] == nil {
		t.Errorf("model missing from body: %v", m["model"])
	}
}

// TestOpenAIAdapter_ToRequest_AzureUsesAPIKeyHeader verifies Azure endpoints
// use the "api-key" header instead of Authorization Bearer.
func TestOpenAIAdapter_ToRequest_AzureUsesAPIKeyHeader(t *testing.T) {
	a, _ := NewOpenAIAdapter(ProviderConfig{
		APIKey:  "az-key-42",
		BaseURL: "https://my-resource.openai.azure.com/openai",
		Model:   "gpt-4o",
	})

	_, headers, err := a.ToRequest(ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ToRequest error: %v", err)
	}

	if got := headers.Get("api-key"); got != "az-key-42" {
		t.Errorf("api-key = %q, want az-key-42", got)
	}
	if got := headers.Get("Authorization"); got != "" {
		t.Errorf("Authorization should be empty for Azure, got %q", got)
	}
}

// TestOpenAIAdapter_ToRequest_StreamFalseOption verifies the stream option
// flows through to the request body.
func TestOpenAIAdapter_ToRequest_StreamFalseOption(t *testing.T) {
	a, _ := NewOpenAIAdapter(ProviderConfig{
		APIKey:  "sk-x",
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4o-mini",
	})

	body, _, err := a.ToRequest(ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{"stream": false},
	})
	if err != nil {
		t.Fatalf("ToRequest error: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	if m["stream"] != false {
		t.Errorf("stream = %v, want false", m["stream"])
	}
}

// TestOpenAIAdapter_FromResponse_ParsesContentAndUsage verifies a canonical
// OpenAI chat-completions response parses into ChatResponse with content and
// non-zero usage numbers.
func TestOpenAIAdapter_FromResponse_ParsesContentAndUsage(t *testing.T) {
	a, _ := NewOpenAIAdapter(ProviderConfig{APIKey: "k", BaseURL: "https://api.openai.com/v1"})
	raw := []byte(`{
		"id":"chatcmpl-1",
		"object":"chat.completion",
		"choices":[{"index":0,"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}
	}`)
	resp, err := a.FromResponse(raw)
	if err != nil {
		t.Fatalf("FromResponse error: %v", err)
	}
	if resp.Content != "hi there" {
		t.Errorf("Content = %q, want %q", resp.Content, "hi there")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", resp.FinishReason)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 12 {
		t.Errorf("Usage = %+v, want TotalTokens=12", resp.Usage)
	}
}

// TestOpenAIAdapter_FromResponse_MalformedReturnsError verifies malformed JSON
// surfaces a decode error rather than panicking.
func TestOpenAIAdapter_FromResponse_MalformedReturnsError(t *testing.T) {
	a, _ := NewOpenAIAdapter(ProviderConfig{APIKey: "k"})
	if _, err := a.FromResponse([]byte(`{not-json`)); err == nil {
		t.Fatal("expected error on malformed JSON, got nil")
	}
}

// TestOpenAIAdapter_FromStreamChunk covers the three main chunk shapes:
// - "[DONE]" → Done=true
// - content delta → Content set
// - reasoning delta → Thinking set
// - empty/unknown → nil (skip)
func TestOpenAIAdapter_FromStreamChunk(t *testing.T) {
	a, _ := NewOpenAIAdapter(ProviderConfig{APIKey: "k"})

	t.Run("done sentinel", func(t *testing.T) {
		sc, err := a.FromStreamChunk([]byte("[DONE]"))
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if sc == nil || !sc.Done {
			t.Errorf("want Done=true, got %+v", sc)
		}
	})

	t.Run("content delta", func(t *testing.T) {
		raw := []byte(`{"choices":[{"delta":{"content":"hello"}}]}`)
		sc, err := a.FromStreamChunk(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if sc == nil || sc.Content != "hello" {
			t.Errorf("want Content=hello, got %+v", sc)
		}
	})

	t.Run("reasoning_content delta", func(t *testing.T) {
		raw := []byte(`{"choices":[{"delta":{"reasoning_content":"thinking..."}}]}`)
		sc, err := a.FromStreamChunk(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if sc == nil || sc.Thinking != "thinking..." {
			t.Errorf("want Thinking=thinking..., got %+v", sc)
		}
	})

	t.Run("reasoning fallback field", func(t *testing.T) {
		raw := []byte(`{"choices":[{"delta":{"reasoning":"alt"}}]}`)
		sc, err := a.FromStreamChunk(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if sc == nil || sc.Thinking != "alt" {
			t.Errorf("want Thinking=alt, got %+v", sc)
		}
	})

	t.Run("empty choices skipped", func(t *testing.T) {
		raw := []byte(`{"choices":[]}`)
		sc, err := a.FromStreamChunk(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if sc != nil {
			t.Errorf("want nil skip, got %+v", sc)
		}
	})

	t.Run("empty delta skipped", func(t *testing.T) {
		raw := []byte(`{"choices":[{"delta":{}}]}`)
		sc, err := a.FromStreamChunk(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if sc != nil {
			t.Errorf("want nil skip (no content), got %+v", sc)
		}
	})

	t.Run("malformed json returns nil", func(t *testing.T) {
		// Decoder swallows malformed chunks so streaming doesn't die.
		sc, err := a.FromStreamChunk([]byte(`{broken`))
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if sc != nil {
			t.Errorf("want nil on malformed, got %+v", sc)
		}
	})
}

// ensure the adapter satisfies the interface at compile time.
var _ ProviderAdapter = (*OpenAIAdapter)(nil)
