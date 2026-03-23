package channels

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// CompactionConfig configures LLM-based history compaction.
type CompactionConfig struct {
	Threshold  int                // trigger compaction when entries exceed this (default 100)
	KeepRecent int                // keep this many recent raw messages (default 15)
	MaxTokens  int                // max output tokens for summarization (default 4096)
	Provider   providers.Provider // LLM provider for summarization
	Model      string             // model to use for summarization
}

// MaybeCompact checks if compaction is needed for a history key and triggers it in background.
// Called from Record() after appending. Thread-safe via sync.Map compaction guard.
// Uses DB count (not RAM count) to correctly detect threshold after server restarts.
func (ph *PendingHistory) MaybeCompact(historyKey string, currentCount int, cfg *CompactionConfig) {
	if ph.store == nil || cfg == nil || cfg.Provider == nil {
		return
	}
	threshold := cfg.Threshold
	if threshold <= 0 {
		threshold = DefaultGroupHistoryLimit
	}

	// RAM count may be stale after restart (LoadFromDB doesn't warm full history).
	// If RAM says below threshold, ask DB for the real count.
	if currentCount <= threshold {
		ctx, cancel := context.WithTimeout(ph.tenantCtx(), 10*time.Second)
		defer cancel()
		dbCount, err := ph.store.CountByKey(ctx, ph.channelName, historyKey)
		if err != nil || dbCount <= threshold {
			return
		}
	}

	// Guard: only one compaction per key at a time
	if _, loaded := ph.compacting.LoadOrStore(historyKey, true); loaded {
		return
	}
	go ph.runCompaction(historyKey, cfg)
}

// CompactGroup performs LLM-based compaction on a pending message group.
// Reused by both auto-compact (channel) and HTTP compact endpoint.
// Returns the number of entries remaining after compaction.
func CompactGroup(ctx context.Context, s store.PendingMessageStore, channelName, historyKey string, provider providers.Provider, model string, keepRecent, maxTokens int) (int, error) {
	if keepRecent <= 0 {
		keepRecent = 15
	}
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	// Step 1: Read entries from DB
	entries, err := s.ListByKey(ctx, channelName, historyKey)
	if err != nil {
		return 0, fmt.Errorf("list entries: %w", err)
	}
	if keepRecent >= len(entries) {
		return len(entries), nil // nothing to summarize
	}

	// Step 2: Split into old (to summarize) and recent (to keep)
	splitIdx := len(entries) - keepRecent
	toSummarize := entries[:splitIdx]
	deleteIDs := make([]uuid.UUID, len(toSummarize))
	for i, e := range toSummarize {
		deleteIDs[i] = e.ID
	}

	// Step 3: Build text and call LLM for summarization
	var sb strings.Builder
	for _, e := range toSummarize {
		prefix := e.Sender
		if e.IsSummary {
			prefix = "[previous summary]"
		}
		ts := ""
		if !e.CreatedAt.IsZero() {
			ts = fmt.Sprintf(" [%s]", e.CreatedAt.Format("15:04"))
		}
		fmt.Fprintf(&sb, "%s%s: %s\n", prefix, ts, e.Body)
	}

	resp, err := provider.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{{
			Role:    "user",
			Content: "Summarize these group chat messages concisely, preserving key topics, decisions, names, and important context:\n\n" + sb.String(),
		}},
		Model:   model,
		Options: map[string]any{"max_tokens": maxTokens, "temperature": 0.3},
	})
	if err != nil {
		return 0, fmt.Errorf("llm summarize: %w", err)
	}

	// Step 4: Compact in DB (atomic tx: delete old + insert summary)
	summary := &store.PendingMessage{
		ChannelName: channelName,
		HistoryKey:  historyKey,
		Sender:      "[summary]",
		Body:        resp.Content,
		IsSummary:   true,
	}
	if err := s.Compact(ctx, deleteIDs, summary); err != nil {
		return 0, fmt.Errorf("db compact: %w", err)
	}

	remaining := keepRecent + 1 // kept messages + new summary
	slog.Info("compaction.done",
		"channel", channelName,
		"key", historyKey,
		"summarized", len(toSummarize),
		"kept", keepRecent,
		"total_after", remaining,
	)
	return remaining, nil
}

// runCompaction performs LLM-based summarization of old messages.
// Wraps CompactGroup with flush + RAM update + threshold check.
func (ph *PendingHistory) runCompaction(historyKey string, cfg *CompactionConfig) {
	defer ph.compacting.Delete(historyKey)

	// Force-flush buffer to ensure DB is consistent
	ph.flushNow()

	ctx, cancel := context.WithTimeout(ph.tenantCtx(), 180*time.Second)
	defer cancel()

	// Check threshold from DB (may have been cleared since trigger)
	entries, err := ph.store.ListByKey(ctx, ph.channelName, historyKey)
	if err != nil {
		slog.Warn("compaction.list_failed", "channel", ph.channelName, "key", historyKey, "error", err)
		return
	}
	threshold := cfg.Threshold
	if threshold <= 0 {
		threshold = DefaultGroupHistoryLimit
	}
	if len(entries) <= threshold {
		return
	}

	_, err = CompactGroup(ctx, ph.store, ph.channelName, historyKey, cfg.Provider, cfg.Model, cfg.KeepRecent, cfg.MaxTokens)
	if err != nil {
		slog.Warn("compaction.failed", "channel", ph.channelName, "key", historyKey, "error", err)
		return
	}

	// Update RAM from DB
	ph.mu.Lock()
	if _, exists := ph.entries[historyKey]; !exists {
		// Key was Clear()ed during compaction — remove stale summary
		ph.mu.Unlock()
		_ = ph.store.DeleteByKey(ctx, ph.channelName, historyKey)
		slog.Info("compaction.cleared_stale", "channel", ph.channelName, "key", historyKey)
		return
	}
	fresh, err := ph.store.ListByKey(ctx, ph.channelName, historyKey)
	if err == nil {
		rebuilt := make([]HistoryEntry, 0, len(fresh))
		for _, f := range fresh {
			rebuilt = append(rebuilt, HistoryEntry{
				Sender:    f.Sender,
				Body:      f.Body,
				Timestamp: f.CreatedAt,
				MessageID: f.PlatformMsgID,
			})
		}
		ph.entries[historyKey] = rebuilt
	}
	ph.mu.Unlock()
}
