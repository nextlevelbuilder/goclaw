package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ListenRawMessagesHandler handles listen-only raw message listing endpoints.
type ListenRawMessagesHandler struct {
	store store.ListenRawMessageStore
}

// NewListenRawMessagesHandler creates a handler for raw message endpoints.
func NewListenRawMessagesHandler(s store.ListenRawMessageStore) *ListenRawMessagesHandler {
	return &ListenRawMessagesHandler{store: s}
}

// RegisterRoutes registers raw message routes on the given mux.
func (h *ListenRawMessagesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/listen-raw-messages", h.authMiddleware(h.handleList))
	mux.HandleFunc("POST /v1/listen-raw-messages/reset-processed", h.authMiddleware(h.handleResetProcessed))
	mux.HandleFunc("POST /v1/listen-raw-messages/reset", h.authMiddleware(h.handleResetByIDs))
}

func (h *ListenRawMessagesHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *ListenRawMessagesHandler) handleList(w http.ResponseWriter, r *http.Request) {
	opts := store.ListenRawMessageListOpts{
		Limit:  50,
		Offset: 0,
	}

	if v := r.URL.Query().Get("channel_name"); v != "" {
		opts.ChannelName = v
	}
	if v := r.URL.Query().Get("chat_id"); v != "" {
		opts.ChatID = v
	}
	if v := r.URL.Query().Get("agent_id"); v != "" {
		opts.AgentID = v
	}
	if v := r.URL.Query().Get("graph_id"); v != "" {
		opts.GraphID = v
	}
	if v := r.URL.Query().Get("processed"); v != "" {
		b := v == "true" || v == "1"
		opts.Processed = &b
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

	msgs, total, err := h.store.List(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if msgs == nil {
		msgs = []store.ListenRawMessage{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"messages": msgs,
		"total":    total,
		"limit":    opts.Limit,
		"offset":   opts.Offset,
	})
}

func (h *ListenRawMessagesHandler) handleResetProcessed(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	graphID := r.URL.Query().Get("graph_id")

	affected, err := h.store.ResetProcessed(r.Context(), agentID, graphID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	resp := map[string]any{
		"reset_count": affected,
	}
	if agentID != "" {
		resp["agent_id"] = agentID
	}
	if graphID != "" {
		resp["graph_id"] = graphID
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *ListenRawMessagesHandler) handleResetByIDs(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if len(body.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ids is required"})
		return
	}

	ids := make([]uuid.UUID, 0, len(body.IDs))
	for _, s := range body.IDs {
		id, err := uuid.Parse(s)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id: " + s})
			return
		}
		ids = append(ids, id)
	}

	affected, err := h.store.ResetProcessedByIDs(r.Context(), ids)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"reset_count": affected,
	})
}
