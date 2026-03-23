package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// BuiltinToolsHandler handles built-in tool management endpoints.
// Built-in tools are seeded at startup; only enabled and settings are editable.
type BuiltinToolsHandler struct {
	store          store.BuiltinToolStore
	tenantCfgStore store.BuiltinToolTenantConfigStore
	token          string
	msgBus         *bus.MessageBus
}

// NewBuiltinToolsHandler creates a handler for built-in tool management endpoints.
func NewBuiltinToolsHandler(s store.BuiltinToolStore, tenantCfgs store.BuiltinToolTenantConfigStore, token string, msgBus *bus.MessageBus) *BuiltinToolsHandler {
	return &BuiltinToolsHandler{store: s, tenantCfgStore: tenantCfgs, token: token, msgBus: msgBus}
}

// RegisterRoutes registers all built-in tool routes on the given mux.
func (h *BuiltinToolsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/tools/builtin", h.auth(h.handleList))
	mux.HandleFunc("GET /v1/tools/builtin/{name}", h.auth(h.handleGet))
	mux.HandleFunc("PUT /v1/tools/builtin/{name}", h.auth(h.handleUpdate))
	mux.HandleFunc("PUT /v1/tools/builtin/{name}/tenant-config", h.auth(h.handleSetTenantConfig))
	mux.HandleFunc("DELETE /v1/tools/builtin/{name}/tenant-config", h.auth(h.handleDeleteTenantConfig))
}

func (h *BuiltinToolsHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(h.token, "", next)
}

func (h *BuiltinToolsHandler) emitCacheInvalidate(key string) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindBuiltinTools, Key: key},
	})
}

func (h *BuiltinToolsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	result, err := h.store.List(r.Context())
	if err != nil {
		slog.Error("builtin_tools.list", "error", err)
		locale := extractLocale(r)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "tools")})
		return
	}

	// Merge per-tenant overrides into response when tenant-scoped
	tid := store.TenantIDFromContext(r.Context())
	if tid != uuid.Nil && h.tenantCfgStore != nil {
		overrides, err := h.tenantCfgStore.ListAll(r.Context(), tid)
		if err == nil && len(overrides) > 0 {
			type toolWithTenant struct {
				store.BuiltinToolDef
				TenantEnabled *bool `json:"tenant_enabled"`
			}
			enriched := make([]toolWithTenant, len(result))
			for i, t := range result {
				enriched[i] = toolWithTenant{BuiltinToolDef: t}
				if enabled, ok := overrides[t.Name]; ok {
					enriched[i].TenantEnabled = &enabled
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"tools": enriched})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"tools": result})
}

func (h *BuiltinToolsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "name")})
		return
	}

	def, err := h.store.Get(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "tool", name)})
		return
	}

	writeJSON(w, http.StatusOK, def)
}

func (h *BuiltinToolsHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "name")})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	// Only allow enabled and settings to be updated
	allowed := make(map[string]any)
	if v, ok := updates["enabled"]; ok {
		allowed["enabled"] = v
	}
	if v, ok := updates["settings"]; ok {
		allowed["settings"] = v
	}

	if len(allowed) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidUpdates)})
		return
	}

	if err := h.store.Update(r.Context(), name, allowed); err != nil {
		slog.Error("builtin_tools.update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	emitAudit(h.msgBus, r, "builtin_tool.updated", "builtin_tool", name)
	h.emitCacheInvalidate(name)
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleSetTenantConfig sets a per-tenant override for a builtin tool.
func (h *BuiltinToolsHandler) handleSetTenantConfig(w http.ResponseWriter, r *http.Request) {
	if h.tenantCfgStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "tenant config not available"})
		return
	}
	name := r.PathValue("name")
	tid := store.TenantIDFromContext(r.Context())
	if tid == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant context required"})
		return
	}

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := h.tenantCfgStore.Set(r.Context(), tid, name, body.Enabled); err != nil {
		slog.Warn("set tenant tool config failed", "tool", name, "tenant", tid, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleDeleteTenantConfig removes a per-tenant override (reverts to default).
func (h *BuiltinToolsHandler) handleDeleteTenantConfig(w http.ResponseWriter, r *http.Request) {
	if h.tenantCfgStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "tenant config not available"})
		return
	}
	name := r.PathValue("name")
	tid := store.TenantIDFromContext(r.Context())
	if tid == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant context required"})
		return
	}

	if err := h.tenantCfgStore.Delete(r.Context(), tid, name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
