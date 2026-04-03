package providers

import (
	"encoding/json"
	"testing"
)

// TestOpenAIMessage_ReasoningAliasUnmarshal tests that the "reasoning" field (Ollama alias)
// and "reasoning_content" field (DeepSeek canonical) are both deserialised correctly.
func TestOpenAIMessage_ReasoningAliasUnmarshal(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		wantRC      string // ReasoningContent
		wantR       string // Reasoning
		wantContent string
	}{
		{
			name:        "only reasoning_content present (DeepSeek style)",
			json:        `{"role":"assistant","content":"hi","reasoning_content":"think1"}`,
			wantRC:      "think1",
			wantR:       "",
			wantContent: "hi",
		},
		{
			name:        "only reasoning present (Ollama style)",
			json:        `{"role":"assistant","content":"hi","reasoning":"think2"}`,
			wantRC:      "",
			wantR:       "think2",
			wantContent: "hi",
		},
		{
			name:        "both fields present — reasoning_content is canonical, reasoning is alias",
			json:        `{"role":"assistant","content":"hi","reasoning_content":"canonical","reasoning":"alias"}`,
			wantRC:      "canonical",
			wantR:       "alias",
			wantContent: "hi",
		},
		{
			name:        "neither field present",
			json:        `{"role":"assistant","content":"hi"}`,
			wantRC:      "",
			wantR:       "",
			wantContent: "hi",
		},
		{
			name:        "reasoning empty string — does not override reasoning_content",
			json:        `{"role":"assistant","content":"hi","reasoning_content":"real","reasoning":""}`,
			wantRC:      "real",
			wantR:       "",
			wantContent: "hi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msg openAIMessage
			if err := json.Unmarshal([]byte(tt.json), &msg); err != nil {
				t.Fatalf("json.Unmarshal() error: %v", err)
			}
			if msg.ReasoningContent != tt.wantRC {
				t.Errorf("ReasoningContent = %q, want %q", msg.ReasoningContent, tt.wantRC)
			}
			if msg.Reasoning != tt.wantR {
				t.Errorf("Reasoning = %q, want %q", msg.Reasoning, tt.wantR)
			}
			if msg.Content != tt.wantContent {
				t.Errorf("Content = %q, want %q", msg.Content, tt.wantContent)
			}
		})
	}
}

// TestOpenAIStreamDelta_ReasoningAliasUnmarshal mirrors the message test for streaming delta.
func TestOpenAIStreamDelta_ReasoningAliasUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		json   string
		wantRC string
		wantR  string
	}{
		{
			name:   "only reasoning_content (DeepSeek streaming)",
			json:   `{"content":"","reasoning_content":"step1"}`,
			wantRC: "step1",
			wantR:  "",
		},
		{
			name:   "only reasoning (Ollama streaming)",
			json:   `{"content":"","reasoning":"step2"}`,
			wantRC: "",
			wantR:  "step2",
		},
		{
			name:   "both present — raw fields preserved individually",
			json:   `{"content":"","reasoning_content":"canonical","reasoning":"alias"}`,
			wantRC: "canonical",
			wantR:  "alias",
		},
		{
			name:   "neither present",
			json:   `{"content":"hello"}`,
			wantRC: "",
			wantR:  "",
		},
		{
			name:   "empty reasoning string — does not pollute reasoning_content",
			json:   `{"reasoning_content":"main","reasoning":""}`,
			wantRC: "main",
			wantR:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var delta openAIStreamDelta
			if err := json.Unmarshal([]byte(tt.json), &delta); err != nil {
				t.Fatalf("json.Unmarshal() error: %v", err)
			}
			if delta.ReasoningContent != tt.wantRC {
				t.Errorf("ReasoningContent = %q, want %q", delta.ReasoningContent, tt.wantRC)
			}
			if delta.Reasoning != tt.wantR {
				t.Errorf("Reasoning = %q, want %q", delta.Reasoning, tt.wantR)
			}
		})
	}
}

