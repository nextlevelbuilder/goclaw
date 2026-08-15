package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// fakeBudgetErr satisfies the contextBudgetExceededError interface that
// ThinkStage detects, standing in for agent.RequestBudgetExceededError without
// importing the agent package (which would be an import cycle).
type fakeBudgetErr struct{}

func (fakeBudgetErr) Error() string               { return "context budget exceeded" }
func (fakeBudgetErr) ContextBudgetExceeded() bool { return true }

// TestThinkStage_BudgetExceeded_ReducesThenRetries verifies that when CallLLM
// returns a request-budget-exceeded error (the central pre-transport guard
// fired), the stage runs a reduction pass and returns Continue to retry the
// iteration rather than aborting.
func TestThinkStage_BudgetExceeded_ReducesThenRetries(t *testing.T) {
	pruned := false
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000, ContextWindow: 200_000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return nil, fakeBudgetErr{}
		},
		PruneMessages: func(msgs []providers.Message, _ int) ([]providers.Message, PruneStats) {
			pruned = true
			// Report a trim so reduceFinalRequestContext sees a change.
			return msgs, PruneStats{ResultsTrimmed: 1}
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "big history"}})

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() should retry after reduction, got error: %v", err)
	}
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue (retry after reduction)", stage.Result())
	}
	if !pruned {
		t.Error("expected PruneMessages to run during budget reduction")
	}
	if state.Think.OverflowRetries != 1 {
		t.Errorf("OverflowRetries = %d, want 1", state.Think.OverflowRetries)
	}
}

// TestThinkStage_BudgetExceeded_AbortsWhenReductionExhausted verifies that when
// no reduction step can change the request, the stage surfaces the budget error
// instead of looping forever. The guard already guaranteed zero transport calls.
func TestThinkStage_BudgetExceeded_AbortsWhenReductionExhausted(t *testing.T) {
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000, ContextWindow: 200_000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return nil, fakeBudgetErr{}
		},
		// No PruneMessages / CompactMessages wired and no memory section, so
		// every reduction step reports "no change".
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	err := stage.Execute(context.Background(), state)
	if err == nil {
		t.Fatal("expected abort when reduction is exhausted, got nil")
	}
	if !errors.As(err, new(interface{ ContextBudgetExceeded() bool })) {
		t.Fatalf("expected wrapped budget error, got %v", err)
	}
}

// TestThinkThenTool_BudgetRetry_DoesNotReexecuteStaleToolCalls is the
// full-pipeline regression guard for the stale-LastResponse hazard: when the
// budget guard rejects iteration N+1's request, ThinkStage retries the
// iteration, but ToolStage runs next in the SAME iteration and reads
// state.Think.LastResponse. If ThinkStage left the PRIOR iteration's response
// in place, ToolStage would re-execute those tool calls a second time. This
// asserts ThinkStage clears LastResponse so ToolStage short-circuits.
func TestThinkThenTool_BudgetRetry_DoesNotReexecuteStaleToolCalls(t *testing.T) {
	executed := 0
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000, ContextWindow: 200_000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return nil, fakeBudgetErr{}
		},
		PruneMessages: func(msgs []providers.Message, _ int) ([]providers.Message, PruneStats) {
			return msgs, PruneStats{ResultsTrimmed: 1} // report a change so reduction "succeeds"
		},
		ExecuteToolCall: func(_ context.Context, _ *RunState, tc providers.ToolCall) ([]providers.Message, error) {
			executed++
			return []providers.Message{{Role: "tool", ToolCallID: tc.ID, Content: "ran"}}, nil
		},
	}
	think := NewThinkStage(deps)
	tool := NewToolStage(deps)
	state := defaultState()
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "big history"}})

	// Simulate a prior iteration having produced a tool-call response that was
	// already executed. It must NOT be executed again when this iteration retries.
	state.Think.LastResponse = &providers.ChatResponse{
		FinishReason: "tool_calls",
		ToolCalls:    []providers.ToolCall{{ID: "stale-1", Name: "write_file", Arguments: map[string]any{"path": "x"}}},
	}

	if err := think.Execute(context.Background(), state); err != nil {
		t.Fatalf("ThinkStage.Execute() should retry, got error: %v", err)
	}
	if state.Think.LastResponse != nil {
		t.Fatal("ThinkStage must clear LastResponse on budget-reduction retry")
	}
	if err := tool.Execute(context.Background(), state); err != nil {
		t.Fatalf("ToolStage.Execute() error: %v", err)
	}
	if executed != 0 {
		t.Fatalf("stale tool call re-executed %d time(s); want 0", executed)
	}
}
