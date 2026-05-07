package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// EmbeddingsHandler handles raw message chunk (embedding) endpoints.
type EmbeddingsHandler struct {
	store store.RawMessageChunkStore
}

// NewEmbeddingsHandler creates a handler for embedding chunk endpoints.
func NewEmbeddingsHandler(s store.RawMessageChunkStore) *EmbeddingsHandler {
	return &EmbeddingsHandler{store: s}
}

// RegisterRoutes registers embedding routes on the given mux.
func (h *EmbeddingsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/embeddings", h.authMiddleware(h.handleList))
	mux.HandleFunc("POST /v1/embeddings/delete", h.authMiddleware(h.handleDelete))
	mux.HandleFunc("POST /v1/embeddings/delete-by-chat", h.authMiddleware(h.handleDeleteByChat))
	mux.HandleFunc("POST /v1/embeddings/re-embed", h.authMiddleware(h.handleReEmbed))
}

func (h *EmbeddingsHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *EmbeddingsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	opts := store.RawMessageChunkListOpts{
		Limit:  50,
		Offset: 0,
	}

	if v := r.URL.Query().Get("agent_id"); v != "" {
		opts.AgentID = v
	}
	if v := r.URL.Query().Get("chat_id"); v != "" {
		opts.ChatID = v
	}
	if v := r.URL.Query().Get("graph_id"); v != "" {
		opts.GraphID = v
	}
	if v := r.URL.Query().Get("sender"); v != "" {
		opts.Sender = v
	}
	if v := r.URL.Query().Get("has_embedding"); v != "" {
		b := v == "true" || v == "1"
		opts.HasEmbedding = &b
	}
	if v := r.URL.Query().Get("from_time"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			opts.FromTime = &t
		}
	}
	if v := r.URL.Query().Get("to_time"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			opts.ToTime = &t
		}
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

	chunks, total, err := h.store.List(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if chunks == nil {
		chunks = []store.RawMessageChunk{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"chunks": chunks,
		"total":  total,
		"limit":  opts.Limit,
		"offset": opts.Offset,
	})
}

func (h *EmbeddingsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		slog.Warn("http.embeddings.delete: invalid body", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if len(body.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ids is required"})
		return
	}

	deleted, err := h.store.DeleteByIDs(r.Context(), body.IDs)
	if err != nil {
		slog.Error("http.embeddings.delete: store error", "count", len(body.IDs), "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	slog.Info("http.embeddings.delete: ok", "requested", len(body.IDs), "deleted", deleted)
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted_count": deleted,
	})
}

func (h *EmbeddingsHandler) handleDeleteByChat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AgentID string `json:"agent_id"`
		ChatID  string `json:"chat_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		slog.Warn("http.embeddings.delete-by-chat: invalid body", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if body.AgentID == "" || body.ChatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent_id and chat_id are required"})
		return
	}

	deleted, err := h.store.DeleteByChatID(r.Context(), body.AgentID, body.ChatID)
	if err != nil {
		slog.Error("http.embeddings.delete-by-chat: store error", "agent_id", body.AgentID, "chat_id", body.ChatID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	slog.Info("http.embeddings.delete-by-chat: ok", "agent_id", body.AgentID, "chat_id", body.ChatID, "deleted", deleted)
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted_count": deleted,
	})
}

func (h *EmbeddingsHandler) handleReEmbed(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AgentID string `json:"agent_id,omitempty"`
		ChatID  string `json:"chat_id,omitempty"`
		GraphID string `json:"graph_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		slog.Warn("http.embeddings.re-embed: invalid body", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	opts := store.RawMessageChunkListOpts{}
	if body.AgentID != "" {
		opts.AgentID = body.AgentID
	}
	if body.ChatID != "" {
		opts.ChatID = body.ChatID
	}
	if body.GraphID != "" {
		opts.GraphID = body.GraphID
	}

	processed, failed, err := h.store.ReEmbedChunks(r.Context(), opts)
	if err != nil {
		slog.Error("http.embeddings.re-embed: store error", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	slog.Info("http.embeddings.re-embed: ok", "processed", processed, "failed", failed)
	writeJSON(w, http.StatusOK, map[string]any{
		"processed": processed,
		"failed":    failed,
	})
}
