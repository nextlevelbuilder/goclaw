package store

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseChatGPTOAuthRoutingNormalizesNames(t *testing.T) {
	agent := &AgentData{
		OtherConfig: json.RawMessage(`{
			"chatgpt_oauth_routing": {
				"strategy": "round_robin",
				"extra_provider_names": [" openai-codex-backup ", "", "openai-codex-backup", "openai-codex-team"]
			}
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
		OtherConfig: json.RawMessage(`{
			"chatgpt_oauth_routing": {
				"strategy": "something_else",
				"extra_provider_names": ["openai-codex-backup"]
			}
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
		OtherConfig: json.RawMessage(`{
			"chatgpt_oauth_routing": {
				"strategy": "manual",
				"extra_provider_names": []
			}
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
		OtherConfig: json.RawMessage(`{
			"chatgpt_oauth_routing": {
				"override_mode": "inherit"
			}
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
