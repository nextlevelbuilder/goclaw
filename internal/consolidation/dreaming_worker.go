package consolidation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bgalert"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/providerresolve"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	dreamingDefaultThreshold = 5
	dreamingDefaultDebounce  = 10 * time.Minute
	dreamingFetchLimit       = 10
	dreamingMaxTokens        = 4096
)

// dreamingWorker consolidates unpromoted episodic summaries into long-term memory.
// Subscribes to episodic.created events; debounces per agent/user pair.
type dreamingWorker struct {
	episodicStore store.EpisodicStore
	memoryStore   store.MemoryStore
	systemConfigs store.SystemConfigStore // per-tenant provider config
	registry      *providers.Registry     // provider resolution
	alertDeps     bgalert.AlertDeps

	// threshold/debounce are the global defaults. Per-agent overrides come
	// from resolveConfig which reads the agent's MemoryConfig.Dreaming JSONB.
	threshold     int
	debounce      time.Duration
	resolveConfig DreamingConfigResolver

	lastRun sync.Map // key: "agentID:userID" → time.Time
}

// resolveProvider delegates to shared background provider resolution.
func (w *dreamingWorker) resolveProvider(ctx context.Context, tenantID uuid.UUID) (providers.Provider, string) {
	return providerresolve.ResolveBackgroundProvider(ctx, tenantID, w.registry, w.systemConfigs)
}

// formatEntryForSynthesis renders a single episodic entry with recall
// metadata for the LLM synthesis prompt. Entries with recall signal are
// tagged so the LLM can weight them higher; unrecalled entries pass through
// as plain summaries.
func formatEntryForSynthesis(e store.EpisodicSummary) string {
	if e.RecallCount == 0 {
		return e.Summary
	}
	lastRecall := "never"
	if e.LastRecalledAt != nil {
		lastRecall = e.LastRecalledAt.Format("Jan 2")
	}
	return fmt.Sprintf("[recalled %dx, last: %s] %s", e.RecallCount, lastRecall, e.Summary)
}

// logSkip emits dreaming skip reasons at debug level by default, elevating
// to info when verbose logging is enabled via per-agent config.
func logSkip(verbose bool, msg string, args ...any) {
	if verbose {
		slog.Info(msg, args...)
		return
	}
	slog.Debug(msg, args...)
}

// effectiveConfig merges the worker's defaults with any per-agent override.
// Centralised so tests can reason about the precedence in one place.
func (w *dreamingWorker) effectiveConfig(ctx context.Context, agentID string) resolvedDreamingConfig {
	base := defaultDreamingConfig()
	if w.threshold > 0 {
		base.Threshold = w.threshold
	}
	if w.debounce > 0 {
		base.Debounce = w.debounce
	}
	if w.resolveConfig == nil {
		return base
	}
	return mergeDreamingConfig(base, w.resolveConfig(ctx, agentID))
}

