package store

import (
	"regexp"
	"testing"
)

// OpenRouter serves Anthropic models under undated slugs; a dated id such as
// anthropic/claude-sonnet-4-5-20250929 is not in its model list and fails.
func TestOpenRouterDefaultModelIsAnUndatedSlug(t *testing.T) {
	if regexp.MustCompile(`-\d{8}$`).MatchString(OpenRouterDefaultModel) {
		t.Fatalf("OpenRouterDefaultModel = %q, want an undated OpenRouter slug", OpenRouterDefaultModel)
	}
	if OpenRouterDefaultModel != "anthropic/claude-sonnet-4.6" {
		t.Fatalf("OpenRouterDefaultModel = %q, want anthropic/claude-sonnet-4.6 to match the Anthropic default", OpenRouterDefaultModel)
	}
}
