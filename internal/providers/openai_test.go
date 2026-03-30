package providers

import (
	"strings"
	"testing"
)

func TestTruncateToolCallID(t *testing.T) {
	t.Run("short IDs stay unchanged", func(t *testing.T) {
		ids := []string{
			"",
			"call_abc123",
			"call_0123456789abcdef0123456789abcdef012", // exactly 40 chars
		}
		for _, id := range ids {
			if got := truncateToolCallID(id); got != id {
				t.Errorf("truncateToolCallID(%q) = %q, want unchanged", id, got)
			}
		}
	})

	t.Run("long IDs are shortened deterministically", func(t *testing.T) {
		id := "call_0123456789abcdef0123456789abcdef01234"
		got1 := truncateToolCallID(id)
		got2 := truncateToolCallID(id)
		if got1 != got2 {
			t.Fatalf("truncateToolCallID(%q) should be deterministic: %q != %q", id, got1, got2)
		}
		if len(got1) != maxToolCallIDLen {
			t.Fatalf("truncateToolCallID(%q) length = %d, want %d", id, len(got1), maxToolCallIDLen)
		}
		if got1 == id {
			t.Fatalf("truncateToolCallID(%q) should shorten long IDs", id)
		}
		if !strings.HasPrefix(got1, "call_") {
			t.Fatalf("truncateToolCallID(%q) should preserve call_ prefix, got %q", id, got1)
		}
	})

	t.Run("shared-prefix long IDs stay unique", func(t *testing.T) {
		prefix40 := "call_0123456789abcdef0123456789abcdef012"
		id1 := prefix40 + "_0"
		id2 := prefix40 + "_1"
		got1 := truncateToolCallID(id1)
		got2 := truncateToolCallID(id2)
		if got1 == got2 {
			t.Fatalf("shared-prefix IDs collided after shortening: %q", got1)
		}
		if len(got1) > maxToolCallIDLen || len(got2) > maxToolCallIDLen {
			t.Fatalf("shared-prefix IDs should be <= %d chars: %q / %q", maxToolCallIDLen, got1, got2)
		}
	})
}

func TestBuildRequestBody_TemperatureSkippedForReasoningModels(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-4")

	// These model families don't support custom temperature (locked to default).
	// This is a model-level constraint, not provider-specific.
	models := []string{
		"gpt-5-mini",
		"gpt-5-mini-2025-01",
		"gpt-5-nano",
		"o1",
		"o1-mini",
		"o1-preview",
		"o3",
		"o3-mini",
		"o4-mini",
		"openai/gpt-5-mini",
		"openai/o3-mini",
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			req := ChatRequest{
				Messages: []Message{{Role: "user", Content: "test"}},
				Options: map[string]any{
					OptTemperature: 0.7,
				},
			}
			body := p.buildRequestBody(model, req, false)
			if _, hasTemp := body["temperature"]; hasTemp {
				t.Errorf("model %q: temperature should be skipped but was included", model)
			}
		})
	}
}

func TestBuildRequestBody_TemperatureKeptForNonReasoningModels(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-4")

	// These models DO support custom temperature -- must not be suppressed.
	models := []string{
		"gpt-4",
		"gpt-4o",
		"gpt-4-turbo",
		"gpt-5",
		"gpt-5.1",
		"gpt-5.4",
		"openai/gpt-5.4",
		"claude-3-sonnet",
		"llama-3",
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			req := ChatRequest{
				Messages: []Message{{Role: "user", Content: "test"}},
				Options: map[string]any{
					OptTemperature: 0.7,
				},
			}
			body := p.buildRequestBody(model, req, false)
			if _, hasTemp := body["temperature"]; !hasTemp {
				t.Errorf("model %q: temperature should be included but was skipped", model)
			}
		})
	}
}

func TestBuildRequestBody_TemperatureDependsOnModelNotAPIBase(t *testing.T) {
	p := NewOpenAIProvider("azure", "key", "https://example.openai.azure.com/openai/deployments/test", "gpt-4")

	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
		Options: map[string]any{
			OptTemperature: 0.7,
		},
	}

	body := p.buildRequestBody("gpt-5.4", req, false)
	if _, hasTemp := body["temperature"]; !hasTemp {
		t.Fatal("azure-backed gpt-5.4 should keep temperature; gating must stay model-based")
	}
}

