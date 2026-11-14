package tools

import "testing"

// Gemini thinking models report thoughtsTokenCount outside candidatesTokenCount.
// totalTokenCount already includes both. Cost accounting treats ThinkingTokens
// as a sub-count of CompletionTokens, so dropping thoughts under-bills the call.
func TestParseGeminiResponse_ThoughtsFoldedIntoCompletion(t *testing.T) {
	body := []byte(`{
		"candidates": [{"content": {"parts": [{"text": "hello"}]}}],
		"usageMetadata": {
			"promptTokenCount": 11,
			"candidatesTokenCount": 74,
			"totalTokenCount": 910,
			"thoughtsTokenCount": 825
		}
	}`)
	resp, err := parseGeminiResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hello" {
		t.Fatalf("content = %q, want hello", resp.Content)
	}
	if resp.Usage == nil {
		t.Fatal("usage is nil")
	}
	if resp.Usage.PromptTokens != 11 || resp.Usage.TotalTokens != 910 {
		t.Fatalf("prompt/total = %d/%d, want 11/910", resp.Usage.PromptTokens, resp.Usage.TotalTokens)
	}
	if resp.Usage.ThinkingTokens != 825 {
		t.Fatalf("ThinkingTokens = %d, want 825", resp.Usage.ThinkingTokens)
	}
	// 74 visible + 825 thoughts. candidatesTokenCount alone leaves thinking unbilled.
	if resp.Usage.CompletionTokens != 74+825 {
		t.Fatalf("CompletionTokens = %d, want 899", resp.Usage.CompletionTokens)
	}
}

func TestParseGeminiResponse_NoThoughtsUnchanged(t *testing.T) {
	body := []byte(`{
		"candidates": [{"content": {"parts": [{"text": "a"}, {"text": "b"}]}}],
		"usageMetadata": {
			"promptTokenCount": 4,
			"candidatesTokenCount": 6,
			"totalTokenCount": 10
		}
	}`)
	resp, err := parseGeminiResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "a\nb" {
		t.Fatalf("content = %q, want %q", resp.Content, "a\nb")
	}
	if resp.Usage == nil {
		t.Fatal("usage is nil")
	}
	if resp.Usage.PromptTokens != 4 || resp.Usage.CompletionTokens != 6 || resp.Usage.TotalTokens != 10 || resp.Usage.ThinkingTokens != 0 {
		t.Fatalf("usage = %+v, want prompt 4 completion 6 total 10 thinking 0", resp.Usage)
	}
}
