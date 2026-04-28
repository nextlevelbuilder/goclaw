package pipeline

import "context"

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

	// 2. Track block replies — only count tool-iteration responses where
	// EmitBlockReply actually fires (think_stage emits block.reply only when
	// tool calls are present). The final answer (no tool calls) must NOT be
	// counted, otherwise the gateway dedup check falsely suppresses delivery.
	if resp.Content != "" && len(resp.ToolCalls) > 0 {
		state.Observe.BlockReplies++
		state.Observe.LastBlockReply = resp.Content
	}

	// 3. Accumulate final content when no tool calls (final answer)
	if len(resp.ToolCalls) == 0 {
		state.Observe.FinalContent = resp.Content
		state.Observe.FinalThinking = resp.Thinking
	}

	// 4. Accumulate assistant-generated final images across iterations.
	// The LLM may emit image_generation_call in iter N alongside a function_call,
	// then respond text-only in iter N+1 — LastResponse.Images would then be empty
	// at finalize time and the iter-N image would be lost. Drain Images here so
	// FinalizeStage sees every image regardless of which iteration produced it.
	// Partial streaming frames are filtered out at source (codex.go only sets
	// non-partial entries via imageState.recordFinal); a defensive filter here
	// avoids coupling to that invariant.
	if len(resp.Images) > 0 {
		for _, img := range resp.Images {
			if img.Partial {
				continue
			}
			state.Observe.AssistantImages = append(state.Observe.AssistantImages, img)
		}
		// Clear on response so a re-processing pass (e.g. retry) doesn't double-count.
		resp.Images = nil
	}

	return nil
}
