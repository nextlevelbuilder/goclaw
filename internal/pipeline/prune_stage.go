package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// PruneStage runs every iteration. 2-phase pruning:
//   - Phase 1 (70% budget): soft trim via PruneMessages callback
//   - Phase 2 (100% budget): memory flush + LLM compaction
//
// Implements StageWithResult — returns AbortRun if still over budget after compaction.
type PruneStage struct {
	deps        *PipelineDeps
	memoryFlush *MemoryFlushStage
	result      StageResult
}

// NewPruneStage creates a PruneStage with inline memory flush.
func NewPruneStage(deps *PipelineDeps, memFlush *MemoryFlushStage) *PruneStage {
	return &PruneStage{deps: deps, memoryFlush: memFlush, result: Continue}
}

func (s *PruneStage) Name() string       { return "prune" }
func (s *PruneStage) Result() StageResult { return s.result }

// Execute checks history tokens against budget, prunes/compacts as needed.
func (s *PruneStage) Execute(ctx context.Context, state *RunState) error {
	s.result = Continue

	// Compute budget: context window minus overhead (system prompt + context files) minus output reserve
	budget := s.deps.Config.ContextWindow - state.Context.OverheadTokens - s.deps.Config.MaxTokens
	if budget <= 0 {
		return nil // no history budget, nothing to prune
	}
	state.Prune.HistoryBudget = budget

	// Count current history tokens
	historyTokens := s.countHistory(state)
	state.Prune.HistoryTokens = historyTokens

	softThreshold := budget * 70 / 100
	if historyTokens <= softThreshold {
		return nil // under budget, no action needed
	}

	// Phase 1: soft prune at 70% budget
	if s.deps.PruneMessages != nil {
		pruned := s.deps.PruneMessages(state.Messages.History(), budget)
		state.Messages.SetHistory(pruned)
		historyTokens = s.countHistory(state)
		state.Prune.HistoryTokens = historyTokens
	}

	if historyTokens <= budget {
		return nil // under budget after soft prune
	}

	// Phase 2: compaction — flush memories first, then compact
	if !state.Compact.MemoryFlushedThisCycle && s.memoryFlush != nil {
		if err := s.memoryFlush.Execute(ctx, state); err != nil {
			slog.Warn("prune: memory flush error", "err", err)
		}
		state.Compact.MemoryFlushedThisCycle = true
	}

	if s.deps.CompactMessages == nil {
		return nil // no compaction available
	}

	compacted, err := s.deps.CompactMessages(ctx, state.Messages.History(), state.Model)
	if err != nil {
		return fmt.Errorf("compact messages: %w", err)
	}
	state.Messages.ReplaceHistory(compacted)
	state.Prune.MidLoopCompacted = true
	state.Compact.CompactionCount++
	state.Compact.MemoryFlushedThisCycle = false // reset for next cycle

	// Recount after compaction
	historyTokens = s.countHistory(state)
	state.Prune.HistoryTokens = historyTokens

	if historyTokens > budget {
		slog.Warn("still over budget after compaction", "tokens", historyTokens, "budget", budget)
		s.result = AbortRun
	}

	return nil
}

// countHistory counts history + pending tokens via TokenCounter.
func (s *PruneStage) countHistory(state *RunState) int {
	h := state.Messages.History()
	p := state.Messages.Pending()
	if s.deps.TokenCounter == nil || (len(h) == 0 && len(p) == 0) {
		return 0
	}
	// Explicit copy to avoid aliasing the history slice.
	msgs := make([]providers.Message, 0, len(h)+len(p))
	msgs = append(msgs, h...)
	msgs = append(msgs, p...)
	return s.deps.TokenCounter.CountMessages(state.Model, msgs)
}
