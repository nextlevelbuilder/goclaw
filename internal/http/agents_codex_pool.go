package http

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type codexPoolProviderCount struct {
	ProviderName string     `json:"provider_name"`
	RequestCount int        `json:"request_count"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
}

type codexPoolRecentRequest struct {
	TraceID           uuid.UUID `json:"trace_id"`
	StartedAt         time.Time `json:"started_at"`
	Status            string    `json:"status"`
	DurationMS        int       `json:"duration_ms"`
	ProviderName      string    `json:"provider_name"`
	Model             string    `json:"model"`
	PoolLLMCalls      int       `json:"pool_llm_calls"`
	FailoverProviders []string  `json:"failover_providers,omitempty"`
}

func markCodexPoolProviderUsed(countsByProvider map[string]*codexPoolProviderCount, providerName string, usedAt time.Time) {
	if providerName == "" {
		return
	}
	stat := countsByProvider[providerName]
	if stat == nil {
		stat = &codexPoolProviderCount{ProviderName: providerName}
		countsByProvider[providerName] = stat
	}
	stat.RequestCount++
	if stat.LastUsedAt == nil || usedAt.After(*stat.LastUsedAt) {
		seenAt := usedAt
		stat.LastUsedAt = &seenAt
	}
}

func (h *AgentsHandler) handleCodexPoolActivity(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	if h.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "database unavailable")})
		return
	}

	agent, statusCode, err := h.lookupAccessibleAgent(r)
	if err != nil {
		writeJSON(w, statusCode, map[string]string{"error": err.Error()})
		return
	}

	limit := 18
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}

	routing := agent.ParseChatGPTOAuthRouting()
	strategy := store.ChatGPTOAuthStrategyManual
	if routing != nil && routing.Strategy != "" {
		strategy = routing.Strategy
	}
	poolProviders := []string{agent.Provider}
	if routing != nil {
		for _, name := range routing.ExtraProviderNames {
			if name != "" && !slices.Contains(poolProviders, name) {
				poolProviders = append(poolProviders, name)
			}
		}
	}

	if len(poolProviders) == 0 || agent.Provider == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"strategy":        strategy,
			"pool_providers":  []string{},
			"provider_counts": []codexPoolProviderCount{},
			"recent_requests": []codexPoolRecentRequest{},
		})
		return
	}

	const query = `
WITH candidate_traces AS (
	SELECT id, start_time, duration_ms, status, llm_call_count
	FROM traces
	WHERE agent_id = $1
	  AND tenant_id = $2
	  AND parent_trace_id IS NULL
	  AND EXISTS (
		SELECT 1
		FROM spans sp
		WHERE sp.trace_id = traces.id
		  AND sp.tenant_id = $2
		  AND sp.span_type = 'llm_call'
		  AND sp.provider = ANY($3)
	  )
	ORDER BY start_time DESC
	LIMIT $4
),
pool_spans AS (
	SELECT
		sp.trace_id,
		sp.provider,
		sp.model,
		sp.start_time,
		ROW_NUMBER() OVER (PARTITION BY sp.trace_id ORDER BY sp.start_time ASC, sp.id ASC) AS rn
	FROM spans sp
	JOIN candidate_traces ct ON ct.id = sp.trace_id
	WHERE sp.span_type = 'llm_call'
	  AND sp.tenant_id = $2
	  AND sp.provider = ANY($3)
)
SELECT
	ct.id,
	ct.start_time,
	ct.duration_ms,
	ct.status,
	first_span.provider,
	first_span.model,
	COALESCE((
		SELECT ARRAY_AGG(provider_name ORDER BY provider_name)
		FROM (
			SELECT DISTINCT ps.provider AS provider_name
			FROM pool_spans ps
			WHERE ps.trace_id = ct.id
			  AND ps.rn > 1
			  AND ps.provider <> first_span.provider
		) dedup
	), ARRAY[]::text[]),
	COALESCE((SELECT COUNT(*) FROM pool_spans ps WHERE ps.trace_id = ct.id), 0)
FROM candidate_traces ct
JOIN LATERAL (
	SELECT ps.provider, ps.model
	FROM pool_spans ps
	WHERE ps.trace_id = ct.id AND ps.rn = 1
	LIMIT 1
) first_span ON true
ORDER BY ct.start_time DESC`

	rows, err := h.db.QueryContext(r.Context(), query, agent.ID, agent.TenantID, pq.Array(poolProviders), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	recent := make([]codexPoolRecentRequest, 0, limit)
	countsByProvider := make(map[string]*codexPoolProviderCount, len(poolProviders))
	for _, name := range poolProviders {
		countsByProvider[name] = &codexPoolProviderCount{ProviderName: name}
	}

	for rows.Next() {
		var item codexPoolRecentRequest
		var failovers pq.StringArray
		if err := rows.Scan(
			&item.TraceID,
			&item.StartedAt,
			&item.DurationMS,
			&item.Status,
			&item.ProviderName,
			&item.Model,
			&failovers,
			&item.PoolLLMCalls,
		); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		item.FailoverProviders = []string(failovers)
		recent = append(recent, item)
		markCodexPoolProviderUsed(countsByProvider, item.ProviderName, item.StartedAt)
		for _, providerName := range item.FailoverProviders {
			if providerName != item.ProviderName {
				markCodexPoolProviderUsed(countsByProvider, providerName, item.StartedAt)
			}
		}
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	providerCounts := make([]codexPoolProviderCount, 0, len(poolProviders))
	for _, name := range poolProviders {
		if stat := countsByProvider[name]; stat != nil {
			providerCounts = append(providerCounts, *stat)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"strategy":        strategy,
		"pool_providers":  poolProviders,
		"provider_counts": providerCounts,
		"recent_requests": recent,
	})
}

func (h *AgentsHandler) lookupAccessibleAgent(r *http.Request) (*store.AgentData, int, error) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	isOwner := h.isOwnerUser(userID)
	rawID := r.PathValue("id")

	var (
		agent *store.AgentData
		err   error
	)
	if parsedID, parseErr := uuid.Parse(rawID); parseErr == nil {
		agent, err = h.agents.GetByID(r.Context(), parsedID)
	} else {
		agent, err = h.agents.GetByKey(r.Context(), rawID)
	}
	if err != nil {
		return nil, http.StatusNotFound, errors.New(i18n.T(locale, i18n.MsgNotFound, "agent", rawID))
	}
	if userID != "" && !isOwner {
		if ok, _, _ := h.agents.CanAccess(r.Context(), agent.ID, userID); !ok {
			return nil, http.StatusForbidden, errors.New(i18n.T(locale, i18n.MsgNoAccess, "agent"))
		}
	}
	return agent, http.StatusOK, nil
}
