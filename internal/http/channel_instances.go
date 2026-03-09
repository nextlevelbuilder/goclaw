package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ChannelInstancesHandler handles channel instance CRUD endpoints.
type ChannelInstancesHandler struct {
	store      store.ChannelInstanceStore
	agentStore store.AgentStore
	token      string
	msgBus     *bus.MessageBus
}

// NewChannelInstancesHandler creates a handler for channel instance management endpoints.
func NewChannelInstancesHandler(s store.ChannelInstanceStore, agentStore store.AgentStore, token string, msgBus *bus.MessageBus) *ChannelInstancesHandler {
	return &ChannelInstancesHandler{store: s, agentStore: agentStore, token: token, msgBus: msgBus}
}

// RegisterRoutes registers all channel instance routes on the given mux.
func (h *ChannelInstancesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/channels/instances", h.auth(h.handleList))
	mux.HandleFunc("POST /v1/channels/instances", h.auth(h.handleCreate))
	mux.HandleFunc("GET /v1/channels/instances/{id}", h.auth(h.handleGet))
	mux.HandleFunc("PUT /v1/channels/instances/{id}", h.auth(h.handleUpdate))
	mux.HandleFunc("DELETE /v1/channels/instances/{id}", h.auth(h.handleDelete))

	// Group file writers (nested under channel instances)
	if h.agentStore != nil {
		mux.HandleFunc("GET /v1/channels/instances/{id}/writers/groups", h.auth(h.handleWriterGroups))
		mux.HandleFunc("GET /v1/channels/instances/{id}/writers", h.auth(h.handleListWriters))
		mux.HandleFunc("POST /v1/channels/instances/{id}/writers", h.auth(h.handleAddWriter))
		mux.HandleFunc("DELETE /v1/channels/instances/{id}/writers/{userId}", h.auth(h.handleRemoveWriter))
	}
}

func (h *ChannelInstancesHandler) auth(next http.HandlerFunc) http.HandlerFunc {
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

func (h *ChannelInstancesHandler) emitCacheInvalidate() {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindChannelInstances},
	})
}

