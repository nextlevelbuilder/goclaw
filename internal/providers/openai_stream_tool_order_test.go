package providers

import (
	"context"
	"testing"
)

// TestChatStream_ParallelToolCallsKeepIndexOrder checks that streamed
// tool calls are returned in ascending Index order. OpenAI-compatible
// providers identify each parallel call with delta.tool_calls[].index,
// and that index is the order the model assigned. A gap is included
// because some proxies (point-p1/9router) do not emit contiguous indexes;
// walking 0..len-1 panics on the hole, and ranging the accumulator map
// without sorting returns the calls in random order.
func TestChatStream_ParallelToolCallsKeepIndexOrder(t *testing.T) {
	t.Parallel()

	// Chunks arrive as index 2, then 0, then 5. The result must still be
	// first (0), third (2), fifth (5).
	chunks := []string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":2,"id":"call_c","type":"function","function":{"name":"third","arguments":""}}]}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":2,"function":{"arguments":"{\"n\":3}"}}]}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"first","arguments":""}}]}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"n\":1}"}}]}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":5,"id":"call_e","type":"function","function":{"name":"fifth","arguments":""}}]}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":5,"function":{"arguments":"{\"n\":5}"}}]}}]}` + "\n\n",
		`data: {"choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}` + "\n\n",
		"data: [DONE]\n\n",
	}
	want := []string{"first", "third", "fifth"}

	server := newOpenAISSEServer(t, chunks)
	p := newTestOpenAIProvider(server.URL)
	req := ChatRequest{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "run the tools"}},
	}

	// One pass can theoretically match index order. Repeating makes a
	// miss essentially certain while the accumulators are ranged as a map.
	const rounds = 25
	for i := 0; i < rounds; i++ {
		result, err := p.ChatStream(context.Background(), req, nil)
		if err != nil {
			t.Fatalf("round %d: ChatStream: %v", i, err)
		}
		if result.FinishReason != "tool_calls" {
			t.Fatalf("round %d: FinishReason = %q, want tool_calls", i, result.FinishReason)
		}
		if len(result.ToolCalls) != len(want) {
			t.Fatalf("round %d: got %d tool calls, want %d (%v)", i, len(result.ToolCalls), len(want), namesOf(result.ToolCalls))
		}
		for j, name := range want {
			got := result.ToolCalls[j]
			if got.Name != name {
				t.Fatalf("round %d: ToolCalls[%d].Name = %q, want %q (full order %v)", i, j, got.Name, name, namesOf(result.ToolCalls))
			}
			if got.ParseError != "" {
				t.Fatalf("round %d: tool %s ParseError = %q", i, name, got.ParseError)
			}
			if got.ID == "" {
				t.Fatalf("round %d: tool %s has empty id", i, name)
			}
		}
	}
}

func namesOf(calls []ToolCall) []string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Name
	}
	return names
}
