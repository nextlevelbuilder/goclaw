package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOllamaBuildRequest_ThinkDefaultsToFalse verifies that when no
// provider-level thinking override is configured, buildRequest disables
// thinking by default (matches the OpenAI-compat Ollama path).
func TestOllamaBuildRequest_ThinkDefaultsToFalse(t *testing.T) {
	p := NewOllamaProvider("ollama", "http://localhost:11434", "llama3.3", nil, nil)
	req := ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}
	ollamaReq := p.buildRequest(context.Background(), req, false)

	require.NotNil(t, ollamaReq.Think)
	value, ok := ollamaReq.Think.Value.(bool)
	require.True(t, ok, "expected Think.Value to be a bool")
	assert.False(t, value)
}

// TestOllamaBuildRequest_ThinkOverride verifies that WithThinkingEnabled
// correctly sets ollamaReq.Think for both true and false overrides.
func TestOllamaBuildRequest_ThinkOverride(t *testing.T) {
	trueVal := true
	falseVal := false

	cases := []struct {
		name     string
		override *bool
		want     bool
	}{
		{name: "override true enables thinking", override: &trueVal, want: true},
		{name: "override false disables thinking", override: &falseVal, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewOllamaProvider("ollama", "http://localhost:11434", "llama3.3", nil, nil).
				WithThinkingEnabled(tc.override)
			req := ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}
			ollamaReq := p.buildRequest(context.Background(), req, false)

			require.NotNil(t, ollamaReq.Think)
			value, ok := ollamaReq.Think.Value.(bool)
			require.True(t, ok, "expected Think.Value to be a bool")
			assert.Equal(t, tc.want, value)
		})
	}
}

// TestOllamaThinkingEnabledAccessor verifies the WithThinkingEnabled /
// ThinkingEnabled getter/setter round-trip.
func TestOllamaThinkingEnabledAccessor(t *testing.T) {
	p := NewOllamaProvider("ollama", "http://localhost:11434", "llama3.3", nil, nil)
	assert.Nil(t, p.ThinkingEnabled())

	trueVal := true
	p.WithThinkingEnabled(&trueVal)
	require.NotNil(t, p.ThinkingEnabled())
	assert.True(t, *p.ThinkingEnabled())
}

// TestOllamaChatStream_ToolCallsBeforeDoneChunk verifies that tool calls are
// kept when Ollama emits them in an intermediate (done=false) chunk and sends
// a final done=true chunk with an empty message, which is how Ollama streams
// tool calls.
func TestOllamaChatStream_ToolCallsBeforeDoneChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, `{"model":"m","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"get_weather","arguments":{"city":"Paris"}}}]},"done":false}`+"\n")
		_, _ = io.WriteString(w, `{"model":"m","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":3,"eval_count":5}`+"\n")
	}))
	defer srv.Close()

	numCtx := 4096
	p := NewOllamaProvider("ollama", srv.URL, "m", &numCtx, nil)
	resp, err := p.ChatStream(context.Background(),
		ChatRequest{Messages: []Message{{Role: "user", Content: "weather in Paris?"}}},
		func(StreamChunk) {})

	require.NoError(t, err)
	require.Len(t, resp.ToolCalls, 1)
	assert.Equal(t, "get_weather", resp.ToolCalls[0].Name)
	assert.Equal(t, "Paris", resp.ToolCalls[0].Arguments["city"])
	assert.Equal(t, "tool_calls", resp.FinishReason)
	assert.Empty(t, resp.Content)
	require.NotNil(t, resp.Usage)
	assert.Equal(t, 8, resp.Usage.TotalTokens)
}

func TestOllamaChatStream_ToolCallsOnlyInDoneChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, `{"model":"m","message":{"role":"assistant","content":""},"done":false}`+"\n")
		_, _ = io.WriteString(w, `{"model":"m","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"get_weather","arguments":{"city":"Paris"}}}]},"done":true,"done_reason":"stop"}`+"\n")
	}))
	defer srv.Close()

	numCtx := 4096
	p := NewOllamaProvider("ollama", srv.URL, "m", &numCtx, nil)
	resp, err := p.ChatStream(context.Background(),
		ChatRequest{Messages: []Message{{Role: "user", Content: "weather in Paris?"}}},
		func(StreamChunk) {})

	require.NoError(t, err)
	require.Len(t, resp.ToolCalls, 1)
	assert.Equal(t, "get_weather", resp.ToolCalls[0].Name)
	assert.Equal(t, "tool_calls", resp.FinishReason)
	assert.Empty(t, resp.Content)
}

func TestOllamaChatStream_ToolCallRepeatedInDoneChunkIsNotDuplicated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		call := `"tool_calls":[{"function":{"name":"get_weather","arguments":{"city":"Paris"}}}]`
		_, _ = io.WriteString(w, `{"model":"m","message":{"role":"assistant","content":"",`+call+`},"done":false}`+"\n")
		_, _ = io.WriteString(w, `{"model":"m","message":{"role":"assistant","content":"",`+call+`},"done":true,"done_reason":"stop"}`+"\n")
	}))
	defer srv.Close()

	numCtx := 4096
	p := NewOllamaProvider("ollama", srv.URL, "m", &numCtx, nil)
	resp, err := p.ChatStream(context.Background(),
		ChatRequest{Messages: []Message{{Role: "user", Content: "weather in Paris?"}}},
		func(StreamChunk) {})

	require.NoError(t, err)
	require.Len(t, resp.ToolCalls, 1)
}
