package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestSOULEchoForGPT verifies SOUL echo is injected for GPT providers.
func TestSOULEchoForGPT(t *testing.T) {
	files := []bootstrap.ContextFile{{
		Path: "SOUL.md", Content: "# Fox\n## Style\nPlayful, curious",
	}}
	reminder := buildPersonaReminder(files, "predefined", "openai")
	joined := strings.Join(reminder, "\n")
	if !strings.Contains(joined, "SOUL echo") {
		t.Error("GPT provider should have SOUL echo")
	}
}

// TestNoSOULEchoForAnthropic verifies SOUL echo is NOT injected for Anthropic.
func TestNoSOULEchoForAnthropic(t *testing.T) {
	files := []bootstrap.ContextFile{{
		Path: "SOUL.md", Content: "# Fox\n## Style\nPlayful",
	}}
	reminder := buildPersonaReminder(files, "predefined", "anthropic")
	joined := strings.Join(reminder, "\n")
	if strings.Contains(joined, "SOUL echo") {
		t.Error("Anthropic should not have SOUL echo")
	}
}

// TestProviderStablePrefixPosition verifies StablePrefix is before cache boundary.
func TestProviderStablePrefixPosition(t *testing.T) {
	cfg := SystemPromptConfig{
		Mode: PromptFull,
		ProviderContribution: &providers.PromptContribution{
			StablePrefix: "## Reasoning Format\nUse <think>...</think>",
		},
	}
	prompt := BuildSystemPrompt(cfg)
	parts := strings.SplitN(prompt, CacheBoundaryMarker, 2)
	if len(parts) != 2 {
		t.Fatal("expected boundary split")
	}
	if !strings.Contains(parts[0], "## Reasoning Format") {
		t.Error("StablePrefix should be above cache boundary")
	}
}

// TestProviderDynamicSuffixPosition verifies DynamicSuffix is after cache boundary.
func TestProviderDynamicSuffixPosition(t *testing.T) {
	cfg := SystemPromptConfig{
		Mode: PromptFull,
		ProviderContribution: &providers.PromptContribution{
			DynamicSuffix: "## Per-Turn Context\nDynamic info here",
		},
	}
	prompt := BuildSystemPrompt(cfg)
	parts := strings.SplitN(prompt, CacheBoundaryMarker, 2)
	if len(parts) != 2 {
		t.Fatal("expected boundary split")
	}
	if !strings.Contains(parts[1], "## Per-Turn Context") {
		t.Error("DynamicSuffix should be below cache boundary")
	}
}

// TestSectionOverrideReplacesDefault verifies section overrides replace defaults.
func TestSectionOverrideReplacesDefault(t *testing.T) {
	cfg := SystemPromptConfig{
		Mode: PromptFull,
		ProviderContribution: &providers.PromptContribution{
			SectionOverrides: map[string]string{
				providers.SectionIDExecutionBias: "## Execution Bias\nCustom GPT bias text.\n",
			},
		},
	}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "Custom GPT bias text") {
		t.Error("override should be present")
	}
	if strings.Contains(prompt, "Commentary-only turns") {
		t.Error("default execution bias should be replaced")
	}
}

// TestNilContributionDefaultBehavior verifies nil contribution = default behavior.
func TestNilContributionDefaultBehavior(t *testing.T) {
	cfg := SystemPromptConfig{Mode: PromptFull, ProviderContribution: nil}
	prompt := BuildSystemPrompt(cfg)
	if !strings.Contains(prompt, "## Execution Bias") {
		t.Error("nil contribution should produce default Execution Bias")
	}
	if !strings.Contains(prompt, "Commentary-only turns") {
		t.Error("default execution bias text should be present")
	}
}
