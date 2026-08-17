package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestPruneStage_CompactionCapReached_SkipsCompactionEmitsNudge verifies that
// once a session has compacted up to MaxCompactionsPerSession, PruneStage stops
// calling CompactMessages, emits a user-facing nudge instead of aborting, and
// leaves CompactionCount unchanged.
func TestPruneStage_CompactionCapReached_SkipsCompactionEmitsNudge(t *testing.T) {
	t.Parallel()
	compactCalls := 0
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 1000,
			MaxTokens:     100,
			Compaction: &config.CompactionConfig{
				MaxCompactionsPerSession: 3,
			},
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 100},
		PruneMessages: func(msgs []providers.Message, _ int) ([]providers.Message, PruneStats) {
			return msgs, PruneStats{} // no reduction — force Phase 2 path
		},
		CompactMessages: func(_ context.Context, msgs []providers.Message, _ string) ([]providers.Message, error) {
			compactCalls++
			return msgs[:1], nil
		},
	}
	stage := NewPruneStage(deps, NewMemoryFlushStage(deps))
	state := defaultState()
	// Session already at the cap: 3 previous compactions.
	state.Compact.CompactionCount = 3

	history := make([]providers.Message, 50)
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue (cap must not abort the run)", stage.Result())
	}
	if compactCalls != 0 {
		t.Errorf("CompactMessages called %d times, want 0 once the cap is reached", compactCalls)
	}
	if state.Compact.CompactionCount != 3 {
		t.Errorf("CompactionCount = %d, want 3 (unchanged)", state.Compact.CompactionCount)
	}
	if state.Prune.MidLoopCompacted {
		t.Error("MidLoopCompacted should be false (no compaction ran)")
	}

	// The nudge must be present in pending messages.
	pending := state.Messages.Pending()
	found := false
	for _, msg := range pending {
		if msg.Role == "user" && strings.Contains(msg.Content, "compaction budget") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a compaction-cap nudge in pending messages, got %#v", pending)
	}
}

// TestPruneStage_CompactionBelowCap_StillCompacts verifies that a session below
// the cap behaves exactly as before — compaction runs and CompactionCount rises.
func TestPruneStage_CompactionBelowCap_StillCompacts(t *testing.T) {
	t.Parallel()
	compactCalls := 0
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 1000,
			MaxTokens:     100,
			Compaction: &config.CompactionConfig{
				MaxCompactionsPerSession: 3,
			},
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 100},
		PruneMessages: func(msgs []providers.Message, _ int) ([]providers.Message, PruneStats) {
			return msgs, PruneStats{} // no reduction
		},
		CompactMessages: func(_ context.Context, msgs []providers.Message, _ string) ([]providers.Message, error) {
			compactCalls++
			return msgs[:1], nil
		},
	}
	stage := NewPruneStage(deps, NewMemoryFlushStage(deps))
	state := defaultState()
	// Session below the cap: only 1 prior compaction.
	state.Compact.CompactionCount = 1

	history := make([]providers.Message, 50)
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue", stage.Result())
	}
	if compactCalls != 1 {
		t.Errorf("CompactMessages called %d times, want 1", compactCalls)
	}
	if state.Compact.CompactionCount != 2 {
		t.Errorf("CompactionCount = %d, want 2", state.Compact.CompactionCount)
	}
	if !state.Prune.MidLoopCompacted {
		t.Error("MidLoopCompacted should be true after a real compaction")
	}
}

// TestPruneStage_CompactionCapZero_LegacyUnlimited verifies that cap == 0 (the
// zero-value / legacy config) preserves the old behaviour exactly: compaction
// runs, CompactionCount rises, and no nudge is emitted.
func TestPruneStage_CompactionCapZero_LegacyUnlimited(t *testing.T) {
	t.Parallel()
	compactCalls := 0
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 1000,
			MaxTokens:     100,
			// Compaction nil → cap resolves to 0 → unlimited legacy.
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 100},
		PruneMessages: func(msgs []providers.Message, _ int) ([]providers.Message, PruneStats) {
			return msgs, PruneStats{}
		},
		CompactMessages: func(_ context.Context, msgs []providers.Message, _ string) ([]providers.Message, error) {
			compactCalls++
			return msgs[:1], nil
		},
	}
	stage := NewPruneStage(deps, NewMemoryFlushStage(deps))
	state := defaultState()
	state.Compact.CompactionCount = 99 // well past any plausible cap

	history := make([]providers.Message, 50)
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue", stage.Result())
	}
	if compactCalls != 1 {
		t.Errorf("CompactMessages called %d times, want 1 (unlimited legacy)", compactCalls)
	}
	if state.Compact.CompactionCount != 100 {
		t.Errorf("CompactionCount = %d, want 100 (incremented)", state.Compact.CompactionCount)
	}
	if len(state.Messages.Pending()) != 0 {
		t.Errorf("pending = %#v, want empty (no nudge in legacy mode)", state.Messages.Pending())
	}
}

// TestPruneStage_CompactionCapReached_StillOverBudget_NoAbort verifies the most
// important safety property: when the cap is reached and history is STILL over
// budget after memory flush, PruneStage continues (never aborts) purely because
// of the cap — the run keeps going with the nudge.
func TestPruneStage_CompactionCapReached_StillOverBudget_NoAbort(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 1000,
			MaxTokens:     100,
			Compaction: &config.CompactionConfig{
				MaxCompactionsPerSession: 2,
			},
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 100},
		PruneMessages: func(msgs []providers.Message, _ int) ([]providers.Message, PruneStats) {
			return msgs, PruneStats{} // no reduction — stays over budget
		},
		CompactMessages: func(_ context.Context, msgs []providers.Message, _ string) ([]providers.Message, error) {
			// Would never be reached — cap is hit first.
			return msgs, nil
		},
	}
	stage := NewPruneStage(deps, NewMemoryFlushStage(deps))
	state := defaultState()
	state.Compact.CompactionCount = 2 // at the cap

	history := make([]providers.Message, 50) // 5000 tokens >> budget 900
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue (must never AbortRun purely due to the cap)", stage.Result())
	}
	if len(state.Messages.Pending()) == 0 {
		t.Error("expected a nudge in pending messages when still over budget at the cap")
	}
}
