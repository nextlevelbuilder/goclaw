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
