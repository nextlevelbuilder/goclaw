package oauth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	// DefaultProviderName is the default alias for the primary OpenAI Codex OAuth account.
	DefaultProviderName = "openai-codex"

	// DefaultProviderDisplayName stays empty so the UI can fall back to the alias unless the
	// user explicitly sets a friendlier label for this OpenAI Codex OAuth account.
	DefaultProviderDisplayName = ""

	// DefaultProviderAPIBase is the default API base for OpenAI Codex OAuth providers.
	DefaultProviderAPIBase = "https://chatgpt.com/backend-api"

	// refreshTokenSecretKey is the config_secrets key for the refresh token.
	refreshTokenSecretKey = "oauth.openai-codex.refresh_token"

	// refreshMargin is how early before expiry we refresh the token.
	refreshMargin = 5 * time.Minute

	routeEligibilityTTL = 20 * time.Second
)

var refreshOpenAITokenFunc = RefreshOpenAIToken

// OAuthSettings is stored in llm_providers.settings JSONB (non-sensitive metadata).
type OAuthSettings struct {
	ExpiresAt int64  `json:"expires_at"` // unix timestamp
	Scopes    string `json:"scopes,omitempty"`
	AccountID string `json:"account_id,omitempty"`
	PlanType  string `json:"plan_type,omitempty"`
}

// ProviderTypeConflictError reports that the requested OAuth provider name is
// already taken by a different provider type.
type ProviderTypeConflictError struct {
	ProviderName string
	ProviderType string
}

func (e *ProviderTypeConflictError) Error() string {
	return fmt.Sprintf("provider %q already exists as type %q", e.ProviderName, e.ProviderType)
}

// DBTokenSource provides a valid access token backed by the llm_providers + config_secrets tables.
// Implements providers.TokenSource.
type DBTokenSource struct {
	providerStore store.ProviderStore
	secretsStore  store.ConfigSecretsStore
	providerName  string
	tenantID      uuid.UUID // tenant context for DB queries

	providerDisplayName string
	providerAPIBase     string

	mu          sync.Mutex
	cachedToken string
	expiresAt   time.Time

	cachedRouteEligibility   providers.RouteEligibility
	cachedRouteEligibilityAt time.Time
	refreshingEligibility    atomic.Bool
}

// NewDBTokenSource creates a DB-backed token source.
func NewDBTokenSource(provStore store.ProviderStore, secretsStore store.ConfigSecretsStore, providerName string) *DBTokenSource {
	if strings.TrimSpace(providerName) == "" {
		providerName = DefaultProviderName
	}
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

// WithProviderMeta sets defaults used when creating or updating OAuth-backed providers.
func (ts *DBTokenSource) WithProviderMeta(displayName, apiBase string) *DBTokenSource {
	if strings.TrimSpace(displayName) != "" {
		ts.providerDisplayName = strings.TrimSpace(displayName)
	}
	if strings.TrimSpace(apiBase) != "" {
		ts.providerAPIBase = strings.TrimSpace(apiBase)
	}
	return ts
}

func (ts *DBTokenSource) resolvedDisplayName() string {
	if ts.providerDisplayName != "" {
		return ts.providerDisplayName
	}
	return DefaultProviderDisplayName
}

func (ts *DBTokenSource) resolvedAPIBase() string {
	if ts.providerAPIBase != "" {
		return ts.providerAPIBase
	}
	return DefaultProviderAPIBase
}

func (ts *DBTokenSource) RouteEligibility(ctx context.Context) providers.RouteEligibility {
	ts.mu.Lock()
	cached := ts.cachedRouteEligibility
	cachedAt := ts.cachedRouteEligibilityAt
	ts.mu.Unlock()

	if cached.Class != "" {
		if time.Since(cachedAt) < routeEligibilityTTL {
			return cached
		}
		// Stale-while-revalidate: return stale cache immediately,
		// refresh asynchronously so the hot path is never blocked by
		// an outbound HTTP quota check. Dedup concurrent refreshes.
		if ts.refreshingEligibility.CompareAndSwap(false, true) {
			go func() {
				defer ts.refreshingEligibility.Store(false)
				ts.refreshRouteEligibility()
			}()
		}
		return cached
	}

	// First call ever — synchronous fetch so we have a baseline.
	return ts.fetchAndCacheEligibility(ctx)
}

// refreshRouteEligibility fetches quota eligibility in the background and updates the cache.
func (ts *DBTokenSource) refreshRouteEligibility() {
	ctx := store.WithTenantID(context.Background(), ts.tenantID)
	ts.fetchAndCacheEligibility(ctx)
}

func (ts *DBTokenSource) fetchAndCacheEligibility(ctx context.Context) providers.RouteEligibility {
	if ctx == nil {
		ctx = context.Background()
	}
	if store.TenantIDFromContext(ctx) == uuid.Nil {
		ctx = store.WithTenantID(ctx, ts.tenantID)
	}

	eligibility := providers.RouteEligibility{Class: providers.RouteEligibilityUnknown, Reason: "unavailable"}
	provider, err := ts.loadOAuthProvider(ctx)
	if err == nil {
		if !provider.Enabled {
			eligibility = providers.RouteEligibility{Class: providers.RouteEligibilityBlocked, Reason: "disabled"}
		} else {
			eligibility = OpenAIQuotaRouteEligibility(FetchOpenAIQuota(ctx, provider, ts))
		}
	}

	ts.mu.Lock()
	ts.cachedRouteEligibility = eligibility
	ts.cachedRouteEligibilityAt = time.Now()
	ts.mu.Unlock()
	return eligibility
}

// RefreshTokenSecretKey returns the tenant-scoped secret key for a provider refresh token.
func RefreshTokenSecretKey(providerName string) string {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" || providerName == DefaultProviderName {
		return refreshTokenSecretKey
	}
	return fmt.Sprintf("oauth.%s.refresh_token", providerName)
}

