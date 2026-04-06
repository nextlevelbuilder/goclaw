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

	// Execute tools: single or parallel
	var allMsgs []providers.Message
	if len(toolCalls) == 1 {
		msgs, err := s.deps.ExecuteToolCall(ctx, state, toolCalls[0])
		if err != nil {
			return fmt.Errorf("execute tool %s: %w", toolCalls[0].Name, err)
		}
		allMsgs = msgs
	} else {
		msgs, err := s.executeParallel(ctx, state, toolCalls)
		if err != nil {
			return err
		}
		allMsgs = msgs
	}

	// Append all tool result messages to pending buffer
	for _, msg := range allMsgs {
		state.Messages.AppendPending(msg)
	}
	state.Tool.TotalToolCalls += len(toolCalls)

	// Exit condition checks
	s.checkExitConditions(state)

	return nil
}

// executeParallel runs multiple tool calls concurrently, returns messages in original order.
// Each goroutine receives only ctx + tc — no shared RunState mutation.
func (s *ToolStage) executeParallel(ctx context.Context, state *RunState, toolCalls []providers.ToolCall) ([]providers.Message, error) {
	type indexedResult struct {
		msgs []providers.Message
		err  error
	}

	var wg sync.WaitGroup
	results := make([]indexedResult, len(toolCalls))

	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, tc providers.ToolCall) {
			defer wg.Done()
			msgs, err := s.deps.ExecuteToolCall(ctx, state, tc)
			results[idx] = indexedResult{msgs: msgs, err: err}
		}(i, tc)
	}
	wg.Wait()

	// Results already in order (indexed by position), collect sequentially
	var allMsgs []providers.Message
	for i, r := range results {
		if r.err != nil {
			return nil, fmt.Errorf("parallel tool %d: %w", i, r.err)
		}
		allMsgs = append(allMsgs, r.msgs...)
	}
	return allMsgs, nil
}

// checkExitConditions checks loop kill, read-only streak, and tool budget.
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
