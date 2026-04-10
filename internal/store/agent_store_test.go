package store

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseReasoningConfigDefaultsToOff(t *testing.T) {
	agent := &AgentData{}

	got := agent.ParseReasoningConfig()
	if got.OverrideMode != ReasoningOverrideInherit {
		t.Fatalf("OverrideMode = %q, want %q", got.OverrideMode, ReasoningOverrideInherit)
	}
	if got.Effort != "off" {
		t.Fatalf("Effort = %q, want off", got.Effort)
	}
	if got.Fallback != ReasoningFallbackDowngrade {
		t.Fatalf("Fallback = %q, want %q", got.Fallback, ReasoningFallbackDowngrade)
	}
	if got.Source != ReasoningSourceUnset {
		t.Fatalf("Source = %q, want %q", got.Source, ReasoningSourceUnset)
	}
}

func TestParseReasoningConfigUsesLegacyThinkingLevel(t *testing.T) {
	agent := &AgentData{
		ThinkingLevel: "medium",
	}

	got := agent.ParseReasoningConfig()
	if got.Effort != "medium" {
		t.Fatalf("Effort = %q, want medium", got.Effort)
	}
	if got.OverrideMode != ReasoningOverrideCustom {
		t.Fatalf("OverrideMode = %q, want %q", got.OverrideMode, ReasoningOverrideCustom)
	}
	if got.Source != ReasoningSourceLegacy {
		t.Fatalf("Source = %q, want %q", got.Source, ReasoningSourceLegacy)
	}
}

func TestParseReasoningConfigPrefersAdvancedSettings(t *testing.T) {
	agent := &AgentData{
		ThinkingLevel:   "high",
		ReasoningConfig: json.RawMessage(`{"effort": "xhigh", "fallback": "provider_default"}`),
	}

	got := agent.ParseReasoningConfig()
	if got.Effort != "xhigh" {
		t.Fatalf("Effort = %q, want xhigh", got.Effort)
	}
	if got.OverrideMode != ReasoningOverrideCustom {
		t.Fatalf("OverrideMode = %q, want %q", got.OverrideMode, ReasoningOverrideCustom)
	}
	if got.Fallback != ReasoningFallbackProviderDefault {
		t.Fatalf("Fallback = %q, want %q", got.Fallback, ReasoningFallbackProviderDefault)
	}
	if got.Source != ReasoningSourceAdvanced {
		t.Fatalf("Source = %q, want %q", got.Source, ReasoningSourceAdvanced)
	}
}

func TestParseReasoningConfigKeepsLegacyEffortWhenAdvancedOnlySetsFallback(t *testing.T) {
	agent := &AgentData{
		ThinkingLevel:   "medium",
		ReasoningConfig: json.RawMessage(`{"fallback": "off"}`),
	}

	got := agent.ParseReasoningConfig()
	if got.Effort != "medium" {
		t.Fatalf("Effort = %q, want medium", got.Effort)
	}
	if got.Fallback != ReasoningFallbackDisable {
		t.Fatalf("Fallback = %q, want %q", got.Fallback, ReasoningFallbackDisable)
	}
}

func TestParseReasoningConfigPreservesExplicitInherit(t *testing.T) {
	agent := &AgentData{
		ThinkingLevel:   "high",
		ReasoningConfig: json.RawMessage(`{"override_mode": "inherit"}`),
	}

	got := agent.ParseReasoningConfig()
	if got.OverrideMode != ReasoningOverrideInherit {
		t.Fatalf("OverrideMode = %q, want %q", got.OverrideMode, ReasoningOverrideInherit)
	}
	if got.Effort != "off" {
		t.Fatalf("Effort = %q, want off", got.Effort)
	}
	if got.Source != ReasoningSourceUnset {
		t.Fatalf("Source = %q, want %q", got.Source, ReasoningSourceUnset)
	}
}

