package consolidation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
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
	provider      providers.Provider
	model         string        // LLM model for synthesis
	threshold     int           // min unpromoted entries before running
	debounce      time.Duration // min interval between runs per agent/user
	lastRun       sync.Map      // key: "agentID:userID" → time.Time
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

	// Debounce: skip if ran recently for this pair.
	key := agentID + ":" + userID
	debounce := w.debounce
	if debounce == 0 {
		debounce = dreamingDefaultDebounce
	}
	if v, ok := w.lastRun.Load(key); ok {
		if last, ok := v.(time.Time); ok && time.Since(last) < debounce {
			slog.Debug("dreaming: debounce skip", "agent", agentID, "user", userID)
			return nil
		}
	}

	// Count unpromoted entries; skip if below threshold.
	threshold := w.threshold
	if threshold <= 0 {
		threshold = dreamingDefaultThreshold
	}
	count, err := w.episodicStore.CountUnpromoted(ctx, agentID, userID)
	if err != nil {
		slog.Warn("dreaming: count unpromoted failed", "err", err, "agent", agentID)
		return nil
	}
	if count < threshold {
		slog.Debug("dreaming: below threshold", "count", count, "threshold", threshold, "agent", agentID)
		return nil
	}

	// Fetch unpromoted entries.
	entries, err := w.episodicStore.ListUnpromoted(ctx, agentID, userID, dreamingFetchLimit)
	if err != nil {
		slog.Warn("dreaming: list unpromoted failed", "err", err, "agent", agentID)
		return nil
	}
	if len(entries) == 0 {
		return nil
	}

	// Build LLM prompt and call provider.
	synthesis, err := w.synthesize(ctx, entries)
	if err != nil {
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
func (w *dreamingWorker) synthesize(ctx context.Context, entries []store.EpisodicSummary) (string, error) {
	summaries := make([]string, len(entries))
	for i, e := range entries {
		summaries[i] = e.Summary
	}
	body := strings.Join(summaries, "\n---\n")

	resp, err := w.provider.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: dreamingSystemPrompt},
			{Role: "user", Content: "Session summaries:\n---\n" + body + "\n---"},
		},
		Model: w.model,
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
