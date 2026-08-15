package providers

import (
	"context"
	"testing"
)

// TestAnthropicStream_StripThinkingDropsReasoningButKeepsUsage verifies that
// when OptStripThinking is set, thinking_delta events do NOT accumulate into
// ChatResponse.Thinking but Usage.ThinkingTokens is still estimated from the
// raw byte count (billing accuracy — Phase 1 depends on this).
func TestAnthropicStream_StripThinkingDropsReasoningButKeepsUsage(t *testing.T) {
	events := []string{
		"event: message_start\n",
		`data: {"message":{"usage":{"input_tokens":10}}}` + "\n\n",

		"event: content_block_start\n",
		`data: {"index":0,"content_block":{"type":"thinking","thinking":""}}` + "\n\n",

		"event: content_block_delta\n",
		`data: {"index":0,"delta":{"type":"thinking_delta","thinking":"raw chain of thought output that must be stripped"}}` + "\n\n",

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

	var chunks []StreamChunk
	req := ChatRequest{
		Model:    "kimi-k2",
		Messages: []Message{{Role: "user", Content: "hello"}},
		Options: map[string]any{
			OptStripThinking: true,
		},
	}
	result, err := p.ChatStream(context.Background(), req, func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Thinking != "" {
		t.Errorf("Thinking = %q, want empty (stripped)", result.Thinking)
	}
	if result.Content != "answer" {
		t.Errorf("Content = %q, want %q (content must still flow through)", result.Content, "answer")
	}
	// Usage.ThinkingTokens should still be counted from raw bytes:
	// len("raw chain of thought output that must be stripped") = 49 → 49/4 = 12 tokens.
	if result.Usage == nil || result.Usage.ThinkingTokens == 0 {
		t.Errorf("Usage.ThinkingTokens = 0, want >0 (billing must still count stripped thinking)")
	}
	// No thinking chunks should have been emitted to the caller.
	for _, c := range chunks {
		if c.Thinking != "" {
			t.Errorf("unexpected thinking chunk emitted while stripping: %q", c.Thinking)
		}
	}
}

// TestAnthropicStream_StripThinkingFalseKeepsReasoning is the backward-compat
// smoke test: when the flag is absent, existing behaviour is unchanged.
func TestAnthropicStream_StripThinkingFalseKeepsReasoning(t *testing.T) {
	events := []string{
		"event: content_block_delta\n",
		`data: {"index":0,"delta":{"type":"thinking_delta","thinking":"visible thought"}}` + "\n\n",
		"event: content_block_stop\n",
		"data: {}\n\n",
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
	if result.Thinking != "visible thought" {
		t.Errorf("Thinking = %q, want %q (no strip flag set)", result.Thinking, "visible thought")
	}
}
