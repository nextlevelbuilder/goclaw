package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	maxSequentialWaitBatchMs     = 300000
	defaultParallelToolCallLimit = 4
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
	if resp == nil || len(resp.ToolCalls) == 0 {
		return nil // no tools — ThinkStage already set BreakLoop
	}

	toolCalls := resp.ToolCalls
	if s.deps.ExecuteToolCall == nil {
		return fmt.Errorf("ExecuteToolCall callback not configured")
	}

	// Parallel path: separate I/O (parallel) from state mutation (sequential).
	// Requires both ExecuteToolRaw and ProcessToolResult callbacks.
	if len(toolCalls) > 1 && s.canExecuteParallel(toolCalls) && !s.batchExceedsBudget(state, toolCalls) {
		preflight := s.preflightToolCalls(ctx, state, toolCalls)
		return s.executeParallel(ctx, state, preflight)
	}

	// Sequential fallback: ExecuteToolCall handles both I/O and state mutation.
	cumulativeWaitMs := 0
	for _, tc := range toolCalls {
		if s.shouldStopBeforeTool(ctx, state) {
			return nil
		}
		if s.deps.SequentialToolCall != nil && s.deps.SequentialToolCall(tc) {
			cumulativeWaitMs += toolCallTimeMs(tc)
			if cumulativeWaitMs > maxSequentialWaitBatchMs {
				s.result = AbortRun
				return nil
			}
		}

		tc, blocked := s.preflightToolCall(ctx, state, tc)
		if blocked != nil {
			state.Messages.AppendPending(*blocked)
			state.Tool.TotalToolCalls++
			continue
		}

		msgs, err := s.deps.ExecuteToolCall(ctx, state, tc)
		if err != nil {
			return fmt.Errorf("execute tool %s: %w", tc.Name, err)
		}
		for _, msg := range msgs {
			state.Messages.AppendPending(msg)
		}
		state.Tool.TotalToolCalls++

		// Hook: async PostToolUse — fire and forget with detached context.
		if s.deps.Hooks != nil {
			detached := context.WithoutCancel(ctx)
			go s.deps.FireHook(detached, hooks.Event{ //nolint:errcheck
				EventID:   uuid.NewString(),
				SessionID: state.Input.SessionKey,
				TenantID:  store.TenantIDFromContext(ctx),
				AgentID:   store.AgentIDFromContext(ctx),
				ToolName:  tc.Name,
				ToolInput: tc.Arguments,
				HookEvent: hooks.EventPostToolUse,
			})
		}

		if state.Tool.LoopKilled {
			s.result = BreakLoop
			return nil
		}
		if ctx.Err() != nil {
			s.result = AbortRun
			return nil
		}
	}

	s.checkExitConditions(state)
	return nil
}

func (s *ToolStage) requiresSequential(toolCalls []providers.ToolCall) bool {
	if s.deps.SequentialToolCall == nil {
		return false
	}
	return slices.ContainsFunc(toolCalls, s.deps.SequentialToolCall)
}

type toolCallPreflightItem struct {
	index   int
	tc      providers.ToolCall
	blocked *providers.Message
}

type toolCallPreflight struct {
	ordered    []toolCallPreflightItem
	executable []toolCallPreflightItem
}

func (s *ToolStage) preflightToolCalls(ctx context.Context, state *RunState, toolCalls []providers.ToolCall) toolCallPreflight {
	out := toolCallPreflight{
		ordered: make([]toolCallPreflightItem, 0, len(toolCalls)),
	}
	for i, tc := range toolCalls {
		tc, blocked := s.preflightToolCall(ctx, state, tc)
		item := toolCallPreflightItem{index: i, tc: tc, blocked: blocked}
		out.ordered = append(out.ordered, item)
		if blocked == nil {
			out.executable = append(out.executable, item)
		}
	}
	return out
}

func (s *ToolStage) preflightToolCall(ctx context.Context, state *RunState, tc providers.ToolCall) (providers.ToolCall, *providers.Message) {
	if s.deps.AuthorizeToolCall != nil {
		if ok, reason := s.deps.AuthorizeToolCall(ctx, state, tc); !ok {
			return tc, &providers.Message{
				Role:       "tool",
				Content:    reason,
				ToolCallID: tc.ID,
				IsError:    true,
			}
		}
	}
	r, _ := s.deps.FireHook(ctx, hooks.Event{
		EventID:   uuid.NewString(),
		SessionID: state.Input.SessionKey,
		TenantID:  store.TenantIDFromContext(ctx),
		AgentID:   store.AgentIDFromContext(ctx),
		ToolName:  tc.Name,
		ToolInput: tc.Arguments,
		HookEvent: hooks.EventPreToolUse,
	})
	if r.Decision == hooks.DecisionBlock {
		return tc, &providers.Message{
			Role:       "tool",
			Content:    "Hook blocked: pre_tool_use",
			ToolCallID: tc.ID,
		}
	}
	if r.UpdatedToolInput != nil {
		tc.Arguments = r.UpdatedToolInput
	}
	return tc, nil
}

