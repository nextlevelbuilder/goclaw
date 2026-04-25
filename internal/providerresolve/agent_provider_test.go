package providerresolve

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

type stubProvider struct {
	name  string
	model string
}

func (p *stubProvider) Chat(context.Context, providers.ChatRequest) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{FinishReason: "stop"}, nil
}

func (p *stubProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{FinishReason: "stop"}, nil
}

func (p *stubProvider) DefaultModel() string { return p.model }
func (p *stubProvider) Name() string         { return p.name }

func TestResolveConfiguredProviderKeepsNonCodexBase(t *testing.T) {
	tenantID := uuid.New()
	registry := providers.NewRegistry(nil)
	base := &stubProvider{name: "anthropic", model: "claude-sonnet-4"}
	registry.RegisterForTenant(tenantID, base)
	registry.RegisterForTenant(tenantID, providers.NewCodexProvider(
		"openai-codex-backup",
		&testTokenSource{token: "backup-token"},
		"http://127.0.0.1",
		"gpt-5.4",
	))

	agent := &store.AgentData{
		TenantID: tenantID,
		Provider: "anthropic",
		ChatGPTOAuthRouting: json.RawMessage(`{
			"strategy": "round_robin",
			"extra_provider_names": ["openai-codex-backup"]
		}`),
	}

	resolved, err := ResolveConfiguredProvider(registry, agent)
	if err != nil {
		t.Fatalf("ResolveConfiguredProvider() error = %v", err)
	}
	if resolved != base {
		t.Fatalf("ResolveConfiguredProvider() returned %T, want original non-Codex provider", resolved)
	}
}

func TestResolveConfiguredProviderUsesRouterForCodexAgents(t *testing.T) {
	tenantID := uuid.New()
	registry := providers.NewRegistry(nil)
	registry.RegisterForTenant(tenantID, providers.NewCodexProvider(
		"openai-codex",
		&testTokenSource{token: "primary-token"},
		"http://127.0.0.1",
		"gpt-5.4",
	))
	registry.RegisterForTenant(tenantID, providers.NewCodexProvider(
		"openai-codex-backup",
		&testTokenSource{token: "backup-token"},
		"http://127.0.0.1",
		"gpt-5.4",
	))

	agent := &store.AgentData{
		TenantID: tenantID,
		Provider: "openai-codex",
		ChatGPTOAuthRouting: json.RawMessage(`{
			"strategy": "round_robin",
			"extra_provider_names": ["openai-codex-backup"]
		}`),
	}

	resolved, err := ResolveConfiguredProvider(registry, agent)
	if err != nil {
		t.Fatalf("ResolveConfiguredProvider() error = %v", err)
	}
	router, ok := resolved.(*providers.ChatGPTOAuthRouter)
	if !ok {
		t.Fatalf("ResolveConfiguredProvider() returned %T, want *providers.ChatGPTOAuthRouter", resolved)
	}
	if !router.HasAvailableProviders() {
		t.Fatal("router.HasAvailableProviders() = false, want true")
	}
	if router.Name() != "openai-codex" {
		t.Fatalf("router.Name() = %q, want %q", router.Name(), "openai-codex")
	}
}

func TestResolveConfiguredProviderUsesProviderDefaultsWhenAgentHasNoOverride(t *testing.T) {
	tenantID := uuid.New()
	registry := providers.NewRegistry(nil)
	registry.RegisterForTenant(tenantID, providers.NewCodexProvider(
		"openai-codex",
		&testTokenSource{token: "primary-token"},
		"http://127.0.0.1",
		"gpt-5.4",
	).WithRoutingDefaults(store.ChatGPTOAuthStrategyRoundRobin, []string{"openai-codex-backup"}))
	registry.RegisterForTenant(tenantID, providers.NewCodexProvider(
		"openai-codex-backup",
		&testTokenSource{token: "backup-token"},
		"http://127.0.0.1",
		"gpt-5.4",
	))

	agent := &store.AgentData{
		TenantID: tenantID,
		Provider: "openai-codex",
	}

	resolved, err := ResolveConfiguredProvider(registry, agent)
	if err != nil {
		t.Fatalf("ResolveConfiguredProvider() error = %v", err)
	}
	router, ok := resolved.(*providers.ChatGPTOAuthRouter)
	if !ok {
		t.Fatalf("ResolveConfiguredProvider() returned %T, want *providers.ChatGPTOAuthRouter", resolved)
	}
	if !router.HasAvailableProviders() {
		t.Fatal("router.HasAvailableProviders() = false, want true")
	}
}

