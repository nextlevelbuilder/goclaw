package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestRegisterProvidersAPIRouteDefaultsAndAuth(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"content": "ok"},
				"finish_reason": "stop",
			}},
		})
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{}
	cfg.Providers.APIRoute.APIKey = "api-route-key"
	cfg.Providers.APIRoute.APIBase = server.URL
	registry := providers.NewRegistry(nil)
	registerProviders(registry, cfg, providers.NewInMemoryRegistry())

	p, err := registry.GetForTenant(providers.MasterTenantID, "api_route")
	if err != nil {
		t.Fatalf("GetForTenant() error = %v", err)
	}
	if p.DefaultModel() != store.APIRouteDefaultModel {
		t.Fatalf("DefaultModel() = %q, want %q", p.DefaultModel(), store.APIRouteDefaultModel)
	}
	if _, err := p.Chat(context.Background(), providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("request path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer api-route-key" {
		t.Fatalf("Authorization = %q, want Bearer api-route-key", gotAuth)
	}
}
