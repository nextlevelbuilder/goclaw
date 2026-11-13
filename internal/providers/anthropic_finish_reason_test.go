package providers

import (
	"context"
	"testing"
)

func TestParseResponse_StopReason(t *testing.T) {
	p := &AnthropicProvider{}
	cases := []struct {
		stop string
		want string
	}{
		{"end_turn", "stop"},
		{"stop_sequence", "stop"},
		{"max_tokens", "length"},
		{"model_context_window_exceeded", "length"},
		{"tool_use", "tool_calls"},
	}
	for _, tc := range cases {
		got := p.parseResponse(&anthropicResponse{StopReason: tc.stop}).FinishReason
		if got != tc.want {
			t.Errorf("stop_reason %q: FinishReason = %q, want %q", tc.stop, got, tc.want)
		}
	}
}

func TestStreamChat_ContextWindowStopReason(t *testing.T) {
	events := []string{
		"event: message_start\n",
		`data: {"message":{"usage":{"input_tokens":10}}}` + "\n\n",
		"event: content_block_start\n",
		`data: {"index":0,"content_block":{"type":"text","text":""}}` + "\n\n",
		"event: content_block_delta\n",
		`data: {"index":0,"delta":{"type":"text_delta","text":"partial"}}` + "\n\n",
		"event: content_block_stop\n",
		"data: {}\n\n",
		"event: message_delta\n",
		`data: {"delta":{"stop_reason":"model_context_window_exceeded"},"usage":{"output_tokens":20}}` + "\n\n",
		"event: message_stop\n",
		"data: {}\n\n",
	}
	server := newAnthropicSSEServer(t, events)
	p := newTestAnthropicProvider(server.URL)

	result, err := p.ChatStream(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-5-20250929",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.FinishReason != "length" {
		t.Errorf("FinishReason = %q, want length", result.FinishReason)
	}
	if result.Content != "partial" {
		t.Errorf("Content = %q, want partial", result.Content)
	}
}
