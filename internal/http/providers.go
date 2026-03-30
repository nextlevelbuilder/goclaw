package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/oauth"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ProvidersHandler handles LLM provider CRUD endpoints.
type ProvidersHandler struct {
	store           store.ProviderStore
	secretStore     store.ConfigSecretsStore
	providerReg     *providers.Registry
	gatewayAddr     string                           // for injecting MCP bridge into Claude CLI providers
	mcpLookup       providers.MCPServerLookup        // optional: resolves per-agent MCP servers
	apiBaseFallback func(providerType string) string // optional: config/env fallback for api_base
	cliMu           sync.Mutex                       // serializes Claude CLI provider create to prevent duplicates
	msgBus          *bus.MessageBus
	sysConfigStore  store.SystemConfigStore
	tracingStore    store.TracingStore   // optional: for provider-scoped pool activity
	agents          store.AgentCRUDStore // optional: for provider pool activity agent lookup
}

// NewProvidersHandler creates a handler for provider management endpoints.
func NewProvidersHandler(s store.ProviderStore, secretStore store.ConfigSecretsStore, providerReg *providers.Registry, gatewayAddr string) *ProvidersHandler {
	return &ProvidersHandler{store: s, secretStore: secretStore, providerReg: providerReg, gatewayAddr: gatewayAddr}
}

// SetMessageBus sets the message bus for audit event broadcasting.
// Must be called before serving requests (not thread-safe).
func (h *ProvidersHandler) SetMessageBus(msgBus *bus.MessageBus) {
	h.msgBus = msgBus
}

// SetSystemConfigStore sets the system config store for embedding status checks.
func (h *ProvidersHandler) SetSystemConfigStore(s store.SystemConfigStore) {
	h.sysConfigStore = s
}

// SetMCPServerLookup sets the per-agent MCP server lookup for Claude CLI providers.
// Must be called before serving requests (not thread-safe).
func (h *ProvidersHandler) SetMCPServerLookup(lookup providers.MCPServerLookup) {
	h.mcpLookup = lookup
}

// SetAPIBaseFallback sets a function that returns config/env api_base by provider type.
// Used as fallback when DB providers have no api_base set.
func (h *ProvidersHandler) SetAPIBaseFallback(fn func(providerType string) string) {
	h.apiBaseFallback = fn
}

// SetTracingStore sets the tracing store for provider-scoped pool activity.
func (h *ProvidersHandler) SetTracingStore(ts store.TracingStore) {
	h.tracingStore = ts
}

// SetAgentStore sets the agent store for provider pool activity agent lookup.
func (h *ProvidersHandler) SetAgentStore(as store.AgentCRUDStore) {
	h.agents = as
}

// resolveAPIBase returns the provider's api_base, falling back to config/env if empty.
func (h *ProvidersHandler) resolveAPIBase(p *store.LLMProviderData) string {
	if p.APIBase != "" {
		return p.APIBase
	}
	if h.apiBaseFallback != nil {
		return h.apiBaseFallback(p.ProviderType)
	}
	return ""
}

// emitProviderCacheInvalidate broadcasts a provider cache invalidation event.
// Subscribers (e.g. ACP re-registration in gateway_managed.go) react to reload from DB.
func (h *ProvidersHandler) emitProviderCacheInvalidate(name string) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindProvider, Key: name},
	})
}

// RegisterRoutes registers all provider management routes on the given mux.
func (h *ProvidersHandler) RegisterRoutes(mux *http.ServeMux) {
	// Provider CRUD
	mux.HandleFunc("GET /v1/providers", h.auth(h.handleListProviders))
	mux.HandleFunc("POST /v1/providers", h.auth(h.handleCreateProvider))
	mux.HandleFunc("GET /v1/providers/{id}", h.auth(h.handleGetProvider))
	mux.HandleFunc("PUT /v1/providers/{id}", h.auth(h.handleUpdateProvider))
	mux.HandleFunc("DELETE /v1/providers/{id}", h.auth(h.handleDeleteProvider))

	// Model listing (proxied to upstream provider API)
	mux.HandleFunc("GET /v1/providers/{id}/models", h.auth(h.handleListProviderModels))

	// Provider + model verification (pre-flight check)
	mux.HandleFunc("POST /v1/providers/{id}/verify", h.auth(h.handleVerifyProvider))
	mux.HandleFunc("POST /v1/providers/{id}/verify-embedding", h.auth(h.handleVerifyEmbedding))

	// Provider-scoped Codex pool activity monitor
	mux.HandleFunc("GET /v1/providers/{id}/codex-pool-activity", h.auth(h.handleProviderCodexPoolActivity))

	// Embedding system status
	mux.HandleFunc("GET /v1/embedding/status", h.auth(h.handleEmbeddingStatus))

	// Claude CLI auth status (global — not per-provider)
	mux.HandleFunc("GET /v1/providers/claude-cli/auth-status", h.auth(h.handleClaudeCLIAuthStatus))
}

