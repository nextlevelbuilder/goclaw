package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const gitHubCopilotRefreshMargin = 5 * time.Minute

type GitHubCopilotTokenSource struct {
	providerStore store.ProviderStore
	secretsStore  store.ConfigSecretsStore
	providerName  string
	tenantID      uuid.UUID

	providerDisplayName string
	providerAPIBase     string

	mu          sync.Mutex
	cachedToken string
	expiresAt   time.Time
}

func NewGitHubCopilotTokenSource(provStore store.ProviderStore, secretsStore store.ConfigSecretsStore, providerName string) *GitHubCopilotTokenSource {
	if strings.TrimSpace(providerName) == "" {
		providerName = DefaultGitHubCopilotProviderName
	}
	return &GitHubCopilotTokenSource{
		providerStore: provStore,
		secretsStore:  secretsStore,
		providerName:  providerName,
		tenantID:      store.MasterTenantID,
	}
}

func (ts *GitHubCopilotTokenSource) WithTenantID(tenantID uuid.UUID) *GitHubCopilotTokenSource {
	ts.tenantID = tenantID
	return ts
}

func (ts *GitHubCopilotTokenSource) WithProviderMeta(displayName, apiBase string) *GitHubCopilotTokenSource {
	if trimmed := strings.TrimSpace(displayName); trimmed != "" {
		ts.providerDisplayName = trimmed
	}
	if trimmed := strings.TrimSpace(apiBase); trimmed != "" {
		ts.providerAPIBase = trimmed
	}
	return ts
}

func (ts *GitHubCopilotTokenSource) APIBase() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if strings.TrimSpace(ts.providerAPIBase) != "" {
		return strings.TrimSpace(ts.providerAPIBase)
	}
	return DefaultGitHubCopilotAPIBase
}

func GitHubCopilotAccessTokenSecretKey(providerName string) string {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" || providerName == DefaultGitHubCopilotProviderName {
		return "oauth.github-copilot.github_token"
	}
	return fmt.Sprintf("oauth.%s.github_token", providerName)
}

func (ts *GitHubCopilotTokenSource) withTenantContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if store.TenantIDFromContext(ctx) == uuid.Nil {
		ctx = store.WithTenantID(ctx, ts.tenantID)
	}
	return ctx
}

func (ts *GitHubCopilotTokenSource) loadProvider(ctx context.Context) (*store.LLMProviderData, error) {
	p, err := ts.providerStore.GetProviderByName(ctx, ts.providerName)
	if err != nil {
		return nil, err
	}
	if p.ProviderType != store.ProviderGitHubCopilotOAuth {
		return nil, &ProviderTypeConflictError{ProviderName: ts.providerName, ProviderType: p.ProviderType}
	}
	return p, nil
}

func (ts *GitHubCopilotTokenSource) Token() (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.cachedToken != "" && time.Until(ts.expiresAt) > gitHubCopilotRefreshMargin {
		return ts.cachedToken, nil
	}
	ctx := store.WithTenantID(context.Background(), ts.tenantID)
	if ts.cachedToken == "" {
		provider, err := ts.loadProvider(ctx)
		if err != nil {
			return "", fmt.Errorf("load github copilot provider %q: %w", ts.providerName, err)
		}
		ts.cachedToken = provider.APIKey
		if strings.TrimSpace(provider.APIBase) != "" {
			ts.providerAPIBase = strings.TrimSpace(provider.APIBase)
		}
		if settings := parseGitHubCopilotOAuthSettings(provider.Settings); settings.ExpiresAt > 0 {
			ts.expiresAt = time.Unix(settings.ExpiresAt, 0)
		}
	}
	if time.Until(ts.expiresAt) < gitHubCopilotRefreshMargin {
		if err := ts.refresh(ctx); err != nil {
			if ts.cachedToken != "" {
				return ts.cachedToken, nil
			}
			return "", err
		}
	}
	return ts.cachedToken, nil
}

func (ts *GitHubCopilotTokenSource) refresh(ctx context.Context) error {
	provider, err := ts.loadProvider(ctx)
	if err != nil {
		return err
	}
	settings := parseGitHubCopilotOAuthSettings(provider.Settings)
	githubToken, err := ts.secretsStore.Get(ctx, GitHubCopilotAccessTokenSecretKey(ts.providerName))
	if err != nil {
		return fmt.Errorf("get GitHub Copilot login token: %w", err)
	}
	tokenResp, err := RefreshGitHubCopilotToken(githubToken, settings.EnterpriseDomain)
	if err != nil {
		return fmt.Errorf("refresh GitHub Copilot token: %w", err)
	}
	ts.cachedToken = tokenResp.Token
	ts.expiresAt = time.Unix(tokenResp.ExpiresAt, 0)
	apiBase := GetGitHubCopilotBaseURL(tokenResp.Token, settings.EnterpriseDomain)
	ts.providerAPIBase = apiBase
	merged := mergeGitHubCopilotOAuthSettings(settings, tokenResp, apiBase)
	return ts.providerStore.UpdateProvider(ctx, provider.ID, map[string]any{
		"api_key":  tokenResp.Token,
		"api_base": apiBase,
		"settings": marshalGitHubCopilotOAuthSettingsInto(provider.Settings, merged),
		"enabled":  true,
	})
}

