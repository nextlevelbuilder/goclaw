package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIProviderChat_NormalizesKimiTextToolCall(t *testing.T) {
	marker := `Dạ anh Hà ơi, em làm lại bản TTS nha!<|tool_calls_section_begin|><|tool_call_begin|>call_9e6c04c8a5df7b4aad726e9f933af3806b4<|tool_call_argument_begin|>{"text":"hello"}<|tool_call_end|><|tool_calls_section_end|>`
	srv := newOpenAIJSONServer(t, fmt.Sprintf(`{
		"id":"chatcmpl-1",
		"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]
	}`, marker))
	defer srv.Close()

	p := NewOpenAIProvider("kimi-coding", "sk", srv.URL, "kimi-k2.7").WithProviderType("kimi_coding")
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "say it"}},
		Tools: []ToolDefinition{{
			Type:     "function",
			Function: &ToolFunctionSchema{Name: "tts_synthesize"},
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if strings.Contains(resp.Content, "<|tool_calls_section_begin|>") {
		t.Fatalf("raw Kimi tool marker leaked into content: %q", resp.Content)
	}
	if resp.Content != "Dạ anh Hà ơi, em làm lại bản TTS nha!" {
		t.Fatalf("Content = %q", resp.Content)
	}
	if resp.FinishReason != "tool_calls" {
		t.Fatalf("FinishReason = %q, want tool_calls", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v, want 1", resp.ToolCalls)
	}
	if got := resp.ToolCalls[0].Name; got != "tts_synthesize" {
		t.Fatalf("ToolCalls[0].Name = %q, want tts_synthesize", got)
	}
	if got := resp.ToolCalls[0].Arguments["text"]; got != "hello" {
		t.Fatalf("ToolCalls[0].Arguments[text] = %v, want hello", got)
	}
}

func TestOpenAIProviderChat_InfersTextToolCallByUniqueSchemaWithMultipleTools(t *testing.T) {
	marker := `prefix<|tool_calls_section_begin|><|tool_call_begin|>call_unknown<|tool_call_argument_begin|>{"text":"hello"}<|tool_call_end|><|tool_calls_section_end|>`
	srv := newOpenAIJSONServer(t, fmt.Sprintf(`{
		"id":"chatcmpl-1",
		"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]
	}`, marker))
	defer srv.Close()

	p := NewOpenAIProvider("kimi-coding", "sk", srv.URL, "kimi-k2.7").WithProviderType("kimi_coding")
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "say it"}},
		Tools: []ToolDefinition{
			functionToolForTest("tts", []string{"text"}),
			functionToolForTest("web_search", []string{"query"}),
			functionToolForTest("create_audio", []string{"prompt"}),
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Content != "prefix" {
		t.Fatalf("Content = %q, want prefix", resp.Content)
	}
	if resp.FinishReason != "tool_calls" {
		t.Fatalf("FinishReason = %q, want tool_calls", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v, want 1", resp.ToolCalls)
	}
	if got := resp.ToolCalls[0].Name; got != "tts" {
		t.Fatalf("ToolCalls[0].Name = %q, want tts", got)
	}
	if got := resp.ToolCalls[0].Arguments["text"]; got != "hello" {
		t.Fatalf("ToolCalls[0].Arguments[text] = %v, want hello", got)
	}
}

func TestOpenAIProviderChat_DoesNotInferTextToolCallWhenSchemaAmbiguous(t *testing.T) {
	marker := `prefix<|tool_calls_section_begin|><|tool_call_begin|>call_unknown<|tool_call_argument_begin|>{"text":"hello"}<|tool_call_end|><|tool_calls_section_end|>`
	srv := newOpenAIJSONServer(t, fmt.Sprintf(`{
		"id":"chatcmpl-1",
		"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]
	}`, marker))
	defer srv.Close()

	p := NewOpenAIProvider("openai-compatible", "sk", srv.URL, "model")
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "say it"}},
		Tools: []ToolDefinition{
			functionToolForTest("tts", []string{"text"}),
			functionToolForTest("announce", []string{"text"}),
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Content != "prefix" {
		t.Fatalf("Content = %q, want prefix", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("ToolCalls = %+v, want none for ambiguous schema match", resp.ToolCalls)
	}
	if resp.FinishReason == "tool_calls" {
		t.Fatalf("FinishReason = %q, must not claim tool_calls when marker is ambiguous", resp.FinishReason)
	}
}

func TestOpenAIProviderChat_HandlesMixedNativeAndTextToolCalls(t *testing.T) {
	marker := `also voice<|tool_calls_section_begin|><|tool_call_begin|>call_voice<|tool_call_argument_begin|>{"text":"hello"}<|tool_call_end|><|tool_calls_section_end|>`
	srv := newOpenAIJSONServer(t, fmt.Sprintf(`{
		"id":"chatcmpl-1",
		"choices":[{
			"index":0,
			"message":{
				"role":"assistant",
				"content":%q,
				"tool_calls":[{
					"id":"call_datetime",
					"type":"function",
					"function":{"name":"datetime","arguments":"{\"timezone\":\"Asia/Ho_Chi_Minh\"}"}
				}]
			},
			"finish_reason":"tool_calls"
		}]
	}`, marker))
	defer srv.Close()

	p := NewOpenAIProvider("openai-compatible", "sk", srv.URL, "model")
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "say it"}},
		Tools: []ToolDefinition{
			functionToolForTest("datetime", []string{"timezone"}),
			functionToolForTest("tts", []string{"text"}),
			functionToolForTest("web_search", []string{"query"}),
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Content != "also voice" {
		t.Fatalf("Content = %q, want also voice", resp.Content)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("ToolCalls = %+v, want native datetime plus text tts", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Name != "datetime" || resp.ToolCalls[1].Name != "tts" {
		t.Fatalf("ToolCalls = %+v, want datetime then tts", resp.ToolCalls)
	}
}

func TestOpenAIProviderChat_HandlesMultipleTextToolCallsIndependently(t *testing.T) {
	marker := `prefix<|tool_calls_section_begin|>` +
		`<|tool_call_begin|>call_voice<|tool_call_argument_begin|>{"text":"hello"}<|tool_call_end|>` +
		`<|tool_call_begin|>call_search<|tool_call_argument_begin|>{"query":"weather"}<|tool_call_end|>` +
		`<|tool_calls_section_end|>`
	srv := newOpenAIJSONServer(t, fmt.Sprintf(`{
		"id":"chatcmpl-1",
		"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"tool_calls"}]
	}`, marker))
	defer srv.Close()

	p := NewOpenAIProvider("openai-compatible", "sk", srv.URL, "model")
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "say it"}},
		Tools: []ToolDefinition{
			functionToolForTest("tts", []string{"text"}),
			functionToolForTest("web_search", []string{"query"}),
			functionToolForTest("create_audio", []string{"prompt"}),
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Content != "prefix" {
		t.Fatalf("Content = %q, want prefix", resp.Content)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("ToolCalls = %+v, want 2", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Name != "tts" || resp.ToolCalls[1].Name != "web_search" {
		t.Fatalf("ToolCalls = %+v, want tts then web_search", resp.ToolCalls)
	}
}

func TestOpenAIProviderChat_DoesNotInferTextToolCallWhenArgsDoNotMatchSchema(t *testing.T) {
	marker := `prefix<|tool_calls_section_begin|><|tool_call_begin|>call_unknown<|tool_call_argument_begin|>{"unknown":"hello"}<|tool_call_end|><|tool_calls_section_end|>`
	srv := newOpenAIJSONServer(t, fmt.Sprintf(`{
		"id":"chatcmpl-1",
		"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]
	}`, marker))
	defer srv.Close()

	p := NewOpenAIProvider("openai-compatible", "sk", srv.URL, "model")
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "say it"}},
		Tools: []ToolDefinition{
			functionToolForTest("tts", []string{"text"}),
			functionToolForTest("web_search", []string{"query"}),
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Content != "prefix" {
		t.Fatalf("Content = %q, want prefix", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("ToolCalls = %+v, want none when args do not match schema", resp.ToolCalls)
	}
}

func TestOpenAIProviderChat_NormalizesThinkingTags(t *testing.T) {
	srv := newOpenAIJSONServer(t, `{
		"id":"chatcmpl-1",
		"choices":[{"index":0,"message":{"role":"assistant","content":"<thinking>hidden reasoning</thinking>visible answer"},"finish_reason":"stop"}]
	}`)
	defer srv.Close()

	p := NewOpenAIProvider("openai-compatible", "sk", srv.URL, "model")
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Content != "visible answer" {
		t.Fatalf("Content = %q, want visible answer", resp.Content)
	}
	if resp.Thinking != "hidden reasoning" {
		t.Fatalf("Thinking = %q, want hidden reasoning", resp.Thinking)
	}
}

func TestOpenAIProviderChatStream_NormalizesSplitKimiTextToolCallBeforeEmittingChunks(t *testing.T) {
	events := []string{
		`data: {"choices":[{"delta":{"content":"Dạ anh Hà ơi, em làm lại bản TTS nha!"}}]}` + "\n\n",
		`data: {"choices":[{"delta":{"content":"<|tool_calls_section_begin|><|tool_call_begin|>call_9e6"}}]}` + "\n\n",
		`data: {"choices":[{"delta":{"content":"c04c8<|tool_call_argument_begin|>{\"text\":\"hello\"}<|tool_call_end|><|tool_calls_section_end|>"}}]}` + "\n\n",
		`data: {"choices":[{"finish_reason":"stop","delta":{}}]}` + "\n\n",
	}
	srv := newOpenAISSEServer(t, events)
	defer srv.Close()

	p := NewOpenAIProvider("kimi-coding", "sk", srv.URL, "kimi-k2.7").WithProviderType("kimi_coding")
	var chunks []StreamChunk
	resp, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "say it"}},
		Tools: []ToolDefinition{{
			Type:     "function",
			Function: &ToolFunctionSchema{Name: "tts_synthesize"},
		}},
	}, func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "<|tool_") {
			t.Fatalf("raw tool marker leaked into stream chunk: %+v", chunk)
		}
	}
	if resp.Content != "Dạ anh Hà ơi, em làm lại bản TTS nha!" {
		t.Fatalf("Content = %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "tts_synthesize" {
		t.Fatalf("ToolCalls = %+v, want tts_synthesize call", resp.ToolCalls)
	}
}

