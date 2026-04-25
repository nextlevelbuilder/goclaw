package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// staticTokenSource implements TokenSource for testing.
type staticTokenSource struct {
	token string
}

func (s *staticTokenSource) Token() (string, error) {
	return s.token, nil
}

// mustJSON marshals v to JSON string; panics on error.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// writeSSEDone writes a minimal SSE response with just a completed event and [DONE].
func writeSSEDone(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "data: %s\n\n", mustJSON(codexSSEEvent{
		Type:     "response.completed",
		Response: &codexAPIResponse{ID: "resp-1", Status: "completed"},
	}))
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func TestCodexProviderName(t *testing.T) {
	p := NewCodexProvider("openai-codex", &staticTokenSource{token: "test"}, "", "gpt-4o")
	if p.Name() != "openai-codex" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openai-codex")
	}
}

func TestCodexProviderDefaultModel(t *testing.T) {
	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, "", "")
	if p.DefaultModel() != "gpt-5.4" {
		t.Errorf("DefaultModel() = %q, want %q", p.DefaultModel(), "gpt-5.4")
	}

	p2 := NewCodexProvider("test", &staticTokenSource{token: "test"}, "", "o3")
	if p2.DefaultModel() != "o3" {
		t.Errorf("DefaultModel() = %q, want %q", p2.DefaultModel(), "o3")
	}
}

func TestCodexProviderBuildRequestBody(t *testing.T) {
	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, "", "gpt-4o")

	req := ChatRequest{
		Model: "gpt-4o",
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
			{Role: "user", Content: "How are you?"},
		},
		Options: map[string]any{
			OptMaxTokens:   1024,
			OptTemperature: 0.7,
		},
	}

	body := p.buildRequestBody(req, false)

	// Check model
	if body["model"] != "gpt-4o" {
		t.Errorf("model = %v, want gpt-4o", body["model"])
	}

	// Check stream
	if body["stream"] != false {
		t.Errorf("stream = %v, want false", body["stream"])
	}

	// Check store is false
	if body["store"] != false {
		t.Errorf("store = %v, want false", body["store"])
	}

	// Check instructions extracted from system message
	if body["instructions"] != "You are helpful." {
		t.Errorf("instructions = %v, want 'You are helpful.'", body["instructions"])
	}

	// Check input items (should exclude system messages)
	input, ok := body["input"].([]any)
	if !ok {
		t.Fatalf("input is not []interface{}: %T", body["input"])
	}
	if len(input) != 3 {
		t.Fatalf("input length = %d, want 3 (user + assistant + user)", len(input))
	}

	// chatgpt.com backend does not support max_output_tokens or temperature
	if _, ok := body["max_output_tokens"]; ok {
		t.Error("max_output_tokens should not be set (unsupported by chatgpt.com backend)")
	}
	if _, ok := body["temperature"]; ok {
		t.Error("temperature should not be set (unsupported by chatgpt.com backend)")
	}
}

func TestCodexProviderBuildRequestBodyWithTools(t *testing.T) {
	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, "", "gpt-4o")

	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "What's the weather?"}},
		Tools: []ToolDefinition{
			{
				Type: "function",
				Function: &ToolFunctionSchema{
					Name:        "get_weather",
					Description: "Get current weather",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
						},
						"required": []string{"city"},
					},
				},
			},
		},
	}

	body := p.buildRequestBody(req, true)

	tools, ok := body["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("tools is not []map[string]interface{}: %T", body["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("tools length = %d, want 1", len(tools))
	}

	tool := tools[0]
	if tool["type"] != "function" {
		t.Errorf("tool type = %v, want function", tool["type"])
	}
	if tool["name"] != "get_weather" {
		t.Errorf("tool name = %v, want get_weather", tool["name"])
	}
	if _, hasStrict := tool["strict"]; hasStrict {
		t.Error("tool should not have strict field")
	}
}

