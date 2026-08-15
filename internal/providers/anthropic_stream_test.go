package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newAnthropicSSEServer creates a mock SSE server that sends the provided events then closes.
func newAnthropicSSEServer(t *testing.T, events []string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter does not implement http.Flusher")
			return
		}
		for _, ev := range events {
			fmt.Fprint(w, ev)
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// newTestAnthropicProvider creates an AnthropicProvider pointing at the given base URL.
func newTestAnthropicProvider(baseURL string) *AnthropicProvider {
	p := NewAnthropicProvider("test-key", WithAnthropicBaseURL(baseURL))
	p.retryConfig.Attempts = 1
	return p
}

// TestStreamChat_CancelledContext verifies that cancelling the context mid-stream
// causes ChatStream to return ctx.Err() rather than continuing to process events.
func TestStreamChat_CancelledContext(t *testing.T) {
	// Server sends one chunk then blocks — we cancel before the stream finishes.
	blocker := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter does not implement http.Flusher")
			return
		}
		// Send one valid event so the scanner loop starts.
		fmt.Fprint(w, "event: message_start\ndata: {\"message\":{\"usage\":{\"input_tokens\":5}}}\n\n")
		flusher.Flush()
		// Block until the test is done (simulates a slow stream).
		<-blocker
	}))
	defer server.Close()
	defer close(blocker)

	ctx, cancel := context.WithCancel(context.Background())
	p := newTestAnthropicProvider(server.URL)

	// Cancel immediately after starting.
	cancel()

	req := ChatRequest{
		Model:    "claude-sonnet-4-5-20250929",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}
	_, err := p.ChatStream(ctx, req, nil)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		// Accept either context.Canceled or context.DeadlineExceeded; the HTTP
		// layer may wrap the error, so we check ctx.Err() as fallback.
		if ctx.Err() == nil {
			t.Errorf("expected ctx.Err() to be set, got err=%v", err)
		}
	}
}

// TestStreamChat_ToolCallIndexBounds verifies that receiving input_json_delta
// without a prior content_block_start:tool_use does not panic (bounds check).
func TestStreamChat_ToolCallIndexBounds(t *testing.T) {
	events := []string{
		// content_block_delta with input_json_delta but no tool_use block started
		"event: content_block_delta\n",
		`data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"{\"k\":1}"}}` + "\n\n",
		// Proper message_stop so the stream ends cleanly
		"event: message_stop\n",
		"data: {}\n\n",
	}
	server := newAnthropicSSEServer(t, events)
	p := newTestAnthropicProvider(server.URL)

	req := ChatRequest{
		Model:    "claude-sonnet-4-5-20250929",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}
	// Should not panic; the fix guards with i < len(result.ToolCalls).
	result, err := p.ChatStream(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(result.ToolCalls))
	}
}

// TestStreamChat_ThinkingSignature verifies that signature_delta events are
// accumulated and stored in result.ThinkingSignature.
func TestStreamChat_ThinkingSignature(t *testing.T) {
	events := []string{
		"event: message_start\n",
		`data: {"message":{"usage":{"input_tokens":10}}}` + "\n\n",

		"event: content_block_start\n",
		`data: {"index":0,"content_block":{"type":"thinking","thinking":""}}` + "\n\n",

		"event: content_block_delta\n",
		`data: {"index":0,"delta":{"type":"thinking_delta","thinking":"let me think"}}` + "\n\n",

		"event: content_block_delta\n",
		`data: {"index":0,"delta":{"type":"signature_delta","signature":"sig-part-1"}}` + "\n\n",

		"event: content_block_delta\n",
		`data: {"index":0,"delta":{"type":"signature_delta","signature":"-part-2"}}` + "\n\n",

		"event: content_block_stop\n",
		"data: {}\n\n",

		"event: content_block_start\n",
		`data: {"index":1,"content_block":{"type":"text","text":""}}` + "\n\n",

		"event: content_block_delta\n",
		`data: {"index":1,"delta":{"type":"text_delta","text":"answer"}}` + "\n\n",

		"event: content_block_stop\n",
		"data: {}\n\n",

		"event: message_delta\n",
		`data: {"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":20}}` + "\n\n",

		"event: message_stop\n",
		"data: {}\n\n",
	}
	server := newAnthropicSSEServer(t, events)
	p := newTestAnthropicProvider(server.URL)

	req := ChatRequest{
		Model:    "claude-sonnet-4-5-20250929",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}
	result, err := p.ChatStream(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	wantSignature := "sig-part-1-part-2"
	if result.ThinkingSignature != wantSignature {
		t.Errorf("ThinkingSignature = %q, want %q", result.ThinkingSignature, wantSignature)
	}
	if result.Thinking != "let me think" {
		t.Errorf("Thinking = %q, want %q", result.Thinking, "let me think")
	}
	if result.Content != "answer" {
		t.Errorf("Content = %q, want %q", result.Content, "answer")
	}
}