func TestParseProviderReasoningConfigNormalizesDefaults(t *testing.T) {
	settings := json.RawMessage(`{
		"reasoning_defaults": {"effort": " xhigh ", "fallback": "provider_default"}
	}`)

	got := ParseProviderReasoningConfig(settings)
	if got == nil {
		t.Fatal("ParseProviderReasoningConfig() = nil, want config")
	}
	if got.Effort != "xhigh" {
		t.Fatalf("Effort = %q, want xhigh", got.Effort)
	}
	if got.Fallback != ReasoningFallbackProviderDefault {
		t.Fatalf("Fallback = %q, want %q", got.Fallback, ReasoningFallbackProviderDefault)
	}
}

func TestResolveEffectiveReasoningConfigUsesProviderDefaults(t *testing.T) {
	got := ResolveEffectiveReasoningConfig(
		&ProviderReasoningConfig{Effort: "medium", Fallback: ReasoningFallbackDisable},
		AgentReasoningConfig{OverrideMode: ReasoningOverrideInherit},
	)

	if got.OverrideMode != ReasoningOverrideInherit {
		t.Fatalf("OverrideMode = %q, want %q", got.OverrideMode, ReasoningOverrideInherit)
	}
	if got.Effort != "medium" {
		t.Fatalf("Effort = %q, want medium", got.Effort)
	}
	if got.Fallback != ReasoningFallbackDisable {
		t.Fatalf("Fallback = %q, want %q", got.Fallback, ReasoningFallbackDisable)
	}
	if got.Source != ReasoningSourceProviderDefault {
		t.Fatalf("Source = %q, want %q", got.Source, ReasoningSourceProviderDefault)
	}
}

func TestResolveEffectiveReasoningConfigPreservesCustomAgentReasoning(t *testing.T) {
	got := ResolveEffectiveReasoningConfig(
		&ProviderReasoningConfig{Effort: "medium", Fallback: ReasoningFallbackDisable},
		AgentReasoningConfig{
			OverrideMode: ReasoningOverrideCustom,
			Effort:       "xhigh",
			Fallback:     ReasoningFallbackProviderDefault,
			Source:       ReasoningSourceAdvanced,
		},
	)

	if got.OverrideMode != ReasoningOverrideCustom {
		t.Fatalf("OverrideMode = %q, want %q", got.OverrideMode, ReasoningOverrideCustom)
	}
	if got.Effort != "xhigh" {
		t.Fatalf("Effort = %q, want xhigh", got.Effort)
	}
	if got.Source != ReasoningSourceAdvanced {
		t.Fatalf("Source = %q, want %q", got.Source, ReasoningSourceAdvanced)
	}
}

func TestParseChatGPTOAuthRoutingNormalizesNames(t *testing.T) {
	agent := &AgentData{
		ChatGPTOAuthRouting: json.RawMessage(`{
			"strategy": "round_robin",
			"extra_provider_names": [" openai-codex-backup ", "", "openai-codex-backup", "openai-codex-team"]
		}`),
	}

	got := agent.ParseChatGPTOAuthRouting()
	if got == nil {
		t.Fatal("ParseChatGPTOAuthRouting() = nil, want config")
	}
	if got.Strategy != ChatGPTOAuthStrategyRoundRobin {
		t.Fatalf("Strategy = %q, want %q", got.Strategy, ChatGPTOAuthStrategyRoundRobin)
	}
	if got.OverrideMode != ChatGPTOAuthOverrideCustom {
		t.Fatalf("OverrideMode = %q, want %q", got.OverrideMode, ChatGPTOAuthOverrideCustom)
	}

	wantExtras := []string{"openai-codex-backup", "openai-codex-team"}
	if !reflect.DeepEqual(got.ExtraProviderNames, wantExtras) {
		t.Fatalf("ExtraProviderNames = %#v, want %#v", got.ExtraProviderNames, wantExtras)
	}
}

func TestParseChatGPTOAuthRoutingFallsBackToManual(t *testing.T) {
	agent := &AgentData{
		ChatGPTOAuthRouting: json.RawMessage(`{
			"strategy": "something_else",
			"extra_provider_names": ["openai-codex-backup"]
		}`),
	}

	got := agent.ParseChatGPTOAuthRouting()
	if got == nil {
		t.Fatal("ParseChatGPTOAuthRouting() = nil, want config")
	}
	if got.Strategy != ChatGPTOAuthStrategyPrimaryFirst {
		t.Fatalf("Strategy = %q, want %q", got.Strategy, ChatGPTOAuthStrategyPrimaryFirst)
	}
}

