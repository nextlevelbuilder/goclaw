package providers

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// erroringTokenSource is a TokenSource that can return a configured error.
// Separate from codex_test.go's staticTokenSource (which returns nil error).
type erroringTokenSource struct {
	token string
	err   error
}

func (s *erroringTokenSource) Token() (string, error) {
	return s.token, s.err
}

// TestCodexAdapter_Defaults verifies empty BaseURL/Model get defaults and that
// Name() + Capabilities() expose expected Responses-API values.
func TestCodexAdapter_Defaults(t *testing.T) {
	a, err := NewCodexAdapter(ProviderConfig{})
	if err != nil {
		t.Fatalf("NewCodexAdapter error: %v", err)
	}
	if a.Name() != "codex" {
		t.Errorf("Name() = %q, want codex", a.Name())
	}
	caps := a.Capabilities()
	if !caps.Streaming || !caps.ToolCalling || !caps.StreamWithTools {
		t.Errorf("expected full streaming+tools+stream-with-tools, got %+v", caps)
	}
	if !caps.Thinking || !caps.Vision {
		t.Errorf("expected Thinking+Vision, got %+v", caps)
	}
	if caps.MaxContextWindow != 1_000_000 {
		t.Errorf("MaxContextWindow = %d, want 1_000_000", caps.MaxContextWindow)
	}
	if caps.TokenizerID != "o200k_base" {
		t.Errorf("TokenizerID = %q, want o200k_base", caps.TokenizerID)
	}
}

