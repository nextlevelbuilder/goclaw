package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// CustomToolsHandler handles custom tool CRUD endpoints.
type CustomToolsHandler struct {
	store    store.CustomToolStore
	token    string
	msgBus   *bus.MessageBus
	toolsReg *tools.Registry // for name collision checking on create
}

// NewCustomToolsHandler creates a handler for custom tool management endpoints.
func NewCustomToolsHandler(s store.CustomToolStore, token string, msgBus *bus.MessageBus, toolsReg *tools.Registry) *CustomToolsHandler {
	return &CustomToolsHandler{store: s, token: token, msgBus: msgBus, toolsReg: toolsReg}
}

// RegisterRoutes registers all custom tool routes on the given mux.
func (h *CustomToolsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/tools/custom", h.auth(h.handleList))
	mux.HandleFunc("POST /v1/tools/custom", h.auth(h.handleCreate))
	mux.HandleFunc("GET /v1/tools/custom/{id}", h.auth(h.handleGet))
	mux.HandleFunc("PUT /v1/tools/custom/{id}", h.auth(h.handleUpdate))
	mux.HandleFunc("DELETE /v1/tools/custom/{id}", h.auth(h.handleDelete))
}

func (h *CustomToolsHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" {
			if extractBearerToken(r) != h.token {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		userID := extractUserID(r)
		if userID != "" {
			ctx := store.WithUserID(r.Context(), userID)
			r = r.WithContext(ctx)
		}
		next(w, r)
	}
}

func (h *CustomToolsHandler) emitCacheInvalidate(key string) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindCustomTools, Key: key},
	})
}

func (h *CustomToolsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	opts := store.CustomToolListOpts{
		Limit:  50,
		Offset: 0,
	}

	if v := r.URL.Query().Get("agent_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid agent_id"})
			return
		}
		opts.AgentID = &id
	}
	if v := r.URL.Query().Get("search"); v != "" {
		opts.Search = v
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			opts.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			opts.Offset = n
		}
	}

	result, err := h.store.ListPaged(r.Context(), opts)
	if err != nil {
		slog.Error("custom_tools.list", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tools"})
		return
	}

	total, _ := h.store.CountTools(r.Context(), opts)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tools":  result,
		"total":  total,
		"limit":  opts.Limit,
		"offset": opts.Offset,
	})
}

func (h *CustomToolsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var def store.CustomToolDef
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&def); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if def.Name == "" || def.Command == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and command are required"})
		return
	}
	if !isValidSlug(def.Name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name must be a valid slug (lowercase letters, numbers, hyphens only)"})
		return
	}

	// Check name collision with built-in/MCP tools
	if h.toolsReg != nil {
		if _, exists := h.toolsReg.Get(def.Name); exists {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "tool name conflicts with existing built-in or MCP tool"})
			return
		}
	}

	userID := store.UserIDFromContext(r.Context())
	if userID != "" {
		def.CreatedBy = userID
	}

	if def.TimeoutSeconds <= 0 {
		def.TimeoutSeconds = 60
	}

	if err := h.store.Create(r.Context(), &def); err != nil {
		slog.Error("custom_tools.create", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.emitCacheInvalidate(def.ID.String())
	writeJSON(w, http.StatusCreated, def)
}

func (h *CustomToolsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tool ID"})
		return
	}

	def, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "tool not found"})
		return
	}

	writeJSON(w, http.StatusOK, def)
}

func (h *CustomToolsHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tool ID"})
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if name, ok := updates["name"]; ok {
		if s, _ := name.(string); !isValidSlug(s) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name must be a valid slug (lowercase letters, numbers, hyphens only)"})
			return
		}
	}

	if err := h.store.Update(r.Context(), id, updates); err != nil {
		slog.Error("custom_tools.update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.emitCacheInvalidate(id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *CustomToolsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tool ID"})
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		slog.Error("custom_tools.delete", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.emitCacheInvalidate(id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
