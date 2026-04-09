package providers

import (
	"regexp"
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

func TestBuildRequestBody_ReasoningEffortOpenAIOnly(t *testing.T) {
	thinkOpts := map[string]any{OptThinkingLevel: "medium"}

	t.Run("Together Qwen omits reasoning_effort", func(t *testing.T) {
		p := NewOpenAIProvider("togetherai", "key", "https://api.together.xyz/v1", "")
		req := ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			Options:  thinkOpts,
		}
		body := p.buildRequestBody("Qwen/Qwen3.5-397B-A17B", req, false)
		if _, ok := body[OptReasoningEffort]; ok {
			t.Fatalf("Qwen on Together must not send %q (HTTP 400 on unknown fields), body=%v", OptReasoningEffort, body)
		}
	})

	t.Run("OpenAI gpt-5 keeps reasoning_effort", func(t *testing.T) {
		p := NewOpenAIProvider("openai", "key", "https://api.openai.com/v1", "gpt-5")
		req := ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			Options:  thinkOpts,
		}
		body := p.buildRequestBody("gpt-5.4", req, false)
		if got, ok := body[OptReasoningEffort].(string); !ok || got != "medium" {
			t.Fatalf("gpt-5.4 should send reasoning_effort medium, got body=%v", body)
		}
	})

	t.Run("OpenAI o3-mini keeps reasoning_effort", func(t *testing.T) {
		p := NewOpenAIProvider("openai", "key", "https://api.openai.com/v1", "")
		req := ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			Options:  thinkOpts,
		}
		body := p.buildRequestBody("o3-mini", req, false)
		if got, ok := body[OptReasoningEffort].(string); !ok || got != "medium" {
			t.Fatalf("o3-mini should send reasoning_effort medium, got body=%v", body)
		}
	})
}

func TestBuildRequestBody_TogetherOmitsStreamOptions(t *testing.T) {
	together := NewOpenAIProvider("togetherai", "key", "https://api.together.xyz/v1", "")
	openai := NewOpenAIProvider("openai", "key", "https://api.openai.com/v1", "gpt-4")

	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}

	gotTogether := together.buildRequestBody("Qwen/Qwen3.5-397B-A17B", req, true)
	if _, ok := gotTogether["stream_options"]; ok {
		t.Fatalf("Together streaming must omit stream_options, got %v", gotTogether["stream_options"])
	}

	gotOpenAI := openai.buildRequestBody("gpt-4o", req, true)
	if _, ok := gotOpenAI["stream_options"]; !ok {
		t.Fatal("OpenAI streaming should include stream_options for usage")
	}
}

func TestBuildRequestBody_TogetherOmitsStrayDashScopeKeys(t *testing.T) {
	p := NewOpenAIProvider("togetherai", "key", "https://api.together.xyz/v1", "")
	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options: map[string]any{
			OptEnableThinking: true,
			OptThinkingBudget: 4096,
		},
	}
	body := p.buildRequestBody("Qwen/Qwen3.5-397B-A17B", req, false)
	if _, ok := body[OptEnableThinking]; ok {
		t.Fatalf("Together must not send %q", OptEnableThinking)
	}
	if _, ok := body[OptThinkingBudget]; ok {
		t.Fatalf("Together must not send %q", OptThinkingBudget)
	}
}

func TestBuildRequestBody_MultimodalTextBeforeImages(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-4")
	req := ChatRequest{
		Messages: []Message{
			{
				Role:    "user",
				Content: "describe",
				Images: []ImageContent{
					{MimeType: "image/jpeg", Data: "qq=="},
				},
			},
		},
	}
	body := p.buildRequestBody("gpt-4o", req, false)
	msgs := body["messages"].([]map[string]any)
	parts, ok := msgs[0]["content"].([]map[string]any)
	if !ok || len(parts) < 2 {
		t.Fatalf("want multimodal parts, got %v", msgs[0]["content"])
	}
	if typ, _ := parts[0]["type"].(string); typ != "text" {
		t.Fatalf("first part should be text, got type=%q parts=%v", typ, parts)
	}
	if typ, _ := parts[1]["type"].(string); typ != "image_url" {
		t.Fatalf("second part should be image_url, got type=%q", typ)
	}
}

