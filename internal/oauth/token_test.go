package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// --- mock stores ---

type mockProviderStore struct {
	providers map[string]*store.LLMProviderData
}

func newMockProviderStore() *mockProviderStore {
	return &mockProviderStore{providers: make(map[string]*store.LLMProviderData)}
}

func (m *mockProviderStore) CreateProvider(_ context.Context, p *store.LLMProviderData) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	m.providers[p.Name] = p
	return nil
}

func (m *mockProviderStore) GetProvider(_ context.Context, id uuid.UUID) (*store.LLMProviderData, error) {
	for _, p := range m.providers {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockProviderStore) GetProviderByName(_ context.Context, name string) (*store.LLMProviderData, error) {
	if p, ok := m.providers[name]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockProviderStore) ListProviders(_ context.Context) ([]store.LLMProviderData, error) {
	var out []store.LLMProviderData
	for _, p := range m.providers {
		out = append(out, *p)
	}
	return out, nil
}

func (m *mockProviderStore) UpdateProvider(_ context.Context, id uuid.UUID, updates map[string]any) error {
	for _, p := range m.providers {
		if p.ID == id {
			if v, ok := updates["api_key"]; ok {
				p.APIKey = v.(string)
			}
			if v, ok := updates["settings"]; ok {
				p.Settings = v.(json.RawMessage)
			}
			if v, ok := updates["enabled"]; ok {
				p.Enabled = v.(bool)
			}
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *mockProviderStore) DeleteProvider(_ context.Context, id uuid.UUID) error {
	for name, p := range m.providers {
		if p.ID == id {
			delete(m.providers, name)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *mockProviderStore) ListAllProviders(_ context.Context) ([]store.LLMProviderData, error) {
	var out []store.LLMProviderData
	for _, p := range m.providers {
		out = append(out, *p)
	}
	return out, nil
}

type mockSecretsStore struct {
	data map[string]string
}

func newMockSecretsStore() *mockSecretsStore {
	return &mockSecretsStore{data: make(map[string]string)}
}

func (m *mockSecretsStore) Get(_ context.Context, key string) (string, error) {
	if v, ok := m.data[key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("not found: %s", key)
}

func (m *mockSecretsStore) Set(_ context.Context, key, value string) error {
	m.data[key] = value
	return nil
}

func (m *mockSecretsStore) Delete(_ context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockSecretsStore) GetAll(_ context.Context) (map[string]string, error) {
	return m.data, nil
}

// --- tests ---

func TestDBTokenSourceSaveAndLoad(t *testing.T) {
	provStore := newMockProviderStore()
	secretStore := newMockSecretsStore()
	ts := NewDBTokenSource(provStore, secretStore, DefaultProviderName)

	resp := &OpenAITokenResponse{
		AccessToken:  "access-token-abc123",
		RefreshToken: "refresh-token-xyz789",
		ExpiresIn:    3600,
	}

	ctx := context.Background()
	id, err := ts.SaveOAuthResult(ctx, resp)
	if err != nil {
		t.Fatalf("SaveOAuthResult: %v", err)
	}
	if id == uuid.Nil {
		t.Fatal("expected non-nil provider ID")
	}

	// Verify provider was created
	p, err := provStore.GetProviderByName(ctx, DefaultProviderName)
	if err != nil {
		t.Fatalf("GetProviderByName: %v", err)
	}
	if p.APIKey != "access-token-abc123" {
		t.Errorf("APIKey = %q, want %q", p.APIKey, "access-token-abc123")
	}

	// Verify refresh token was saved
	rt, err := secretStore.Get(ctx, refreshTokenSecretKey)
	if err != nil {
		t.Fatalf("Get refresh token: %v", err)
	}
	if rt != "refresh-token-xyz789" {
		t.Errorf("refresh token = %q, want %q", rt, "refresh-token-xyz789")
	}

	// Load token from fresh instance
	ts2 := NewDBTokenSource(provStore, secretStore, DefaultProviderName)
	token, err := ts2.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "access-token-abc123" {
		t.Errorf("Token() = %q, want %q", token, "access-token-abc123")
	}
}

func TestDBTokenSourceCaching(t *testing.T) {
	provStore := newMockProviderStore()
	secretStore := newMockSecretsStore()
	ts := NewDBTokenSource(provStore, secretStore, DefaultProviderName)

	resp := &OpenAITokenResponse{
		AccessToken:  "cached-token",
		RefreshToken: "refresh",
		ExpiresIn:    3600,
	}

	ctx := context.Background()
	if _, err := ts.SaveOAuthResult(ctx, resp); err != nil {
		t.Fatalf("SaveOAuthResult: %v", err)
	}

	// Token is cached from SaveOAuthResult
	token1, err := ts.Token()
	if err != nil {
		t.Fatalf("Token (1): %v", err)
	}

	// Delete provider from store — cached token should still work
	p, _ := provStore.GetProviderByName(ctx, DefaultProviderName)
	delete(provStore.providers, DefaultProviderName)

	token2, err := ts.Token()
	if err != nil {
		t.Fatalf("Token (2): %v", err)
	}

	if token1 != token2 {
		t.Errorf("cached tokens differ: %q vs %q", token1, token2)
	}

	// Restore for cleanup
	provStore.providers[DefaultProviderName] = p
}

func TestDBTokenSourceExists(t *testing.T) {
	provStore := newMockProviderStore()
	secretStore := newMockSecretsStore()
	ts := NewDBTokenSource(provStore, secretStore, DefaultProviderName)
	ctx := context.Background()

	if ts.Exists(ctx) {
		t.Error("Exists() = true before save, want false")
	}

	resp := &OpenAITokenResponse{
		AccessToken:  "token",
		RefreshToken: "refresh",
		ExpiresIn:    3600,
	}
	if _, err := ts.SaveOAuthResult(ctx, resp); err != nil {
		t.Fatalf("SaveOAuthResult: %v", err)
	}

	if !ts.Exists(ctx) {
		t.Error("Exists() = false after save, want true")
	}
}

func TestDBTokenSourceDelete(t *testing.T) {
	provStore := newMockProviderStore()
	secretStore := newMockSecretsStore()
	ts := NewDBTokenSource(provStore, secretStore, DefaultProviderName)
	ctx := context.Background()

	resp := &OpenAITokenResponse{
		AccessToken:  "to-delete",
		RefreshToken: "refresh-to-delete",
		ExpiresIn:    3600,
	}
	if _, err := ts.SaveOAuthResult(ctx, resp); err != nil {
		t.Fatalf("SaveOAuthResult: %v", err)
	}

	if err := ts.Delete(ctx); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if ts.Exists(ctx) {
		t.Error("Exists() = true after delete, want false")
	}

	if _, err := secretStore.Get(ctx, refreshTokenSecretKey); err == nil {
		t.Error("refresh token still exists after delete")
	}
}

func TestDBTokenSourceUpdateExisting(t *testing.T) {
	provStore := newMockProviderStore()
	secretStore := newMockSecretsStore()
	ts := NewDBTokenSource(provStore, secretStore, DefaultProviderName)
	ctx := context.Background()

	// Save first time
	resp1 := &OpenAITokenResponse{
		AccessToken:  "token-v1",
		RefreshToken: "refresh-v1",
		ExpiresIn:    3600,
	}
	id1, err := ts.SaveOAuthResult(ctx, resp1)
	if err != nil {
		t.Fatalf("SaveOAuthResult (1): %v", err)
	}

	// Save second time — should update, not create duplicate
	resp2 := &OpenAITokenResponse{
		AccessToken:  "token-v2",
		RefreshToken: "refresh-v2",
		ExpiresIn:    7200,
	}
	id2, err := ts.SaveOAuthResult(ctx, resp2)
	if err != nil {
		t.Fatalf("SaveOAuthResult (2): %v", err)
	}

	if id1 != id2 {
		t.Errorf("IDs differ on update: %s vs %s", id1, id2)
	}

	p, _ := provStore.GetProviderByName(ctx, DefaultProviderName)
	if p.APIKey != "token-v2" {
		t.Errorf("APIKey = %q after update, want %q", p.APIKey, "token-v2")
	}
}

func TestDBTokenSourceSettings(t *testing.T) {
	provStore := newMockProviderStore()
	secretStore := newMockSecretsStore()
	ts := NewDBTokenSource(provStore, secretStore, DefaultProviderName)
	ctx := context.Background()

	resp := &OpenAITokenResponse{
		AccessToken:  "token",
		RefreshToken: "refresh",
		ExpiresIn:    3600,
		Scope:        "openid profile",
	}
	if _, err := ts.SaveOAuthResult(ctx, resp); err != nil {
		t.Fatalf("SaveOAuthResult: %v", err)
	}

	p, _ := provStore.GetProviderByName(ctx, DefaultProviderName)
	var settings OAuthSettings
	if err := json.Unmarshal(p.Settings, &settings); err != nil {
		t.Fatalf("Unmarshal settings: %v", err)
	}

	if settings.Scopes != "openid profile" {
		t.Errorf("Scopes = %q, want %q", settings.Scopes, "openid profile")
	}

	expectedMin := time.Now().Add(3600 * time.Second).Unix()
	if settings.ExpiresAt < expectedMin-5 {
		t.Errorf("ExpiresAt = %d, expected >= %d", settings.ExpiresAt, expectedMin-5)
	}
}
