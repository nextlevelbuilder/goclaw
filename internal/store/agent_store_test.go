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

func TestPublicChatGPTOAuthRoutingMigratesLegacyStrategiesToPriorityOrder(t *testing.T) {
	for _, tc := range []struct {
		name     string
		strategy string
	}{
		{name: "unknown", strategy: "something_else"},
		{name: "manual", strategy: "manual"},
		{name: "primary_first", strategy: "primary_first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := &AgentData{
				ChatGPTOAuthRouting: json.RawMessage(`{
					"strategy": "` + tc.strategy + `",
					"extra_provider_names": ["openai-codex-backup"]
				}`),
			}

			got := agent.ParseChatGPTOAuthRouting()
			if got == nil {
				t.Fatal("ParseChatGPTOAuthRouting() = nil, want config")
			}
			public := PublicChatGPTOAuthRouting(got)
			if public == nil {
				t.Fatal("PublicChatGPTOAuthRouting() = nil, want config")
			}
			if public.Strategy != ChatGPTOAuthStrategyPriority {
				t.Fatalf("Strategy = %q, want %q", public.Strategy, ChatGPTOAuthStrategyPriority)
			}
		})
	}
}

func TestPublicChatGPTOAuthRoutingCanonicalizesSingleAccountOverrideToPriorityOrder(t *testing.T) {
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
	public := PublicChatGPTOAuthRouting(got)
	if public == nil {
		t.Fatal("PublicChatGPTOAuthRouting() = nil, want config")
	}
	if public.Strategy != ChatGPTOAuthStrategyPriority {
		t.Fatalf("Strategy = %q, want %q", public.Strategy, ChatGPTOAuthStrategyPriority)
	}
	if got.ExtraProviderNames == nil {
		t.Fatal("ExtraProviderNames = nil, want explicit empty slice preserved")
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
	public := PublicChatGPTOAuthRouting(got)
	if public == nil {
		t.Fatal("PublicChatGPTOAuthRouting() = nil, want config")
	}
	if public.Strategy != ChatGPTOAuthStrategyPriority {
		t.Fatalf("Strategy = %q, want %q", public.Strategy, ChatGPTOAuthStrategyPriority)
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
		OverrideMode:       ChatGPTOAuthOverrideCustom,
		Strategy:           ChatGPTOAuthStrategyPriority,
		ExtraProviderNames: []string{},
	}

	got := ResolveEffectiveChatGPTOAuthRouting(defaults, override)
	if got == nil {
		t.Fatal("ResolveEffectiveChatGPTOAuthRouting() = nil, want config")
	}
	if got.Strategy != ChatGPTOAuthStrategyPriority {
		t.Fatalf("Strategy = %q, want %q", got.Strategy, ChatGPTOAuthStrategyPriority)
	}
	if len(got.ExtraProviderNames) != 0 {
		t.Fatalf("ExtraProviderNames = %#v, want empty", got.ExtraProviderNames)
	}
}

func TestResolveEffectiveChatGPTOAuthRoutingRoundRobinEmptyExtrasKeepsDefaults(t *testing.T) {
	defaults := &ChatGPTOAuthRoutingConfig{
		Strategy:           ChatGPTOAuthStrategyRoundRobin,
		ExtraProviderNames: []string{"codex-work"},
	}
	override := &ChatGPTOAuthRoutingConfig{
		OverrideMode:       ChatGPTOAuthOverrideCustom,
		Strategy:           ChatGPTOAuthStrategyRoundRobin,
		ExtraProviderNames: []string{},
	}

	got := ResolveEffectiveChatGPTOAuthRouting(defaults, override)
	if got == nil {
		t.Fatal("ResolveEffectiveChatGPTOAuthRouting() = nil, want config")
	}
	if !reflect.DeepEqual(got.ExtraProviderNames, defaults.ExtraProviderNames) {
		t.Fatalf("ExtraProviderNames = %#v, want %#v", got.ExtraProviderNames, defaults.ExtraProviderNames)
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

// ─── ParseAllowImageGeneration ────────────────────────────────────────────

func TestParseAllowImageGeneration_DefaultTrue_NoOtherConfig(t *testing.T) {
	ag := &AgentData{}
	if !ag.ParseAllowImageGeneration() {
		t.Error("empty other_config must default to true (image gen enabled)")
	}
}

func TestParseAllowImageGeneration_DefaultTrue_EmptyObject(t *testing.T) {
	ag := &AgentData{OtherConfig: json.RawMessage(`{}`)}
	if !ag.ParseAllowImageGeneration() {
		t.Error("empty JSONB object must default to true")
	}
}

func TestParseAllowImageGeneration_ExplicitTrue(t *testing.T) {
	ag := &AgentData{OtherConfig: json.RawMessage(`{"allow_image_generation":true}`)}
	if !ag.ParseAllowImageGeneration() {
		t.Error("explicit true must return true")
	}
}

func TestParseAllowImageGeneration_ExplicitFalse(t *testing.T) {
	ag := &AgentData{OtherConfig: json.RawMessage(`{"allow_image_generation":false}`)}
	if ag.ParseAllowImageGeneration() {
		t.Error("explicit false must return false")
	}
}

func TestParseAllowImageGeneration_MalformedJSON_DefaultsTrue(t *testing.T) {
	ag := &AgentData{OtherConfig: json.RawMessage(`{not-json`)}
	if !ag.ParseAllowImageGeneration() {
		t.Error("malformed other_config must default to true")
	}
}

func TestParseAllowImageGeneration_UnrelatedKeys_DefaultsTrue(t *testing.T) {
	ag := &AgentData{OtherConfig: json.RawMessage(`{"self_evolve":true,"skill_evolve":false}`)}
	if !ag.ParseAllowImageGeneration() {
		t.Error("other_config without allow_image_generation key must default to true")
	}
}