func TestBuildRequestBody_TogetherDetectedByProviderType(t *testing.T) {
	// Together behind reverse proxy — detected by providerType, not URL.
	p := NewOpenAIProvider("my-proxy", "key", "https://proxy.internal/v1", "")
	p.WithProviderType("together")

	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptThinkingLevel: "medium"},
	}

	body := p.buildRequestBody("Qwen/Qwen3.5-397B-A17B", req, true)
	if _, ok := body["stream_options"]; ok {
		t.Fatal("Together (via providerType) must omit stream_options")
	}
	if _, ok := body[OptReasoningEffort]; ok {
		t.Fatal("Together (via providerType) must omit reasoning_effort")
	}
}

func TestBuildRequestBody_TogetherDetectedByName(t *testing.T) {
	// Together detected by provider name.
	p := NewOpenAIProvider("together-prod", "key", "https://proxy.internal/v1", "")

	body := p.buildRequestBody("Qwen/Qwen3.5-397B-A17B", ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, true)
	if _, ok := body["stream_options"]; ok {
		t.Fatal("Together (via name) must omit stream_options")
	}
}

func TestBuildRequestBody_MultimodalImagesOnlyNoEmptyTextPart(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-4")
	req := ChatRequest{
		Messages: []Message{
			{
				Role:    "user",
				Content: "", // no text
				Images:  []ImageContent{{MimeType: "image/png", Data: "abc=="}},
			},
		},
	}
	body := p.buildRequestBody("gpt-4o", req, false)
	msgs := body["messages"].([]map[string]any)
	parts, ok := msgs[0]["content"].([]map[string]any)
	if !ok {
		t.Fatal("want multimodal parts array")
	}
	// Only image parts — no empty text part should be present
	for _, part := range parts {
		if typ, _ := part["type"].(string); typ == "text" {
			t.Fatalf("images-only message should not have text part, got %v", parts)
		}
	}
	if len(parts) != 1 {
		t.Fatalf("want 1 image part, got %d parts: %v", len(parts), parts)
	}
}

func TestBuildRequestBody_OllamaModelOmitsReasoningContent(t *testing.T) {
	// Ollama/vLLM self-hosted models should NOT get reasoning_content
	// since most don't support it and it causes HTTP 400.
	p := NewOpenAIProvider("ollama", "key", "http://localhost:11434/v1", "")

	req := ChatRequest{
		Messages: []Message{
			{Role: "assistant", Content: "sure", Thinking: "let me think..."},
			{Role: "user", Content: "continue"},
		},
	}

	body := p.buildRequestBody("qwen2:7b", req, false)
	msgs := body["messages"].([]map[string]any)
	for _, m := range msgs {
		if role, _ := m["role"].(string); role == "assistant" {
			if _, has := m["reasoning_content"]; has {
				t.Fatalf("Ollama qwen2:7b must omit reasoning_content, got %v", m)
			}
		}
	}
}

func TestBuildRequestBody_DeepSeekKeepsReasoningContent(t *testing.T) {
	// DeepSeek models must keep reasoning_content (required for thinking replay).
	p := NewOpenAIProvider("deepseek", "key", "https://api.deepseek.com/v1", "")

	req := ChatRequest{
		Messages: []Message{
			{Role: "assistant", Content: "result", Thinking: "step by step"},
			{Role: "user", Content: "next"},
		},
	}

	body := p.buildRequestBody("deepseek-r1", req, false)
	msgs := body["messages"].([]map[string]any)
	for _, m := range msgs {
		if role, _ := m["role"].(string); role == "assistant" {
			if _, has := m["reasoning_content"]; !has {
				t.Fatalf("DeepSeek must keep reasoning_content, got %v", m)
			}
		}
	}
}

func TestBuildRequestBody_KimiKeepsReasoningContent(t *testing.T) {
	p := NewOpenAIProvider("kimi", "key", "https://api.moonshot.cn/v1", "")

	req := ChatRequest{
		Messages: []Message{
			{Role: "assistant", Content: "ok", Thinking: "reasoning"},
			{Role: "user", Content: "next"},
		},
	}

	body := p.buildRequestBody("kimi-k2", req, false)
	msgs := body["messages"].([]map[string]any)
	for _, m := range msgs {
		if role, _ := m["role"].(string); role == "assistant" {
			if _, has := m["reasoning_content"]; !has {
				t.Fatalf("Kimi must keep reasoning_content, got %v", m)
			}
		}
	}
}

