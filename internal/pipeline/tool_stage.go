package pipeline

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
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
	if len(toolCalls) > 1 && s.deps.ExecuteToolRaw != nil && s.deps.ProcessToolResult != nil {
		return s.executeParallel(ctx, state, toolCalls)
	}

	// Sequential fallback: ExecuteToolCall handles both I/O and state mutation.
	for _, tc := range toolCalls {
		// Hook: sync PreToolUse — block if hook denies. Builtin-source hooks may
		// rewrite tc.Arguments via UpdatedToolInput (e.g. path-sanitizer); apply
		// before ExecuteToolCall so the rewrite is authoritative.
		if r, _ := s.deps.FireHook(ctx, hooks.Event{
			EventID:   uuid.NewString(),
			SessionID: state.Input.SessionKey,
			TenantID:  store.TenantIDFromContext(ctx),
			AgentID:   store.AgentIDFromContext(ctx),
			ToolName:  tc.Name,
			ToolInput: tc.Arguments,
			HookEvent: hooks.EventPreToolUse,
		}); r.Decision == hooks.DecisionBlock {
			// Inject synthetic blocked tool message and skip actual execution.
			state.Messages.AppendPending(providers.Message{
				Role:       "tool",
				Content:    "Hook blocked: pre_tool_use",
				ToolCallID: tc.ID,
			})
			state.Tool.TotalToolCalls++
			continue
		} else if r.UpdatedToolInput != nil {
			tc.Arguments = r.UpdatedToolInput
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

		// Hook: async PostToolUse for parallel path — fire and forget.
		// PreToolUse is not instrumented in the parallel path (TODO: add when parallel path matures).
		if s.deps.Hooks != nil {
			detached := context.WithoutCancel(ctx)
			go s.deps.FireHook(detached, hooks.Event{ //nolint:errcheck
				EventID:   uuid.NewString(),
				SessionID: state.Input.SessionKey,
				TenantID:  store.TenantIDFromContext(ctx),
				AgentID:   store.AgentIDFromContext(ctx),
				ToolName:  r.tc.Name,
				ToolInput: r.tc.Arguments,
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
