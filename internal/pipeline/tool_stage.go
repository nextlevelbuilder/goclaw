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

func (s *ToolStage) Name() string        { return "tool" }
func (s *ToolStage) Result() StageResult { return s.result }

// Execute extracts tool calls, dispatches them, checks exit conditions.
func (s *ToolStage) Execute(ctx context.Context, state *RunState) error {
	s.result = Continue

	resp := state.Think.LastResponse
	if resp == nil {
		return nil
	}

	toolCalls := resp.ToolCalls

	if executor := state.Tool.StreamExecutor; executor != nil {
		if executor.Count() > 0 {
			if err := s.executeStreaming(ctx, state, toolCalls); err != nil {
				return err
			}
			return nil
		}
		state.Tool.StreamExecutor = nil
		state.Tool.StreamedToolIDs = nil
	}

	if len(toolCalls) == 0 {
		return nil // no tools — ThinkStage already set BreakLoop
	}
	if s.deps.ExecuteToolCall == nil {
		return fmt.Errorf("ExecuteToolCall callback not configured")
	}

	return s.executeBatches(ctx, state, toolCalls)
}

func (s *ToolStage) executeBatches(ctx context.Context, state *RunState, toolCalls []providers.ToolCall) error {
	batches := PartitionToolCalls(toolCalls, s.deps.IsToolConcurrencySafe, s.deps.Config.MaxToolConcurrency)
	if len(batches) == 0 {
		batches = []ToolBatch{{Calls: toolCalls}}
	}

	for _, batch := range batches {
		var err error
		switch {
		case batch.IsConcurrent && len(batch.Calls) > 1 && s.deps.ExecuteToolRaw != nil && s.deps.ProcessToolResult != nil:
			err = s.executeParallelBatch(ctx, state, batch.Calls)
		default:
			err = s.executeSequential(ctx, state, batch.Calls)
		}
		if err != nil {
			return err
		}
		if s.result == BreakLoop || state.Turn.ForceAnswerOnly {
			return nil
		}
	}

	s.checkExitConditions(state)
	return nil
}

func (s *ToolStage) executeSequential(ctx context.Context, state *RunState, toolCalls []providers.ToolCall) error {
	for _, tc := range toolCalls {
		if tracker := state.EnsureNoveltyTracker(); tracker.CheckExactRepeat(tc.Name, tc.Arguments) {
			blockedCount := state.EnsureConstraintStore().RecordBlocked(tc.Name)
			state.Messages.AppendPending(providers.Message{
				Role:    "system",
				Content: fmt.Sprintf("[Tool %q blocked] exact duplicate of an earlier call in this turn. Do not retry it. Choose a different approach.", tc.Name),
			})
			state.Tool.TotalToolCalls++
			s.checkConstraintTransition(state, nil, blockedCount)
			if state.Turn.ForceAnswerOnly {
				return nil
			}
			continue
		}
		if blocked, constraint := state.EnsureConstraintStore().Check(tc.Name, tc.Arguments); blocked {
			blockedCount := state.EnsureConstraintStore().RecordBlocked(tc.Name)
			state.Messages.AppendPending(providers.Message{
				Role:    "system",
				Content: fmt.Sprintf("[Tool %q blocked] %s: %s. Do not retry this path. Choose an alternative approach or answer from gathered evidence.", tc.Name, constraint.Kind, constraint.Message),
			})
			state.Tool.TotalToolCalls++
			s.checkConstraintTransition(state, constraint, blockedCount)
			if state.Turn.ForceAnswerOnly {
				return nil
			}
			continue
		}
		state.EnsureConstraintStore().RecordAllowed()

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
		if state.Turn.ForceAnswerOnly {
			return nil
		}
	}

	s.checkExitConditions(state)
	return nil
}

