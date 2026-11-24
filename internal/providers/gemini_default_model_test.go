package providers

import "testing"

// gemini-2.0-flash was shut down by Google on 2026-06-01; the default registered
// for the native Gemini provider must not point at it.
func TestGeminiDefaultModel(t *testing.T) {
	if GeminiDefaultModel == "" || GeminiDefaultModel == "gemini-2.0-flash" {
		t.Fatalf("GeminiDefaultModel = %q, want a model that is still served", GeminiDefaultModel)
	}

	p := NewOpenAIProvider("gemini", "k", "https://generativelanguage.googleapis.com/v1beta/openai", GeminiDefaultModel)
	if got := p.resolveModel(""); got != GeminiDefaultModel {
		t.Fatalf("resolveModel(\"\") = %q, want %q", got, GeminiDefaultModel)
	}

	body := p.buildRequestBody(p.resolveModel(""), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptThinkingLevel: "low"},
	}, false)
	if body["model"] != GeminiDefaultModel {
		t.Errorf("body model = %v, want %q", body["model"], GeminiDefaultModel)
	}
	if body[OptReasoningEffort] != "low" {
		t.Errorf("reasoning_effort = %v, want low", body[OptReasoningEffort])
	}
}