func (ts *DBTokenSource) loadOAuthProvider(ctx context.Context) (*store.LLMProviderData, error) {
	p, err := ts.providerStore.GetProviderByName(ctx, ts.providerName)
	if err != nil {
		return nil, err
	}
	if p.ProviderType != store.ProviderChatGPTOAuth {
		return nil, &ProviderTypeConflictError{
			ProviderName: ts.providerName,
			ProviderType: p.ProviderType,
		}
	}
	return p, nil
}

func (ts *DBTokenSource) withTenantContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if store.TenantIDFromContext(ctx) == uuid.Nil {
		ctx = store.WithTenantID(ctx, ts.tenantID)
	}
	return ctx
}

func (ts *DBTokenSource) persistProviderMetadata(ctx context.Context, provider *store.LLMProviderData, metadata openAITokenMetadata) (*store.LLMProviderData, bool, error) {
	settings := parseOAuthSettings(provider.Settings)
	changed := false

	if settings.AccountID == "" && strings.TrimSpace(metadata.AccountID) != "" {
		settings.AccountID = strings.TrimSpace(metadata.AccountID)
		changed = true
	}
	if settings.PlanType == "" && strings.TrimSpace(metadata.PlanType) != "" {
		settings.PlanType = strings.TrimSpace(metadata.PlanType)
		changed = true
	}

	if changed {
		raw := marshalOAuthSettingsInto(provider.Settings, settings)
		if err := ts.providerStore.UpdateProvider(ctx, provider.ID, map[string]any{
			"settings": raw,
		}); err != nil {
			return provider, false, err
		}
		provider.Settings = raw
		ts.mu.Lock()
		ts.cachedRouteEligibility = providers.RouteEligibility{}
		ts.cachedRouteEligibilityAt = time.Time{}
		ts.mu.Unlock()
	}

	return provider, strings.TrimSpace(settings.AccountID) != "", nil
}

func (ts *DBTokenSource) backfillProviderMetadataFromToken(ctx context.Context, provider *store.LLMProviderData, token string) (*store.LLMProviderData, bool, error) {
	metadata, ok := parseOpenAIJWTMetadata(token)
	if !ok {
		return provider, false, nil
	}
	return ts.persistProviderMetadata(ctx, provider, metadata)
}

