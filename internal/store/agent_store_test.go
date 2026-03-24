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
	if got.Strategy != ChatGPTOAuthStrategyManual {
		t.Fatalf("Strategy = %q, want %q", got.Strategy, ChatGPTOAuthStrategyManual)
	}
}

func TestParseChatGPTOAuthRoutingManualWithoutExtrasReturnsNil(t *testing.T) {
	agent := &AgentData{
		OtherConfig: json.RawMessage(`{
			"chatgpt_oauth_routing": {
				"strategy": "manual",
				"extra_provider_names": []
			}
		}`),
	}

	if got := agent.ParseChatGPTOAuthRouting(); got != nil {
		t.Fatalf("ParseChatGPTOAuthRouting() = %#v, want nil", got)
	}
}
