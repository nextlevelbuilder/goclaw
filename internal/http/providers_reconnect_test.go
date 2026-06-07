package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func TestProvidersHandlerReconnectRejectsNonAdmin(t *testing.T) {
	token := setupTraceReadToken(t, "viewer")
	providerStore := newMockProviderStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     uuid.New(),
		Name:         "openrouter-main",
		ProviderType: store.ProviderOpenAICompat,
		APIKey:       "token",
		Enabled:      true,
	}
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	mux := http.NewServeMux()
	NewProvidersHandler(providerStore, newMockSecretsStore(), providers.NewRegistry(nil), "").RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/providers/"+provider.ID.String()+"/reconnect", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestProvidersHandlerReconnectRejectsVerifyTrue(t *testing.T) {
	token := setupProvidersAdminToken(t)
	providerStore := newMockProviderStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     uuid.New(),
		Name:         "openrouter-main",
		ProviderType: store.ProviderOpenAICompat,
		APIKey:       "token",
		Enabled:      true,
	}
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	mux := http.NewServeMux()
	NewProvidersHandler(providerStore, newMockSecretsStore(), providers.NewRegistry(nil), "").RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/providers/"+provider.ID.String()+"/reconnect", bytes.NewBufferString(`{"verify":true}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestProvidersHandlerReconnectRegistersEnabledProviderAndInvalidatesCache(t *testing.T) {
	token := setupProvidersAdminToken(t)
	tenantID := uuid.New()
	providerStore := newMockProviderStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "openrouter-main",
		ProviderType: store.ProviderOpenAICompat,
		APIBase:      "https://openrouter.ai/api/v1",
		APIKey:       "secret-token",
		Enabled:      true,
	}
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	providerReg := providers.NewRegistry(nil)
	msgBus := bus.New()
	var cacheEvents []bus.Event
	msgBus.Subscribe(bus.TopicCacheProvider, func(event bus.Event) {
		cacheEvents = append(cacheEvents, event)
	})
	handler := NewProvidersHandler(providerStore, newMockSecretsStore(), providerReg, "")
	handler.SetMessageBus(msgBus)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/providers/"+provider.ID.String()+"/reconnect", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if _, err := providerReg.GetForTenant(tenantID, provider.Name); err != nil {
		t.Fatalf("GetForTenant() error = %v, want provider registered", err)
	}

	var body struct {
		Status           string                `json:"status"`
		Provider         store.LLMProviderData `json:"provider"`
		RegistryUpdated  bool                  `json:"registry_updated"`
		CacheInvalidated bool                  `json:"cache_invalidated"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "reconnected" || !body.RegistryUpdated || !body.CacheInvalidated {
		t.Fatalf("body = %+v, want reconnected with registry/cache flags", body)
	}
	if body.Provider.APIKey != "***" {
		t.Fatalf("provider.api_key = %q, want masked", body.Provider.APIKey)
	}
	if len(cacheEvents) == 0 {
		t.Fatal("cache invalidation event not emitted")
	}
	got := cacheEvents[0]
	if got.Name != protocol.EventCacheInvalidate || got.TenantID != tenantID {
		t.Fatalf("cache event = %+v, want provider cache invalidation for tenant %s", got, tenantID)
	}
	payload, ok := got.Payload.(bus.CacheInvalidatePayload)
	if !ok {
		t.Fatalf("payload = %T, want CacheInvalidatePayload", got.Payload)
	}
	if payload.Kind != bus.CacheKindProvider || payload.Key != provider.Name || payload.TenantID != tenantID {
		t.Fatalf("payload = %+v, want provider %q tenant %s", payload, provider.Name, tenantID)
	}
}

func TestProvidersHandlerReconnectDisabledProviderUnregisters(t *testing.T) {
	token := setupProvidersAdminToken(t)
	tenantID := uuid.New()
	providerStore := newMockProviderStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "disabled-main",
		ProviderType: store.ProviderOpenAICompat,
		APIKey:       "secret-token",
		Enabled:      false,
	}
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	providerReg := providers.NewRegistry(nil)
	providerReg.RegisterForTenant(tenantID, providers.NewOpenAIProvider(provider.Name, "old", "", ""))
	mux := http.NewServeMux()
	NewProvidersHandler(providerStore, newMockSecretsStore(), providerReg, "").RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/providers/"+provider.ID.String()+"/reconnect", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if _, err := providerReg.GetForTenant(tenantID, provider.Name); err == nil {
		t.Fatal("GetForTenant() succeeded, want provider unregistered")
	}
	var body struct {
		Status          string `json:"status"`
		RegistryUpdated bool   `json:"registry_updated"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "disabled" || body.RegistryUpdated {
		t.Fatalf("body = %+v, want disabled without registry update", body)
	}
}

func TestProvidersHandlerReconnectMissingCredentialReturnsNotRegistered(t *testing.T) {
	token := setupProvidersAdminToken(t)
	tenantID := uuid.New()
	providerStore := newMockProviderStore()
	provider := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     tenantID,
		Name:         "missing-key",
		ProviderType: store.ProviderOpenAICompat,
		Enabled:      true,
	}
	if err := providerStore.CreateProvider(t.Context(), provider); err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}

	providerReg := providers.NewRegistry(nil)
	mux := http.NewServeMux()
	NewProvidersHandler(providerStore, newMockSecretsStore(), providerReg, "").RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/providers/"+provider.ID.String()+"/reconnect", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Status          string `json:"status"`
		RegistryUpdated bool   `json:"registry_updated"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "not_registered" || body.RegistryUpdated {
		t.Fatalf("body = %+v, want not_registered without registry update", body)
	}
}