func (s *ToolStage) canExecuteParallel(toolCalls []providers.ToolCall) bool {
	if s.deps.ExecuteToolRaw == nil || s.deps.ProcessToolResult == nil || s.deps.ParallelEligibleToolCall == nil {
		return false
	}
	if s.requiresSequential(toolCalls) {
		return false
	}
	for _, tc := range toolCalls {
		if !s.deps.ParallelEligibleToolCall(tc) {
			return false
		}
	}
	return true
}

func (s *ToolStage) batchExceedsBudget(state *RunState, toolCalls []providers.ToolCall) bool {
	return s.deps.Config.MaxToolCalls > 0 && state.Tool.TotalToolCalls+len(toolCalls) > s.deps.Config.MaxToolCalls
}

func (s *ToolStage) shouldStopBeforeTool(ctx context.Context, state *RunState) bool {
	if ctx.Err() != nil {
		s.result = AbortRun
		return true
	}
	if s.deps.Config.MaxToolCalls > 0 && state.Tool.TotalToolCalls >= s.deps.Config.MaxToolCalls {
		s.result = BreakLoop
		return true
	}
	return false
}

func toolCallTimeMs(tc providers.ToolCall) int {
	v, ok := tc.Arguments["timeMs"]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, err := strconv.Atoi(n.String())
		if err == nil {
			return i
		}
	}
	return 0
}

// executeParallel runs tool I/O concurrently, then processes results sequentially.
func (s *ToolStage) executeParallel(ctx context.Context, state *RunState, preflight toolCallPreflight) error {
	type rawResult struct {
		index   int
		tc      providers.ToolCall
		msg     providers.Message
		rawData any
		err     error
	}

	startedAt := time.Now()
	slog.Info("tool.parallel.batch.start",
		"session", state.Input.SessionKey,
		"run_id", state.RunID,
		"count", len(preflight.executable),
		"limit", defaultParallelToolCallLimit)

	// Phase 1: parallel I/O (no state mutation)
	results := make([]rawResult, len(preflight.executable))
	sem := make(chan struct{}, defaultParallelToolCallLimit)
	var wg sync.WaitGroup
	for i, item := range preflight.executable {
		wg.Add(1)
		go func(idx int, item toolCallPreflightItem) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = rawResult{index: item.index, tc: item.tc, err: ctx.Err()}
				return
			}
			msg, rawData, err := s.deps.ExecuteToolRaw(ctx, item.tc)
			results[idx] = rawResult{index: item.index, tc: item.tc, msg: msg, rawData: rawData, err: err}
		}(i, item)
	}
	wg.Wait()
	slog.Info("tool.parallel.batch.end",
		"session", state.Input.SessionKey,
		"run_id", state.RunID,
		"count", len(preflight.executable),
		"limit", defaultParallelToolCallLimit,
		"duration_ms", time.Since(startedAt).Milliseconds())

	// Phase 2: sequential state mutation (safe, deterministic order)
	resultByIndex := make(map[int]rawResult, len(results))
	for _, r := range results {
		resultByIndex[r.index] = r
	}
	for _, item := range preflight.ordered {
		tc := item.tc
		if item.blocked != nil {
			state.Messages.AppendPending(*item.blocked)
			state.Tool.TotalToolCalls++
			continue
		}
		r, ok := resultByIndex[item.index]
		if !ok {
			return fmt.Errorf("execute tool %s: missing parallel result", tc.Name)
		}
		if r.err != nil {
			return fmt.Errorf("execute tool %s: %w", tc.Name, r.err)
		}
		processed := s.deps.ProcessToolResult(ctx, state, tc, r.msg, r.rawData)
		for _, msg := range processed {
			state.Messages.AppendPending(msg)
		}
		state.Tool.TotalToolCalls++

		// Hook: async PostToolUse for parallel path — fire and forget.
		// PreToolUse already ran in preflight before any raw I/O was scheduled.
		if s.deps.Hooks != nil {
			detached := context.WithoutCancel(ctx)
			go s.deps.FireHook(detached, hooks.Event{ //nolint:errcheck
				EventID:   uuid.NewString(),
				SessionID: state.Input.SessionKey,
				TenantID:  store.TenantIDFromContext(ctx),
				AgentID:   store.AgentIDFromContext(ctx),
				ToolName:  tc.Name,
				ToolInput: tc.Arguments,
				HookEvent: hooks.EventPostToolUse,
			})
		}

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