func TestCodexProviderBuildRequestBodyToolCallMessages(t *testing.T) {
	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, "", "gpt-4o")

	req := ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "What's the weather?"},
			{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{ID: "call_123", Name: "get_weather", Arguments: map[string]any{"city": "London"}},
				},
			},
			{Role: "tool", ToolCallID: "call_123", Content: `{"temp": 15}`},
		},
	}

	body := p.buildRequestBody(req, false)

	input, ok := body["input"].([]any)
	if !ok {
		t.Fatalf("input is not []interface{}: %T", body["input"])
	}
	if len(input) != 3 {
		t.Fatalf("input length = %d, want 3", len(input))
	}

	// Second item should be function_call
	fc, ok := input[1].(map[string]any)
	if !ok {
		t.Fatalf("input[1] is not map: %T", input[1])
	}
	if fc["type"] != "function_call" {
		t.Errorf("input[1] type = %v, want function_call", fc["type"])
	}
	if fc["name"] != "get_weather" {
		t.Errorf("input[1] name = %v, want get_weather", fc["name"])
	}

	// Third item should be function_call_output
	fco, ok := input[2].(map[string]any)
	if !ok {
		t.Fatalf("input[2] is not map: %T", input[2])
	}
	if fco["type"] != "function_call_output" {
		t.Errorf("input[2] type = %v, want function_call_output", fco["type"])
	}
	if fco["call_id"] != "fc_123" {
		t.Errorf("input[2] call_id = %v, want fc_123", fco["call_id"])
	}
}

func TestCodexProviderBuildRequestBodyThinking(t *testing.T) {
	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, "", "gpt-4o")

	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "Think about this"}},
		Options:  map[string]any{OptThinkingLevel: "high"},
	}

	body := p.buildRequestBody(req, false)

	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("reasoning is not map: %T", body["reasoning"])
	}
	if reasoning["effort"] != "high" {
		t.Errorf("reasoning effort = %v, want high", reasoning["effort"])
	}
}

func TestCodexProviderChat(t *testing.T) {
	// Mock Responses API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", r.Header.Get("Authorization"))
		}
		if r.Header.Get("OpenAI-Beta") != "responses=v1" {
			t.Errorf("OpenAI-Beta = %q, want responses=v1", r.Header.Get("OpenAI-Beta"))
		}

		// Verify endpoint
		if r.URL.Path != "/codex/responses" {
			t.Errorf("path = %q, want /responses", r.URL.Path)
		}

		// Verify request body
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "gpt-4o" {
			t.Errorf("request model = %v, want gpt-4o", body["model"])
		}
		if body["store"] != false {
			t.Errorf("request store = %v, want false", body["store"])
		}

		// Return SSE mock response (Chat delegates to ChatStream)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(codexSSEEvent{Type: "response.output_text.delta", Delta: "Hello! I'm doing great."}))
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(codexSSEEvent{
			Type: "response.output_item.done",
			Item: &codexItem{Type: "message", Role: "assistant"},
		}))
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(codexSSEEvent{
			Type: "response.completed",
			Response: &codexAPIResponse{
				ID: "resp-123", Status: "completed",
				Usage: &codexUsage{InputTokens: 10, OutputTokens: 8, TotalTokens: 18},
			},
		}))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := NewCodexProvider("openai-codex", &staticTokenSource{token: "test-token"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	result, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if result.Content != "Hello! I'm doing great." {
		t.Errorf("Content = %q, want 'Hello! I'm doing great.'", result.Content)
	}
	if result.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", result.FinishReason)
	}
	if result.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if result.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", result.Usage.PromptTokens)
	}
}

func TestCodexProviderChatStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		events := []string{
			`{"type":"response.output_text.delta","delta":"Hello"}`,
			`{"type":"response.output_text.delta","delta":" world"}`,
			`{"type":"response.completed","response":{"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}`,
		}

		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	p := NewCodexProvider("openai-codex", &staticTokenSource{token: "test"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	var chunks []string
	result, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	}, func(chunk StreamChunk) {
		if chunk.Content != "" {
			chunks = append(chunks, chunk.Content)
		}
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	if result.Content != "Hello world" {
		t.Errorf("Content = %q, want 'Hello world'", result.Content)
	}
	if len(chunks) != 2 {
		t.Errorf("chunks = %v, want 2 chunks", chunks)
	}
	if result.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if result.Usage.TotalTokens != 7 {
		t.Errorf("TotalTokens = %d, want 7", result.Usage.TotalTokens)
	}
}

func TestCodexProviderChatFallsBackToOutputTextDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(codexSSEEvent{
			Type: "response.output_text.done",
			Text: "<file name=\"SOUL.md\">hello</file>",
		}))
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(codexSSEEvent{
			Type: "response.completed",
			Response: &codexAPIResponse{
				ID: "resp-done", Status: "completed",
				Usage: &codexUsage{InputTokens: 5, OutputTokens: 4, TotalTokens: 9},
			},
		}))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	result, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Generate a file"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if result.Content != "<file name=\"SOUL.md\">hello</file>" {
		t.Errorf("Content = %q, want fallback output_text.done content", result.Content)
	}
}

func TestCodexProviderChatFallsBackToOutputItemMessageContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(codexSSEEvent{
			Type: "response.output_item.done",
			Item: &codexItem{
				Type:  "message",
				Role:  "assistant",
				Phase: "final_answer",
				Content: []codexContent{
					{Type: "output_text", Text: "<file name=\"IDENTITY.md\">world</file>"},
				},
			},
		}))
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(codexSSEEvent{
			Type: "response.completed",
			Response: &codexAPIResponse{
				ID: "resp-item", Status: "completed",
				Usage: &codexUsage{InputTokens: 5, OutputTokens: 4, TotalTokens: 9},
			},
		}))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	result, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Generate a file"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if result.Content != "<file name=\"IDENTITY.md\">world</file>" {
		t.Errorf("Content = %q, want fallback output_item.done content", result.Content)
	}
	if result.Phase != "final_answer" {
		t.Errorf("Phase = %q, want final_answer", result.Phase)
	}
}

func TestCodexProviderChatIgnoresCommentaryAndKeepsFinalAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		events := []codexSSEEvent{
			{
				Type:        "response.output_item.added",
				OutputIndex: 0,
				Item: &codexItem{
					ID:    "msg_commentary",
					Type:  "message",
					Phase: "commentary",
				},
			},
			{
				Type:         "response.output_text.delta",
				ItemID:       "msg_commentary",
				OutputIndex:  0,
				ContentIndex: 0,
				Delta:        "Planning out the answer...",
			},
			{
				Type:        "response.output_item.done",
				OutputIndex: 0,
				Item: &codexItem{
					ID:    "msg_commentary",
					Type:  "message",
					Phase: "commentary",
				},
			},
			{
				Type:        "response.output_item.added",
				OutputIndex: 1,
				Item: &codexItem{
					ID:    "msg_final",
					Type:  "message",
					Phase: "final_answer",
				},
			},
			{
				Type:         "response.output_text.delta",
				ItemID:       "msg_final",
				OutputIndex:  1,
				ContentIndex: 0,
				Delta:        "<file name=\"SOUL.md\">hello</file>",
			},
			{
				Type:        "response.output_item.done",
				OutputIndex: 1,
				Item: &codexItem{
					ID:    "msg_final",
					Type:  "message",
					Phase: "final_answer",
				},
			},
			{
				Type: "response.completed",
				Response: &codexAPIResponse{
					ID: "resp-commentary", Status: "completed",
					Usage: &codexUsage{InputTokens: 5, OutputTokens: 4, TotalTokens: 9},
				},
			},
		}

		for _, event := range events {
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(event))
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	var chunks []string
	result, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Generate files"}},
	}, func(chunk StreamChunk) {
		if chunk.Content != "" {
			chunks = append(chunks, chunk.Content)
		}
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	if result.Content != "<file name=\"SOUL.md\">hello</file>" {
		t.Errorf("Content = %q, want final_answer only", result.Content)
	}
	if len(chunks) != 1 || chunks[0] != "<file name=\"SOUL.md\">hello</file>" {
		t.Errorf("chunks = %v, want only final_answer chunk", chunks)
	}
	if result.Phase != "final_answer" {
		t.Errorf("Phase = %q, want final_answer", result.Phase)
	}
}

