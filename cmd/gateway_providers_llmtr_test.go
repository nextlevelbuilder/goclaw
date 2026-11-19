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

func TestRegisterProvidersLLMTRDefaultsAndAuth(t *testing.T) {
	var gotPath, gotAuth, gotModel string
	var sawReasoning bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotModel, _ = body["model"].(string)
		_, sawReasoning = body["reasoning"]
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"content": "ok"},
				"finish_reason": "stop",
			}},
		})
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{}
	cfg.Providers.LLMTR.APIKey = "llmtr-key"
	cfg.Providers.LLMTR.APIBase = server.URL
	registry := providers.NewRegistry(nil)
	registerProviders(registry, cfg, providers.NewInMemoryRegistry())

	p, err := registry.GetForTenant(providers.MasterTenantID, "llmtr")
	if err != nil {
		t.Fatalf("GetForTenant() error = %v", err)
	}
	if p.DefaultModel() != store.LLMTRDefaultModel {
		t.Fatalf("DefaultModel() = %q, want %q", p.DefaultModel(), store.LLMTRDefaultModel)
	}
	if _, err := p.Chat(context.Background(), providers.ChatRequest{
		Model:    "llmtr/qwen3-8-27b",
		Messages: []providers.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("request path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer llmtr-key" {
		t.Fatalf("Authorization = %q, want Bearer llmtr-key", gotAuth)
	}
	if gotModel != "llmtr/qwen3-8-27b" {
		t.Fatalf("model = %q, want llmtr/qwen3-8-27b", gotModel)
	}
	if sawReasoning {
		t.Fatal("request included a reasoning field")
	}
}

func TestRegisterProvidersFromDBUsesLLMTRDefaults(t *testing.T) {
	tenantID := uuid.New()
	providerStore := gatewayProvidersStoreStub{
		providers: []store.LLMProviderData{{
			BaseModel:    store.BaseModel{ID: uuid.New()},
			TenantID:     tenantID,
			Name:         "db-llmtr",
			ProviderType: store.ProviderLLMTR,
			APIKey:       "llmtr-key",
			Enabled:      true,
		}},
	}

	registry := providers.NewRegistry(nil)
	registerProvidersFromDB(registry, providerStore, nil, "", "", nil, &config.Config{}, providers.NewInMemoryRegistry())

	assertProviderDefault(t, registry, tenantID, "db-llmtr", store.LLMTRDefaultModel, store.LLMTRDefaultAPIBase)
}
