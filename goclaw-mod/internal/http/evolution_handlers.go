package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// EvolutionHandler serves evolution metrics and suggestion endpoints.
type EvolutionHandler struct {
	metrics     store.EvolutionMetricsStore
	suggestions store.EvolutionSuggestionStore

	// Optional: skill creation on SuggestSkillAdd approval.
	// Nil-safe — skill creation disabled if any is nil.
	skillStore  store.SkillManageStore
	skillLoader *skills.Loader
	dataDir     string

	// Optional: agent store for applying threshold suggestions.
	agentStore store.AgentStore

	// Optional: tenant tool config for disabling tools on SuggestToolOrder approval.
	toolTenantCfgs store.BuiltinToolTenantConfigStore
}

// EvolutionHandlerOpt configures optional EvolutionHandler dependencies.
type EvolutionHandlerOpt func(*EvolutionHandler)

// WithSkillCreation enables skill creation when approving skill_add suggestions.
func WithSkillCreation(ss store.SkillManageStore, loader *skills.Loader, dataDir string) EvolutionHandlerOpt {
	return func(h *EvolutionHandler) {
		h.skillStore = ss
		h.skillLoader = loader
		h.dataDir = dataDir
	}
}

// WithAgentStore enables threshold suggestion auto-apply on approval.
func WithAgentStore(as store.AgentStore) EvolutionHandlerOpt {
	return func(h *EvolutionHandler) { h.agentStore = as }
}

// WithToolTenantCfgs enables tool disabling on SuggestToolOrder approval.
func WithToolTenantCfgs(tc store.BuiltinToolTenantConfigStore) EvolutionHandlerOpt {
	return func(h *EvolutionHandler) { h.toolTenantCfgs = tc }
}

func NewEvolutionHandler(m store.EvolutionMetricsStore, s store.EvolutionSuggestionStore, opts ...EvolutionHandlerOpt) *EvolutionHandler {
	h := &EvolutionHandler{metrics: m, suggestions: s}
	for _, opt := range opts {
		opt(h)
	}
	return h
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
		SkillDraft string `json:"skill_draft,omitempty"` // override draft content for skill_add approval
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

	// Handle approval: dispatch by suggestion type.
	if body.Status == "approved" {
		switch existing.SuggestionType {
		case store.SuggestSkillAdd:
			if err := h.applySkillDraft(r.Context(), *existing, body.SkillDraft, reviewedBy); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "action": "skill_created"})
			return

		case store.SuggestToolOrder:
			action := "tool_order_approved"
			if h.toolTenantCfgs != nil {
				// Extract tool name from suggestion parameters.
				var params map[string]any
				if err := json.Unmarshal(existing.Parameters, &params); err == nil {
					if toolName, _ := params["tool"].(string); toolName != "" {
						// Disable tool at tenant level using existing infrastructure.
						if err := h.toolTenantCfgs.Set(r.Context(), existing.TenantID, toolName, false); err != nil {
							slog.Warn("evolution.tool_order.disable_failed", "tool", toolName, "error", err)
						} else {
							action = "tool_disabled"
						}
					}
				}
			}
			if err := h.suggestions.UpdateSuggestionStatus(r.Context(), suggestionID, "applied", reviewedBy); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "action": action})
			return

		case store.SuggestThreshold:
			if h.agentStore == nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "threshold auto-apply not available"})
				return
			}
			// Count recent retrieval data points for guardrail check.
			since := time.Now().AddDate(0, 0, -7)
			recentMetrics, err := h.metrics.QueryMetrics(r.Context(), agentID, store.MetricRetrieval, since, 500)
			if err != nil {
				slog.Warn("evolution.query_metrics_for_guardrail failed", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to query metrics for guardrail check"})
				return
			}
			guardrails := agent.DefaultGuardrails()
			if err := agent.CheckGuardrails(guardrails, *existing, len(recentMetrics)); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if err := agent.ApplySuggestion(r.Context(), h.agentStore, h.suggestions, *existing, guardrails); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "action": "threshold_applied"})
			return
		}
		// Other types: fall through to status-only update.
	}

	if err := h.suggestions.UpdateSuggestionStatus(r.Context(), suggestionID, body.Status, reviewedBy); err != nil {
		slog.Warn("evolution.update_suggestion failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