func TestBuildRequestBody_PrefixedModelsUseCorrectTokenField(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-4")

	tests := []struct {
		model             string
		wantCompletionKey bool
	}{
		{model: "openai/o3-mini", wantCompletionKey: true},
		{model: "openai/gpt-5.4", wantCompletionKey: true},
		{model: "openai/gpt-4o", wantCompletionKey: false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			req := ChatRequest{
				Messages: []Message{{Role: "user", Content: "test"}},
				Options: map[string]any{
					OptMaxTokens: 123,
				},
			}
			body := p.buildRequestBody(tt.model, req, false)
			_, hasCompletionKey := body["max_completion_tokens"]
			_, hasMaxTokens := body["max_tokens"]
			if hasCompletionKey != tt.wantCompletionKey {
				t.Fatalf("model %q: max_completion_tokens present = %v, want %v", tt.model, hasCompletionKey, tt.wantCompletionKey)
			}
			if hasMaxTokens == tt.wantCompletionKey {
				t.Fatalf("model %q: max_tokens/max_completion_tokens routing incorrect: body=%v", tt.model, body)
			}
		})
	}
}

func TestBuildRequestBody_ToolCallIDsTruncated(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-4")

	longID := "call_0123456789abcdef0123456789abcdef01234" // 42 chars

	req := ChatRequest{
		Messages: []Message{
			{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{ID: longID, Name: "test_fn", Arguments: map[string]any{"arg": "val"}},
				},
			},
			{
				Role:       "tool",
				ToolCallID: longID,
				Content:    "result",
			},
			{Role: "user", Content: "continue"},
		},
	}

	body := p.buildRequestBody("gpt-4", req, false)
	msgs := body["messages"].([]map[string]any)

	var assistantID, toolResultID string
	for _, msg := range msgs {
		if tcs, ok := msg["tool_calls"]; ok {
			toolCalls := tcs.([]map[string]any)
			assistantID = toolCalls[0]["id"].(string)
			if len(assistantID) > 40 {
				t.Errorf("tool_calls[0].id length = %d, want <= 40", len(assistantID))
			}
		}
		if tcid, ok := msg["tool_call_id"]; ok {
			toolResultID = tcid.(string)
			if len(toolResultID) > 40 {
				t.Errorf("tool_call_id length = %d, want <= 40", len(toolResultID))
			}
		}
	}

	// Critical: truncated IDs must match for API correlation
	if assistantID != toolResultID {
		t.Errorf("ID correlation broken: tool_calls.id=%q != tool_call_id=%q", assistantID, toolResultID)
	}
}

func TestBuildRequestBody_LegacyLongToolCallIDsStayUnique(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-4")

	prefix40 := "call_0123456789abcdef0123456789abcdef012"
	id1 := prefix40 + "_0"
	id2 := prefix40 + "_1"

	req := ChatRequest{
		Messages: []Message{
			{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{ID: id1, Name: "fn1", Arguments: map[string]any{}},
					{ID: id2, Name: "fn2", Arguments: map[string]any{}},
				},
			},
			{Role: "tool", ToolCallID: id1, Content: "result-1"},
			{Role: "tool", ToolCallID: id2, Content: "result-2"},
			{Role: "user", Content: "continue"},
		},
	}

	body := p.buildRequestBody("gpt-4", req, false)
	msgs := body["messages"].([]map[string]any)

	toolCalls := msgs[0]["tool_calls"].([]map[string]any)
	assistantID1 := toolCalls[0]["id"].(string)
	assistantID2 := toolCalls[1]["id"].(string)
	if assistantID1 == assistantID2 {
		t.Fatalf("legacy long IDs collided after shortening: %q", assistantID1)
	}
	if len(assistantID1) > maxToolCallIDLen || len(assistantID2) > maxToolCallIDLen {
		t.Fatalf("assistant IDs should be <= %d chars: %q / %q", maxToolCallIDLen, assistantID1, assistantID2)
	}

	if got := msgs[1]["tool_call_id"].(string); got != assistantID1 {
		t.Fatalf("first tool result ID = %q, want %q", got, assistantID1)
	}
	if got := msgs[2]["tool_call_id"].(string); got != assistantID2 {
		t.Fatalf("second tool result ID = %q, want %q", got, assistantID2)
	}
}