func TestParseChatGPTOAuthRoutingManualWithoutExtrasPreservesExplicitSingleAccount(t *testing.T) {
	agent := &AgentData{
		ChatGPTOAuthRouting: json.RawMessage(`{
			"strategy": "manual",
			"extra_provider_names": []
		}`),
	}

	got := agent.ParseChatGPTOAuthRouting()
	if got == nil {
		t.Fatal("ParseChatGPTOAuthRouting() = nil, want config")
	}
	if got.OverrideMode != ChatGPTOAuthOverrideCustom {
		t.Fatalf("OverrideMode = %q, want %q", got.OverrideMode, ChatGPTOAuthOverrideCustom)
	}
	if got.Strategy != ChatGPTOAuthStrategyPrimaryFirst {
		t.Fatalf("Strategy = %q, want %q", got.Strategy, ChatGPTOAuthStrategyPrimaryFirst)
	}
}

func TestParseChatGPTOAuthRoutingPreservesExplicitInheritMode(t *testing.T) {
	agent := &AgentData{
		ChatGPTOAuthRouting: json.RawMessage(`{
			"override_mode": "inherit"
		}`),
	}

	got := agent.ParseChatGPTOAuthRouting()
	if got == nil {
		t.Fatal("ParseChatGPTOAuthRouting() = nil, want config")
	}
	if got.OverrideMode != ChatGPTOAuthOverrideInherit {
		t.Fatalf("OverrideMode = %q, want %q", got.OverrideMode, ChatGPTOAuthOverrideInherit)
	}
	if got.Strategy != ChatGPTOAuthStrategyPrimaryFirst {
		t.Fatalf("Strategy = %q, want %q", got.Strategy, ChatGPTOAuthStrategyPrimaryFirst)
	}
}

func TestResolveEffectiveChatGPTOAuthRoutingUsesProviderDefaultsWhenAgentUnset(t *testing.T) {
	defaults := &ChatGPTOAuthRoutingConfig{
		Strategy:           ChatGPTOAuthStrategyRoundRobin,
		ExtraProviderNames: []string{"codex-work"},
	}

	got := ResolveEffectiveChatGPTOAuthRouting(defaults, nil)
	if got == nil {
		t.Fatal("ResolveEffectiveChatGPTOAuthRouting() = nil, want config")
	}
	if got.Strategy != ChatGPTOAuthStrategyRoundRobin {
		t.Fatalf("Strategy = %q, want %q", got.Strategy, ChatGPTOAuthStrategyRoundRobin)
	}
	if !reflect.DeepEqual(got.ExtraProviderNames, []string{"codex-work"}) {
		t.Fatalf("ExtraProviderNames = %#v, want %#v", got.ExtraProviderNames, []string{"codex-work"})
	}
}

func TestResolveEffectiveChatGPTOAuthRoutingAllowsInheritWithoutSavedProviderPool(t *testing.T) {
	override := &ChatGPTOAuthRoutingConfig{
		OverrideMode: ChatGPTOAuthOverrideInherit,
	}

	got := ResolveEffectiveChatGPTOAuthRouting(nil, override)
	if got != nil {
		t.Fatalf("ResolveEffectiveChatGPTOAuthRouting() = %#v, want nil", got)
	}
}