// executeParallelBatch runs tool I/O concurrently, then processes results sequentially.
func (s *ToolStage) executeParallelBatch(ctx context.Context, state *RunState, toolCalls []providers.ToolCall) error {
	type rawResult struct {
		tc      providers.ToolCall
		msg     providers.Message
		rawData any
		err     error
	}

	execCtx, abortCtrl := NewSiblingAbortController(ctx)
	results := make([]rawResult, len(toolCalls))

	var wg sync.WaitGroup
	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, tc providers.ToolCall) {
			defer wg.Done()
			msg, rawData, err := s.deps.ExecuteToolRaw(execCtx, tc)
			if err != nil {
				abortCtrl.ToolErrored(tc.Name, err)
			}
			results[idx] = rawResult{tc: tc, msg: msg, rawData: rawData, err: err}
		}(i, tc)
	}
	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			return fmt.Errorf("execute tool %s: %w", r.tc.Name, r.err)
		}
		if err := s.processRawResult(ctx, state, r.tc, r.msg, r.rawData); err != nil {
			return err
		}
		if s.result == BreakLoop || state.Turn.ForceAnswerOnly {
			return nil
		}
	}

	s.checkExitConditions(state)
	return nil
}

func (s *ToolStage) executeStreaming(ctx context.Context, state *RunState, toolCalls []providers.ToolCall) error {
	executor := state.Tool.StreamExecutor
	if executor == nil {
		return nil
	}

	executed := state.Tool.StreamedToolIDs
	if executed == nil {
		executed = make(map[string]bool)
	}

	for update := range executor.Done() {
		if update.Err != nil {
			return fmt.Errorf("execute tool %s: %w", update.Call.Name, update.Err)
		}
		if err := s.processRawResult(ctx, state, update.Call, update.RawMsg, update.RawData); err != nil {
			return err
		}
		executed[update.Call.ID] = true
		if s.result == BreakLoop || state.Turn.ForceAnswerOnly {
			executor.Cancel()
			state.Tool.StreamExecutor = nil
			state.Tool.StreamedToolIDs = executed
			return nil
		}
	}

	state.Tool.StreamExecutor = nil
	state.Tool.StreamedToolIDs = executed

	if len(toolCalls) == 0 {
		s.checkExitConditions(state)
		return nil
	}

	remaining := make([]providers.ToolCall, 0, len(toolCalls))
	for _, tc := range toolCalls {
		if !executed[tc.ID] {
			remaining = append(remaining, tc)
		}
	}
	if len(remaining) == 0 {
		s.checkExitConditions(state)
		return nil
	}

	return s.executeBatches(ctx, state, remaining)
}

func (s *ToolStage) processRawResult(
	ctx context.Context,
	state *RunState,
	tc providers.ToolCall,
	rawMsg providers.Message,
	rawData any,
) error {
	processed := s.deps.ProcessToolResult(ctx, state, tc, rawMsg, rawData)
	for _, msg := range processed {
		state.Messages.AppendPending(msg)
	}
	state.Tool.TotalToolCalls++
	if state.Tool.LoopKilled {
		s.result = BreakLoop
	}
	return nil
}

func (s *ToolStage) checkConstraintTransition(state *RunState, constraint *Constraint, blockedCount int) {
	if state == nil {
		return
	}
	if constraint != nil &&
		constraint.Severity == SeverityHard &&
		constraint.Resolution == ResolutionHumanRequired {
		state.Turn.ArmNeedsHuman(TurnCloseoutReasonConstraintNeedsHuman)
		return
	}
	if blockedCount >= 2 && !state.Turn.ForceAnswerOnly {
		state.Turn.ArmCloseout(TurnCloseoutReasonNoProgressLoop)
	}
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
		if !state.Turn.ForceAnswerOnly {
			state.Turn.ArmCloseout(TurnCloseoutReasonToolBudgetExhausted)
			state.Turn.CloseoutHintInjected = true
			state.Messages.AppendPending(providers.Message{
				Role:    "system",
				Content: state.Turn.CloseoutInstruction(),
			})
			return
		}
		s.result = BreakLoop
	}
}
