package pipeline

import (
	"context"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// MemoryFlushStage flushes memories to long-term storage before compaction.
// NOT registered as a pipeline stage — invoked inline by PruneStage.
type MemoryFlushStage struct {
	deps *PipelineDeps
}

// NewMemoryFlushStage creates a MemoryFlushStage.
func NewMemoryFlushStage(deps *PipelineDeps) *MemoryFlushStage {
	return &MemoryFlushStage{deps: deps}
}

func (s *MemoryFlushStage) Name() string { return "memory_flush" }

// Execute flushes memories via callback. Dedup guard is caller's responsibility.
func (s *MemoryFlushStage) Execute(ctx context.Context, state *RunState) error {
	if s.deps.RunMemoryFlush == nil {
		return nil
	}
	start := time.Now().UTC()
	err := s.deps.RunMemoryFlush(ctx, state)
	tracing.EmitEventSpan(ctx, "memory_flush", start, err, map[string]any{
		"history_tokens": state.Prune.HistoryTokens,
		"budget":         state.Prune.HistoryBudget,
	})
	if err != nil {
		// Memory flush failure is non-fatal — log and continue to compaction.
		slog.Warn("memory flush failed, continuing to compaction", "err", err)
	}
	return nil
}