// Handle processes an episodic.created event for the dreaming pipeline.
func (w *dreamingWorker) Handle(ctx context.Context, event eventbus.DomainEvent) error {
	// Inject tenant context so store queries scope correctly
	if event.TenantID != "" {
		if tid, err := uuid.Parse(event.TenantID); err == nil {
			ctx = store.WithTenantID(ctx, tid)
		}
	}

	agentID := event.AgentID
	userID := event.UserID

	// Resolve per-agent config (threshold/debounce/enabled). Falls back to
	// struct defaults when no override is stored on the agent.
	cfg := w.effectiveConfig(ctx, agentID)
	if !cfg.Enabled {
		if cfg.VerboseLog {
			slog.Info("dreaming: disabled for agent", "agent", agentID, "user", userID)
		}
		return nil
	}

	// Debounce: skip if ran recently for this pair.
	key := agentID + ":" + userID
	if v, ok := w.lastRun.Load(key); ok {
		if last, ok := v.(time.Time); ok && time.Since(last) < cfg.Debounce {
			logSkip(cfg.VerboseLog, "dreaming: debounce skip", "agent", agentID, "user", userID)
			return nil
		}
	}

	// Count unpromoted entries; skip if below threshold.
	count, err := w.episodicStore.CountUnpromoted(ctx, agentID, userID)
	if err != nil {
		slog.Warn("dreaming: count unpromoted failed", "err", err, "agent", agentID)
		return nil
	}
	if count < cfg.Threshold {
		logSkip(cfg.VerboseLog, "dreaming: below threshold", "count", count, "threshold", cfg.Threshold, "agent", agentID)
		return nil
	}

	// Fetch unpromoted entries ordered by recall_score DESC (Phase 10). Falls
	// back to created_at ASC for ties so agents with no recall history still
	// get oldest-first behaviour.
	entries, err := w.episodicStore.ListUnpromotedScored(ctx, agentID, userID, dreamingFetchLimit)
	if err != nil {
		slog.Warn("dreaming: list unpromoted failed", "err", err, "agent", agentID)
		return nil
	}
	// Filter by recall-score thresholds before synthesis so weak entries
	// don't burn LLM tokens. Never-recalled entries bypass the count minimum
	// via the freshness component in ComputeRecallScore.
	entries = filterByRecallThresholds(entries, defaultRecallThresholds(), time.Now().UTC())
	if len(entries) == 0 {
		// Stamp lastRun even on empty-filter skips — otherwise every subsequent
		// episodic.created event re-runs CountUnpromoted + ListUnpromotedScored
		// + filter in a tight loop until the agent accumulates fresh content.
		// See code review note P10.1.
		w.lastRun.Store(key, time.Now())
		logSkip(cfg.VerboseLog, "dreaming: all entries below recall thresholds", "agent", agentID, "user", userID)
		return nil
	}

	// Resolve provider for this tenant at processing time.
	tenantUUID, _ := uuid.Parse(event.TenantID)
	provider, model := w.resolveProvider(ctx, tenantUUID)
	if provider == nil {
		slog.Warn("dreaming: no provider available", "tenant", event.TenantID, "agent", agentID)
		return nil
	}

	// Build LLM prompt and call provider.
	synthesis, err := w.synthesize(ctx, provider, model, entries)
	if err != nil {
		bgalert.ReportProviderError(ctx, w.alertDeps, "dreaming", err)
		slog.Warn("dreaming: LLM synthesis failed", "err", err, "agent", agentID)
		return nil
	}

	// Store result in memory under a dated path and index for search.
	path := fmt.Sprintf("_system/dreaming/%s-consolidated.md", time.Now().UTC().Format("20060102"))
	if err := w.memoryStore.PutDocument(ctx, agentID, userID, path, synthesis); err != nil {
		slog.Warn("dreaming: store document failed", "err", err, "path", path, "agent", agentID)
		return nil
	}
	if err := w.memoryStore.IndexDocument(ctx, agentID, userID, path); err != nil {
		slog.Warn("dreaming: index document failed", "err", err, "path", path, "agent", agentID)
	}

	// Mark entries as promoted.
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID.String()
	}
	if err := w.episodicStore.MarkPromoted(ctx, ids); err != nil {
		slog.Warn("dreaming: mark promoted failed", "err", err, "agent", agentID)
		return nil
	}

	// Update debounce tracker.
	w.lastRun.Store(key, time.Now())

	slog.Info("dreaming: consolidated", "agent", agentID, "user", userID,
		"entries", len(entries), "path", path)
	return nil
}

// synthesize calls the LLM to extract long-term facts from session summaries.
// Each entry is annotated with its recall metadata so the LLM can weight
// frequently-recalled memories higher during synthesis.
func (w *dreamingWorker) synthesize(ctx context.Context, provider providers.Provider, model string, entries []store.EpisodicSummary) (string, error) {
	summaries := make([]string, len(entries))
	for i, e := range entries {
		summaries[i] = formatEntryForSynthesis(e)
	}
	body := strings.Join(summaries, "\n---\n")

	resp, err := provider.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: dreamingSystemPrompt},
			{Role: "user", Content: "Session summaries:\n---\n" + body + "\n---"},
		},
		Model: model,
		Options: map[string]any{
			providers.OptMaxTokens: dreamingMaxTokens,
		},
	})
	if err != nil {
		return "", fmt.Errorf("dreaming chat: %w", err)
	}
	return resp.Content, nil
}

// dreamingSystemPrompt instructs the LLM to extract long-term facts.
const dreamingSystemPrompt = `You are consolidating session memories into long-term facts.

Review these session summaries and extract:
1. **User Preferences** — communication style, tool usage patterns, coding preferences
2. **Project Facts** — architecture decisions, tech stack, naming conventions
3. **Recurring Patterns** — frequently used workflows, common requests
4. **Key Decisions** — important choices made with rationale

Output format: Markdown with ## sections for each category.
Only include facts that appear across multiple sessions or are explicitly stated as important.
Do NOT include one-off tasks or transient details.`