func TestResolveConfiguredProviderKeepsExplicitSingleAccountOverride(t *testing.T) {
	tenantID := uuid.New()
	registry := providers.NewRegistry(nil)
	baseProvider := providers.NewCodexProvider(
		"openai-codex",
		&testTokenSource{token: "primary-token"},
		"http://127.0.0.1",
		"gpt-5.4",
	).WithRoutingDefaults(store.ChatGPTOAuthStrategyRoundRobin, []string{"openai-codex-backup"})
	registry.RegisterForTenant(tenantID, baseProvider)
	registry.RegisterForTenant(tenantID, providers.NewCodexProvider(
		"openai-codex-backup",
		&testTokenSource{token: "backup-token"},
		"http://127.0.0.1",
		"gpt-5.4",
	))

	agent := &store.AgentData{
		TenantID: tenantID,
		Provider: "openai-codex",
		ChatGPTOAuthRouting: json.RawMessage(`{
			"strategy": "manual"
		}`),
	}

	resolved, err := ResolveConfiguredProvider(registry, agent)
	if err != nil {
		t.Fatalf("ResolveConfiguredProvider() error = %v", err)
	}
	router, ok := resolved.(*providers.ChatGPTOAuthRouter)
	if !ok {
		t.Fatalf("ResolveConfiguredProvider() returned %T, want *providers.ChatGPTOAuthRouter", resolved)
	}
	if router.Name() != "openai-codex" {
		t.Fatalf("router.Name() = %q, want %q", router.Name(), "openai-codex")
	}
}

type blockedTokenSource struct {
	token string
}

func (s *blockedTokenSource) Token() (string, error) {
	return s.token, nil
}

func (s *blockedTokenSource) RouteEligibility(context.Context) providers.RouteEligibility {
	return providers.RouteEligibility{Class: providers.RouteEligibilityBlocked, Reason: "reauth"}
}

func TestResolveConfiguredProviderReturnsRouterEvenWhenPrimaryNeedsFailover(t *testing.T) {
	tenantID := uuid.New()
	registry := providers.NewRegistry(nil)
	registry.RegisterForTenant(tenantID, providers.NewCodexProvider(
		"openai-codex",
		&blockedTokenSource{token: "primary-token"},
		"http://127.0.0.1",
		"gpt-5.4",
	))
	registry.RegisterForTenant(tenantID, providers.NewCodexProvider(
		"openai-codex-backup",
		&testTokenSource{token: "backup-token"},
		"http://127.0.0.1",
		"gpt-5.4",
	))

	agent := &store.AgentData{
		TenantID: tenantID,
		Provider: "openai-codex",
		ChatGPTOAuthRouting: json.RawMessage(`{
			"strategy": "round_robin",
			"extra_provider_names": ["openai-codex-backup"]
		}`),
	}

	resolved, err := ResolveConfiguredProvider(registry, agent)
	if err != nil {
		t.Fatalf("ResolveConfiguredProvider() error = %v", err)
	}
	if _, ok := resolved.(*providers.ChatGPTOAuthRouter); !ok {
		t.Fatalf("ResolveConfiguredProvider() returned %T, want *providers.ChatGPTOAuthRouter", resolved)
	}
}