func TestCodexProviderChatConcatenatesMultipleOutputTextDoneParts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		events := []codexSSEEvent{
			{
				Type:        "response.output_item.added",
				OutputIndex: 0,
				Item: &codexItem{
					ID:    "msg_final",
					Type:  "message",
					Phase: "final_answer",
				},
			},
			{
				Type:         "response.output_text.done",
				ItemID:       "msg_final",
				OutputIndex:  0,
				ContentIndex: 1,
				Text:         "hello</file>",
			},
			{
				Type:         "response.output_text.done",
				ItemID:       "msg_final",
				OutputIndex:  0,
				ContentIndex: 0,
				Text:         "<file name=\"SOUL.md\">",
			},
			{
				Type: "response.completed",
				Response: &codexAPIResponse{
					ID: "resp-multipart", Status: "completed",
					Usage: &codexUsage{InputTokens: 5, OutputTokens: 4, TotalTokens: 9},
				},
			},
		}

		for _, event := range events {
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(event))
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	result, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Generate a file"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if result.Content != "<file name=\"SOUL.md\">hello</file>" {
		t.Errorf("Content = %q, want concatenated output_text.done parts", result.Content)
	}
}

func TestCodexProviderChatFallsBackToContentPartDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		events := []codexSSEEvent{
			{
				Type:        "response.output_item.added",
				OutputIndex: 0,
				Item: &codexItem{
					ID:    "msg_final",
					Type:  "message",
					Phase: "final_answer",
				},
			},
			{
				Type:         "response.content_part.done",
				ItemID:       "msg_final",
				OutputIndex:  0,
				ContentIndex: 0,
				Part: &codexContentPart{
					Type: "output_text",
					Text: "<frontmatter>summary</frontmatter>",
				},
			},
			{
				Type: "response.completed",
				Response: &codexAPIResponse{
					ID: "resp-content-part", Status: "completed",
					Usage: &codexUsage{InputTokens: 5, OutputTokens: 4, TotalTokens: 9},
				},
			},
		}

		for _, event := range events {
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(event))
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	result, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Generate a summary"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if result.Content != "<frontmatter>summary</frontmatter>" {
		t.Errorf("Content = %q, want content_part.done fallback content", result.Content)
	}
}

func TestCodexProviderChatFallsBackToCompletedResponseOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(codexSSEEvent{
			Type: "response.completed",
			Response: &codexAPIResponse{
				ID:     "resp-completed",
				Status: "completed",
				Output: []codexItem{
					{
						ID:    "msg_commentary",
						Type:  "message",
						Role:  "assistant",
						Phase: "commentary",
						Content: []codexContent{
							{Type: "output_text", Text: "Draft commentary"},
						},
					},
					{
						ID:    "msg_final",
						Type:  "message",
						Role:  "assistant",
						Phase: "final_answer",
						Content: []codexContent{
							{Type: "output_text", Text: "<frontmatter>summary</frontmatter>"},
						},
					},
				},
				Usage: &codexUsage{InputTokens: 6, OutputTokens: 3, TotalTokens: 9},
			},
		}))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	result, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Generate a summary"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if result.Content != "<frontmatter>summary</frontmatter>" {
		t.Errorf("Content = %q, want fallback response.completed output content", result.Content)
	}
	if result.Phase != "final_answer" {
		t.Errorf("Phase = %q, want final_answer", result.Phase)
	}
}

func TestCodexProviderChatStreamToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		events := []string{
			`{"type":"response.function_call_arguments.delta","item_id":"item_1","delta":"{\"city\""}`,
			`{"type":"response.function_call_arguments.delta","item_id":"item_1","delta":":\"London\"}"}`,
			`{"type":"response.output_item.done","item":{"type":"function_call","id":"item_1","call_id":"call_abc","name":"get_weather","arguments":"{\"city\":\"London\"}"}}`,
			`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`,
		}

		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	result, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Weather?"}},
	}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	if result.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want tool_calls", result.FinishReason)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("ToolCalls length = %d, want 1", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.Name != "get_weather" {
		t.Errorf("ToolCall name = %q, want get_weather", tc.Name)
	}
	if tc.ID != "call_abc" {
		t.Errorf("ToolCall ID = %q, want call_abc", tc.ID)
	}
}

func TestCodexProviderChatToolCallsNonStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(codexSSEEvent{
			Type: "response.output_item.done",
			Item: &codexItem{
				Type:      "function_call",
				ID:        "item_1",
				CallID:    "call_xyz",
				Name:      "search",
				Arguments: `{"query":"test"}`,
			},
		}))
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(codexSSEEvent{
			Type: "response.completed",
			Response: &codexAPIResponse{
				ID: "resp-456", Status: "completed",
				Usage: &codexUsage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
			},
		}))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	result, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Search"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if result.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want tool_calls", result.FinishReason)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %d, want 1", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Arguments["query"] != "test" {
		t.Errorf("ToolCall arg query = %v, want test", result.ToolCalls[0].Arguments["query"])
	}
}

func TestCodexProviderHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error = %q, expected to contain 'rate limited'", err.Error())
	}
}

func TestCodexProviderMultipleSystemMessages(t *testing.T) {
	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, "", "gpt-4o")

	req := ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "First instruction."},
			{Role: "system", Content: "Second instruction."},
			{Role: "user", Content: "Hello"},
		},
	}

	body := p.buildRequestBody(req, false)

	expected := "First instruction.\n\nSecond instruction."
	if body["instructions"] != expected {
		t.Errorf("instructions = %q, want %q", body["instructions"], expected)
	}
}

func TestCodexProviderStreamReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		events := []string{
			`{"type":"response.output_item.done","item":{"type":"reasoning","summary":[{"type":"summary_text","text":"Thinking about this..."}]}}`,
			`{"type":"response.output_text.delta","delta":"The answer is 42."}`,
			`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30,"output_tokens_details":{"reasoning_tokens":15}}}}`,
		}

		for _, e := range events {
			fmt.Fprintf(w, "data: %s\n\n", e)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	var thinkingChunks []string
	result, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Think"}},
	}, func(chunk StreamChunk) {
		if chunk.Thinking != "" {
			thinkingChunks = append(thinkingChunks, chunk.Thinking)
		}
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	if result.Thinking != "Thinking about this..." {
		t.Errorf("Thinking = %q, want 'Thinking about this...'", result.Thinking)
	}
	if result.Content != "The answer is 42." {
		t.Errorf("Content = %q, want 'The answer is 42.'", result.Content)
	}
	if result.Usage != nil && result.Usage.ThinkingTokens != 15 {
		t.Errorf("ThinkingTokens = %d, want 15", result.Usage.ThinkingTokens)
	}
}

// Verify the endpoint format (apiBase + /responses)
func TestCodexProviderEndpoint(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		writeSSEDone(w)
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1

	p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	})

	if capturedPath != "/codex/responses" {
		t.Errorf("endpoint path = %q, want /responses", capturedPath)
	}
}

// Verify trailing slash is stripped from apiBase
func TestCodexProviderAPIBaseTrailingSlash(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		writeSSEDone(w)
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL+"/", "gpt-4o")
	p.retryConfig.Attempts = 1
	p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	})

	if capturedPath != "/codex/responses" {
		t.Errorf("endpoint path = %q, want /responses (trailing slash should be stripped)", capturedPath)
	}
}

// Ensure token is used from TokenSource
func TestCodexProviderTokenSource(t *testing.T) {
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		writeSSEDone(w)
	}))
	defer server.Close()

	p := NewCodexProvider("test", &staticTokenSource{token: "my-oauth-token"}, server.URL, "gpt-4o")
	p.retryConfig.Attempts = 1
	p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	})

	if capturedAuth != "Bearer my-oauth-token" {
		t.Errorf("Authorization = %q, want 'Bearer my-oauth-token'", capturedAuth)
	}
}

