package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// BuiltinToolsHandler handles built-in tool management endpoints.
// Built-in tools are seeded at startup; only enabled and settings are editable.
type BuiltinToolsHandler struct {
	store  store.BuiltinToolStore
	token  string
	msgBus *bus.MessageBus
}

// NewBuiltinToolsHandler creates a handler for built-in tool management endpoints.
func NewBuiltinToolsHandler(s store.BuiltinToolStore, token string, msgBus *bus.MessageBus) *BuiltinToolsHandler {
	return &BuiltinToolsHandler{store: s, token: token, msgBus: msgBus}
}

// RegisterRoutes registers all built-in tool routes on the given mux.
func (h *BuiltinToolsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/tools/builtin", h.auth(h.handleList))
	mux.HandleFunc("GET /v1/tools/builtin/{name}", h.auth(h.handleGet))
	mux.HandleFunc("PUT /v1/tools/builtin/{name}", h.auth(h.handleUpdate))
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