// TestParseResponse_ReasoningCoalesce verifies parseResponse coalesces reasoning/reasoning_content
// into result.Thinking in the correct precedence order.
func TestParseResponse_ReasoningCoalesce(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-4")

	tests := []struct {
		name          string
		msg           openAIMessage
		wantThinking  string
	}{
		{
			name:         "reasoning_content wins",
			msg:          openAIMessage{Content: "hi", ReasoningContent: "canonical"},
			wantThinking: "canonical",
		},
		{
			name:         "reasoning alias used when reasoning_content empty",
			msg:          openAIMessage{Content: "hi", Reasoning: "alias"},
			wantThinking: "alias",
		},
		{
			name:         "reasoning_content takes precedence over reasoning alias",
			msg:          openAIMessage{Content: "hi", ReasoningContent: "canonical", Reasoning: "alias"},
			wantThinking: "canonical",
		},
		{
			name:         "neither field — thinking is empty",
			msg:          openAIMessage{Content: "hi"},
			wantThinking: "",
		},
		{
			name:         "empty reasoning_content falls through to reasoning alias",
			msg:          openAIMessage{Content: "hi", ReasoningContent: "", Reasoning: "fallback"},
			wantThinking: "fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &openAIResponse{
				Choices: []openAIChoice{
					{Message: tt.msg, FinishReason: "stop"},
				},
			}
			got := p.parseResponse(resp)
			if got.Thinking != tt.wantThinking {
				t.Errorf("Thinking = %q, want %q", got.Thinking, tt.wantThinking)
			}
		})
	}
}

// TestBuildRequestBody_NoReasoningAliasInOutput verifies that buildRequestBody emits
// "reasoning_content" (not "reasoning") for assistant messages that have Thinking content.
// The "reasoning" key is an input-only alias; it must never appear in outgoing requests.
func TestBuildRequestBody_NoReasoningAliasInOutput(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "deepseek-reasoner")

	req := ChatRequest{
		Messages: []Message{
			{
				Role:     "assistant",
				Content:  "I will help.",
				Thinking: "internal step-by-step reasoning here",
			},
			{Role: "user", Content: "continue"},
		},
	}

	body := p.buildRequestBody("deepseek-reasoner", req, false)
	msgs, ok := body["messages"].([]map[string]any)
	if !ok {
		t.Fatal("buildRequestBody: messages field missing or wrong type")
	}

	var assistantMsg map[string]any
	for _, m := range msgs {
		if role, _ := m["role"].(string); role == "assistant" {
			assistantMsg = m
			break
		}
	}
	if assistantMsg == nil {
		t.Fatal("buildRequestBody: assistant message not found in output")
	}

	// Must emit reasoning_content (canonical key)
	rc, hasRC := assistantMsg["reasoning_content"]
	if !hasRC {
		t.Error("buildRequestBody: missing 'reasoning_content' in assistant message output")
	}
	if rcStr, _ := rc.(string); rcStr != "internal step-by-step reasoning here" {
		t.Errorf("reasoning_content = %q, want %q", rcStr, "internal step-by-step reasoning here")
	}

	// Must NOT emit the alias key "reasoning"
	if _, hasR := assistantMsg["reasoning"]; hasR {
		t.Error("buildRequestBody: outgoing message must not contain 'reasoning' alias key; use 'reasoning_content'")
	}
}

// TestBuildRequestBody_NoReasoningContentWhenThinkingEmpty confirms that assistant messages
// without Thinking content do not emit a reasoning_content key at all.
func TestBuildRequestBody_NoReasoningContentWhenThinkingEmpty(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-4")

	req := ChatRequest{
		Messages: []Message{
			{Role: "assistant", Content: "Sure.", Thinking: ""},
			{Role: "user", Content: "ok"},
		},
	}

	body := p.buildRequestBody("gpt-4", req, false)
	msgs := body["messages"].([]map[string]any)

	for _, m := range msgs {
		if role, _ := m["role"].(string); role == "assistant" {
			if _, hasRC := m["reasoning_content"]; hasRC {
				t.Error("reasoning_content must not be emitted when Thinking is empty")
			}
			if _, hasR := m["reasoning"]; hasR {
				t.Error("reasoning alias must not be emitted in outgoing messages")
			}
		}
	}
}