func TestBuildRequestBody_DashScopePassthroughByProviderType(t *testing.T) {
	// DashScope behind proxy — detected by providerType.
	p := NewOpenAIProvider("my-qwen", "key", "https://proxy.internal/v1", "")
	p.WithProviderType("dashscope")

	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options: map[string]any{
			OptEnableThinking: true,
			OptThinkingBudget: 8192,
		},
	}

	body := p.buildRequestBody("qwen3-max", req, false)
	if _, ok := body[OptEnableThinking]; !ok {
		t.Fatal("DashScope (via providerType) must pass through enable_thinking")
	}
	if _, ok := body[OptThinkingBudget]; !ok {
		t.Fatal("DashScope (via providerType) must pass through thinking_budget")
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

var mistralWireIDRe = regexp.MustCompile(`^[0-9a-f]{9}$`)

func TestNormalizeMistralToolCallID_DeterministicNineChars(t *testing.T) {
	id := "call_85f419357e554e8983a7edb4d2317e93e15"
	a := normalizeMistralToolCallID(id)
	b := normalizeMistralToolCallID(id)
	if a != b {
		t.Fatalf("normalizeMistralToolCallID not deterministic: %q vs %q", a, b)
	}
	if !mistralWireIDRe.MatchString(a) {
		t.Fatalf("got %q, want exactly 9 hex chars", a)
	}
}

func TestNormalizeMistralToolCallID_DistinctIDsStayUnique(t *testing.T) {
	// IDs sharing a long prefix must not collide after normalization.
	id1 := "call_a1b2c3d4e5f6a1b2c3d4e5f6_suffix1"
	id2 := "call_a1b2c3d4e5f6a1b2c3d4e5f6_suffix2"
	if normalizeMistralToolCallID(id1) == normalizeMistralToolCallID(id2) {
		t.Fatal("distinct IDs must not collide after normalization")
	}
}

func TestBuildRequestBody_MistralToolCallIDsWireFormat(t *testing.T) {
	p := NewOpenAIProvider("mistral", "key", "https://api.mistral.ai/v1", "mistral-large-latest")
	longID := "call_85f419357e554e8983a7edb4d2317e93e15"

	req := ChatRequest{
		Messages: []Message{
			{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{ID: longID, Name: "test_fn", Arguments: map[string]any{"x": 1}},
				},
			},
			{Role: "tool", ToolCallID: longID, Content: "ok"},
			{Role: "user", Content: "next"},
		},
	}

	body := p.buildRequestBody("mistral-large-latest", req, false)
	msgs := body["messages"].([]map[string]any)

	var assistantID, toolResultID string
	for _, msg := range msgs {
		if tcs, ok := msg["tool_calls"]; ok {
			toolCalls := tcs.([]map[string]any)
			assistantID = toolCalls[0]["id"].(string)
		}
		if tcid, ok := msg["tool_call_id"]; ok {
			toolResultID = tcid.(string)
		}
	}

	if assistantID != toolResultID {
		t.Fatalf("IDs must match: tool_calls.id=%q tool_call_id=%q", assistantID, toolResultID)
	}
	if !mistralWireIDRe.MatchString(assistantID) {
		t.Fatalf("mistral wire id %q must be 9 hex chars", assistantID)
	}
}

func TestBuildRequestBody_MistralDBProviderTypeDetected(t *testing.T) {
	// DB-loaded Mistral providers use providerType="mistral" with a user-chosen name.
	p := NewOpenAIProvider("my-mistral", "key", "https://api.mistral.ai/v1", "mistral-large-latest")
	p.WithProviderType("mistral")

	id := "call_85f419357e554e8983a7edb4d2317e93e15"
	got := p.wireToolCallID(id)
	if !mistralWireIDRe.MatchString(got) {
		t.Fatalf("DB provider with providerType=mistral: got %q, want 9 hex chars", got)
	}
}
