package providers

import (
	"testing"
)

// TestResolveReasoningDecisionStripsForLeakyModels verifies that known-leaky
// models (Kimi, DeepSeek-Reasoner) flag StripThinking when effort resolves to
// "off", so stream handlers can drop raw CoT from user-visible output.
func TestResolveReasoningDecisionStripsForLeakyModels(t *testing.T) {
	cases := []struct {
		name       string
		model      string
		wantStrip  bool
		wantEffort string
	}{
		{"kimi k2 default off", "kimi-k2", true, "off"},
		{"kimi thinking preview", "moonshot/kimi-k2-thinking", true, "off"},
		{"deepseek reasoner", "deepseek-reasoner", true, "off"},
		{"deepseek chat not flagged", "deepseek-chat", false, "off"},
		{"gpt-5.4 not flagged", "gpt-5.4", false, "off"},
		{"empty model", "", false, "off"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := ResolveReasoningDecision(
				testThinkingProvider{thinking: true},
				tc.model, "off", "downgrade", "reasoning",
			)
			if decision.EffectiveEffort != tc.wantEffort {
				t.Fatalf("EffectiveEffort = %q, want %q", decision.EffectiveEffort, tc.wantEffort)
			}
			if decision.StripThinking != tc.wantStrip {
				t.Fatalf("StripThinking = %v, want %v", decision.StripThinking, tc.wantStrip)
			}
		})
	}
}

// TestResolveReasoningDecisionNoStripWhenEffortActive ensures that when a user
// explicitly asks for reasoning on a leaky model, StripThinking stays false
// (user wants to see the reasoning output).
func TestResolveReasoningDecisionNoStripWhenEffortActive(t *testing.T) {
	decision := ResolveReasoningDecision(
		testThinkingProvider{thinking: true},
		"kimi-k2", "high", "downgrade", "reasoning",
	)
	if decision.StripThinking {
		t.Fatalf("StripThinking = true, want false (effort is active)")
	}
}

// TestModelLeaksReasoning spot-checks the model allowlist directly.
func TestModelLeaksReasoning(t *testing.T) {
	leaky := []string{"kimi-k2", "KIMI-K2-Thinking", "deepseek-reasoner"}
	for _, m := range leaky {
		if !modelLeaksReasoning(m) {
			t.Errorf("modelLeaksReasoning(%q) = false, want true", m)
		}
	}
	safe := []string{"", "gpt-5.4", "claude-4-opus", "deepseek-chat", "o3-mini"}
	for _, m := range safe {
		if modelLeaksReasoning(m) {
			t.Errorf("modelLeaksReasoning(%q) = true, want false", m)
		}
	}
}