// BackfillProviderMetadata restores missing ChatGPT workspace metadata for legacy OAuth providers.
// It first tries to recover metadata from the currently stored access token, then forces a token refresh
// so modern refresh responses can repopulate account settings when older providers were saved without them.
func (ts *DBTokenSource) BackfillProviderMetadata(ctx context.Context, provider *store.LLMProviderData) (*store.LLMProviderData, error) {
	if provider == nil {
		return nil, nil
	}

	ctx = ts.withTenantContext(ctx)
	settings := parseOAuthSettings(provider.Settings)
	if strings.TrimSpace(settings.AccountID) != "" {
		return provider, nil
	}

	updatedProvider, recovered, err := ts.backfillProviderMetadataFromToken(ctx, provider, provider.APIKey)
	if err != nil {
		return provider, err
	}
	if recovered {
		return updatedProvider, nil
	}

	ts.mu.Lock()
	refreshErr := ts.refresh(ctx)
	ts.mu.Unlock()
	if refreshErr != nil {
		return provider, refreshErr
	}

	refreshedProvider, err := ts.loadOAuthProvider(ctx)
	if err != nil {
		return provider, err
	}
	return refreshedProvider, nil
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
		p, err := ts.loadOAuthProvider(ctx)
		if err != nil {
			return "", fmt.Errorf("load oauth provider %q: %w", ts.providerName, err)
		}
		ts.cachedToken = p.APIKey

		settings := parseOAuthSettings(p.Settings)
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
	refreshToken, err := ts.secretsStore.Get(ctx, RefreshTokenSecretKey(ts.providerName))
	if err != nil {
		return fmt.Errorf("get refresh token: %w", err)
	}

	slog.Info("refreshing OpenAI OAuth token")
	newToken, err := refreshOpenAITokenFunc(refreshToken)
	if err != nil {
		return err
	}

	// Update cached values
	ts.cachedToken = newToken.AccessToken
	ts.expiresAt = time.Now().Add(time.Duration(newToken.ExpiresIn) * time.Second)
	ts.cachedRouteEligibility = providers.RouteEligibility{}
	ts.cachedRouteEligibilityAt = time.Time{}

	// Update provider api_key (access token) in DB
	p, err := ts.loadOAuthProvider(ctx)
	if err != nil {
		return fmt.Errorf("get provider for update: %w", err)
	}

	settings := mergeOAuthSettings(parseOAuthSettings(p.Settings), newToken, ts.expiresAt)

	if err := ts.providerStore.UpdateProvider(ctx, p.ID, map[string]any{
		"api_key":  newToken.AccessToken,
		"settings": marshalOAuthSettingsInto(p.Settings, settings),
	}); err != nil {
		slog.Warn("failed to persist refreshed access token", "error", err)
	}

	// Update refresh token if a new one was issued
	if newToken.RefreshToken != "" {
		if err := ts.secretsStore.Set(ctx, RefreshTokenSecretKey(ts.providerName), newToken.RefreshToken); err != nil {
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

	// Update cache
	ts.mu.Lock()
	ts.cachedToken = tokenResp.AccessToken
	ts.expiresAt = expiresAt
	ts.cachedRouteEligibility = providers.RouteEligibility{}
	ts.cachedRouteEligibilityAt = time.Time{}
	ts.mu.Unlock()

	// Check if provider already exists
	existing, err := ts.loadOAuthProvider(ctx)
	if err == nil {
		settings := mergeOAuthSettings(parseOAuthSettings(existing.Settings), tokenResp, expiresAt)
		// Update existing provider
		updates := map[string]any{
			"api_key":  tokenResp.AccessToken,
			"settings": marshalOAuthSettingsInto(existing.Settings, settings),
			"enabled":  true,
		}
		if ts.providerDisplayName != "" {
			updates["display_name"] = ts.providerDisplayName
		}
		if ts.providerAPIBase != "" {
			updates["api_base"] = ts.providerAPIBase
		}
		if err := ts.providerStore.UpdateProvider(ctx, existing.ID, updates); err != nil {
			return uuid.Nil, fmt.Errorf("update provider: %w", err)
		}

		// Save refresh token
		if tokenResp.RefreshToken != "" {
			if err := ts.secretsStore.Set(ctx, RefreshTokenSecretKey(ts.providerName), tokenResp.RefreshToken); err != nil {
				return uuid.Nil, fmt.Errorf("save refresh token: %w", err)
			}
		}

		return existing.ID, nil
	}
	if _, ok := err.(*ProviderTypeConflictError); ok {
		return uuid.Nil, err
	}

	settings := mergeOAuthSettings(OAuthSettings{}, tokenResp, expiresAt)
	// Create new provider
	p := &store.LLMProviderData{
		Name:         ts.providerName,
		DisplayName:  ts.resolvedDisplayName(),
		ProviderType: store.ProviderChatGPTOAuth,
		APIBase:      ts.resolvedAPIBase(),
		APIKey:       tokenResp.AccessToken,
		Enabled:      true,
		Settings:     marshalOAuthSettings(settings),
	}
	if err := ts.providerStore.CreateProvider(ctx, p); err != nil {
		return uuid.Nil, fmt.Errorf("create provider: %w", err)
	}

	// Save refresh token
	if tokenResp.RefreshToken != "" {
		if err := ts.secretsStore.Set(ctx, RefreshTokenSecretKey(ts.providerName), tokenResp.RefreshToken); err != nil {
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
	ts.cachedRouteEligibility = providers.RouteEligibility{}
	ts.cachedRouteEligibilityAt = time.Time{}
	ts.mu.Unlock()

	// Delete refresh token from config_secrets
	_ = ts.secretsStore.Delete(ctx, RefreshTokenSecretKey(ts.providerName))

	// Delete provider from llm_providers
	p, err := ts.loadOAuthProvider(ctx)
	if err != nil {
		if _, ok := err.(*ProviderTypeConflictError); ok {
			return err
		}
		return nil // already gone
	}
	return ts.providerStore.DeleteProvider(ctx, p.ID)
}

// Exists checks if an OAuth provider exists and has a valid token.
func (ts *DBTokenSource) Exists(ctx context.Context) bool {
	p, err := ts.loadOAuthProvider(ctx)
	return err == nil && p.APIKey != ""
}
