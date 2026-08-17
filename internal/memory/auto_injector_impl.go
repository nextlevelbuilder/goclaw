package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
)

// pgAutoInjector implements AutoInjector backed by EpisodicStore + FTS search.
type pgAutoInjector struct {
	episodicStore store.EpisodicStore
	metricsStore  store.EvolutionMetricsStore // nil = metrics disabled
}

// NewAutoInjector creates an AutoInjector backed by episodic store search.
func NewAutoInjector(es store.EpisodicStore, ms store.EvolutionMetricsStore) AutoInjector {
	return &pgAutoInjector{episodicStore: es, metricsStore: ms}
}

// Inject searches episodic memory for relevant L0 abstracts and formats a prompt section.
func (a *pgAutoInjector) Inject(ctx context.Context, params InjectParams) (*InjectResult, error) {
	if a.episodicStore == nil {
		return &InjectResult{}, nil
	}
	if isTrivialMessage(params.UserMessage) {
		return &InjectResult{}, nil
	}

	maxEntries := params.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 5
	}
	// Token budget caps the assembled L0 section (Gap E). Mirrors maxEntries:
	// 0 (unset by caller) falls back to the documented default of 200 tokens so
	// a single oversized abstract can't blow the system prompt beyond the fixed
	// overhead pool. A positive value overrides the budget; the legacy behavior
	// (count-only cap) is what <= 0 defaulted to, so nothing regresses.
	maxTokens := params.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 200
	}
	threshold := params.Threshold
	if threshold <= 0 {
		threshold = 0.3
	}

	// Phase 9: context-aware recall. When the caller supplied RecentContext,
	// build a richer search query that captures conversational intent. Without
	// this, vector search on "what's my favorite?" misses memories about the
	// topic under discussion. With it, the query embedding captures the
	// follow-up semantics and returns materially better matches.
	searchQuery := buildRecallQuery(params.UserMessage, params.RecentContext)

	// Search with FTS bias (faster than vector for auto-inject)
	results, err := a.episodicStore.Search(ctx, searchQuery, params.AgentID, params.UserID,
		store.EpisodicSearchOptions{
			MaxResults:   maxEntries * 2, // fetch more, filter by threshold
			MinScore:     threshold,
			VectorWeight: 0.3,
			TextWeight:   0.7,
		})
	if err != nil {
		return nil, fmt.Errorf("auto-inject search: %w", err)
	}
	if len(results) == 0 {
		return &InjectResult{}, nil
	}

	// Build prompt section from L0 abstracts
	var sb strings.Builder
	sb.WriteString("## Memory Context\n\nRelevant memories from past sessions (use memory_search for details):\n")

	// Token-budget enforcement: the section is capped at MaxTokens (default 200).
	// This keeps the L0 auto-inject from ballooning the system prompt beyond the
	// fixed overhead pool. Counter is shared across a run so a single blown
	// entry is clipped without breaking subsequent injections.
	counter := tokencount.NewBudgetCounter()
	prefix := sb.String()
	sectionTokens, _ := counter.CountText(prefix)

	injected := 0
	var topScore float64
	for _, r := range results {
		if injected >= maxEntries {
			break
		}
		if r.L0Abstract == "" {
			continue
		}
		entry := "- " + r.L0Abstract + "\n"
		if maxTokens > 0 {
			entryTokens, err := counter.CountText(entry)
			if err == nil && sectionTokens+entryTokens > maxTokens {
				remaining := maxTokens - sectionTokens
				if remaining > 0 {
					// The entry is too big to fit whole. If nothing is injected yet,
					// the top-relevant hit is preserved by clipping it to the budget
					// (plan: "keep the most relevant entries, then clip to budget").
					// Otherwise skip it and keep looking for a smaller entry.
					if injected > 0 {
						continue
					}
					// Clip with 1-token headroom: the counter counts the entry in
					// isolation, but the assembled section tokenizes it adjacent to
					// the prefix, which can drift by a token at the boundary.
					entry = clipEntryToBudget(entry, remaining-1, counter)
					entryTokens, _ = counter.CountText(entry)
					if entryTokens <= 0 {
						continue // clip produced no usable text
					}
				} else {
					continue
				}
			}
			sectionTokens += entryTokens
		}
		sb.WriteString(entry)
		injected++
		if r.Score > topScore {
			topScore = r.Score
		}
	}

	if injected == 0 {
		return &InjectResult{MatchCount: len(results)}, nil
	}

	result := &InjectResult{
		Section:    sb.String(),
		MatchCount: len(results),
		Injected:   injected,
		TopScore:   topScore,
	}

	// Record retrieval metric non-blocking (best-effort).
	a.recordRetrievalMetric(params, result)

	return result, nil
}

// clipEntryToBudget rune-clips a bullet entry so it fits within budgetTokens.
// It trims the bullet prefix ("- ") plus as many runes as fit, keeping the
// head of the abstract (the topic is front-loaded in L0 abstracts). The result
// always ends with a newline so a subsequent entry starts on a fresh line.
// Returns "" when not even a bare bullet fits. The caller recounts the result.
func clipEntryToBudget(entry string, budgetTokens int, counter tokencount.BudgetCounter) string {
	// Account for the bullet prefix once, then binary-search the longest prefix
	// whose token count stays within budget. Rune-based so multi-byte
	// vi/zh abstracts are never split mid-character.
	const bullet = "- "
	body := strings.TrimSuffix(strings.TrimPrefix(entry, bullet), "\n")
	runes := []rune(body)

	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		candidate := bullet + string(runes[:mid])
		n, err := counter.CountText(candidate)
		if err == nil && n <= budgetTokens {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo == 0 {
		return "" // not even a bare bullet fits — caller skips the entry
	}
	return bullet + string(runes[:lo]) + "\n"
}

// recordRetrievalMetric records an auto-inject retrieval metric in a background goroutine.
func (a *pgAutoInjector) recordRetrievalMetric(params InjectParams, result *InjectResult) {
	if a.metricsStore == nil || params.TenantID == "" {
		return
	}
	tenantID, err := uuid.Parse(params.TenantID)
	if err != nil {
		return
	}
	agentID, err := uuid.Parse(params.AgentID)
	if err != nil {
		return
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(store.WithTenantID(context.Background(), tenantID), 5*time.Second)
		defer cancel()
		value, _ := json.Marshal(map[string]any{
			"result_count":  result.MatchCount,
			"injected":      result.Injected,
			"top_score":     result.TopScore,
			"used_in_reply": result.Injected > 0,
		})
		if err := a.metricsStore.RecordMetric(bgCtx, store.EvolutionMetric{
			ID:         uuid.New(),
			TenantID:   tenantID,
			AgentID:    agentID,
			MetricType: store.MetricRetrieval,
			MetricKey:  "auto_inject",
			Value:      value,
		}); err != nil {
			slog.Debug("evolution.metric.auto_inject_failed", "error", err)
		}
	}()
}
