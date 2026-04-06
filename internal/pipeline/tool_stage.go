package pipeline

import (
	"context"
	"fmt"
)

// ToolStage runs per iteration after PruneStage. Executes tool calls from
// ThinkState.LastResponse, checks exit conditions (loop kill, read-only streak, budget).
type ToolStage struct {
	deps   *PipelineDeps
	result StageResult
}

// NewToolStage creates a ToolStage.
func NewToolStage(deps *PipelineDeps) *ToolStage {
	return &ToolStage{deps: deps, result: Continue}
}

func (s *ToolStage) Name() string       { return "tool" }
func (s *ToolStage) Result() StageResult { return s.result }

// Execute extracts tool calls, dispatches them, checks exit conditions.
func (s *ToolStage) Execute(ctx context.Context, state *RunState) error {
	s.result = Continue

	resp := state.Think.LastResponse
	if resp == nil || len(resp.ToolCalls) == 0 {
		return nil // no tools — ThinkStage already set BreakLoop
	}

	toolCalls := resp.ToolCalls
	if s.deps.ExecuteToolCall == nil {
		return fmt.Errorf("ExecuteToolCall callback not configured")
	}

	// Execute tools sequentially to avoid data races on shared state
	// (loop detector, media results, deliverables mutated by callbacks).
	// Parallel execution can be re-added with per-goroutine state isolation.
	for _, tc := range toolCalls {
		msgs, err := s.deps.ExecuteToolCall(ctx, state, tc)
		if err != nil {
			return fmt.Errorf("execute tool %s: %w", tc.Name, err)
		}
		for _, msg := range msgs {
			state.Messages.AppendPending(msg)
		}
		state.Tool.TotalToolCalls++

		// Check exit conditions after each tool (loop kill may fire mid-batch)
		if state.Tool.LoopKilled {
			s.result = BreakLoop
			return nil
		}
	}

	// Exit condition checks
	s.checkExitConditions(state)

	return nil
}

// checkExitConditions checks read-only streak and tool budget.
func (s *ToolStage) checkExitConditions(state *RunState) {
	// Exit condition #4: loop detector triggered critical
	if state.Tool.LoopKilled {
		s.result = BreakLoop
		return
	}

	// Exit condition #5: read-only streak
	if s.deps.CheckReadOnly != nil {
		warningMsg, shouldBreak := s.deps.CheckReadOnly(state)
		if warningMsg != nil {
			state.Messages.AppendPending(*warningMsg)
		}
		if shouldBreak {
			s.result = BreakLoop
			return
		}
	}

	// Exit condition #6: tool budget exceeded
	if s.deps.Config.MaxToolCalls > 0 && state.Tool.TotalToolCalls >= s.deps.Config.MaxToolCalls {
		s.result = BreakLoop
	}
}
