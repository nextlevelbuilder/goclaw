package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestRegisterProvidersRuncrateDefaultsAndAuth(t *testing.T) {
	var gotPath, gotAuth, gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel = body.Model
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"content": "ok"},
				"finish_reason": "stop",
			}},
		})
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{}
	cfg.Providers.Runcrate.APIKey = "runcrate-key"
	cfg.Providers.Runcrate.APIBase = server.URL
	registry := providers.NewRegistry(nil)
	registerProviders(registry, cfg, providers.NewInMemoryRegistry())

	p, err := registry.GetForTenant(providers.MasterTenantID, "runcrate")
	if err != nil {
		t.Fatalf("GetForTenant() error = %v", err)
	}
	if p.DefaultModel() != store.RuncrateDefaultModel {
		t.Fatalf("DefaultModel() = %q, want %q", p.DefaultModel(), store.RuncrateDefaultModel)
	}
	if _, err := p.Chat(context.Background(), providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("request path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer runcrate-key" {
		t.Fatalf("Authorization = %q, want Bearer runcrate-key", gotAuth)
	}
	if gotModel != store.RuncrateDefaultModel {
		t.Fatalf("model = %q, want vendor-prefixed id %q sent unchanged", gotModel, store.RuncrateDefaultModel)
	}
}

func TestRegisterProvidersFromDBUsesRuncrateDefaults(t *testing.T) {
	tenantID := uuid.New()
	providerStore := gatewayProvidersStoreStub{
		providers: []store.LLMProviderData{{
			BaseModel:    store.BaseModel{ID: uuid.New()},
			TenantID:     tenantID,
			Name:         "db-runcrate",
			ProviderType: store.ProviderRuncrate,
			APIKey:       "runcrate-key",
			Enabled:      true,
		}},
	}

	registry := providers.NewRegistry(nil)
	registerProvidersFromDB(registry, providerStore, nil, "", "", nil, &config.Config{}, providers.NewInMemoryRegistry())

	assertProviderDefault(t, registry, tenantID, "db-runcrate", store.RuncrateDefaultModel, store.RuncrateDefaultAPIBase)
}
