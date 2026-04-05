package http

import (
	"math"
	"slices"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const codexPoolRuntimeHealthSampleSize = 120

func buildCodexPoolActivity(poolProviders []string, spans []store.CodexPoolSpan) ([]codexPoolProviderCount, []codexPoolRecentRequest) {
	sortedSpans := append([]store.CodexPoolSpan(nil), spans...)
	slices.SortFunc(sortedSpans, func(a, b store.CodexPoolSpan) int {
		return b.StartedAt.Compare(a.StartedAt)
	})

	countsByProvider := make(map[string]*codexPoolProviderCount, len(poolProviders))
	outcomesByProvider := make(map[string][]bool, len(poolProviders))
	for _, name := range poolProviders {
		countsByProvider[name] = &codexPoolProviderCount{ProviderName: name}
	}

	recent := make([]codexPoolRecentRequest, 0, len(sortedSpans))
	for _, span := range sortedSpans {
		evidence := providers.ExtractChatGPTOAuthRoutingEvidence(span.Metadata)
		selectedProvider := firstNonEmpty(span.Provider, evidence.SelectedProvider)
		if evidence.SelectedProvider != "" {
			selectedProvider = evidence.SelectedProvider
		}
		servingProvider := firstNonEmpty(evidence.ServingProvider)
		if span.Status == "completed" {
			servingProvider = firstNonEmpty(evidence.ServingProvider, span.Provider, selectedProvider)
		}
		failoverProviders := append([]string(nil), evidence.FailoverProviders...)
		usedFailover := len(failoverProviders) > 0 || (selectedProvider != "" && servingProvider != "" && servingProvider != selectedProvider)
		attemptedProviders := poolAttemptedProviders(poolProviders, evidence, selectedProvider, servingProvider)

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
		recordCodexPoolOutcomes(
			countsByProvider,
			outcomesByProvider,
			attemptedProviders,
			servingProvider,
			span.Status,
			span.StartedAt,
		)

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
			finalizeCodexPoolProviderHealth(stat, outcomesByProvider[name])
			providerCounts = append(providerCounts, *stat)
		}
	}
	return providerCounts, recent
}

func poolAttemptedProviders(
	poolProviders []string,
	evidence providers.ChatGPTOAuthRoutingEvidence,
	selectedProvider string,
	servingProvider string,
) []string {
	attempted := make([]string, 0, len(evidence.AttemptedProviders)+2)
	for _, providerName := range evidence.AttemptedProviders {
		if providerInPool(poolProviders, providerName) && !slices.Contains(attempted, providerName) {
			attempted = append(attempted, providerName)
		}
	}
	if providerInPool(poolProviders, selectedProvider) && !slices.Contains(attempted, selectedProvider) {
		attempted = append(attempted, selectedProvider)
	}
	if providerInPool(poolProviders, servingProvider) && !slices.Contains(attempted, servingProvider) {
		attempted = append(attempted, servingProvider)
	}
	return attempted
}

func recordCodexPoolOutcomes(
	countsByProvider map[string]*codexPoolProviderCount,
	outcomesByProvider map[string][]bool,
	attemptedProviders []string,
	servingProvider string,
	status string,
	startedAt time.Time,
) {
	switch status {
	case "completed":
		if stat := countsByProvider[servingProvider]; stat != nil {
			stat.SuccessCount++
			updateLatestTime(&stat.LastSuccessAt, startedAt)
			updateLatestTime(&stat.LastUsedAt, startedAt)
			outcomesByProvider[servingProvider] = append(outcomesByProvider[servingProvider], true)
		}
		for _, providerName := range attemptedProviders {
			if providerName == "" || providerName == servingProvider {
				continue
			}
			if stat := countsByProvider[providerName]; stat != nil {
				stat.FailureCount++
				updateLatestTime(&stat.LastFailureAt, startedAt)
				updateLatestTime(&stat.LastUsedAt, startedAt)
				outcomesByProvider[providerName] = append(outcomesByProvider[providerName], false)
			}
		}
	case "error":
		for _, providerName := range attemptedProviders {
			if stat := countsByProvider[providerName]; stat != nil {
				stat.FailureCount++
				updateLatestTime(&stat.LastFailureAt, startedAt)
				updateLatestTime(&stat.LastUsedAt, startedAt)
				outcomesByProvider[providerName] = append(outcomesByProvider[providerName], false)
			}
		}
	}
}

func finalizeCodexPoolProviderHealth(stat *codexPoolProviderCount, outcomes []bool) {
	if stat == nil {
		return
	}
	total := stat.SuccessCount + stat.FailureCount
	if total > 0 {
		stat.SuccessRate = int(math.Round(float64(stat.SuccessCount) * 100 / float64(total)))
	}
	for _, outcome := range outcomes {
		if outcome {
			break
		}
		stat.ConsecutiveFailures++
	}
	if total == 0 {
		stat.HealthScore = 0
		stat.HealthState = "idle"
		return
	}
	score := stat.SuccessRate - minInt(45, stat.ConsecutiveFailures*15)
	if stat.LastSuccessAt == nil && stat.FailureCount > 0 {
		score -= 10
	}
	stat.HealthScore = clampInt(score, 0, 100)
	switch {
	case stat.ConsecutiveFailures >= 3 || stat.HealthScore < 40:
		stat.HealthState = "critical"
	case stat.HealthScore < 80:
		stat.HealthState = "degraded"
	default:
		stat.HealthState = "healthy"
	}
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func providerInPool(poolProviders []string, providerName string) bool {
	return providerName != "" && slices.Contains(poolProviders, providerName)
}
