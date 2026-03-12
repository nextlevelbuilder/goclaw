package http

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// UsageHandler serves pre-computed usage analytics from snapshots.
type UsageHandler struct {
	snapshots store.SnapshotStore
	db        *sql.DB
	token     string
}

func NewUsageHandler(snapshots store.SnapshotStore, db *sql.DB, token string) *UsageHandler {
	return &UsageHandler{snapshots: snapshots, db: db, token: token}
}

func (h *UsageHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/usage/timeseries", h.authMiddleware(h.handleTimeSeries))
	mux.HandleFunc("GET /v1/usage/breakdown", h.authMiddleware(h.handleBreakdown))
	mux.HandleFunc("GET /v1/usage/summary", h.authMiddleware(h.handleSummary))
}

func (h *UsageHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" {
			if extractBearerToken(r) != h.token {
				locale := extractLocale(r)
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": i18n.T(locale, i18n.MsgUnauthorized)})
				return
			}
		}
		next(w, r)
	}
}

func (h *UsageHandler) handleTimeSeries(w http.ResponseWriter, r *http.Request) {
	q := parseSnapshotFilters(r)
	if q.From.IsZero() || q.To.IsZero() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from and to are required"})
		return
	}
	if q.GroupBy == "" {
		q.GroupBy = "hour"
	}

	points, err := h.snapshots.GetTimeSeries(r.Context(), q)
	if err != nil {
		slog.Error("usage.timeseries query failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	// Gap-fill: if "to" extends into current incomplete hour, query live traces
	now := time.Now().UTC()
	currentHourStart := now.Truncate(time.Hour)
	if q.To.After(currentHourStart) && currentHourStart.After(q.From) {
		livePoint := h.queryLiveHour(r, currentHourStart, now, q)
		if livePoint != nil {
			points = append(points, *livePoint)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"points": points})
}

func (h *UsageHandler) handleBreakdown(w http.ResponseWriter, r *http.Request) {
	q := parseSnapshotFilters(r)
	if q.From.IsZero() || q.To.IsZero() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from and to are required"})
		return
	}
	if q.GroupBy == "" {
		q.GroupBy = "provider"
	}

	rows, err := h.snapshots.GetBreakdown(r.Context(), q)
	if err != nil {
		slog.Error("usage.breakdown query failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}

func (h *UsageHandler) handleSummary(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "24h"
	}

	now := time.Now().UTC()
	var currentFrom, previousFrom time.Time

	switch period {
	case "today":
		currentFrom = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		previousFrom = currentFrom.AddDate(0, 0, -1)
	case "7d":
		currentFrom = now.Add(-7 * 24 * time.Hour)
		previousFrom = currentFrom.Add(-7 * 24 * time.Hour)
	case "30d":
		currentFrom = now.Add(-30 * 24 * time.Hour)
		previousFrom = currentFrom.Add(-30 * 24 * time.Hour)
	default: // "24h"
		currentFrom = now.Add(-24 * time.Hour)
		previousFrom = currentFrom.Add(-24 * time.Hour)
	}

	baseQ := parseSnapshotFilters(r)

	// Current period
	currentQ := baseQ
	currentQ.From = currentFrom
	currentQ.To = now
	currentQ.GroupBy = "hour"

	// Previous period (same duration, shifted back)
	previousQ := baseQ
	previousQ.From = previousFrom
	previousQ.To = currentFrom
	previousQ.GroupBy = "hour"

	currentSummary := h.aggregateTimeSeries(r, currentQ)
	previousSummary := h.aggregateTimeSeries(r, previousQ)

	writeJSON(w, http.StatusOK, map[string]any{
		"current":  currentSummary,
		"previous": previousSummary,
	})
}

// usageSummary is the response shape for summary endpoint.
type usageSummary struct {
	Requests      int     `json:"requests"`
	InputTokens   int64   `json:"input_tokens"`
	OutputTokens  int64   `json:"output_tokens"`
	Cost          float64 `json:"cost"`
	UniqueUsers   int     `json:"unique_users"`
	Errors        int     `json:"errors"`
	LLMCalls      int     `json:"llm_calls"`
	ToolCalls     int     `json:"tool_calls"`
	AvgDurationMS int     `json:"avg_duration_ms"`
}

func (h *UsageHandler) aggregateTimeSeries(r *http.Request, q store.SnapshotQuery) usageSummary {
	points, err := h.snapshots.GetTimeSeries(r.Context(), q)
	if err != nil {
		return usageSummary{}
	}

	var s usageSummary
	var totalWeightedDuration int64
	for _, p := range points {
		s.Requests += p.RequestCount
		s.InputTokens += p.InputTokens
		s.OutputTokens += p.OutputTokens
		s.Cost += p.TotalCost
		s.UniqueUsers += p.UniqueUsers
		s.Errors += p.ErrorCount
		s.LLMCalls += p.LLMCallCount
		s.ToolCalls += p.ToolCallCount
		totalWeightedDuration += int64(p.AvgDurationMS) * int64(p.RequestCount)
	}
	if s.Requests > 0 {
		s.AvgDurationMS = int(totalWeightedDuration / int64(s.Requests))
	}
	return s
}

// queryLiveHour runs a lightweight query on traces for the current incomplete hour.
func (h *UsageHandler) queryLiveHour(r *http.Request, from, to time.Time, q store.SnapshotQuery) *store.SnapshotTimeSeries {
	query := `SELECT
		COUNT(*),
		COUNT(*) FILTER (WHERE status = 'error'),
		COUNT(DISTINCT user_id),
		COALESCE(SUM(total_input_tokens), 0),
		COALESCE(SUM(total_output_tokens), 0),
		COALESCE(SUM(total_cost), 0),
		COALESCE(SUM(llm_call_count), 0),
		COALESCE(SUM(tool_call_count), 0),
		COALESCE(AVG(duration_ms), 0)::INTEGER
	FROM traces
	WHERE start_time >= $1 AND start_time < $2
	  AND parent_trace_id IS NULL`

	args := []any{from, to}
	idx := 3
	if q.AgentID != nil {
		query += fmt.Sprintf(" AND agent_id = $%d", idx)
		args = append(args, *q.AgentID)
		idx++
	}
	if q.Channel != "" {
		query += fmt.Sprintf(" AND channel = $%d", idx)
		args = append(args, q.Channel)
		idx++
	}
	// Note: provider/model filters are not applied here because the traces table
	// does not have provider/model columns (those live on spans). The live gap-fill
	// is a rough approximation for the current incomplete hour.

	var p store.SnapshotTimeSeries
	p.BucketTime = from
	err := h.db.QueryRowContext(r.Context(), query, args...).Scan(
		&p.RequestCount, &p.ErrorCount, &p.UniqueUsers,
		&p.InputTokens, &p.OutputTokens, &p.TotalCost,
		&p.LLMCallCount, &p.ToolCallCount, &p.AvgDurationMS,
	)
	if err != nil || p.RequestCount == 0 {
		return nil
	}
	return &p
}

func parseSnapshotFilters(r *http.Request) store.SnapshotQuery {
	q := store.SnapshotQuery{}
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.From = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			q.To = t
		}
	}
	if v := r.URL.Query().Get("agent_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			q.AgentID = &id
		}
	}
	q.Provider = r.URL.Query().Get("provider")
	q.Model = r.URL.Query().Get("model")
	q.Channel = r.URL.Query().Get("channel")
	q.GroupBy = r.URL.Query().Get("group_by")
	return q
}