func TestOpenAIProviderChatStream_PreservesWhitespaceAcrossContentDeltas(t *testing.T) {
	events := []string{
		`data: {"choices":[{"delta":{"content":"Dạ"}}]}` + "\n\n",
		`data: {"choices":[{"delta":{"content":" em"}}]}` + "\n\n",
		`data: {"choices":[{"delta":{"content":" xin"}}]}` + "\n\n",
		`data: {"choices":[{"delta":{"content":" lỗi"}}]}` + "\n\n",
		`data: {"choices":[{"delta":{"content":" anh"}}]}` + "\n\n",
		`data: {"choices":[{"finish_reason":"stop","delta":{}}]}` + "\n\n",
	}
	srv := newOpenAISSEServer(t, events)
	defer srv.Close()

	p := NewOpenAIProvider("openai-compatible", "sk", srv.URL, "model")
	var streamed strings.Builder
	resp, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "say it"}},
	}, func(chunk StreamChunk) {
		streamed.WriteString(chunk.Content)
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	const want = "Dạ em xin lỗi anh"
	if resp.Content != want {
		t.Fatalf("Content = %q, want %q", resp.Content, want)
	}
	if got := streamed.String(); got != want {
		t.Fatalf("streamed content = %q, want %q", got, want)
	}
}

func TestControlOutputNormalizer_PreservesWhitespaceAroundNormalText(t *testing.T) {
	n := newControlOutputNormalizer(nil)
	var got strings.Builder
	got.WriteString(n.Append("Dạ").Content)
	got.WriteString(n.Append(" em").Content)
	got.WriteString(n.Append(" xin").Content)
	got.WriteString(n.Append(" lỗi").Content)
	got.WriteString(n.Finish().Content)

	if got.String() != "Dạ em xin lỗi" {
		t.Fatalf("normalized stream content = %q, want %q", got.String(), "Dạ em xin lỗi")
	}
}

func newOpenAIJSONServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func functionToolForTest(name string, required []string) ToolDefinition {
	properties := make(map[string]any, len(required))
	for _, key := range required {
		properties[key] = map[string]any{"type": "string"}
	}
	return ToolDefinition{
		Type: "function",
		Function: &ToolFunctionSchema{
			Name: name,
			Parameters: map[string]any{
				"type":       "object",
				"properties": properties,
				"required":   required,
			},
		},
	}
}
