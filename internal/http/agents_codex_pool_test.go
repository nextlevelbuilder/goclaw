package http

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type testTokenSource struct {
	token string
}

func (s *testTokenSource) Token() (string, error) {
	return s.token, nil
}

func (s *testTokenSource) RouteEligibility(context.Context) providers.RouteEligibility {
	return providers.RouteEligibility{Class: providers.RouteEligibilityHealthy}
}

func TestResolveCodexPoolRoutingUsesProviderDefaults(t *testing.T) {
	providers := newMockProviderStore()
	tenantID := uuid.New()
	if err := providers.CreateProvider(context.Background(), &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "openai-codex",
		ProviderType: store.ProviderChatGPTOAuth,
		Enabled:      true,
		Settings: json.RawMessage(`{
			"codex_pool": {
				"strategy": "round_robin",
				"extra_provider_names": ["codex-work"]
			}
		}`),
	}); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	agent := &store.AgentData{
		TenantID: tenantID,
		Provider: "openai-codex",
	}

	providerType, routing, poolProviders := resolveCodexPoolRouting(context.Background(), providers, nil, agent)
	if providerType != store.ProviderChatGPTOAuth {
		t.Fatalf("providerType = %q, want %q", providerType, store.ProviderChatGPTOAuth)
	}
	if routing == nil {
		t.Fatal("routing = nil, want effective routing")
	}
	if routing.Strategy != store.ChatGPTOAuthStrategyRoundRobin {
		t.Fatalf("Strategy = %q, want %q", routing.Strategy, store.ChatGPTOAuthStrategyRoundRobin)
	}
	if len(routing.ExtraProviderNames) != 1 || routing.ExtraProviderNames[0] != "codex-work" {
		t.Fatalf("ExtraProviderNames = %#v, want [\"codex-work\"]", routing.ExtraProviderNames)
	}
	if len(poolProviders) != 2 || poolProviders[0] != "openai-codex" || poolProviders[1] != "codex-work" {
		t.Fatalf("poolProviders = %#v, want primary + extra", poolProviders)
	}
}

func TestResolveCodexPoolRoutingHonorsInheritOverride(t *testing.T) {
	providers := newMockProviderStore()
	tenantID := uuid.New()
	if err := providers.CreateProvider(context.Background(), &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "openai-codex",
		ProviderType: store.ProviderChatGPTOAuth,
		Enabled:      true,
		Settings: json.RawMessage(`{
			"codex_pool": {
				"strategy": "priority_order",
				"extra_provider_names": ["codex-team"]
			}
		}`),
	}); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	agent := &store.AgentData{
		TenantID: tenantID,
		Provider: "openai-codex",
		OtherConfig: json.RawMessage(`{
			"chatgpt_oauth_routing": {
				"override_mode": "inherit",
				"strategy": "round_robin",
				"extra_provider_names": ["ignored-backup"]
			}
		}`),
	}

	_, routing, poolProviders := resolveCodexPoolRouting(context.Background(), providers, nil, agent)
	if routing == nil {
		t.Fatal("routing = nil, want inherited routing")
	}
	if routing.Strategy != store.ChatGPTOAuthStrategyPriority {
		t.Fatalf("Strategy = %q, want %q", routing.Strategy, store.ChatGPTOAuthStrategyPriority)
	}
	if len(routing.ExtraProviderNames) != 1 || routing.ExtraProviderNames[0] != "codex-team" {
		t.Fatalf("ExtraProviderNames = %#v, want [\"codex-team\"]", routing.ExtraProviderNames)
	}
	if len(poolProviders) != 2 || poolProviders[1] != "codex-team" {
		t.Fatalf("poolProviders = %#v, want inherited pool order", poolProviders)
	}
}

func TestResolveCodexPoolRoutingIgnoresNonCodexBaseProvider(t *testing.T) {
	providers := newMockProviderStore()
	tenantID := uuid.New()
	if err := providers.CreateProvider(context.Background(), &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "anthropic",
		ProviderType: store.ProviderAnthropicNative,
		Enabled:      true,
	}); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	agent := &store.AgentData{
		TenantID: tenantID,
		Provider: "anthropic",
		OtherConfig: json.RawMessage(`{
			"chatgpt_oauth_routing": {
				"strategy": "round_robin",
				"extra_provider_names": ["codex-backup"]
			}
		}`),
	}

	providerType, routing, poolProviders := resolveCodexPoolRouting(context.Background(), providers, nil, agent)
	if providerType != store.ProviderAnthropicNative {
		t.Fatalf("providerType = %q, want %q", providerType, store.ProviderAnthropicNative)
	}
	if routing != nil {
		t.Fatalf("routing = %#v, want nil for non-Codex provider", routing)
	}
	if len(poolProviders) != 0 {
		t.Fatalf("poolProviders = %#v, want empty for non-Codex provider", poolProviders)
	}
}

func TestResolveCodexPoolRoutingUsesRegistryMasterFallback(t *testing.T) {
	tenantID := uuid.New()
	registry := providers.NewRegistry(nil)
	registry.RegisterForTenant(providers.MasterTenantID, providers.NewCodexProvider(
		"openai-codex",
		&testTokenSource{token: "primary-token"},
		"http://127.0.0.1",
		"gpt-5.4",
	).WithRoutingDefaults(store.ChatGPTOAuthStrategyRoundRobin, []string{"codex-work"}))
	registry.RegisterForTenant(tenantID, providers.NewCodexProvider(
		"codex-work",
		&testTokenSource{token: "backup-token"},
		"http://127.0.0.1",
		"gpt-5.4",
	))

	agent := &store.AgentData{
		TenantID: tenantID,
		Provider: "openai-codex",
	}

	providerType, routing, poolProviders := resolveCodexPoolRouting(context.Background(), nil, registry, agent)
	if providerType != store.ProviderChatGPTOAuth {
		t.Fatalf("providerType = %q, want %q", providerType, store.ProviderChatGPTOAuth)
	}
	if routing == nil {
		t.Fatal("routing = nil, want effective routing")
	}
	if routing.Strategy != store.ChatGPTOAuthStrategyRoundRobin {
		t.Fatalf("Strategy = %q, want %q", routing.Strategy, store.ChatGPTOAuthStrategyRoundRobin)
	}
	if len(poolProviders) != 2 || poolProviders[0] != "openai-codex" || poolProviders[1] != "codex-work" {
		t.Fatalf("poolProviders = %#v, want master fallback pool", poolProviders)
	}
}
