package consolidation

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/providerresolve"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DigestWorker aggregates episodic summaries into daily digests.
// Runs on a schedule (typically end-of-day) to create structured reports.
type DigestWorker struct {
	episodicStore store.EpisodicStore
	digestStore   store.DailyDigestStore
	systemConfigs store.SystemConfigStore
	registry      *providers.Registry
}

// NewDigestWorker creates a new digest worker.
func NewDigestWorker(
	episodicStore store.EpisodicStore,
	digestStore store.DailyDigestStore,
	systemConfigs store.SystemConfigStore,
	registry *providers.Registry,
) *DigestWorker {
	return &DigestWorker{
		episodicStore: episodicStore,
		digestStore:   digestStore,
		systemConfigs: systemConfigs,
		registry:      registry,
	}
}

// GenerateDigest creates a daily digest for a specific agent/user/date.
// Aggregates all episodic summaries from that day and extracts structured data.
func (w *DigestWorker) GenerateDigest(ctx context.Context, tenantID, agentID uuid.UUID, userID string, date time.Time) error {
	ctx = store.WithTenantID(ctx, tenantID)

	// List episodic summaries for this agent/user (limit to recent for performance)
	summaries, err := w.episodicStore.List(ctx, agentID.String(), userID, 100, 0)
	if err != nil {
		return err
	}

	// Filter to summaries from the target date
	dateStr := date.Format("2006-01-02")
	var daySummaries []store.EpisodicSummary
	for _, s := range summaries {
		if s.CreatedAt.Format("2006-01-02") == dateStr {
			daySummaries = append(daySummaries, s)
		}
	}

	if len(daySummaries) == 0 {
		slog.Debug("digest: no summaries for date", "agent", agentID, "user", userID, "date", dateStr)
		return nil
	}

	// Aggregate structured data from all summaries
	var allDecisions []store.EpisodicDecision
	var allActions []store.EpisodicActionItem
	entitiesSet := make(map[string]bool)
	var sessionCount, messageCount int
	var summaryParts []string

	for _, s := range daySummaries {
		sessionCount++
		messageCount += s.TurnCount

		// Collect from metadata if present
		if s.Metadata != nil {
			allDecisions = append(allDecisions, s.Metadata.Decisions...)
			allActions = append(allActions, s.Metadata.ActionItems...)
			for _, e := range s.Metadata.Entities {
				entitiesSet[e] = true
			}
		}

		// Also collect from key_topics
		for _, t := range s.KeyTopics {
			entitiesSet[t] = true
		}

		// Collect L0 abstracts for summary generation
		if s.L0Abstract != "" {
			summaryParts = append(summaryParts, s.L0Abstract)
		}
	}

	// Convert entities set to slice
	var entities []string
	for e := range entitiesSet {
		entities = append(entities, e)
	}

	// Generate digest summary using LLM (optional)
	digestSummary := ""
	if len(summaryParts) > 0 {
		provider, model := providerresolve.ResolveBackgroundProvider(ctx, tenantID, w.registry, w.systemConfigs)
		if provider != nil {
			digestSummary = w.generateDigestSummary(ctx, provider, model, summaryParts)
		}
	}

	// Create/update the daily digest
	digest := &store.DailyDigest{
		TenantID:     tenantID,
		AgentID:      agentID,
		UserID:       userID,
		DigestDate:   date,
		Decisions:    deduplicateDecisions(allDecisions),
		ActionItems:  deduplicateActions(allActions),
		KeyTopics:    entities,
		Summary:      digestSummary,
		SessionCount: sessionCount,
		MessageCount: messageCount,
	}

	if err := w.digestStore.Upsert(ctx, digest); err != nil {
		return err
	}

	slog.Info("digest: created daily digest",
		"agent", agentID, "user", userID, "date", dateStr,
		"sessions", sessionCount, "decisions", len(digest.Decisions),
		"actions", len(digest.ActionItems))

	return nil
}

// generateDigestSummary creates a concise daily summary from L0 abstracts.
func (w *DigestWorker) generateDigestSummary(ctx context.Context, provider providers.Provider, model string, abstracts []string) string {
	combined := strings.Join(abstracts, "\n- ")

	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := provider.Chat(sctx, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: digestSummaryPrompt},
			{Role: "user", Content: "Sessions:\n- " + combined},
		},
		Model:   model,
		Options: map[string]any{"max_tokens": 512, "temperature": 0.3},
	})
	if err != nil {
		slog.Warn("digest: summary generation failed", "error", err)
		return ""
	}
	return resp.Content
}

const digestSummaryPrompt = `Summarize the day's activities from these session abstracts.
Write 1-2 paragraphs covering:
- Main accomplishments
- Key decisions made
- Open items or next steps

Be concise and factual. Do not add speculation or filler.`

// deduplicateDecisions removes duplicate decisions by content.
func deduplicateDecisions(decisions []store.EpisodicDecision) []store.EpisodicDecision {
	seen := make(map[string]bool)
	var result []store.EpisodicDecision
	for _, d := range decisions {
		key := strings.ToLower(strings.TrimSpace(d.Content))
		if !seen[key] {
			seen[key] = true
			result = append(result, d)
		}
	}
	return result
}

// deduplicateActions removes duplicate action items by content.
func deduplicateActions(actions []store.EpisodicActionItem) []store.EpisodicActionItem {
	seen := make(map[string]bool)
	var result []store.EpisodicActionItem
	for _, a := range actions {
		key := strings.ToLower(strings.TrimSpace(a.Content))
		if !seen[key] {
			seen[key] = true
			result = append(result, a)
		}
	}
	return result
}
