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

func TestRegisterProvidersRequestyDefaultsAndAuth(t *testing.T) {
	var gotPath, gotAuth, gotTitle string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotTitle = r.Header.Get("X-Title")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"content": "ok"},
				"finish_reason": "stop",
			}},
		})
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{}
	cfg.Providers.Requesty.APIKey = "requesty-key"
	cfg.Providers.Requesty.APIBase = server.URL
	registry := providers.NewRegistry(nil)
	registerProviders(registry, cfg, providers.NewInMemoryRegistry())

	p, err := registry.GetForTenant(providers.MasterTenantID, "requesty")
	if err != nil {
		t.Fatalf("GetForTenant() error = %v", err)
	}
	if p.DefaultModel() != store.RequestyDefaultModel {
		t.Fatalf("DefaultModel() = %q, want %q", p.DefaultModel(), store.RequestyDefaultModel)
	}
	if _, err := p.Chat(context.Background(), providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("request path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer requesty-key" {
		t.Fatalf("Authorization = %q, want Bearer requesty-key", gotAuth)
	}
	if gotTitle != "GoClaw" {
		t.Fatalf("X-Title = %q, want GoClaw", gotTitle)
	}
}

func TestRegisterProvidersFromDBUsesRequestyDefaults(t *testing.T) {
	tenantID := uuid.New()
	providerStore := gatewayProvidersStoreStub{
		providers: []store.LLMProviderData{{
			BaseModel:    store.BaseModel{ID: uuid.New()},
			TenantID:     tenantID,
			Name:         "db-requesty",
			ProviderType: store.ProviderRequesty,
			APIKey:       "requesty-key",
			Enabled:      true,
		}},
	}

	registry := providers.NewRegistry(nil)
	registerProvidersFromDB(registry, providerStore, nil, "", "", nil, &config.Config{}, providers.NewInMemoryRegistry())

	assertProviderDefault(t, registry, tenantID, "db-requesty", store.RequestyDefaultModel, store.RequestyDefaultAPIBase)
}
