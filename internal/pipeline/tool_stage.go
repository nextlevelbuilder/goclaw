package pipeline

import (
	"context"
	"fmt"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
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

	// Parallel path: separate I/O (parallel) from state mutation (sequential).
	// Requires both ExecuteToolRaw and ProcessToolResult callbacks.
	if len(toolCalls) > 1 && s.deps.ExecuteToolRaw != nil && s.deps.ProcessToolResult != nil {
		return s.executeParallel(ctx, state, toolCalls)
	}

	// Sequential fallback: ExecuteToolCall handles both I/O and state mutation.
	for _, tc := range toolCalls {
		msgs, err := s.deps.ExecuteToolCall(ctx, state, tc)
		if err != nil {
			return fmt.Errorf("execute tool %s: %w", tc.Name, err)
		}
		for _, msg := range msgs {
			state.Messages.AppendPending(msg)
		}
		state.Tool.TotalToolCalls++
		if state.Tool.LoopKilled {
			s.result = BreakLoop
			return nil
		}
	}

	s.checkExitConditions(state)
	return nil
}

// executeParallel runs tool I/O concurrently, then processes results sequentially.
func (s *ToolStage) executeParallel(ctx context.Context, state *RunState, toolCalls []providers.ToolCall) error {
	type rawResult struct {
		tc      providers.ToolCall
		msg     providers.Message
		rawData any
		err     error
	}

	// Phase 1: parallel I/O (no state mutation)
	results := make([]rawResult, len(toolCalls))
	var wg sync.WaitGroup
	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, tc providers.ToolCall) {
			defer wg.Done()
			msg, rawData, err := s.deps.ExecuteToolRaw(ctx, tc)
			results[idx] = rawResult{tc: tc, msg: msg, rawData: rawData, err: err}
		}(i, tc)
	}
	wg.Wait()

	// Phase 2: sequential state mutation (safe, deterministic order)
	for _, r := range results {
		if r.err != nil {
			return fmt.Errorf("execute tool %s: %w", r.tc.Name, r.err)
		}
		processed := s.deps.ProcessToolResult(ctx, state, r.tc, r.msg, r.rawData)
		for _, msg := range processed {
			state.Messages.AppendPending(msg)
		}
		state.Tool.TotalToolCalls++
		if state.Tool.LoopKilled {
			s.result = BreakLoop
			return nil
		}
	}

	s.checkExitConditions(state)
	return nil
}

// checkExitConditions checks read-only streak and tool budget.
func (s *ToolStage) checkExitConditions(state *RunState) {
	if state.Tool.LoopKilled {
		s.result = BreakLoop
		return
	}
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
	if s.deps.Config.MaxToolCalls > 0 && state.Tool.TotalToolCalls >= s.deps.Config.MaxToolCalls {
		s.result = BreakLoop
	}
}
