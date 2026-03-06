package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ProvidersHandler handles LLM provider CRUD endpoints.
type ProvidersHandler struct {
	store       store.ProviderStore
	token       string
	providerReg *providers.Registry
}

// NewProvidersHandler creates a handler for provider management endpoints.
func NewProvidersHandler(s store.ProviderStore, token string, providerReg *providers.Registry) *ProvidersHandler {
	return &ProvidersHandler{store: s, token: token, providerReg: providerReg}
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
}

func (h *ProvidersHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" {
			if extractBearerToken(r) != h.token {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
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
	if h.providerReg == nil || !p.Enabled || p.APIKey == "" {
		return
	}
	if p.ProviderType == store.ProviderAnthropicNative {
		h.providerReg.Register(providers.NewAnthropicProvider(p.APIKey,
			providers.WithAnthropicBaseURL(p.APIBase)))
	} else if p.ProviderType == store.ProviderDashScope {
		h.providerReg.Register(providers.NewDashScopeProvider(p.APIKey, p.APIBase, ""))
	} else if p.ProviderType == store.ProviderBailian {
		base := p.APIBase
		if base == "" {
			base = "https://coding-intl.dashscope.aliyuncs.com/v1"
		}
		h.providerReg.Register(providers.NewOpenAIProvider(p.Name, p.APIKey, base, "qwen3.5-plus"))
	} else {
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list providers"})
		return
	}

	for i := range providers {
		maskAPIKey(&providers[i])
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"providers": providers})
}

func (h *ProvidersHandler) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	var p store.LLMProviderData
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if p.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if !isValidSlug(p.Name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name must be a valid slug (lowercase letters, numbers, hyphens only)"})
		return
	}
	if !store.ValidProviderTypes[p.ProviderType] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported provider_type"})
		return
	}

	if err := h.store.CreateProvider(r.Context(), &p); err != nil {
		slog.Error("providers.create", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Register in-memory so verify/chat work without restart
	h.registerInMemory(&p)

	maskAPIKey(&p)
	writeJSON(w, http.StatusCreated, p)
}

func (h *ProvidersHandler) handleGetProvider(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid provider ID"})
		return
	}

	p, err := h.store.GetProvider(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "provider not found"})
		return
	}

	maskAPIKey(p)
	writeJSON(w, http.StatusOK, p)
}

func (h *ProvidersHandler) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid provider ID"})
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	// Validate name if being updated
	if name, ok := updates["name"]; ok {
		if s, _ := name.(string); !isValidSlug(s) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name must be a valid slug"})
			return
		}
	}

	// Validate provider_type if being updated
	if pt, ok := updates["provider_type"]; ok {
		if s, _ := pt.(string); !store.ValidProviderTypes[s] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported provider_type"})
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

	if err := h.store.UpdateProvider(r.Context(), id, updates); err != nil {
		slog.Error("providers.update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Sync in-memory registry with updated provider
	if h.providerReg != nil {
		if updated, err := h.store.GetProvider(r.Context(), id); err == nil {
			if !updated.Enabled {
				h.providerReg.Unregister(updated.Name)
			} else {
				h.registerInMemory(updated)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *ProvidersHandler) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid provider ID"})
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

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