func (ts *GitHubCopilotTokenSource) SaveLoginResult(ctx context.Context, githubAccessToken string, tokenResp *GitHubCopilotTokenResponse, enterpriseDomain string) (uuid.UUID, error) {
	ctx = ts.withTenantContext(ctx)
	apiBase := ts.providerAPIBase
	if apiBase == "" {
		apiBase = GetGitHubCopilotBaseURL(tokenResp.Token, enterpriseDomain)
	}
	ts.mu.Lock()
	ts.cachedToken = tokenResp.Token
	ts.expiresAt = time.Unix(tokenResp.ExpiresAt, 0)
	ts.providerAPIBase = apiBase
	ts.mu.Unlock()

	settings := mergeGitHubCopilotOAuthSettings(store.GitHubCopilotOAuthProviderSettings{EnterpriseDomain: NormalizeGitHubCopilotDomain(enterpriseDomain)}, tokenResp, apiBase)
	existing, err := ts.loadProvider(ctx)
	if err == nil {
		updates := map[string]any{
			"api_key":  tokenResp.Token,
			"api_base": apiBase,
			"settings": marshalGitHubCopilotOAuthSettingsInto(existing.Settings, settings),
			"enabled":  true,
		}
		if ts.providerDisplayName != "" {
			updates["display_name"] = ts.providerDisplayName
		}
		if err := ts.providerStore.UpdateProvider(ctx, existing.ID, updates); err != nil {
			return uuid.Nil, err
		}
		if err := ts.secretsStore.Set(ctx, GitHubCopilotAccessTokenSecretKey(ts.providerName), githubAccessToken); err != nil {
			return uuid.Nil, err
		}
		return existing.ID, nil
	}
	if _, ok := err.(*ProviderTypeConflictError); ok {
		return uuid.Nil, err
	}
	p := &store.LLMProviderData{
		Name:         ts.providerName,
		DisplayName:  ts.providerDisplayName,
		ProviderType: store.ProviderGitHubCopilotOAuth,
		APIBase:      apiBase,
		APIKey:       tokenResp.Token,
		Enabled:      true,
		Settings:     marshalGitHubCopilotOAuthSettings(settings),
	}
	if err := ts.providerStore.CreateProvider(ctx, p); err != nil {
		return uuid.Nil, err
	}
	if err := ts.secretsStore.Set(ctx, GitHubCopilotAccessTokenSecretKey(ts.providerName), githubAccessToken); err != nil {
		return uuid.Nil, err
	}
	return p.ID, nil
}

func (ts *GitHubCopilotTokenSource) Delete(ctx context.Context) error {
	ctx = ts.withTenantContext(ctx)
	ts.mu.Lock()
	ts.cachedToken = ""
	ts.expiresAt = time.Time{}
	ts.mu.Unlock()
	_ = ts.secretsStore.Delete(ctx, GitHubCopilotAccessTokenSecretKey(ts.providerName))
	p, err := ts.loadProvider(ctx)
	if err != nil {
		if _, ok := err.(*ProviderTypeConflictError); ok {
			return err
		}
		return nil
	}
	return ts.providerStore.DeleteProvider(ctx, p.ID)
}

func (ts *GitHubCopilotTokenSource) Exists(ctx context.Context) bool {
	ctx = ts.withTenantContext(ctx)
	p, err := ts.loadProvider(ctx)
	return err == nil && p.APIKey != ""
}

func parseGitHubCopilotOAuthSettings(raw json.RawMessage) store.GitHubCopilotOAuthProviderSettings {
	if len(raw) == 0 {
		return store.GitHubCopilotOAuthProviderSettings{}
	}
	var settings store.GitHubCopilotOAuthProviderSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return store.GitHubCopilotOAuthProviderSettings{}
	}
	return settings
}

func mergeGitHubCopilotOAuthSettings(existing store.GitHubCopilotOAuthProviderSettings, tokenResp *GitHubCopilotTokenResponse, apiBase string) store.GitHubCopilotOAuthProviderSettings {
	settings := existing
	settings.ExpiresAt = tokenResp.ExpiresAt
	if tokenResp.SKU != "" {
		settings.CopilotPlan = tokenResp.SKU
	}
	if apiBase != "" {
		settings.BaseURL = apiBase
	}
	return settings
}

func marshalGitHubCopilotOAuthSettings(settings store.GitHubCopilotOAuthProviderSettings) json.RawMessage {
	data, _ := json.Marshal(settings)
	return json.RawMessage(data)
}

func marshalGitHubCopilotOAuthSettingsInto(raw json.RawMessage, settings store.GitHubCopilotOAuthProviderSettings) json.RawMessage {
	if len(raw) == 0 {
		return marshalGitHubCopilotOAuthSettings(settings)
	}
	next := make(map[string]any)
	if err := json.Unmarshal(raw, &next); err != nil {
		return marshalGitHubCopilotOAuthSettings(settings)
	}
	next["expires_at"] = settings.ExpiresAt
	if settings.Scopes != "" {
		next["scopes"] = settings.Scopes
	} else {
		delete(next, "scopes")
	}
	if settings.EnterpriseDomain != "" {
		next["enterprise_domain"] = settings.EnterpriseDomain
	} else {
		delete(next, "enterprise_domain")
	}
	if settings.GitHubLogin != "" {
		next["github_login"] = settings.GitHubLogin
	} else {
		delete(next, "github_login")
	}
	if settings.CopilotPlan != "" {
		next["copilot_plan"] = settings.CopilotPlan
	} else {
		delete(next, "copilot_plan")
	}
	if settings.BaseURL != "" {
		next["base_url"] = settings.BaseURL
	} else {
		delete(next, "base_url")
	}
	data, _ := json.Marshal(next)
	return json.RawMessage(data)
}