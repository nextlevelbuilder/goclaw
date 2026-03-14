package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (h *MemoryHandler) handleListAllDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := h.store.ListAllDocumentsGlobal(r.Context())
	if err != nil {
		slog.Warn("memory.list_all_documents failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if docs == nil {
		docs = []store.DocumentInfo{}
	}
	writeJSON(w, http.StatusOK, docs)
}

func (h *MemoryHandler) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	userID := r.URL.Query().Get("user_id")

	var docs []store.DocumentInfo
	var err error
	if userID == "" {
		docs, err = h.store.ListAllDocuments(r.Context(), agentID)
	} else {
		docs, err = h.store.ListDocuments(r.Context(), agentID, userID)
	}
	if err != nil {
		slog.Warn("memory.list_documents failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if docs == nil {
		docs = []store.DocumentInfo{}
	}
	writeJSON(w, http.StatusOK, docs)
}

func (h *MemoryHandler) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID := r.PathValue("agentID")
	path := r.PathValue("path")
	userID := r.URL.Query().Get("user_id")

	detail, err := h.store.GetDocumentDetail(r.Context(), agentID, userID, path)
	if err != nil {
		slog.Warn("memory.get_document failed", "error", err, "path", path)
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "document", path)})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *MemoryHandler) handlePutDocument(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID := r.PathValue("agentID")
	path := r.PathValue("path")

	var body struct {
		Content string `json:"content"`
		UserID  string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if err := h.store.PutDocument(r.Context(), agentID, body.UserID, path, body.Content); err != nil {
		slog.Warn("memory.put_document failed", "error", err, "path", path)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "path": path})
}

func (h *MemoryHandler) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	path := r.PathValue("path")
	userID := r.URL.Query().Get("user_id")

	if err := h.store.DeleteDocument(r.Context(), agentID, userID, path); err != nil {
		slog.Warn("memory.delete_document failed", "error", err, "path", path)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *MemoryHandler) handleListChunks(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID := r.PathValue("agentID")
	path := r.URL.Query().Get("path")
	userID := r.URL.Query().Get("user_id")

	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "path")})
		return
	}

	chunks, err := h.store.ListChunks(r.Context(), agentID, userID, path)
	if err != nil {
		slog.Warn("memory.list_chunks failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if chunks == nil {
		chunks = []store.ChunkInfo{}
	}
	writeJSON(w, http.StatusOK, chunks)
}

func (h *MemoryHandler) handleIndexDocument(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID := r.PathValue("agentID")

	var body struct {
		Path   string `json:"path"`
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	if body.Path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "path")})
		return
	}

	if err := h.store.IndexDocument(r.Context(), agentID, body.UserID, body.Path); err != nil {
		slog.Warn("memory.index_document failed", "error", err, "path", body.Path)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "indexed", "path": body.Path})
}

func (h *MemoryHandler) handleIndexAll(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")

	var body struct {
		UserID string `json:"user_id"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if err := h.store.IndexAll(r.Context(), agentID, body.UserID); err != nil {
		slog.Warn("memory.index_all failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "indexed_all"})
}

func (h *MemoryHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID := r.PathValue("agentID")

	var body struct {
		Query      string  `json:"query"`
		UserID     string  `json:"user_id"`
		MaxResults int     `json:"max_results"`
		MinScore   float64 `json:"min_score"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	if body.Query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "query")})
		return
	}

	results, err := h.store.Search(r.Context(), body.Query, agentID, body.UserID, store.MemorySearchOptions{
		MaxResults: body.MaxResults,
		MinScore:   body.MinScore,
	})
	if err != nil {
		slog.Warn("memory.search failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if results == nil {
		results = []store.MemorySearchResult{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results": results,
		"count":   len(results),
	})
}
