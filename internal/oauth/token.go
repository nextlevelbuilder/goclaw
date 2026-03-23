package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	// DefaultProviderName is the provider name for ChatGPT OAuth.
	DefaultProviderName = "openai-codex"

	// refreshTokenSecretKey is the config_secrets key for the refresh token.
	refreshTokenSecretKey = "oauth.openai-codex.refresh_token"

	// refreshMargin is how early before expiry we refresh the token.
	refreshMargin = 5 * time.Minute
)

// OAuthSettings is stored in llm_providers.settings JSONB (non-sensitive metadata).
type OAuthSettings struct {
	ExpiresAt int64  `json:"expires_at"` // unix timestamp
	Scopes    string `json:"scopes,omitempty"`
}

// DBTokenSource provides a valid access token backed by the llm_providers + config_secrets tables.
// Implements providers.TokenSource.
type DBTokenSource struct {
	providerStore store.ProviderStore
	secretsStore  store.ConfigSecretsStore
	providerName  string
	tenantID      uuid.UUID // tenant context for DB queries

	mu          sync.Mutex
	cachedToken string
	expiresAt   time.Time
}

// NewDBTokenSource creates a DB-backed token source.
func NewDBTokenSource(provStore store.ProviderStore, secretsStore store.ConfigSecretsStore, providerName string) *DBTokenSource {
	return &DBTokenSource{
		providerStore: provStore,
		secretsStore:  secretsStore,
		providerName:  providerName,
		tenantID:      store.MasterTenantID,
	}
}

// WithTenantID sets the tenant context for DB queries. Must be called at init time before Token().
func (ts *DBTokenSource) WithTenantID(tenantID uuid.UUID) *DBTokenSource {
	ts.tenantID = tenantID
	return ts
}

// Token returns a valid access token, refreshing if expired or about to expire.
func (ts *DBTokenSource) Token() (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Use cached token if still valid
	if ts.cachedToken != "" && time.Until(ts.expiresAt) > refreshMargin {
		return ts.cachedToken, nil
	}

	ctx := store.WithTenantID(context.Background(), ts.tenantID)

	// Load from DB if not cached
	if ts.cachedToken == "" {
		p, err := ts.providerStore.GetProviderByName(ctx, ts.providerName)
		if err != nil {
			return "", fmt.Errorf("load oauth provider %q: %w", ts.providerName, err)
		}
		ts.cachedToken = p.APIKey

		var settings OAuthSettings
		if len(p.Settings) > 0 {
			_ = json.Unmarshal(p.Settings, &settings)
		}
		if settings.ExpiresAt > 0 {
			ts.expiresAt = time.Unix(settings.ExpiresAt, 0)
		}
	}

	// Refresh if expired or expiring soon
	if time.Until(ts.expiresAt) < refreshMargin {
		if err := ts.refresh(ctx); err != nil {
			// If refresh fails but we still have a token, return it (might still work)
			if ts.cachedToken != "" {
				slog.Warn("oauth token refresh failed, using existing token", "error", err)
				return ts.cachedToken, nil
			}
			return "", fmt.Errorf("refresh oauth token: %w", err)
		}
	}

	return ts.cachedToken, nil
}

// refresh gets the refresh token from config_secrets, calls RefreshOpenAIToken, and updates DB.
func (ts *DBTokenSource) refresh(ctx context.Context) error {
	refreshToken, err := ts.secretsStore.Get(ctx, refreshTokenSecretKey)
	if err != nil {
		return fmt.Errorf("get refresh token: %w", err)
	}

	slog.Info("refreshing OpenAI OAuth token")
	newToken, err := RefreshOpenAIToken(refreshToken)
	if err != nil {
		return err
	}

	// Update cached values
	ts.cachedToken = newToken.AccessToken
	ts.expiresAt = time.Now().Add(time.Duration(newToken.ExpiresIn) * time.Second)

	// Update provider api_key (access token) in DB
	p, err := ts.providerStore.GetProviderByName(ctx, ts.providerName)
	if err != nil {
		return fmt.Errorf("get provider for update: %w", err)
	}

	settings := OAuthSettings{
		ExpiresAt: ts.expiresAt.Unix(),
	}
	settingsJSON, _ := json.Marshal(settings)

	if err := ts.providerStore.UpdateProvider(ctx, p.ID, map[string]any{
		"api_key":  newToken.AccessToken,
		"settings": json.RawMessage(settingsJSON),
	}); err != nil {
		slog.Warn("failed to persist refreshed access token", "error", err)
	}

	// Update refresh token if a new one was issued
	if newToken.RefreshToken != "" {
		if err := ts.secretsStore.Set(ctx, refreshTokenSecretKey, newToken.RefreshToken); err != nil {
			slog.Warn("failed to persist new refresh token", "error", err)
		}
	}

	return nil
}

