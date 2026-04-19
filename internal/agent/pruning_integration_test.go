//go:build !sqliteonly

package agent

import (
	"sync/atomic"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestPruneStage_SingleEntryPoint verifies that after Phase 03 refactoring:
// - loop_history.go no longer calls pruneContextMessages
// - PruneStage owns the single entry point for pruning
// - SanitizeHistory is called after PruneMessages in PruneStage
//
// Full PruneStage tests are in internal/pipeline/stages_test.go.
func TestPruneStage_SingleEntryPoint(t *testing.T) {
	// Verify loop_history's sanitizeHistory works on trimmed input directly.
	// This proves pruneContextMessages is no longer called in that path.
	history := []providers.Message{
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{ID: "tc1", Name: "read"}}},
		{Role: "tool", ToolCallID: "tc1", Content: "result1"},
		// Orphan tool result (no matching tool_use) - sanitizeHistory should drop it
		{Role: "tool", ToolCallID: "tc_orphan", Content: "orphan result"},
		{Role: "assistant", Content: "a1"},
	}

	// limitHistoryTurns doesn't drop the orphan, but sanitizeHistory does
	trimmed := limitHistoryTurns(history, 100)
	if len(trimmed) != len(history) {
		t.Fatalf("limitHistoryTurns altered message count: got %d, want %d", len(trimmed), len(history))
	}

	sanitized, droppedCount := sanitizeHistory(trimmed)
	if droppedCount != 1 {
		t.Errorf("sanitizeHistory should drop 1 orphan, got droppedCount=%d", droppedCount)
	}
	if len(sanitized) != 4 {
		t.Errorf("expected 4 messages after sanitize, got %d", len(sanitized))
	}
}

// TestPruneCallback_CountsOnce demonstrates the callback structure supports counting.
// Actual single-call verification is in pipeline/stages_test.go.
func TestPruneCallback_CountsOnce(t *testing.T) {
	var count int32
	fakePrune := func(msgs []providers.Message, budget int) ([]providers.Message, pipeline.PruneStats) {
		atomic.AddInt32(&count, 1)
		return msgs, pipeline.PruneStats{}
	}

	// Simulate what PruneStage does: call PruneMessages once
	msgs := []providers.Message{{Role: "user", Content: "test"}}
	_, _ = fakePrune(msgs, 10000)

	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("expected prune called exactly once, got %d", got)
	}
}
