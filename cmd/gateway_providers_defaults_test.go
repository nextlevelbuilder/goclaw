package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestRegisterProvidersUsesCurrentMiniMaxAndZaiDefaults(t *testing.T) {
	cfg := &config.Config{}
	cfg.Providers.MiniMax.APIKey = "minimax-token"
	cfg.Providers.Zai.APIKey = "zai-token"
	cfg.Providers.ZaiCoding.APIKey = "zai-coding-token"

	registry := providers.NewRegistry(nil)
	registerProviders(registry, cfg, providers.NewInMemoryRegistry())

	assertProviderDefault(t, registry, providers.MasterTenantID, "minimax", "MiniMax-M3", "https://api.minimax.io/v1")
	assertProviderDefault(t, registry, providers.MasterTenantID, "zai", "glm-5.2", "https://api.z.ai/api/paas/v4")
	assertProviderDefault(t, registry, providers.MasterTenantID, "zai-coding", "glm-5.2", "https://api.z.ai/api/coding/paas/v4")
}

func TestRegisterProvidersMiniMaxUsesOpenAIChatCompletionsPath(t *testing.T) {
	var capturedPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message":       map[string]string{"content": "ok"},
					"finish_reason": "stop",
				},
			},
		})
	}))
	t.Cleanup(upstream.Close)

	cfg := &config.Config{}
	cfg.Providers.MiniMax.APIKey = "minimax-token"
	cfg.Providers.MiniMax.APIBase = upstream.URL

	registry := providers.NewRegistry(nil)
	registerProviders(registry, cfg, providers.NewInMemoryRegistry())

	runtimeProvider, err := registry.GetForTenant(providers.MasterTenantID, "minimax")
	if err != nil {
		t.Fatalf("GetForTenant() error = %v", err)
	}
	_, err = runtimeProvider.Chat(context.Background(), providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if capturedPath != "/chat/completions" {
		t.Fatalf("captured path = %q, want /chat/completions", capturedPath)
	}
}

func TestRegisterProvidersFromDBUsesCurrentMiniMaxAndZaiDefaults(t *testing.T) {
	tenantID := uuid.New()
	providerStore := gatewayProvidersStoreStub{
		providers: []store.LLMProviderData{
			{
				BaseModel:    store.BaseModel{ID: uuid.New()},
				TenantID:     tenantID,
				Name:         "db-aimlapi",
				ProviderType: store.ProviderAIMLAPI,
				APIKey:       "aimlapi-token",
				Enabled:      true,
			},
			{
				BaseModel:    store.BaseModel{ID: uuid.New()},
				TenantID:     tenantID,
				Name:         "db-minimax",
				ProviderType: store.ProviderMiniMax,
				APIKey:       "minimax-token",
				Enabled:      true,
			},
			{
				BaseModel:    store.BaseModel{ID: uuid.New()},
				TenantID:     tenantID,
				Name:         "db-zai",
				ProviderType: store.ProviderZai,
				APIKey:       "zai-token",
				Enabled:      true,
			},
			{
				BaseModel:    store.BaseModel{ID: uuid.New()},
				TenantID:     tenantID,
				Name:         "db-zai-coding",
				ProviderType: store.ProviderZaiCoding,
				APIKey:       "zai-coding-token",
				Enabled:      true,
			},
		},
	}

	registry := providers.NewRegistry(nil)
	registerProvidersFromDB(registry, providerStore, nil, "", "", nil, &config.Config{}, providers.NewInMemoryRegistry())

	assertProviderDefault(t, registry, tenantID, "db-aimlapi", providers.AIMLAPIDefaultModel, providers.AIMLAPIDefaultAPIBase)
	assertProviderDefault(t, registry, tenantID, "db-minimax", "MiniMax-M3", "https://api.minimax.io/v1")
	assertProviderDefault(t, registry, tenantID, "db-zai", "glm-5.2", "https://api.z.ai/api/paas/v4")
	assertProviderDefault(t, registry, tenantID, "db-zai-coding", "glm-5.2", "https://api.z.ai/api/coding/paas/v4")
}

func assertProviderDefault(t *testing.T, registry *providers.Registry, tenantID uuid.UUID, name, wantModel, wantBase string) {
	t.Helper()
	runtimeProvider, err := registry.GetForTenant(tenantID, name)
	if err != nil {
		t.Fatalf("GetForTenant(%q) error = %v", name, err)
	}
	if got := runtimeProvider.DefaultModel(); got != wantModel {
		t.Fatalf("%s DefaultModel() = %q, want %q", name, got, wantModel)
	}
	openai, ok := runtimeProvider.(*providers.OpenAIProvider)
	if !ok {
		t.Fatalf("%s runtime provider = %T, want *providers.OpenAIProvider", name, runtimeProvider)
	}
	if got := openai.APIBase(); got != wantBase {
		t.Fatalf("%s APIBase() = %q, want %q", name, got, wantBase)
	}
}

type gatewayProvidersStoreStub struct {
	providers []store.LLMProviderData
}

func (s gatewayProvidersStoreStub) CreateProvider(context.Context, *store.LLMProviderData) error {
	return errors.New("not implemented")
}

func (s gatewayProvidersStoreStub) GetProvider(context.Context, uuid.UUID) (*store.LLMProviderData, error) {
	return nil, errors.New("not implemented")
}

func (s gatewayProvidersStoreStub) GetProviderByName(context.Context, string) (*store.LLMProviderData, error) {
	return nil, errors.New("not implemented")
}

func (s gatewayProvidersStoreStub) ListProviders(context.Context) ([]store.LLMProviderData, error) {
	return nil, errors.New("not implemented")
}

func (s gatewayProvidersStoreStub) ListAllProviders(context.Context) ([]store.LLMProviderData, error) {
	return s.providers, nil
}

func (s gatewayProvidersStoreStub) UpdateProvider(context.Context, uuid.UUID, map[string]any) error {
	return errors.New("not implemented")
}

func (s gatewayProvidersStoreStub) DeleteProvider(context.Context, uuid.UUID) error {
	return errors.New("not implemented")
}
