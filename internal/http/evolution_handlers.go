package http

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// EvolutionHandler serves evolution metrics and suggestion endpoints.
type EvolutionHandler struct {
	metrics     store.EvolutionMetricsStore
	suggestions store.EvolutionSuggestionStore
}

func NewEvolutionHandler(m store.EvolutionMetricsStore, s store.EvolutionSuggestionStore) *EvolutionHandler {
	return &EvolutionHandler{metrics: m, suggestions: s}
}

func (h *EvolutionHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agents/{agentID}/evolution/metrics", h.auth(h.handleGetMetrics))
	mux.HandleFunc("GET /v1/agents/{agentID}/evolution/suggestions", h.auth(h.handleListSuggestions))
	mux.HandleFunc("PATCH /v1/agents/{agentID}/evolution/suggestions/{suggestionID}", h.auth(h.handleUpdateSuggestion))
}

func (h *EvolutionHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

// handleGetMetrics returns raw or aggregated evolution metrics for an agent.
// Query params: type (tool|retrieval|feedback), since (ISO timestamp), aggregate (true/false).
func (h *EvolutionHandler) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(r.PathValue("agentID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid agent ID"})
		return
	}

	metricType := store.MetricType(r.URL.Query().Get("type"))
	aggregate := r.URL.Query().Get("aggregate") == "true"

	since := time.Now().AddDate(0, 0, -7) // default 7 days
	if s := r.URL.Query().Get("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			since = t
		}
	}

	ctx := r.Context()

	// Aggregated response: tool + retrieval aggregates combined.
	if aggregate {
		toolAggs, err := h.metrics.AggregateToolMetrics(ctx, agentID, since)
		if err != nil {
			slog.Warn("evolution.aggregate_tool failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		retrievalAggs, err := h.metrics.AggregateRetrievalMetrics(ctx, agentID, since)
		if err != nil {
			slog.Warn("evolution.aggregate_retrieval failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if toolAggs == nil {
			toolAggs = []store.ToolAggregate{}
		}
		if retrievalAggs == nil {
			retrievalAggs = []store.RetrievalAggregate{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"tool_aggregates":      toolAggs,
			"retrieval_aggregates": retrievalAggs,
		})
		return
	}

	// Raw metrics query.
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	metrics, err := h.metrics.QueryMetrics(ctx, agentID, metricType, since, limit)
	if err != nil {
		slog.Warn("evolution.query_metrics failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if metrics == nil {
		metrics = []store.EvolutionMetric{}
	}
	writeJSON(w, http.StatusOK, metrics)
}

// handleListSuggestions returns evolution suggestions for an agent.
// Query params: status (pending|approved|applied|rejected|rolled_back), limit.
func (h *EvolutionHandler) handleListSuggestions(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(r.PathValue("agentID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid agent ID"})
		return
	}

	status := r.URL.Query().Get("status")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	suggestions, err := h.suggestions.ListSuggestions(r.Context(), agentID, status, limit)
	if err != nil {
		slog.Warn("evolution.list_suggestions failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if suggestions == nil {
		suggestions = []store.EvolutionSuggestion{}
	}
	writeJSON(w, http.StatusOK, suggestions)
}

// handleUpdateSuggestion updates a suggestion's status (approve/reject/rollback).
func (h *EvolutionHandler) handleUpdateSuggestion(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID, err := uuid.Parse(r.PathValue("agentID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid agent ID"})
		return
	}

	suggestionID, err := uuid.Parse(r.PathValue("suggestionID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid suggestion ID"})
		return
	}

	// Verify suggestion belongs to the agent in the URL path.
	existing, err := h.suggestions.GetSuggestion(r.Context(), suggestionID)
	if err != nil || existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "suggestion not found"})
		return
	}
	if existing.AgentID != agentID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "suggestion does not belong to this agent"})
		return
	}

	var body struct {
		Status     string `json:"status"`
		ReviewedBy string `json:"reviewed_by"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}

	// Validate status transition.
	switch body.Status {
	case "approved", "rejected", "rolled_back":
		// valid
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be approved, rejected, or rolled_back"})
		return
	}

	// Use auth context user if reviewed_by not provided.
	reviewedBy := body.ReviewedBy
	if reviewedBy == "" {
		reviewedBy = store.UserIDFromContext(r.Context())
	}

	if err := h.suggestions.UpdateSuggestionStatus(r.Context(), suggestionID, body.Status, reviewedBy); err != nil {
		slog.Warn("evolution.update_suggestion failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
