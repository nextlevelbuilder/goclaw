package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
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
				locale := extractLocale(r)
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": i18n.T(locale, i18n.MsgUnauthorized)})
				return
			}
		}
		userID := extractUserID(r)
		ctx := store.WithLocale(r.Context(), extractLocale(r))
		if userID != "" {
			ctx = store.WithUserID(ctx, userID)
		}
		r = r.WithContext(ctx)
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
	locale := store.LocaleFromContext(r.Context())
	opts := store.CustomToolListOpts{
		Limit:  50,
		Offset: 0,
	}

	if v := r.URL.Query().Get("agent_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "tools")})
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
	locale := store.LocaleFromContext(r.Context())
	var def store.CustomToolDef
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&def); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if def.Name == "" || def.Command == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "name and command")})
		return
	}
	if !isValidSlug(def.Name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "name")})
		return
	}

	// Check name collision with built-in/MCP tools
	if h.toolsReg != nil {
		if _, exists := h.toolsReg.Get(def.Name); exists {
			writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.T(locale, i18n.MsgAlreadyExists, "tool name", def.Name)})
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

	emitAudit(h.msgBus, r, "custom_tool.created", "custom_tool", def.ID.String())
	h.emitCacheInvalidate(def.ID.String())
	writeJSON(w, http.StatusCreated, def)
}

func (h *CustomToolsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tool")})
		return
	}

	def, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "tool", id.String())})
		return
	}

	writeJSON(w, http.StatusOK, def)
}

func (h *CustomToolsHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tool")})
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if name, ok := updates["name"]; ok {
		if s, _ := name.(string); !isValidSlug(s) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "name")})
			return
		}
	}

	if err := h.store.Update(r.Context(), id, updates); err != nil {
		slog.Error("custom_tools.update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	emitAudit(h.msgBus, r, "custom_tool.updated", "custom_tool", id.String())
	h.emitCacheInvalidate(id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *CustomToolsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tool")})
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		slog.Error("custom_tools.delete", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	emitAudit(h.msgBus, r, "custom_tool.deleted", "custom_tool", id.String())
	h.emitCacheInvalidate(id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
