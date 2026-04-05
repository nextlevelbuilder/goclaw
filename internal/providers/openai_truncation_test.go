package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newOpenAISSEServer creates a mock SSE server for OpenAI-compatible streaming.
func newOpenAISSEServer(t *testing.T, chunks []string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter does not implement http.Flusher")
			return
		}
		for _, chunk := range chunks {
			fmt.Fprint(w, chunk)
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func newTestOpenAIProvider(baseURL string) *OpenAIProvider {
	p := NewOpenAIProvider("test", "test-key", baseURL, "gpt-4")
	p.retryConfig.Attempts = 1
	return p
}

// TestChatStream_TruncatedToolCallArgs verifies that when a stream is cut mid-JSON
// (finish_reason: "length"), FinishReason is preserved as "length" and ParseError is set.
func TestChatStream_TruncatedToolCallArgs(t *testing.T) {
	chunks := []string{
		// Tool call with partial arguments (truncated JSON)
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_abc123","type":"function","function":{"name":"write_file","arguments":""}}]}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"/tmp/test.txt\",\"content\":\"hello wor"}}]}}]}` + "\n\n",
		// Stream truncated — finish_reason: "length"
		`data: {"choices":[{"index":0,"finish_reason":"length","delta":{}}]}` + "\n\n",
		"data: [DONE]\n\n",
	}

	server := newOpenAISSEServer(t, chunks)
	p := newTestOpenAIProvider(server.URL)

	req := ChatRequest{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "write a file"}},
	}
	result, err := p.ChatStream(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// FinishReason must be "length", NOT "tool_calls"
	if result.FinishReason != "length" {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, "length")
	}

	// Tool call should still be present (for logging) but with ParseError set
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ParseError == "" {
		t.Error("expected ParseError to be set for truncated JSON args")
	}
	if tc.Name != "write_file" {
		t.Errorf("tool name = %q, want %q", tc.Name, "write_file")
	}
	// Arguments should be empty map (parse failed)
	if len(tc.Arguments) != 0 {
		t.Errorf("expected empty args for truncated JSON, got %v", tc.Arguments)
	}
}

// TestChatStream_CompleteToolCallArgs verifies that normal (non-truncated) tool calls
// still get FinishReason = "tool_calls" and no ParseError.
func TestChatStream_CompleteToolCallArgs(t *testing.T) {
	chunks := []string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_ok","type":"function","function":{"name":"read_file","arguments":""}}]}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"/tmp/test.txt\"}"}}]}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"finish_reason":"stop","delta":{}}]}` + "\n\n",
		"data: [DONE]\n\n",
	}

	server := newOpenAISSEServer(t, chunks)
	p := newTestOpenAIProvider(server.URL)

	req := ChatRequest{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "read a file"}},
	}
	result, err := p.ChatStream(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Normal tool call: FinishReason should be "tool_calls"
	if result.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, "tool_calls")
	}

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ParseError != "" {
		t.Errorf("expected no ParseError, got %q", tc.ParseError)
	}
	if tc.Arguments["path"] != "/tmp/test.txt" {
		t.Errorf("expected path=/tmp/test.txt, got %v", tc.Arguments["path"])
	}
}

// TestChatStream_MultipleToolCalls_OneTruncated verifies that when one tool call
// has valid args and another is truncated, ParseError is set only on the truncated one.
func TestChatStream_MultipleToolCalls_OneTruncated(t *testing.T) {
	chunks := []string{
		// Tool 0: complete args
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read_file","arguments":""}}]}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"ok.txt\"}"}}]}}]}` + "\n\n",
		// Tool 1: truncated args
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"write_file","arguments":""}}]}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"path\":\"big.txt\",\"content\":\"trunc"}}]}}]}` + "\n\n",
		// Truncated
		`data: {"choices":[{"index":0,"finish_reason":"length","delta":{}}]}` + "\n\n",
		"data: [DONE]\n\n",
	}

	server := newOpenAISSEServer(t, chunks)
	p := newTestOpenAIProvider(server.URL)

	req := ChatRequest{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "multi tool"}},
	}
	result, err := p.ChatStream(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.FinishReason != "length" {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, "length")
	}
	if len(result.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(result.ToolCalls))
	}

	// Tool 0 should be valid
	if result.ToolCalls[0].ParseError != "" {
		t.Errorf("tool 0: unexpected ParseError %q", result.ToolCalls[0].ParseError)
	}
	// Tool 1 should have parse error
	if result.ToolCalls[1].ParseError == "" {
		t.Error("tool 1: expected ParseError for truncated args")
	}
}

// TestParseResponse_TruncatedToolCallArgs verifies the non-streaming path
// preserves FinishReason "length" and sets ParseError.
func TestParseResponse_TruncatedToolCallArgs(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-4")

	// Simulate a non-streaming response with truncated tool call args
	resp := &openAIResponse{
		Choices: []openAIChoice{
			{
				FinishReason: "length",
				Message: openAIMessage{
					Role: "assistant",
					ToolCalls: []openAIToolCall{
						{
							ID:   "call_trunc",
							Type: "function",
							Function: openAIFunctionCall{
								Name:      "write_file",
								Arguments: `{"path":"/tmp/x","content":"hello wor`, // truncated
							},
						},
					},
				},
			},
		},
	}

	result := p.parseResponse(resp)

	// FinishReason must stay "length"
	if result.FinishReason != "length" {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, "length")
	}

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].ParseError == "" {
		t.Error("expected ParseError for truncated JSON")
	}
}

// TestParseResponse_ValidToolCallArgs verifies the non-streaming path
// overrides FinishReason to "tool_calls" when args are valid.
func TestParseResponse_ValidToolCallArgs(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-4")

	resp := &openAIResponse{
		Choices: []openAIChoice{
			{
				FinishReason: "stop",
				Message: openAIMessage{
					Role: "assistant",
					ToolCalls: []openAIToolCall{
						{
							ID:   "call_ok",
							Type: "function",
							Function: openAIFunctionCall{
								Name:      "read_file",
								Arguments: `{"path":"/tmp/test.txt"}`,
							},
						},
					},
				},
			},
		},
	}

	result := p.parseResponse(resp)

	if result.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, "tool_calls")
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].ParseError != "" {
		t.Errorf("unexpected ParseError: %q", result.ToolCalls[0].ParseError)
	}
}
