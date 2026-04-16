package pipeline

import (
	"context"
	"log/slog"
)

// ObserveStage runs per iteration after ToolStage. Drains InjectCh,
// accumulates final content when no tool calls, tracks block replies.
// Does NOT implement StageWithResult — never controls flow.
type ObserveStage struct {
	deps *PipelineDeps
}

// NewObserveStage creates an ObserveStage.
func NewObserveStage(deps *PipelineDeps) *ObserveStage {
	return &ObserveStage{deps: deps}
}

func (s *ObserveStage) Name() string { return "observe" }

// Execute drains injected messages, accumulates final content + block replies.
func (s *ObserveStage) Execute(_ context.Context, state *RunState) error {
	// 1. Drain InjectCh (non-blocking) — messages from tool side effects, subagent results
	if s.deps.DrainInjectCh != nil {
		for _, msg := range s.deps.DrainInjectCh() {
			state.Messages.AppendPending(msg)
		}
	}

	resp := state.Think.LastResponse
	if resp == nil {
		return nil
	}

	// 2. Track block replies for intermediate responses only (with tool calls).
	// Final answers (no tool calls) are delivered through the normal consumer path,
	// not via block.reply — counting them here causes false dedup suppression.
	if resp.Content != "" && len(resp.ToolCalls) > 0 {
		state.Observe.BlockReplies++
		state.Observe.LastBlockReply = resp.Content
	}

	// 3. Accumulate final content when no tool calls (final answer)
	if len(resp.ToolCalls) == 0 {
		state.Observe.FinalContent = resp.Content
		state.Observe.FinalThinking = resp.Thinking
		// Fallback: some providers (e.g. MiniMax M2.7) return response text in
		// reasoning_content instead of content. Use thinking as FinalContent.
		if state.Observe.FinalContent == "" && state.Observe.FinalThinking != "" {
			state.Observe.FinalContent = state.Observe.FinalThinking
			state.Observe.FinalThinking = ""
		}
	}

	// 4. Warn when the final LLM response is completely empty.
	if len(resp.ToolCalls) == 0 && resp.Content == "" && resp.Thinking == "" {
		slog.Warn("pipeline observe: final LLM response has empty content and thinking",
			"finish_reason", resp.FinishReason,
		)
	}

	return nil
}