func TestResolveEffectiveChatGPTOAuthRoutingInheritForwardsProviderDefaults(t *testing.T) {
	defaults := &ChatGPTOAuthRoutingConfig{
		Strategy:           ChatGPTOAuthStrategyRoundRobin,
		ExtraProviderNames: []string{"codex-work"},
	}
	override := &ChatGPTOAuthRoutingConfig{
		OverrideMode: ChatGPTOAuthOverrideInherit,
	}

	got := ResolveEffectiveChatGPTOAuthRouting(defaults, override)
	if got == nil {
		t.Fatal("ResolveEffectiveChatGPTOAuthRouting() = nil, want config forwarding provider defaults")
	}
	if got.Strategy != ChatGPTOAuthStrategyRoundRobin {
		t.Fatalf("Strategy = %q, want %q", got.Strategy, ChatGPTOAuthStrategyRoundRobin)
	}
	if !reflect.DeepEqual(got.ExtraProviderNames, []string{"codex-work"}) {
		t.Fatalf("ExtraProviderNames = %#v, want provider defaults", got.ExtraProviderNames)
	}
}

func TestResolveEffectiveChatGPTOAuthRoutingAllowsCustomSingleAccountToDisableDefaults(t *testing.T) {
	defaults := &ChatGPTOAuthRoutingConfig{
		Strategy:           ChatGPTOAuthStrategyRoundRobin,
		ExtraProviderNames: []string{"codex-work"},
	}
	override := &ChatGPTOAuthRoutingConfig{
		OverrideMode: ChatGPTOAuthOverrideCustom,
		Strategy:     ChatGPTOAuthStrategyPrimaryFirst,
	}

	got := ResolveEffectiveChatGPTOAuthRouting(defaults, override)
	if got == nil {
		t.Fatal("ResolveEffectiveChatGPTOAuthRouting() = nil, want config")
	}
	if got.Strategy != ChatGPTOAuthStrategyPrimaryFirst {
		t.Fatalf("Strategy = %q, want %q", got.Strategy, ChatGPTOAuthStrategyPrimaryFirst)
	}
	if len(got.ExtraProviderNames) != 0 {
		t.Fatalf("ExtraProviderNames = %#v, want empty", got.ExtraProviderNames)
	}
}

func TestResolveEffectiveChatGPTOAuthRoutingKeepsProviderOwnedMembersForStrategyOverride(t *testing.T) {
	defaults := &ChatGPTOAuthRoutingConfig{
		Strategy:           ChatGPTOAuthStrategyRoundRobin,
		ExtraProviderNames: []string{"codex-work", "codex-team"},
	}
	override := &ChatGPTOAuthRoutingConfig{
		OverrideMode: ChatGPTOAuthOverrideCustom,
		Strategy:     ChatGPTOAuthStrategyPriority,
	}

	got := ResolveEffectiveChatGPTOAuthRouting(defaults, override)
	if got == nil {
		t.Fatal("ResolveEffectiveChatGPTOAuthRouting() = nil, want config")
	}
	if got.Strategy != ChatGPTOAuthStrategyPriority {
		t.Fatalf("Strategy = %q, want %q", got.Strategy, ChatGPTOAuthStrategyPriority)
	}
	if !reflect.DeepEqual(got.ExtraProviderNames, defaults.ExtraProviderNames) {
		t.Fatalf("ExtraProviderNames = %#v, want %#v", got.ExtraProviderNames, defaults.ExtraProviderNames)
	}
}

func TestResolveEffectiveChatGPTOAuthRoutingIgnoresCustomMembersWhenProviderOwnsPool(t *testing.T) {
	defaults := &ChatGPTOAuthRoutingConfig{
		Strategy:           ChatGPTOAuthStrategyRoundRobin,
		ExtraProviderNames: []string{"codex-work", "codex-team"},
	}
	override := &ChatGPTOAuthRoutingConfig{
		OverrideMode:       ChatGPTOAuthOverrideCustom,
		Strategy:           ChatGPTOAuthStrategyRoundRobin,
		ExtraProviderNames: []string{"rogue-provider"},
	}

	got := ResolveEffectiveChatGPTOAuthRouting(defaults, override)
	if got == nil {
		t.Fatal("ResolveEffectiveChatGPTOAuthRouting() = nil, want config")
	}
	if !reflect.DeepEqual(got.ExtraProviderNames, defaults.ExtraProviderNames) {
		t.Fatalf("ExtraProviderNames = %#v, want provider defaults %#v", got.ExtraProviderNames, defaults.ExtraProviderNames)
	}
}