func (h *ProvidersHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
}

// maskAPIKey replaces non-empty API keys with "***".
func maskAPIKey(p *store.LLMProviderData) {
	if p.APIKey != "" {
		p.APIKey = "***"
	}
}

// registerInMemory adds (or replaces) a provider in the in-memory registry
// so it's immediately usable for verify/chat without a gateway restart.
func (h *ProvidersHandler) registerInMemory(p *store.LLMProviderData) {
	if h.providerReg == nil || !p.Enabled {
		return
	}
	// ACP agents don't need an API key — skip in-memory registration
	// (ACP providers are registered via gateway_providers.go on startup or restart)
	if p.ProviderType == store.ProviderACP {
		return
	}
	// Claude CLI doesn't need an API key — register immediately
	if p.ProviderType == store.ProviderClaudeCLI {
		cliPath := p.APIBase // reuse APIBase field for CLI path
		if cliPath == "" {
			cliPath = "claude"
		}
		var cliOpts []providers.ClaudeCLIOption
		cliOpts = append(cliOpts, providers.WithClaudeCLISecurityHooks("", true))
		if h.gatewayAddr != "" {
			mcpData := providers.BuildCLIMCPConfigData(nil, h.gatewayAddr, pkgGatewayToken)
			mcpData.AgentMCPLookup = h.mcpLookup
			cliOpts = append(cliOpts, providers.WithClaudeCLIMCPConfigData(mcpData))
		}
		h.providerReg.RegisterForTenant(p.TenantID, providers.NewClaudeCLIProvider(cliPath, cliOpts...))
		return
	}
	// Ollama doesn't need an API key — handle before the key guard (same as startup).
	// In Docker, swap localhost → host.docker.internal so the container can reach the host.
	if p.ProviderType == store.ProviderOllama {
		host := p.APIBase
		if host == "" {
			host = "http://localhost:11434"
		}
		h.providerReg.RegisterForTenant(p.TenantID, providers.NewOpenAIProvider(p.Name, "ollama", config.DockerLocalhost(host+"/v1"), "llama3.3"))
		return
	}
	if p.APIKey == "" {
		return
	}
	apiBase := h.resolveAPIBase(p)
	switch p.ProviderType {
	case store.ProviderChatGPTOAuth:
		ts := oauth.NewDBTokenSource(h.store, h.secretStore, p.Name).WithTenantID(p.TenantID)
		codex := providers.NewCodexProvider(p.Name, ts, apiBase, "")
		if oauthSettings := store.ParseChatGPTOAuthProviderSettings(p.Settings); oauthSettings != nil {
			codex.WithRoutingDefaults(oauthSettings.CodexPool.Strategy, oauthSettings.CodexPool.ExtraProviderNames)
		}
		h.providerReg.RegisterForTenant(p.TenantID, codex)
	case store.ProviderAnthropicNative:
		h.providerReg.RegisterForTenant(p.TenantID, providers.NewAnthropicProvider(p.APIKey,
			providers.WithAnthropicBaseURL(apiBase)))
	case store.ProviderDashScope:
		h.providerReg.RegisterForTenant(p.TenantID, providers.NewDashScopeProvider(p.Name, p.APIKey, apiBase, ""))
	case store.ProviderBailian:
		base := apiBase
		if base == "" {
			base = "https://coding-intl.dashscope.aliyuncs.com/v1"
		}
		h.providerReg.RegisterForTenant(p.TenantID, providers.NewOpenAIProvider(p.Name, p.APIKey, base, "qwen3.5-plus"))
	case store.ProviderNovita:
		base := apiBase
		if base == "" {
			base = store.NovitaDefaultAPIBase
		}
		h.providerReg.RegisterForTenant(p.TenantID, providers.NewOpenAIProvider(p.Name, p.APIKey, base, store.NovitaDefaultModel))
	default:
		prov := providers.NewOpenAIProvider(p.Name, p.APIKey, apiBase, "")
		if p.ProviderType == store.ProviderMiniMax {
			prov.WithChatPath("/text/chatcompletion_v2")
		}
		h.providerReg.RegisterForTenant(p.TenantID, prov)
	}
}

