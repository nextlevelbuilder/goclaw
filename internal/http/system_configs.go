package http

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SystemConfigsHandler handles system config CRUD endpoints.
type SystemConfigsHandler struct {
	store  store.SystemConfigStore
	msgBus *bus.MessageBus
}

func NewSystemConfigsHandler(s store.SystemConfigStore, msgBus *bus.MessageBus) *SystemConfigsHandler {
	return &SystemConfigsHandler{store: s, msgBus: msgBus}
}

// validKeyRe allows alphanumeric, dots, underscores, hyphens (1-100 chars).
var validKeyRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,100}$`)

// RegisterRoutes registers system config endpoints on the given mux.
func (h *SystemConfigsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/system-configs", requireAuth("", h.handleList))
	mux.HandleFunc("GET /v1/system-configs/{key}", requireAuth("", h.handleGet))
	mux.HandleFunc("PUT /v1/system-configs/{key}", requireAuth("admin", h.handleSet))
	mux.HandleFunc("DELETE /v1/system-configs/{key}", requireAuth("admin", h.handleDelete))
}

func (h *SystemConfigsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	configs, err := h.store.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, configs)
}

func (h *SystemConfigsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	key := r.PathValue("key")
	val, err := h.store.Get(r.Context(), key)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "config", key)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key, "value": val})
}

func (h *SystemConfigsHandler) handleSet(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	key := r.PathValue("key")

	if !validKeyRe.MatchString(key) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid key format (alphanumeric, dots, hyphens, 1-100 chars)"})
		return
	}

	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if err := h.store.Set(r.Context(), key, req.Value); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Broadcast change with tenant context so in-memory config refreshes for the right tenant.
	// Use fresh context with tenant ID — request context may be canceled before subscriber runs.
	if h.msgBus != nil {
		freshCtx := store.WithTenantID(context.Background(), store.TenantIDFromContext(r.Context()))
		h.msgBus.Broadcast(bus.Event{
			Name:    bus.TopicSystemConfigChanged,
			Payload: freshCtx,
		})
	}

	writeJSON(w, http.StatusOK, map[string]string{"key": key, "value": req.Value})
}

func (h *SystemConfigsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if err := h.store.Delete(r.Context(), key); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.msgBus != nil {
		freshCtx := store.WithTenantID(context.Background(), store.TenantIDFromContext(r.Context()))
		h.msgBus.Broadcast(bus.Event{
			Name:    bus.TopicSystemConfigChanged,
			Payload: freshCtx,
		})
	}

	w.WriteHeader(http.StatusNoContent)
}
