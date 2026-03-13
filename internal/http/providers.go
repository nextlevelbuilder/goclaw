package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/oauth"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ProvidersHandler handles LLM provider CRUD endpoints.
type ProvidersHandler struct {
	store          store.ProviderStore
	secretStore    store.ConfigSecretsStore
	token          string
	providerReg    *providers.Registry
	gatewayAddr    string                    // for injecting MCP bridge into Claude CLI providers
	mcpLookup      providers.MCPServerLookup // optional: resolves per-agent MCP servers
	cliMu          sync.Mutex                // serializes Claude CLI provider create to prevent duplicates
	msgBus         *bus.MessageBus
}

// NewProvidersHandler creates a handler for provider management endpoints.
func NewProvidersHandler(s store.ProviderStore, secretStore store.ConfigSecretsStore, token string, providerReg *providers.Registry, gatewayAddr string) *ProvidersHandler {
	return &ProvidersHandler{store: s, secretStore: secretStore, token: token, providerReg: providerReg, gatewayAddr: gatewayAddr}
}

// SetMessageBus sets the message bus for audit event broadcasting.
// Must be called before serving requests (not thread-safe).
func (h *ProvidersHandler) SetMessageBus(msgBus *bus.MessageBus) {
	h.msgBus = msgBus
}

// SetMCPServerLookup sets the per-agent MCP server lookup for Claude CLI providers.
// Must be called before serving requests (not thread-safe).
func (h *ProvidersHandler) SetMCPServerLookup(lookup providers.MCPServerLookup) {
	h.mcpLookup = lookup
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

	// Claude CLI auth status (global — not per-provider)
	mux.HandleFunc("GET /v1/providers/claude-cli/auth-status", h.auth(h.handleClaudeCLIAuthStatus))
}

func (h *ProvidersHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" {
			if extractBearerToken(r) != h.token {
				locale := extractLocale(r)
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": i18n.T(locale, i18n.MsgUnauthorized)})
				return
			}
		}
		next(w, r)
	}
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
	// Claude CLI doesn't need an API key — register immediately
	if p.ProviderType == store.ProviderClaudeCLI {
		cliPath := p.APIBase // reuse APIBase field for CLI path
		if cliPath == "" {
			cliPath = "claude"
		}
		var cliOpts []providers.ClaudeCLIOption
		cliOpts = append(cliOpts, providers.WithClaudeCLISecurityHooks("", true))
		if h.gatewayAddr != "" {
			mcpData := providers.BuildCLIMCPConfigData(nil, h.gatewayAddr, h.token)
			mcpData.AgentMCPLookup = h.mcpLookup
			cliOpts = append(cliOpts, providers.WithClaudeCLIMCPConfigData(mcpData))
		}
		h.providerReg.Register(providers.NewClaudeCLIProvider(cliPath, cliOpts...))
		return
	}
	if p.APIKey == "" {
		return
	}
	switch p.ProviderType {
	case store.ProviderChatGPTOAuth:
		ts := oauth.NewDBTokenSource(h.store, h.secretStore, p.Name)
		h.providerReg.Register(providers.NewCodexProvider(p.Name, ts, p.APIBase, ""))
	case store.ProviderAnthropicNative:
		h.providerReg.Register(providers.NewAnthropicProvider(p.APIKey,
			providers.WithAnthropicBaseURL(p.APIBase)))
	case store.ProviderDashScope:
		h.providerReg.Register(providers.NewDashScopeProvider(p.APIKey, p.APIBase, ""))
	case store.ProviderBailian:
		base := p.APIBase
		if base == "" {
			base = "https://coding-intl.dashscope.aliyuncs.com/v1"
		}
		h.providerReg.Register(providers.NewOpenAIProvider(p.Name, p.APIKey, base, "qwen3.5-plus"))
	default:
		prov := providers.NewOpenAIProvider(p.Name, p.APIKey, p.APIBase, "")
		if p.ProviderType == store.ProviderMiniMax {
			prov.WithChatPath("/text/chatcompletion_v2")
		}
		h.providerReg.Register(prov)
	}
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

	writeJSON(w, http.StatusOK, map[string]interface{}{"providers": providers})
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

	if err := h.store.CreateProvider(r.Context(), &p); err != nil {
		slog.Error("providers.create", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Register in-memory so verify/chat work without restart
	h.registerInMemory(&p)

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

	var updates map[string]interface{}
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

	// Prevent updating immutable fields
	delete(updates, "id")
	delete(updates, "created_at")

	// Track old name before update for registry cleanup
	var oldName string
	if h.providerReg != nil {
		if old, err := h.store.GetProvider(r.Context(), id); err == nil {
			oldName = old.Name
		}
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
				h.providerReg.Unregister(oldName)
			}
			if !updated.Enabled {
				h.providerReg.Unregister(updated.Name)
			} else {
				h.registerInMemory(updated)
			}
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

	// Read provider name before deleting so we can unregister it
	var providerName string
	if p, err := h.store.GetProvider(r.Context(), id); err == nil {
		providerName = p.Name
	}

	if err := h.store.DeleteProvider(r.Context(), id); err != nil {
		slog.Error("providers.delete", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.providerReg != nil && providerName != "" {
		h.providerReg.Unregister(providerName)
	}

	emitAudit(h.msgBus, r, "provider.deleted", "provider", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
