package http

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strconv"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// providerCodexPoolAgentCount tracks per-agent request counts for provider-scoped activity.
type providerCodexPoolAgentCount struct {
	AgentID      uuid.UUID `json:"agent_id"`
	AgentKey     string    `json:"agent_key,omitempty"`
	RequestCount int       `json:"request_count"`
}

// handleProviderCodexPoolActivity returns pool activity aggregated across all agents
// that use this provider's Codex pool. Only valid for chatgpt_oauth pool owners.
func (h *ProvidersHandler) handleProviderCodexPoolActivity(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	if h.tracingStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": i18n.T(locale, i18n.MsgInvalidRequest, "tracing store unavailable"),
		})
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "provider")})
		return
	}

	provider, err := h.store.GetProvider(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "provider", id.String())})
		return
	}

	if provider.ProviderType != store.ProviderChatGPTOAuth {
		writeJSON(w, http.StatusOK, emptyProviderPoolActivityResponse())
		return
	}

	// Resolve pool providers from provider settings (capped at 20 to bound query size)
	const maxPoolCandidates = 20
	settings := store.ParseChatGPTOAuthProviderSettings(provider.Settings)
	poolCandidates := []string{provider.Name}
	strategy := store.ChatGPTOAuthStrategyPrimaryFirst
	if settings != nil && settings.CodexPool != nil {
		if settings.CodexPool.Strategy != "" {
			strategy = settings.CodexPool.Strategy
		}
		for _, name := range settings.CodexPool.ExtraProviderNames {
			if name != "" && !slices.Contains(poolCandidates, name) {
				poolCandidates = append(poolCandidates, name)
				if len(poolCandidates) >= maxPoolCandidates {
					break
				}
			}
		}
	}

	// Filter to registered Codex providers
	poolProviders := registeredCodexPoolProviders(h.providerReg, provider.TenantID, poolCandidates)
	if len(poolProviders) == 0 {
		writeJSON(w, http.StatusOK, emptyProviderPoolActivityResponse())
		return
	}

	limit := 18
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 50 {
			limit = parsed
		}
	}
	statsLimit := maxInt(limit, codexPoolRuntimeHealthSampleSize)

	rawSpans, err := h.tracingStore.ListCodexPoolSpansByProviders(r.Context(), provider.TenantID, poolProviders, statsLimit)
	if err != nil {
		slog.Error("providers.codex_pool_activity", "provider", provider.Name, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "pool activity")})
		return
	}

	// Filter spans to those with routing evidence matching pool providers
	spans := make([]store.CodexPoolSpan, 0, len(rawSpans))
	agentRequestCounts := make(map[uuid.UUID]int)
	for _, item := range rawSpans {
		if evidence := providers.ExtractChatGPTOAuthRoutingEvidence(item.Metadata); evidence.HasData() {
			if !providerInPool(poolProviders, evidence.SelectedProvider) && !providerInPool(poolProviders, evidence.ServingProvider) {
				continue
			}
		} else if !providerInPool(poolProviders, item.Provider) {
			continue
		}
		spans = append(spans, item.CodexPoolSpan)
		agentRequestCounts[item.AgentID]++
	}

	providerCounts, recent := buildCodexPoolActivity(poolProviders, spans)
	if len(recent) > limit {
		recent = recent[:limit]
	}

	// Build top agents list sorted by request count
	topAgents := buildTopAgents(r.Context(), agentRequestCounts, h.agents)

	writeJSON(w, http.StatusOK, map[string]any{
		"strategy":          strategy,
		"pool_providers":    poolProviders,
		"stats_sample_size": len(spans),
		"provider_counts":   providerCounts,
		"recent_requests":   recent,
		"top_agents":        topAgents,
	})
}

func emptyProviderPoolActivityResponse() map[string]any {
	return map[string]any{
		"strategy":          store.ChatGPTOAuthStrategyPrimaryFirst,
		"pool_providers":    []string{},
		"stats_sample_size": 0,
		"provider_counts":   []codexPoolProviderCount{},
		"recent_requests":   []codexPoolRecentRequest{},
		"top_agents":        []providerCodexPoolAgentCount{},
	}
}

// buildTopAgents converts agent request counts into a sorted slice, resolving agent keys.
func buildTopAgents(ctx context.Context, counts map[uuid.UUID]int, agentStore store.AgentCRUDStore) []providerCodexPoolAgentCount {
	if len(counts) == 0 {
		return []providerCodexPoolAgentCount{}
	}

	agentIDs := make([]uuid.UUID, 0, len(counts))
	for id := range counts {
		agentIDs = append(agentIDs, id)
	}

	keyByID := make(map[uuid.UUID]string, len(agentIDs))
	if agentStore != nil {
		if agents, err := agentStore.GetByIDs(ctx, agentIDs); err != nil {
			slog.Warn("providers.codex_pool_activity.resolve_agents", "error", err, "count", len(agentIDs))
		} else {
			for _, ag := range agents {
				keyByID[ag.ID] = ag.AgentKey
			}
		}
	}

	result := make([]providerCodexPoolAgentCount, 0, len(counts))
	for id, count := range counts {
		result = append(result, providerCodexPoolAgentCount{
			AgentID:      id,
			AgentKey:     keyByID[id],
			RequestCount: count,
		})
	}

	slices.SortFunc(result, func(a, b providerCodexPoolAgentCount) int {
		return b.RequestCount - a.RequestCount
	})

	if len(result) > 10 {
		result = result[:10]
	}

	return result
}