// TestCodexBuildRequestBodyImageGenerationTool verifies that a ToolDefinition with
// Type="image_generation" produces a native image_generation object in the tools array,
// not a function-shaped entry.
func TestCodexBuildRequestBodyImageGenerationTool(t *testing.T) {
	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, "", "gpt-4o")

	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "Draw a cat"}},
		Tools: []ToolDefinition{
			{Type: "image_generation"},
		},
	}

	body := p.buildRequestBody(req, false)

	tools, ok := body["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("tools is not []map[string]any: %T", body["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("tools length = %d, want 1", len(tools))
	}

	tool := tools[0]
	if tool["type"] != "image_generation" {
		t.Errorf("tool[type] = %v, want image_generation", tool["type"])
	}
	if tool["action"] != "generate" {
		t.Errorf("tool[action] = %v, want generate", tool["action"])
	}
	if tool["model"] != "gpt-image-2" {
		t.Errorf("tool[model] = %v, want gpt-image-2", tool["model"])
	}
	if tool["output_format"] != "png" {
		t.Errorf("tool[output_format] = %v, want png", tool["output_format"])
	}
	if tool["partial_images"] != 1 {
		t.Errorf("tool[partial_images] = %v, want 1", tool["partial_images"])
	}
	// Must NOT contain function-specific fields.
	if _, has := tool["name"]; has {
		t.Error("image_generation tool must not have 'name' field")
	}
	if _, has := tool["parameters"]; has {
		t.Error("image_generation tool must not have 'parameters' field")
	}
}

// TestCodexBuildRequestBodyMixedTools verifies that a request containing both function
// tools and an image_generation tool produces both in the correct shapes, in order.
func TestCodexBuildRequestBodyMixedTools(t *testing.T) {
	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, "", "gpt-4o")

	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "Search and draw"}},
		Tools: []ToolDefinition{
			{
				Type: "function",
				Function: &ToolFunctionSchema{
					Name:        "web_search",
					Description: "Search the web",
					Parameters: map[string]any{
						"type":       "object",
						"properties": map[string]any{"query": map[string]any{"type": "string"}},
						"required":   []string{"query"},
					},
				},
			},
			{Type: "image_generation"},
		},
	}

	body := p.buildRequestBody(req, false)

	tools, ok := body["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("tools is not []map[string]any: %T", body["tools"])
	}
	if len(tools) != 2 {
		t.Fatalf("tools length = %d, want 2", len(tools))
	}

	// First tool: function shape.
	fn := tools[0]
	if fn["type"] != "function" {
		t.Errorf("tools[0] type = %v, want function", fn["type"])
	}
	if fn["name"] != "web_search" {
		t.Errorf("tools[0] name = %v, want web_search", fn["name"])
	}

	// Second tool: native image_generation shape.
	img := tools[1]
	if img["type"] != "image_generation" {
		t.Errorf("tools[1] type = %v, want image_generation", img["type"])
	}
	if img["action"] != "generate" {
		t.Errorf("tools[1] action = %v, want generate", img["action"])
	}
	if img["model"] != "gpt-image-2" {
		t.Errorf("tools[1] model = %v, want gpt-image-2", img["model"])
	}
	// Function field must not bleed into image tool.
	if _, has := img["name"]; has {
		t.Error("image_generation tool must not contain 'name'")
	}
}

// Verify request body includes image content
func TestCodexProviderBuildRequestBodyWithImages(t *testing.T) {
	p := NewCodexProvider("test", &staticTokenSource{token: "test"}, "", "gpt-4o")

	req := ChatRequest{
		Messages: []Message{
			{
				Role:    "user",
				Content: "What's this?",
				Images:  []ImageContent{{MimeType: "image/png", Data: "base64data"}},
			},
		},
	}

	body := p.buildRequestBody(req, false)

	input, ok := body["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("expected 1 input item, got %v", body["input"])
	}

	item, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("input[0] is not map: %T", input[0])
	}

	content, ok := item["content"].([]map[string]any)
	if !ok {
		t.Fatalf("content is not []map: %T", item["content"])
	}

	// Should have image + text
	if len(content) != 2 {
		t.Fatalf("content length = %d, want 2 (image + text)", len(content))
	}
	if content[0]["type"] != "input_image" {
		t.Errorf("content[0] type = %v, want input_image", content[0]["type"])
	}
	if content[1]["type"] != "input_text" {
		t.Errorf("content[1] type = %v, want input_text", content[1]["type"])
	}
}
