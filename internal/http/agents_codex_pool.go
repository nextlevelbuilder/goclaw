package http

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type codexPoolProviderCount struct {
	ProviderName         string     `json:"provider_name"`
	RequestCount         int        `json:"request_count"`
	DirectSelectionCount int        `json:"direct_selection_count"`
	FailoverServeCount   int        `json:"failover_serve_count"`
	SuccessCount         int        `json:"success_count"`
	FailureCount         int        `json:"failure_count"`
	ConsecutiveFailures  int        `json:"consecutive_failures"`
	SuccessRate          int        `json:"success_rate"`
	HealthScore          int        `json:"health_score"`
	HealthState          string     `json:"health_state"`
	LastSelectedAt       *time.Time `json:"last_selected_at,omitempty"`
	LastFailoverAt       *time.Time `json:"last_failover_at,omitempty"`
	LastUsedAt           *time.Time `json:"last_used_at,omitempty"`
	LastSuccessAt        *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt        *time.Time `json:"last_failure_at,omitempty"`
}

type codexPoolRecentRequest struct {
	SpanID            uuid.UUID `json:"span_id"`
	TraceID           uuid.UUID `json:"trace_id"`
	StartedAt         time.Time `json:"started_at"`
	Status            string    `json:"status"`
	DurationMS        int       `json:"duration_ms"`
	ProviderName      string    `json:"provider_name"`
	SelectedProvider  string    `json:"selected_provider,omitempty"`
	Model             string    `json:"model"`
	AttemptCount      int       `json:"attempt_count"`
	UsedFailover      bool      `json:"used_failover"`
	FailoverProviders []string  `json:"failover_providers,omitempty"`
}

const runtimeNonCodexProviderType = "runtime_non_codex"

func lookupProviderByNameWithMasterFallback(
	ctx context.Context,
	providerStore store.ProviderStore,
	tenantID uuid.UUID,
	name string,
) (*store.LLMProviderData, error) {
	if providerStore == nil || name == "" {
		return nil, errors.New("provider store unavailable")
	}

	tenantIDs := []uuid.UUID{tenantID}
	if tenantID != store.MasterTenantID {
		tenantIDs = append(tenantIDs, store.MasterTenantID)
	}

	var lastErr error
	for _, scopedTenantID := range tenantIDs {
		providerCtx := store.WithTenantID(ctx, scopedTenantID)
		providerData, err := providerStore.GetProviderByName(providerCtx, name)
		if err == nil {
			return providerData, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("provider not found")
	}
	return nil, lastErr
}

func registeredCodexPoolProviders(
	providerReg *providers.Registry,
	tenantID uuid.UUID,
	names []string,
) []string {
	if providerReg == nil || len(names) == 0 {
		return nil
	}

	poolProviders := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" || slices.Contains(poolProviders, name) {
			continue
		}
		provider, err := providerReg.GetForTenant(tenantID, name)
		if err != nil {
			continue
		}
		if _, ok := provider.(*providers.CodexProvider); !ok {
			continue
		}
		poolProviders = append(poolProviders, name)
	}
	return poolProviders
}