// validateProviderURL rejects provider base URLs pointing to internal/private networks.
// Defense-in-depth: prevents SSRF when providers are later used for API calls.
func validateProviderURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	// Only allow http/https schemes — block file://, gopher://, dict://, etc.
	switch u.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("provider URL must use http or https scheme, got %q", u.Scheme)
	}
	host := u.Hostname()
	// Block obvious internal targets
	blocked := []string{"localhost", "127.0.0.1", "::1", "0.0.0.0", "169.254.169.254", "metadata.google.internal"}
	for _, b := range blocked {
		if strings.EqualFold(host, b) {
			return fmt.Errorf("provider URL cannot point to %s", b)
		}
	}
	// Block private IP ranges (normalize IPv6-mapped IPv4 to catch ::ffff:127.0.0.1)
	ip := net.ParseIP(host)
	if ip != nil {
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("provider URL cannot point to private network: %s", host)
		}
	}
	// Block common internal hostnames
	if strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".local") {
		return fmt.Errorf("provider URL cannot point to internal hostname: %s", host)
	}
	return nil
}

// --- Provider CRUD ---

func (h *ProvidersHandler) handleListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := h.store.ListProviders(r.Context())
	if err != nil {
		slog.Error("providers.list", "error", err)
		locale := extractLocale(r)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "providers")})
		return
	}

	for i := range providers {
		maskAPIKey(&providers[i])
	}

	writeJSON(w, http.StatusOK, map[string]any{"providers": providers})
}

func (h *ProvidersHandler) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	var p store.LLMProviderData
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if p.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "name")})
		return
	}
	if !isValidSlug(p.Name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "name")})
		return
	}
	if !store.ValidProviderTypes[p.ProviderType] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "unsupported provider_type")})
		return
	}

	// Only one Claude CLI provider is allowed per instance (1 machine = 1 auth session).
	// Mutex serializes check+create to prevent TOCTOU race.
	if p.ProviderType == store.ProviderClaudeCLI {
		h.cliMu.Lock()
		defer h.cliMu.Unlock()

		existing, _ := h.store.ListProviders(r.Context())
		for _, ep := range existing {
			if ep.ProviderType == store.ProviderClaudeCLI {
				writeJSON(w, http.StatusConflict, map[string]string{
					"error": i18n.T(locale, i18n.MsgAlreadyExists, "Claude CLI provider", "only one is allowed per instance"),
				})
				return
			}
		}
	}

	if err := validateChatGPTOAuthProviderCandidate(r.Context(), h.store, uuid.Nil, &p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateProviderEmbeddingSettings(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, err.Error())})
		return
	}

	if err := validateProviderURL(p.APIBase); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := h.store.CreateProvider(r.Context(), &p); err != nil {
		slog.Error("providers.create", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Register in-memory so verify/chat work without restart
	h.registerInMemory(&p)
	h.emitProviderCacheInvalidate(p.Name)

	emitAudit(h.msgBus, r, "provider.created", "provider", p.ID.String())
	maskAPIKey(&p)
	writeJSON(w, http.StatusCreated, p)
}

func (h *ProvidersHandler) handleGetProvider(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "provider")})
		return
	}

	p, err := h.store.GetProvider(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "provider", id.String())})
		return
	}

	maskAPIKey(p)
	writeJSON(w, http.StatusOK, p)
}