// TestCodexAdapter_BaseURLTrimmed verifies trailing slashes are stripped from
// the base URL (important because codex_build.go appends a path).
func TestCodexAdapter_BaseURLTrimmed(t *testing.T) {
	a, err := NewCodexAdapter(ProviderConfig{BaseURL: "https://example.test/api/"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	ca, ok := a.(*CodexAdapter)
	if !ok {
		t.Fatalf("unexpected type %T", a)
	}
	if ca.apiBase != "https://example.test/api" {
		t.Errorf("apiBase = %q, want trimmed", ca.apiBase)
	}
}

// TestCodexAdapter_ExtractsTokenSource verifies the token source is pulled
// from ExtraOpts["token_source"].
func TestCodexAdapter_ExtractsTokenSource(t *testing.T) {
	ts := &erroringTokenSource{token: "tok-ok"}
	a, err := NewCodexAdapter(ProviderConfig{
		ExtraOpts: map[string]any{"token_source": TokenSource(ts)},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	_, headers, err := a.ToRequest(ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ToRequest: %v", err)
	}
	if got := headers.Get("Authorization"); got != "Bearer tok-ok" {
		t.Errorf("Authorization = %q, want Bearer tok-ok", got)
	}
	if got := headers.Get("OpenAI-Beta"); got != "responses=v1" {
		t.Errorf("OpenAI-Beta = %q, want responses=v1", got)
	}
}

// TestCodexAdapter_ToRequest_NoTokenSource verifies a missing token source
// omits the Authorization header (caller is responsible for auth) rather
// than erroring or panicking.
func TestCodexAdapter_ToRequest_NoTokenSource(t *testing.T) {
	a, _ := NewCodexAdapter(ProviderConfig{})
	body, headers, err := a.ToRequest(ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ToRequest: %v", err)
	}
	if headers.Get("Authorization") != "" {
		t.Errorf("Authorization should be empty with no token source, got %q", headers.Get("Authorization"))
	}
	if !strings.HasPrefix(headers.Get("Content-Type"), "application/json") {
		t.Errorf("Content-Type missing: %q", headers.Get("Content-Type"))
	}
	if len(body) == 0 {
		t.Error("empty body")
	}
}

// TestCodexAdapter_ToRequest_TokenSourceError verifies a Token() error
// surfaces from ToRequest (don't silently build an unauthenticated request).
func TestCodexAdapter_ToRequest_TokenSourceError(t *testing.T) {
	boom := errors.New("token expired")
	ts := &erroringTokenSource{err: boom}
	a, _ := NewCodexAdapter(ProviderConfig{
		ExtraOpts: map[string]any{"token_source": TokenSource(ts)},
	})
	_, _, err := a.ToRequest(ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrapped %v", err, boom)
	}
}

// TestCodexAdapter_FromResponse_TextAndUsage verifies a simple "message" item
// with "output_text" content parses correctly plus usage accounting including
// reasoning token detail.
func TestCodexAdapter_FromResponse_TextAndUsage(t *testing.T) {
	a, _ := NewCodexAdapter(ProviderConfig{})
	raw := []byte(`{
		"id":"resp_1",
		"object":"response",
		"status":"completed",
		"output":[
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}
		],
		"usage":{
			"input_tokens":10,
			"output_tokens":3,
			"total_tokens":13,
			"output_tokens_details":{"reasoning_tokens":2}
		}
	}`)
	resp, err := a.FromResponse(raw)
	if err != nil {
		t.Fatalf("FromResponse: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("Content = %q, want hello", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", resp.FinishReason)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 13 {
		t.Errorf("Usage TotalTokens = %+v, want 13", resp.Usage)
	}
	if resp.Usage == nil || resp.Usage.ThinkingTokens != 2 {
		t.Errorf("Usage ThinkingTokens = %+v, want 2", resp.Usage)
	}
}

// TestCodexAdapter_FromResponse_FunctionCallAndIncomplete covers two branches:
// - a function_call output item populates ToolCalls and bumps FinishReason to "tool_calls"
// - status="incomplete" overrides FinishReason to "length"
func TestCodexAdapter_FromResponse_FunctionCallAndIncomplete(t *testing.T) {
	a, _ := NewCodexAdapter(ProviderConfig{})

	t.Run("function_call sets tool_calls finish", func(t *testing.T) {
		raw := []byte(`{
			"id":"resp_2",
			"status":"completed",
			"output":[
				{"type":"function_call","call_id":"fc_abc","name":"lookup","arguments":"{\"q\":\"hi\"}"}
			]
		}`)
		resp, err := a.FromResponse(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if len(resp.ToolCalls) != 1 {
			t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
		}
		tc := resp.ToolCalls[0]
		if tc.Name != "lookup" {
			t.Errorf("ToolCall.Name = %q, want lookup", tc.Name)
		}
		if q, _ := tc.Arguments["q"].(string); q != "hi" {
			t.Errorf("Arguments[q] = %v, want hi", tc.Arguments["q"])
		}
		if resp.FinishReason != "tool_calls" {
			t.Errorf("FinishReason = %q, want tool_calls", resp.FinishReason)
		}
	})

	t.Run("status=incomplete sets length finish", func(t *testing.T) {
		raw := []byte(`{
			"id":"resp_3",
			"status":"incomplete",
			"output":[
				{"type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}
			]
		}`)
		resp, err := a.FromResponse(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if resp.FinishReason != "length" {
			t.Errorf("FinishReason = %q, want length", resp.FinishReason)
		}
	})

	t.Run("reasoning summary populates Thinking", func(t *testing.T) {
		raw := []byte(`{
			"id":"resp_4",
			"status":"completed",
			"output":[
				{"type":"reasoning","summary":[{"type":"text","text":"step1 "},{"type":"text","text":"step2"}]}
			]
		}`)
		resp, err := a.FromResponse(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if resp.Thinking != "step1 step2" {
			t.Errorf("Thinking = %q, want %q", resp.Thinking, "step1 step2")
		}
	})
}

// TestCodexAdapter_FromResponse_MalformedReturnsError verifies malformed JSON
// errors out instead of panicking.
func TestCodexAdapter_FromResponse_MalformedReturnsError(t *testing.T) {
	a, _ := NewCodexAdapter(ProviderConfig{})
	if _, err := a.FromResponse([]byte(`{not`)); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestCodexAdapter_FromStreamChunk covers the three event-dispatch branches in
// adapter_codex.go: output_text.delta, response.completed, unknown → skip.
func TestCodexAdapter_FromStreamChunk(t *testing.T) {
	a, _ := NewCodexAdapter(ProviderConfig{})

	t.Run("done literal", func(t *testing.T) {
		sc, err := a.FromStreamChunk([]byte("[DONE]"))
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if sc == nil || !sc.Done {
			t.Errorf("want Done, got %+v", sc)
		}
	})

	t.Run("text delta", func(t *testing.T) {
		raw := []byte(`{"type":"response.output_text.delta","delta":"tok"}`)
		sc, err := a.FromStreamChunk(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if sc == nil || sc.Content != "tok" {
			t.Errorf("want Content=tok, got %+v", sc)
		}
	})

	t.Run("empty text delta skipped", func(t *testing.T) {
		raw := []byte(`{"type":"response.output_text.delta","delta":""}`)
		sc, err := a.FromStreamChunk(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if sc != nil {
			t.Errorf("empty delta should yield nil, got %+v", sc)
		}
	})

	t.Run("completed event signals done", func(t *testing.T) {
		raw := []byte(`{"type":"response.completed"}`)
		sc, err := a.FromStreamChunk(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if sc == nil || !sc.Done {
			t.Errorf("want Done on completed, got %+v", sc)
		}
	})

	t.Run("failed event signals done", func(t *testing.T) {
		raw := []byte(`{"type":"response.failed"}`)
		sc, err := a.FromStreamChunk(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if sc == nil || !sc.Done {
			t.Errorf("want Done on failed, got %+v", sc)
		}
	})

	t.Run("unknown event skipped", func(t *testing.T) {
		raw := []byte(`{"type":"response.reasoning.delta","text":"thinking"}`)
		sc, err := a.FromStreamChunk(raw)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if sc != nil {
			t.Errorf("unknown event should yield nil, got %+v", sc)
		}
	})

	t.Run("malformed returns nil", func(t *testing.T) {
		// Decoder intentionally swallows malformed chunks.
		sc, err := a.FromStreamChunk([]byte(`not-json`))
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if sc != nil {
			t.Errorf("want nil, got %+v", sc)
		}
	})
}

// TestCodexAdapter_ToRequest_StreamOption verifies stream option flows into body.
func TestCodexAdapter_ToRequest_StreamOption(t *testing.T) {
	a, _ := NewCodexAdapter(ProviderConfig{})
	body, _, err := a.ToRequest(ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{"stream": false},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	// Responses-API marshals the stream flag; codex sets stream in buildRequestBody.
	if s, ok := m["stream"].(bool); !ok || s != false {
		t.Errorf("stream = %v, want false", m["stream"])
	}
}

// ensure the adapter satisfies the interface at compile time.
var _ ProviderAdapter = (*CodexAdapter)(nil)
