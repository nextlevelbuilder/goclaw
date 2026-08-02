package providerresolve

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestResolveTeamWorkClassifierDefaultsToAgentProviderModel(t *testing.T) {
	agent := &stubProvider{name: "agent-provider", model: "agent-default"}
	got := ResolveTeamWorkClassifier(context.Background(), nil, "", "", agent, "agent-model")
	if got.Provider != agent || got.Model != "agent-model" || got.Source != "agent_default" || got.Warning != "" {
		t.Fatalf("ResolveTeamWorkClassifier() = %+v", got)
	}
}

func TestResolveTeamWorkClassifierUsesOverrideProviderAndModel(t *testing.T) {
	tenantID := uuid.New()
	registry := providers.NewRegistry(store.TenantIDFromContext)
	override := &stubProvider{name: "classifier", model: "classifier-default"}
	registry.RegisterForTenant(tenantID, override)
	ctx := store.WithTenantID(context.Background(), tenantID)
	got := ResolveTeamWorkClassifier(ctx, registry, "classifier", "classifier-model", &stubProvider{name: "agent", model: "agent-default"}, "agent-model")
	if got.Provider != override || got.Model != "classifier-model" || got.Source != "override_provider_model" || got.Warning != "" {
		t.Fatalf("ResolveTeamWorkClassifier() = %+v", got)
	}
}

func TestResolveTeamWorkClassifierProviderOnlyUsesProviderDefault(t *testing.T) {
	tenantID := uuid.New()
	registry := providers.NewRegistry(store.TenantIDFromContext)
	override := &stubProvider{name: "classifier", model: "classifier-default"}
	registry.RegisterForTenant(tenantID, override)
	got := ResolveTeamWorkClassifier(store.WithTenantID(context.Background(), tenantID), registry, "classifier", "", &stubProvider{name: "agent", model: "agent-default"}, "agent-model")
	if got.Provider != override || got.Model != "classifier-default" || got.Source != "override_provider_default_model" {
		t.Fatalf("ResolveTeamWorkClassifier() = %+v", got)
	}
}

func TestResolveTeamWorkClassifierProviderOnlyEmptyDefaultFallsBackToAgentPair(t *testing.T) {
	tenantID := uuid.New()
	registry := providers.NewRegistry(store.TenantIDFromContext)
	override := &stubProvider{name: "classifier", model: ""}
	agent := &stubProvider{name: "agent", model: "agent-default"}
	registry.RegisterForTenant(tenantID, override)
	got := ResolveTeamWorkClassifier(store.WithTenantID(context.Background(), tenantID), registry, "classifier", "", agent, "agent-model")
	if got.Provider != agent || got.Model != "agent-model" || got.Source != "fallback_agent" || !strings.Contains(got.Warning, "default model") {
		t.Fatalf("ResolveTeamWorkClassifier() = %+v", got)
	}
}

func TestResolveTeamWorkClassifierModelOnlyUsesAgentProvider(t *testing.T) {
	agent := &stubProvider{name: "agent", model: "agent-default"}
	got := ResolveTeamWorkClassifier(context.Background(), nil, "", "classifier-model", agent, "agent-model")
	if got.Provider != agent || got.Model != "classifier-model" || got.Source != "override_model_only" || got.Warning != "" {
		t.Fatalf("ResolveTeamWorkClassifier() = %+v", got)
	}
}

func TestResolveTeamWorkClassifierInvalidProviderFallsBackToAgent(t *testing.T) {
	agent := &stubProvider{name: "agent", model: "agent-default"}
	registry := providers.NewRegistry(store.TenantIDFromContext)
	got := ResolveTeamWorkClassifier(context.Background(), registry, "missing", "model", agent, "agent-model")
	if got.Provider != agent || got.Model != "agent-model" || got.Source != "fallback_agent" || got.Warning == "" {
		t.Fatalf("ResolveTeamWorkClassifier() = %+v", got)
	}
}

func TestResolveTeamWorkClassifierUsesTenantProviderBeforeMaster(t *testing.T) {
	tenantID := uuid.New()
	registry := providers.NewRegistry(store.TenantIDFromContext)
	master := &stubProvider{name: "classifier", model: "master-model"}
	tenant := &stubProvider{name: "classifier", model: "tenant-model"}
	registry.Register(master)
	registry.RegisterForTenant(tenantID, tenant)
	got := ResolveTeamWorkClassifier(store.WithTenantID(context.Background(), tenantID), registry, "classifier", "", &stubProvider{name: "agent", model: "agent-default"}, "agent-model")
	if got.Provider != tenant || got.Model != "tenant-model" {
		t.Fatalf("ResolveTeamWorkClassifier() = %+v, want tenant provider", got)
	}
}
