package handlers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks/handlers"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestRegistryResolver_ExplicitProviderIsAuthoritative(t *testing.T) {
	tenantID := uuid.New()
	registry := providers.NewRegistry(nil)
	registry.RegisterForTenant(tenantID, &fakeProvider{name: "cppai", defaultModel: "gpt-default"})
	registry.RegisterForTenant(tenantID, &fakeProvider{name: "anthropic", defaultModel: "claude-default"})
	resolver := handlers.NewRegistryResolver(registry, nil)

	provider, model, err := resolver.ResolveForHook(context.Background(), tenantID, "cppai", "gpt-5.6-terra")
	if err != nil {
		t.Fatalf("ResolveForHook() error: %v", err)
	}
	if provider.Name() != "cppai" || model != "gpt-5.6-terra" {
		t.Fatalf("resolved=(%q, %q), want (cppai, gpt-5.6-terra)", provider.Name(), model)
	}
}

func TestRegistryResolver_ExplicitProviderUsesItsDefaultModel(t *testing.T) {
	tenantID := uuid.New()
	registry := providers.NewRegistry(nil)
	registry.RegisterForTenant(tenantID, &fakeProvider{name: "bailian", defaultModel: "qwen3.7-plus"})
	resolver := handlers.NewRegistryResolver(registry, nil)

	provider, model, err := resolver.ResolveForHook(context.Background(), tenantID, "bailian", "")
	if err != nil {
		t.Fatalf("ResolveForHook() error: %v", err)
	}
	if provider.Name() != "bailian" || model != "qwen3.7-plus" {
		t.Fatalf("resolved=(%q, %q), want (bailian, qwen3.7-plus)", provider.Name(), model)
	}
}

func TestRegistryResolver_MissingExplicitProviderDoesNotFallback(t *testing.T) {
	tenantID := uuid.New()
	registry := providers.NewRegistry(nil)
	registry.RegisterForTenant(tenantID, &fakeProvider{name: "bailian", defaultModel: "qwen3.7-plus"})
	resolver := handlers.NewRegistryResolver(registry, nil)

	provider, model, err := resolver.ResolveForHook(context.Background(), tenantID, "missing", "gpt-5.6-terra")
	if err == nil {
		t.Fatal("ResolveForHook() error=nil, want configured-provider error")
	}
	if provider != nil || model != "" {
		t.Fatalf("resolved=(%v, %q), want nil provider and empty model", provider, model)
	}
}

func TestRegistryResolver_LegacyModelAliasStillResolves(t *testing.T) {
	tenantID := uuid.New()
	registry := providers.NewRegistry(nil)
	registry.RegisterForTenant(tenantID, &fakeProvider{name: "anthropic", defaultModel: "claude-default"})
	resolver := handlers.NewRegistryResolver(registry, nil)

	provider, model, err := resolver.ResolveForHook(context.Background(), tenantID, "", "haiku")
	if err != nil {
		t.Fatalf("ResolveForHook() error: %v", err)
	}
	if provider.Name() != "anthropic" || model != "haiku" {
		t.Fatalf("resolved=(%q, %q), want (anthropic, haiku)", provider.Name(), model)
	}
}
