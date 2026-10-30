package providers

import (
	"context"
	"testing"
)

// TestChatStream_ToolCallsKeepIndexOrder pins the order of streamed tool calls to
// their tc.Index. The accumulator is a map, so a plain range reordered multi-tool
// responses at random; the loop makes the old behaviour fail every run instead of
// one run in six. Indexes are non-contiguous on purpose (point-p1/9router skips slots).
func TestChatStream_ToolCallsKeepIndexOrder(t *testing.T) {
	chunks := []string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.txt\"}"}}]}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":2,"id":"call_b","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"b.txt\"}"}}]}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":5,"id":"call_c","type":"function","function":{"name":"list_dir","arguments":"{\"path\":\"c\"}"}}]}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}` + "\n\n",
		"data: [DONE]\n\n",
	}
	server := newOpenAISSEServer(t, chunks)
	p := newTestOpenAIProvider(server.URL)
	req := ChatRequest{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "three tools"}},
	}
	want := []string{"call_a", "call_b", "call_c"}

	for run := 0; run < 25; run++ {
		result, err := p.ChatStream(context.Background(), req, nil)
		if err != nil {
			t.Fatalf("run %d: unexpected error: %v", run, err)
		}
		if len(result.ToolCalls) != len(want) {
			t.Fatalf("run %d: expected %d tool calls, got %d", run, len(want), len(result.ToolCalls))
		}
		for i, tc := range result.ToolCalls {
			if tc.ID != want[i] {
				t.Fatalf("run %d: tool call %d is %q, want %q (order must follow tc.Index)", run, i, tc.ID, want[i])
			}
			if tc.ParseError != "" {
				t.Fatalf("run %d: tool call %q has ParseError %q", run, tc.ID, tc.ParseError)
			}
		}
	}
}
