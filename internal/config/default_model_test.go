package config

import "testing"

// claude-sonnet-4-5-20250929 retires on the Anthropic API on 2026-11-30.
func TestDefaultAgentModelIsNotRetiring(t *testing.T) {
	got := Default().Agents.Defaults.Model
	if got == "claude-sonnet-4-5-20250929" || got == "claude-sonnet-4-20250514" {
		t.Fatalf("Default().Agents.Defaults.Model = %q, which Anthropic retires", got)
	}
}
