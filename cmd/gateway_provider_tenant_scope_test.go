package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// tenantScopeProviderStore records the tenant carried by the catalogue reads so
// the test can prove registration scopes them to the provider's owning tenant.
type tenantScopeProviderStore struct {
	store.ProviderStore

	provider store.LLMProviderData

	modelTenant uuid.UUID
	quirkTenant uuid.UUID
}

func (s *tenantScopeProviderStore) ListAllProviders(context.Context) ([]store.LLMProviderData, error) {
	return []store.LLMProviderData{s.provider}, nil
}

func (s *tenantScopeProviderStore) ListModels(ctx context.Context, providerID uuid.UUID) ([]store.LLMModel, error) {
	s.modelTenant = store.TenantIDFromContext(ctx)
	if s.modelTenant == uuid.Nil {
		return nil, store.ErrTenantRequired
	}
	return []store.LLMModel{{
		ProviderID: providerID,
		ModelID:    "gpt-test",
		Compat:     json.RawMessage(`{"headers":{"x-model-compat":"1"}}`),
	}}, nil
}

func (s *tenantScopeProviderStore) ListQuirks(ctx context.Context, wireAPI string) ([]store.ProviderQuirk, error) {
	s.quirkTenant = store.TenantIDFromContext(ctx)
	if s.quirkTenant == uuid.Nil {
		return nil, nil // real store: bundled rows only
	}
	return []store.ProviderQuirk{{
		WireAPI: wireAPI,
		Compat:  json.RawMessage(`{"extra_body":{"tenant_override":true}}`),
		Source:  "operator",
		Enabled: true,
	}}, nil
}

// TestRegisterProvidersFromDBScopesCatalogueReadsToProviderTenant is the
// regression for the "tenant_id required" warning storm: registration used a
// bare context, so ListModels always failed and per-model compat plus tenant
// quirk overrides were silently dropped.
func TestRegisterProvidersFromDBScopesCatalogueReadsToProviderTenant(t *testing.T) {
	logs := captureSlog(t)

	tenantID := uuid.New()
	provider := store.LLMProviderData{
		TenantID:     tenantID,
		Name:         "tenant-scoped",
		WireAPI:      "openai-completions",
		APIKey:       "sk-test",
		APIBase:      "http://127.0.0.1:1",
		Enabled:      true,
		ProviderType: "openai",
	}
	provider.ID = uuid.New()
	provStore := &tenantScopeProviderStore{provider: provider}

	registerProvidersFromDB(providers.NewRegistry(nil), provStore, nil, "", "", nil,
		&config.Config{}, providers.NewInMemoryRegistry(), nil)

	if provStore.modelTenant != tenantID {
		t.Fatalf("ListModels tenant = %s, want provider tenant %s", provStore.modelTenant, tenantID)
	}
	if provStore.quirkTenant != tenantID {
		t.Fatalf("ListQuirks tenant = %s, want provider tenant %s", provStore.quirkTenant, tenantID)
	}
	if out := logs.String(); strings.Contains(out, "provider.models.load_failed") {
		t.Fatalf("registration still failed to load the model catalogue:\n%s", out)
	}
}

func TestProviderTenantContextFallsBackToMasterTenant(t *testing.T) {
	if got := store.TenantIDFromContext(providerTenantContext(uuid.Nil)); got != store.MasterTenantID {
		t.Fatalf("tenant for nil id = %s, want master %s", got, store.MasterTenantID)
	}
	tid := uuid.New()
	if got := store.TenantIDFromContext(providerTenantContext(tid)); got != tid {
		t.Fatalf("tenant = %s, want %s", got, tid)
	}
}

// TestLoadModelCompatCarriesTenantCompatibleRows keeps the decode contract: a
// tenant-scoped read yields the per-model compat by model id.
func TestLoadModelCompatCarriesTenantCompatibleRows(t *testing.T) {
	providerID := uuid.New()
	stub := &tenantScopeProviderStore{}
	got := loadModelCompat(providerTenantContext(uuid.New()), stub, providerID)
	if len(got) != 1 {
		t.Fatalf("compat entries = %d, want 1 (%v)", len(got), got)
	}
	if _, ok := got["gpt-test"]; !ok {
		t.Fatalf("compat missing gpt-test: %v", got)
	}
}
