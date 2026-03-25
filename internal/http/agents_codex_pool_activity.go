package http

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

type codexPoolSpanActivity struct {
	SpanID     uuid.UUID
	TraceID    uuid.UUID
	StartedAt  time.Time
	DurationMS int
	Status     string
	Provider   string
	Model      string
	Metadata   json.RawMessage
}

func buildCodexPoolActivity(poolProviders []string, spans []codexPoolSpanActivity) ([]codexPoolProviderCount, []codexPoolRecentRequest) {
	countsByProvider := make(map[string]*codexPoolProviderCount, len(poolProviders))
	for _, name := range poolProviders {
		countsByProvider[name] = &codexPoolProviderCount{ProviderName: name}
	}

	recent := make([]codexPoolRecentRequest, 0, len(spans))
	for _, span := range spans {
		evidence := providers.ExtractChatGPTOAuthRoutingEvidence(span.Metadata)
		selectedProvider := firstNonEmpty(span.Provider, evidence.SelectedProvider)
		if evidence.SelectedProvider != "" {
			selectedProvider = evidence.SelectedProvider
		}
		servingProvider := firstNonEmpty(evidence.ServingProvider, span.Provider, selectedProvider)
		failoverProviders := append([]string(nil), evidence.FailoverProviders...)
		usedFailover := len(failoverProviders) > 0 || (selectedProvider != "" && servingProvider != "" && servingProvider != selectedProvider)

		if stat := countsByProvider[selectedProvider]; stat != nil {
			stat.RequestCount++
			stat.DirectSelectionCount++
			updateLatestTime(&stat.LastSelectedAt, span.StartedAt)
			updateLatestTime(&stat.LastUsedAt, span.StartedAt)
		}
		if usedFailover && servingProvider != "" && servingProvider != selectedProvider {
			if stat := countsByProvider[servingProvider]; stat != nil {
				stat.FailoverServeCount++
				updateLatestTime(&stat.LastFailoverAt, span.StartedAt)
				updateLatestTime(&stat.LastUsedAt, span.StartedAt)
			}
		}

		recent = append(recent, codexPoolRecentRequest{
			SpanID:            span.SpanID,
			TraceID:           span.TraceID,
			StartedAt:         span.StartedAt,
			Status:            span.Status,
			DurationMS:        span.DurationMS,
			ProviderName:      servingProvider,
			SelectedProvider:  selectedProvider,
			Model:             span.Model,
			AttemptCount:      maxInt(1, evidence.AttemptCount),
			UsedFailover:      usedFailover,
			FailoverProviders: failoverProviders,
		})
	}

	providerCounts := make([]codexPoolProviderCount, 0, len(poolProviders))
	for _, name := range poolProviders {
		if stat := countsByProvider[name]; stat != nil {
			providerCounts = append(providerCounts, *stat)
		}
	}
	return providerCounts, recent
}

func updateLatestTime(target **time.Time, value time.Time) {
	if target == nil {
		return
	}
	if *target == nil || value.After(**target) {
		seenAt := value
		*target = &seenAt
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func providerInPool(poolProviders []string, providerName string) bool {
	return providerName != "" && slices.Contains(poolProviders, providerName)
}
