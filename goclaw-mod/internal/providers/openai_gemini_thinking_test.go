package providers

import (
	"testing"
)

// TestBuildRequestBody_GeminiForwardsReasoningEffort verifies that
// `OptThinkingLevel` maps to `reasoning_effort` on the wire for Gemini routes.
// Without this forwarding, Gemini 3 defaults to its highest thinking budget and
// consumes the full max_tokens allocation, leaving no tokens for tool args.
// Trace: 019d8f33-2de1-7ab2-9a32-9df92cd610dd.
func TestBuildRequestBody_GeminiForwardsReasoningEffort(t *testing.T) {
	cases := []struct {
		name       string
		level      string
		wantValue  string
		wantExists bool
	}{
		{"low_verbatim", "low", "low", true},
		{"minimal_verbatim", "minimal", "minimal", true},
		{"high_verbatim", "high", "high", true},
		{"medium_maps_to_high", "medium", "high", true},
		{"off_maps_to_low", "off", "low", true},
		{"empty_omitted", "", "", false},
		{"unknown_omitted", "garbage", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewOpenAIProvider("test-gemini", "key",
				"https://generativelanguage.googleapis.com/v1beta/openai", "gemini-3-flash-preview")
			req := ChatRequest{
				Messages: []Message{{Role: "user", Content: "hi"}},
				Options:  map[string]any{OptThinkingLevel: tc.level},
			}
			body := p.buildRequestBody("gemini-3-flash-preview", req, false)
			got, exists := body[OptReasoningEffort]
			if exists != tc.wantExists {
				t.Fatalf("level=%q: reasoning_effort presence = %v, want %v (body=%v)", tc.level, exists, tc.wantExists, body)
			}
			if exists {
				if str, ok := got.(string); !ok || str != tc.wantValue {
					t.Fatalf("level=%q: reasoning_effort = %v, want %q", tc.level, got, tc.wantValue)
				}
			}
		})
	}
}

// TestBuildRequestBody_GeminiDetectionByModelName verifies that gemini routing
// detection works via model substring even when the apiBase is a proxy
// (OpenRouter, LiteLLM), and that non-gemini models on the same proxy don't
// receive `reasoning_effort`.
func TestBuildRequestBody_GeminiDetectionByModelName(t *testing.T) {
	t.Run("openrouter_gemini_forwards", func(t *testing.T) {
		p := NewOpenAIProvider("openrouter", "key",
			"https://openrouter.ai/api/v1", "google/gemini-2.5-pro")
		req := ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			Options:  map[string]any{OptThinkingLevel: "low"},
		}
		body := p.buildRequestBody("google/gemini-2.5-pro", req, false)
		if got, ok := body[OptReasoningEffort].(string); !ok || got != "low" {
			t.Fatalf("openrouter gemini must forward reasoning_effort=low; got %v (exists=%v)", got, ok)
		}
	})

	t.Run("openrouter_claude_omits", func(t *testing.T) {
		p := NewOpenAIProvider("openrouter", "key",
			"https://openrouter.ai/api/v1", "anthropic/claude-sonnet-4")
		req := ChatRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
			Options:  map[string]any{OptThinkingLevel: "low"},
		}
		body := p.buildRequestBody("anthropic/claude-sonnet-4", req, false)
		if _, exists := body[OptReasoningEffort]; exists {
			t.Fatalf("non-gemini route must NOT receive reasoning_effort (body=%v)", body)
		}
	})
}

// TestBuildRequestBody_NonGeminiUnaffected verifies that vanilla OpenAI-compat
// hosts (Together, Groq, vLLM, etc.) are not affected by the gemini branch —
// they should continue to reject `reasoning_effort` via the existing gate.
func TestBuildRequestBody_NonGeminiUnaffected(t *testing.T) {
	p := NewOpenAIProvider("together", "key",
		"https://api.together.xyz/v1", "Qwen/Qwen2.5-72B-Instruct-Turbo")
	req := ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptThinkingLevel: "low"},
	}
	body := p.buildRequestBody("Qwen/Qwen2.5-72B-Instruct-Turbo", req, false)
	if _, exists := body[OptReasoningEffort]; exists {
		t.Fatalf("together/qwen must NOT receive reasoning_effort; body=%v", body)
	}
}
