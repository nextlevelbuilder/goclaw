package providers

import "testing"

// claude-sonnet-4-5-20250929 retires on the Anthropic API on 2026-11-30, so the
// built-in default follows the "sonnet" alias instead of a dated snapshot.
func TestDefaultClaudeModelFollowsSonnetAlias(t *testing.T) {
	if defaultClaudeModel != claudeModelAliases["sonnet"] {
		t.Fatalf("defaultClaudeModel = %q, want the sonnet alias %q", defaultClaudeModel, claudeModelAliases["sonnet"])
	}
}

func TestResolveAnthropicModelEmptyUsesDefault(t *testing.T) {
	if got := resolveAnthropicModel("", defaultClaudeModel, nil); got != claudeModelAliases["sonnet"] {
		t.Fatalf("resolveAnthropicModel(\"\") = %q, want %q", got, claudeModelAliases["sonnet"])
	}
}
