package providers

import (
	"context"
	"testing"
)

type testThinkingProvider struct {
	thinking bool
}

func (p testThinkingProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return nil, nil
}
func (p testThinkingProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	return nil, nil
}
func (p testThinkingProvider) DefaultModel() string   { return "gpt-5.4" }
func (p testThinkingProvider) Name() string           { return "test" }
func (p testThinkingProvider) SupportsThinking() bool { return p.thinking }

func TestLookupReasoningCapability(t *testing.T) {
	capability := LookupReasoningCapability("openai/gpt-5.1-codex-max")
	if capability == nil {
		t.Fatal("LookupReasoningCapability() = nil, want capability")
	}
	if !capability.Supports("xhigh") {
		t.Fatal("expected gpt-5.1-codex-max to support xhigh")
	}
	if capability.Supports("low") {
		t.Fatal("expected gpt-5.1-codex-max to reject low")
	}
}

func TestResolveReasoningDecisionDowngradesUnsupportedEffort(t *testing.T) {
	decision := ResolveReasoningDecision(testThinkingProvider{thinking: true}, "gpt-5.1-codex", "xhigh", "downgrade", "reasoning")
	if decision.EffectiveEffort != "high" {
		t.Fatalf("EffectiveEffort = %q, want high", decision.EffectiveEffort)
	}
	if decision.RequestEffort() != "high" {
		t.Fatalf("RequestEffort() = %q, want high", decision.RequestEffort())
	}
}

func TestResolveReasoningDecisionDowngradeNeverRaisesEffort(t *testing.T) {
	decision := ResolveReasoningDecision(testThinkingProvider{thinking: true}, "gpt-5.1-codex-max", "low", "downgrade", "reasoning")
	if decision.EffectiveEffort != "none" {
		t.Fatalf("EffectiveEffort = %q, want none", decision.EffectiveEffort)
	}
	if decision.RequestEffort() != "none" {
		t.Fatalf("RequestEffort() = %q, want none", decision.RequestEffort())
	}
}

func TestResolveReasoningDecisionDowngradeDisablesWhenNoLowerLevelExists(t *testing.T) {
	decision := ResolveReasoningDecision(testThinkingProvider{thinking: true}, "gpt-5.1-codex", "minimal", "downgrade", "reasoning")
	if decision.EffectiveEffort != "off" {
		t.Fatalf("EffectiveEffort = %q, want off", decision.EffectiveEffort)
	}
	if decision.RequestEffort() != "" {
		t.Fatalf("RequestEffort() = %q, want empty", decision.RequestEffort())
	}
}

func TestLookupReasoningCapabilityRejectsUnknownSuffixVariants(t *testing.T) {
	if capability := LookupReasoningCapability("gpt-5.4-experimental"); capability != nil {
		t.Fatalf("LookupReasoningCapability() = %#v, want nil", capability)
	}
}

func TestResolveReasoningDecisionUsesProviderDefaultForAuto(t *testing.T) {
	decision := ResolveReasoningDecision(testThinkingProvider{thinking: true}, "gpt-5.4", "auto", "provider_default", "reasoning")
	if !decision.UsedProviderDefault {
		t.Fatal("UsedProviderDefault = false, want true")
	}
	if decision.EffectiveEffort != "none" {
		t.Fatalf("EffectiveEffort = %q, want none", decision.EffectiveEffort)
	}
	if decision.RequestEffort() != "" {
		t.Fatalf("RequestEffort() = %q, want empty", decision.RequestEffort())
	}
}

func TestResolveReasoningDecisionDisablesWhenProviderCannotReason(t *testing.T) {
	decision := ResolveReasoningDecision(testThinkingProvider{}, "gpt-5.4", "high", "downgrade", "thinking_level")
	if decision.EffectiveEffort != "off" {
		t.Fatalf("EffectiveEffort = %q, want off", decision.EffectiveEffort)
	}
}