func (h *ProvidersHandler) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "provider")})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	// Validate name if being updated
	if name, ok := updates["name"]; ok {
		if s, _ := name.(string); !isValidSlug(s) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "name")})
			return
		}
	}

	// Validate provider_type if being updated.
	// IMPORTANT: Do NOT replace this with delete(updates, "provider_type").
	// We must return 400 so the caller knows the value is invalid,
	// silently deleting it would hide the error from the end user.
	if pt, ok := updates["provider_type"]; ok {
		if s, _ := pt.(string); !store.ValidProviderTypes[s] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "unsupported provider_type")})
			return
		}
	}

	// Strip masked API key — don't overwrite real value with "***"
	if apiKey, ok := updates["api_key"]; ok {
		if s, _ := apiKey.(string); s == "***" || s == "" {
			delete(updates, "api_key")
		}
	}

	// Allowlist: only permit known provider columns.
	updates = filterAllowedKeys(updates, providerAllowedFields)

	currentProvider, err := h.store.GetProvider(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "provider", id.String())})
		return
	}

	candidate := *currentProvider
	if name, ok := updates["name"].(string); ok && name != "" {
		candidate.Name = name
	}
	if providerType, ok := updates["provider_type"].(string); ok && providerType != "" {
		candidate.ProviderType = providerType
	}
	if apiKey, ok := updates["api_key"].(string); ok {
		candidate.APIKey = apiKey
	}
	if apiBase, ok := updates["api_base"].(string); ok {
		candidate.APIBase = apiBase
	}
	if enabled, ok := updates["enabled"].(bool); ok {
		candidate.Enabled = enabled
	}
	if displayName, ok := updates["display_name"].(string); ok {
		candidate.DisplayName = displayName
	}
	if settings, ok := updates["settings"]; ok {
		rawSettings, err := marshalJSONRaw(settings)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
			return
		}
		candidate.Settings = rawSettings
	}

	if apiBase, ok := updates["api_base"]; ok {
		if s, _ := apiBase.(string); s != "" {
			if err := validateProviderURL(s); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
	}
	if baseURL, ok := updates["base_url"]; ok {
		if s, _ := baseURL.(string); s != "" {
			if err := validateProviderURL(s); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
	}

	if err := validateChatGPTOAuthProviderCandidate(r.Context(), h.store, id, &candidate); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateProviderEmbeddingSettings(&candidate); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, err.Error())})
		return
	}

	// Track old name before update for registry cleanup
	var oldName string
	if h.providerReg != nil {
		oldName = currentProvider.Name
	}

	if err := h.store.UpdateProvider(r.Context(), id, updates); err != nil {
		slog.Error("providers.update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Sync in-memory registry with updated provider
	if h.providerReg != nil {
		if updated, err := h.store.GetProvider(r.Context(), id); err == nil {
			// Unregister old name if renamed to prevent ghost entries
			if oldName != "" && oldName != updated.Name {
				h.providerReg.UnregisterForTenant(updated.TenantID, oldName)
			}
			if !updated.Enabled {
				h.providerReg.UnregisterForTenant(updated.TenantID, updated.Name)
			} else {
				h.registerInMemory(updated)
			}
		}
	}

	// Notify subscribers (e.g. ACP re-registration) about the change
	if updated, err := h.store.GetProvider(r.Context(), id); err == nil {
		h.emitProviderCacheInvalidate(updated.Name)
		if oldName != "" && oldName != updated.Name {
			h.emitProviderCacheInvalidate(oldName)
		}
	}

	emitAudit(h.msgBus, r, "provider.updated", "provider", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *ProvidersHandler) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "provider")})
		return
	}

	// Read provider before deleting so we can unregister it
	var providerName string
	var providerTenantID uuid.UUID
	if p, err := h.store.GetProvider(r.Context(), id); err == nil {
		providerName = p.Name
		providerTenantID = p.TenantID
	}

	if err := h.store.DeleteProvider(r.Context(), id); err != nil {
		slog.Error("providers.delete", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.providerReg != nil && providerName != "" {
		h.providerReg.UnregisterForTenant(providerTenantID, providerName)
	}
	if providerName != "" {
		h.emitProviderCacheInvalidate(providerName)
	}

	emitAudit(h.msgBus, r, "provider.deleted", "provider", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
