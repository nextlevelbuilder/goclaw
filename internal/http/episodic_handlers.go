package http

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// EpisodicHandler serves episodic memory summary endpoints.
type EpisodicHandler struct {
	store store.EpisodicStore
}

func NewEpisodicHandler(s store.EpisodicStore) *EpisodicHandler {
	return &EpisodicHandler{store: s}
}

func (h *EpisodicHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agents/{agentID}/episodic", h.auth(h.handleList))
	mux.HandleFunc("POST /v1/agents/{agentID}/episodic/search", h.auth(h.handleSearch))
}

func (h *EpisodicHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

// handleList returns episodic summaries for an agent, optionally filtered by user.
func (h *EpisodicHandler) handleList(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	userID := r.URL.Query().Get("user_id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 500 {
		limit = 500
	}

	summaries, err := h.store.List(r.Context(), agentID, userID, limit, offset)
	if err != nil {
		slog.Warn("episodic.list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if summaries == nil {
		summaries = []store.EpisodicSummary{}
	}
	writeJSON(w, http.StatusOK, summaries)
}

// handleSearch runs hybrid search on episodic summaries.
func (h *EpisodicHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID := r.PathValue("agentID")

	var body struct {
		Query      string  `json:"query"`
		UserID     string  `json:"user_id"`
		MaxResults int     `json:"max_results"`
		MinScore   float64 `json:"min_score"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if body.Query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	if body.MaxResults <= 0 {
		body.MaxResults = 10
	}

	results, err := h.store.Search(r.Context(), body.Query, agentID, body.UserID, store.EpisodicSearchOptions{
		MaxResults: body.MaxResults,
		MinScore:   body.MinScore,
	})
	if err != nil {
		slog.Warn("episodic.search failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if results == nil {
		results = []store.EpisodicSearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}