func (h *ChannelInstancesHandler) handleList(w http.ResponseWriter, r *http.Request) {
	opts := store.ChannelInstanceListOpts{
		Limit:  50,
		Offset: 0,
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

	instances, err := h.store.ListPaged(r.Context(), opts)
	if err != nil {
		slog.Error("channel_instances.list", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list instances"})
		return
	}

	total, _ := h.store.CountInstances(r.Context(), opts)

	result := make([]map[string]interface{}, 0, len(instances))
	for _, inst := range instances {
		result = append(result, maskInstanceHTTP(inst))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"instances": result,
		"total":     total,
		"limit":     opts.Limit,
		"offset":    opts.Offset,
	})
}

func (h *ChannelInstancesHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string          `json:"name"`
		DisplayName string          `json:"display_name"`
		ChannelType string          `json:"channel_type"`
		AgentID     string          `json:"agent_id"`
		Credentials json.RawMessage `json:"credentials"`
		Config      json.RawMessage `json:"config"`
		Enabled     *bool           `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if body.Name == "" || body.ChannelType == "" || body.AgentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name, channel_type, and agent_id are required"})
		return
	}

	if !isValidChannelType(body.ChannelType) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid channel_type"})
		return
	}

	agentID, err := uuid.Parse(body.AgentID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid agent_id"})
		return
	}

	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	userID := store.UserIDFromContext(r.Context())

	inst := &store.ChannelInstanceData{
		Name:        body.Name,
		DisplayName: body.DisplayName,
		ChannelType: body.ChannelType,
		AgentID:     agentID,
		Credentials: body.Credentials,
		Config:      body.Config,
		Enabled:     enabled,
		CreatedBy:   userID,
	}

	if err := h.store.Create(r.Context(), inst); err != nil {
		slog.Error("channel_instances.create", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.emitCacheInvalidate()
	writeJSON(w, http.StatusCreated, maskInstanceHTTP(*inst))
}

func (h *ChannelInstancesHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid instance ID"})
		return
	}

	inst, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "instance not found"})
		return
	}

	writeJSON(w, http.StatusOK, maskInstanceHTTP(*inst))
}

func (h *ChannelInstancesHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid instance ID"})
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if err := h.store.Update(r.Context(), id, updates); err != nil {
		slog.Error("channel_instances.update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.emitCacheInvalidate()
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *ChannelInstancesHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid instance ID"})
		return
	}

	// Look up instance to check if it's a default (seeded) instance.
	inst, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "instance not found"})
		return
	}
	if store.IsDefaultChannelInstance(inst.Name) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cannot delete default channel instance"})
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		slog.Error("channel_instances.delete", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.emitCacheInvalidate()
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// maskInstanceHTTP returns a map with credentials masked for HTTP responses.
func maskInstanceHTTP(inst store.ChannelInstanceData) map[string]interface{} {
	result := map[string]interface{}{
		"id":           inst.ID,
		"name":         inst.Name,
		"display_name": inst.DisplayName,
		"channel_type": inst.ChannelType,
		"agent_id":     inst.AgentID,
		"config":       inst.Config,
		"enabled":      inst.Enabled,
		"is_default":       store.IsDefaultChannelInstance(inst.Name),
		"has_credentials":  len(inst.Credentials) > 0,
		"created_by":       inst.CreatedBy,
		"created_at":       inst.CreatedAt,
		"updated_at":       inst.UpdatedAt,
	}

	if len(inst.Credentials) > 0 {
		var raw map[string]interface{}
		if json.Unmarshal(inst.Credentials, &raw) == nil {
			masked := make(map[string]interface{}, len(raw))
			for k := range raw {
				masked[k] = "***"
			}
			result["credentials"] = masked
		} else {
			result["credentials"] = map[string]string{}
		}
	} else {
		result["credentials"] = map[string]string{}
	}

	return result
}

// --- Group file writers ---

// resolveAgentID looks up the channel instance and returns its agent_id.
func (h *ChannelInstancesHandler) resolveAgentID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	instID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid instance ID"})
		return uuid.Nil, false
	}
	inst, err := h.store.Get(r.Context(), instID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "instance not found"})
		return uuid.Nil, false
	}
	return inst.AgentID, true
}

func (h *ChannelInstancesHandler) handleWriterGroups(w http.ResponseWriter, r *http.Request) {
	agentID, ok := h.resolveAgentID(w, r)
	if !ok {
		return
	}
	groups, err := h.agentStore.ListGroupFileWriterGroups(r.Context(), agentID)
	if err != nil {
		slog.Error("channel_instances.writer_groups", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list writer groups"})
		return
	}
	if groups == nil {
		groups = []store.GroupWriterGroupInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"groups": groups})
}

func (h *ChannelInstancesHandler) handleListWriters(w http.ResponseWriter, r *http.Request) {
	agentID, ok := h.resolveAgentID(w, r)
	if !ok {
		return
	}
	groupID := r.URL.Query().Get("group_id")
	if groupID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "group_id query parameter is required"})
		return
	}
	writers, err := h.agentStore.ListGroupFileWriters(r.Context(), agentID, groupID)
	if err != nil {
		slog.Error("channel_instances.list_writers", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list writers"})
		return
	}
	if writers == nil {
		writers = []store.GroupFileWriterData{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"writers": writers})
}

func (h *ChannelInstancesHandler) handleAddWriter(w http.ResponseWriter, r *http.Request) {
	agentID, ok := h.resolveAgentID(w, r)
	if !ok {
		return
	}
	var body struct {
		GroupID     string `json:"group_id"`
		UserID      string `json:"user_id"`
		DisplayName string `json:"display_name"`
		Username    string `json:"username"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if body.GroupID == "" || body.UserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "group_id and user_id are required"})
		return
	}
	if err := h.agentStore.AddGroupFileWriter(r.Context(), agentID, body.GroupID, body.UserID, body.DisplayName, body.Username); err != nil {
		slog.Error("channel_instances.add_writer", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to add writer"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

func (h *ChannelInstancesHandler) handleRemoveWriter(w http.ResponseWriter, r *http.Request) {
	agentID, ok := h.resolveAgentID(w, r)
	if !ok {
		return
	}
	userID := r.PathValue("userId")
	groupID := r.URL.Query().Get("group_id")
	if groupID == "" || userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "group_id query parameter and userId path parameter are required"})
		return
	}
	if err := h.agentStore.RemoveGroupFileWriter(r.Context(), agentID, groupID, userID); err != nil {
		slog.Error("channel_instances.remove_writer", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to remove writer"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// isValidChannelType checks if the channel type is supported.
func isValidChannelType(ct string) bool {
	switch ct {
	case "telegram", "discord", "slack", "whatsapp", "zalo_oa", "zalo_personal", "feishu":
		return true
	}
	return false
}