func resolveCodexPoolRouting(
	ctx context.Context,
	providerStore store.ProviderStore,
	providerReg *providers.Registry,
	agent *store.AgentData,
) (string, *store.ChatGPTOAuthRoutingConfig, []string) {
	if agent == nil {
		return "", nil, nil
	}

	agentRouting := agent.ParseChatGPTOAuthRouting()

	baseProviderType := ""
	var defaults *store.ChatGPTOAuthRoutingConfig

	if providerData, err := lookupProviderByNameWithMasterFallback(ctx, providerStore, agent.TenantID, agent.Provider); err == nil {
		baseProviderType = providerData.ProviderType
		if providerData.ProviderType != store.ProviderChatGPTOAuth {
			return providerData.ProviderType, nil, nil
		}
		if settings := store.ParseChatGPTOAuthProviderSettings(providerData.Settings); settings != nil {
			defaults = settings.CodexPool
		}
	}

	if providerReg != nil && agent.Provider != "" {
		runtimeProvider, err := providerReg.GetForTenant(agent.TenantID, agent.Provider)
		if err == nil {
			codex, ok := runtimeProvider.(*providers.CodexProvider)
			if !ok {
				if baseProviderType == "" {
					baseProviderType = runtimeNonCodexProviderType
				}
				return baseProviderType, nil, nil
			}
			baseProviderType = store.ProviderChatGPTOAuth
			defaults = nil
			if runtimeDefaults := codex.RoutingDefaults(); runtimeDefaults != nil {
				defaults = &store.ChatGPTOAuthRoutingConfig{
					Strategy:           runtimeDefaults.Strategy,
					ExtraProviderNames: runtimeDefaults.ExtraProviderNames,
				}
			}
		}
	}

	routing := store.ResolveEffectiveChatGPTOAuthRouting(defaults, agentRouting)
	poolCandidates := make([]string, 0, 1+len(agentRoutingExtraNames(routing)))
	if agent.Provider != "" && (baseProviderType == store.ProviderChatGPTOAuth || (baseProviderType == "" && routing != nil)) {
		poolCandidates = append(poolCandidates, agent.Provider)
	}
	if routing != nil {
		for _, name := range routing.ExtraProviderNames {
			if name != "" && !slices.Contains(poolCandidates, name) {
				poolCandidates = append(poolCandidates, name)
			}
		}
	}
	if providerReg != nil {
		return baseProviderType, routing, registeredCodexPoolProviders(providerReg, agent.TenantID, poolCandidates)
	}
	if baseProviderType != store.ProviderChatGPTOAuth {
		return baseProviderType, routing, nil
	}
	return baseProviderType, routing, poolCandidates
}

func agentRoutingExtraNames(routing *store.ChatGPTOAuthRoutingConfig) []string {
	if routing == nil {
		return nil
	}
	return routing.ExtraProviderNames
}

func (h *AgentsHandler) handleCodexPoolActivity(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	if h.tracingStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "tracing store unavailable")})
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
	statsLimit := maxInt(limit, codexPoolRuntimeHealthSampleSize)

	baseProviderType, routing, poolProviders := resolveCodexPoolRouting(r.Context(), h.providers, h.providerReg, agent)
	strategy := store.ChatGPTOAuthStrategyPrimaryFirst
	if routing != nil && routing.Strategy != "" {
		strategy = routing.Strategy
	}

	if baseProviderType != "" && baseProviderType != store.ProviderChatGPTOAuth {
		poolProviders = nil
	}
	if len(poolProviders) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"strategy":          strategy,
			"pool_providers":    []string{},
			"stats_sample_size": 0,
			"provider_counts":   []codexPoolProviderCount{},
			"recent_requests":   []codexPoolRecentRequest{},
		})
		return
	}

	rawSpans, err := h.tracingStore.ListCodexPoolSpans(r.Context(), agent.ID, agent.TenantID, poolProviders, statsLimit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	spans := make([]store.CodexPoolSpan, 0, len(rawSpans))
	for _, item := range rawSpans {
		if evidence := providers.ExtractChatGPTOAuthRoutingEvidence(item.Metadata); evidence.HasData() {
			if !providerInPool(poolProviders, evidence.SelectedProvider) && !providerInPool(poolProviders, evidence.ServingProvider) {
				continue
			}
		} else if !providerInPool(poolProviders, item.Provider) {
			continue
		}
		spans = append(spans, item)
	}

	providerCounts, recent := buildCodexPoolActivity(poolProviders, spans)
	if len(recent) > limit {
		recent = recent[:limit]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"strategy":          strategy,
		"pool_providers":    poolProviders,
		"stats_sample_size": len(spans),
		"provider_counts":   providerCounts,
		"recent_requests":   recent,
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