// SaveOAuthResult persists OAuth tokens after a successful exchange.
// Creates or updates the provider in llm_providers and stores refresh token in config_secrets.
// Returns the provider ID.
func (ts *DBTokenSource) SaveOAuthResult(ctx context.Context, tokenResp *OpenAITokenResponse) (uuid.UUID, error) {
	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	settings := OAuthSettings{
		ExpiresAt: expiresAt.Unix(),
		Scopes:    tokenResp.Scope,
	}
	settingsJSON, _ := json.Marshal(settings)

	// Update cache
	ts.mu.Lock()
	ts.cachedToken = tokenResp.AccessToken
	ts.expiresAt = expiresAt
	ts.mu.Unlock()

	// Check if provider already exists
	existing, err := ts.providerStore.GetProviderByName(ctx, ts.providerName)
	if err == nil {
		// Update existing provider
		if err := ts.providerStore.UpdateProvider(ctx, existing.ID, map[string]any{
			"api_key":  tokenResp.AccessToken,
			"settings": json.RawMessage(settingsJSON),
			"enabled":  true,
		}); err != nil {
			return uuid.Nil, fmt.Errorf("update provider: %w", err)
		}

		// Save refresh token
		if tokenResp.RefreshToken != "" {
			if err := ts.secretsStore.Set(ctx, refreshTokenSecretKey, tokenResp.RefreshToken); err != nil {
				return uuid.Nil, fmt.Errorf("save refresh token: %w", err)
			}
		}

		return existing.ID, nil
	}

	// Create new provider
	p := &store.LLMProviderData{
		Name:         ts.providerName,
		DisplayName:  "ChatGPT (OAuth)",
		ProviderType: store.ProviderChatGPTOAuth,
		APIBase:      "https://chatgpt.com/backend-api",
		APIKey:       tokenResp.AccessToken,
		Enabled:      true,
		Settings:     settingsJSON,
	}
	if err := ts.providerStore.CreateProvider(ctx, p); err != nil {
		return uuid.Nil, fmt.Errorf("create provider: %w", err)
	}

	// Save refresh token
	if tokenResp.RefreshToken != "" {
		if err := ts.secretsStore.Set(ctx, refreshTokenSecretKey, tokenResp.RefreshToken); err != nil {
			return uuid.Nil, fmt.Errorf("save refresh token: %w", err)
		}
	}

	return p.ID, nil
}

// Delete removes the OAuth provider from DB and its refresh token from config_secrets.
func (ts *DBTokenSource) Delete(ctx context.Context) error {
	ts.mu.Lock()
	ts.cachedToken = ""
	ts.expiresAt = time.Time{}
	ts.mu.Unlock()

	// Delete refresh token from config_secrets
	_ = ts.secretsStore.Delete(ctx, refreshTokenSecretKey)

	// Delete provider from llm_providers
	p, err := ts.providerStore.GetProviderByName(ctx, ts.providerName)
	if err != nil {
		return nil // already gone
	}
	return ts.providerStore.DeleteProvider(ctx, p.ID)
}

// Exists checks if an OAuth provider exists and has a valid token.
func (ts *DBTokenSource) Exists(ctx context.Context) bool {
	p, err := ts.providerStore.GetProviderByName(ctx, ts.providerName)
	return err == nil && p.APIKey != ""
}
