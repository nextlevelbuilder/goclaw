package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

type perItemBudgetCounter struct {
	perMessage int
	perTool    int
	err        error
}

func (c perItemBudgetCounter) CountText(string) (int, error) { return c.perMessage, c.err }
func (c perItemBudgetCounter) CountMessages(msgs []providers.Message) (int, error) {
	return len(msgs) * c.perMessage, c.err
}
func (c perItemBudgetCounter) CountToolSchemas(tools []providers.ToolDefinition) (int, error) {
	return len(tools) * c.perTool, c.err
}
func (c perItemBudgetCounter) CountRequest(req providers.ChatRequest) (int, error) {
	return len(req.Messages)*c.perMessage + len(req.Tools)*c.perTool, c.err
}

// Unregistered models make TokenCounter fall back to a chars/2 heuristic that
// overcounts; PruneStage must budget with the same counter as the request guard.
func TestPruneStage_CountsHistoryWithBudgetCounter(t *testing.T) {
	t.Parallel()
	compactCalls := 0
	deps := &PipelineDeps{
		Config:        PipelineConfig{ContextWindow: 1000, MaxTokens: 100},
		TokenCounter:  &mockTokenCounter{countPerMessage: 100},
		BudgetCounter: perItemBudgetCounter{perMessage: 1},
		CompactMessages: func(_ context.Context, msgs []providers.Message, _ string) ([]providers.Message, error) {
			compactCalls++
			return msgs, nil
		},
	}
	stage := NewPruneStage(deps, NewMemoryFlushStage(deps))
	state := defaultState()
	history := make([]providers.Message, 50)
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Prune.HistoryTokens != 50 {
		t.Errorf("HistoryTokens = %d, want 50 from BudgetCounter", state.Prune.HistoryTokens)
	}
	if compactCalls != 0 {
		t.Errorf("CompactMessages calls = %d, want 0 (history is under budget)", compactCalls)
	}
}

func TestContextStage_OverheadUsesBudgetCounter(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		TokenCounter:  &spyTokenCounter{fixed: 100, toolFixed: 50},
		BudgetCounter: perItemBudgetCounter{perMessage: 7, perTool: 3},
		BuildMessages: func(_ context.Context, _ *RunInput, _ []providers.Message, _ string) ([]providers.Message, error) {
			return []providers.Message{{Role: "system", Content: "system prompt"}}, nil
		},
		BuildFilteredTools: func(_ *RunState) ([]providers.ToolDefinition, error) {
			return fixtureTools(5), nil
		},
	}
	state := defaultState()

	if err := NewContextStage(deps).Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if want := 7 + 5*3; state.Context.OverheadTokens != want {
		t.Errorf("OverheadTokens = %d, want %d from BudgetCounter", state.Context.OverheadTokens, want)
	}
}

func TestContextStage_OverheadFallsBackToTokenCounterOnBudgetError(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		TokenCounter:  &spyTokenCounter{fixed: 100, toolFixed: 50},
		BudgetCounter: perItemBudgetCounter{err: errors.New("encoder unavailable")},
		BuildMessages: func(_ context.Context, _ *RunInput, _ []providers.Message, _ string) ([]providers.Message, error) {
			return []providers.Message{{Role: "system", Content: "system prompt"}}, nil
		},
		BuildFilteredTools: func(_ *RunState) ([]providers.ToolDefinition, error) {
			return fixtureTools(5), nil
		},
	}
	state := defaultState()

	if err := NewContextStage(deps).Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if want := 100 + 5*50; state.Context.OverheadTokens != want {
		t.Errorf("OverheadTokens = %d, want %d from TokenCounter fallback", state.Context.OverheadTokens, want)
	}
}

func TestThinkStage_FinalRequestGuard_PruneBudgetUsesBudgetCounter(t *testing.T) {
	t.Parallel()
	gotBudget := -1
	deps := &PipelineDeps{
		TokenCounter:  &mockTokenCounter{countPerMessage: 1000},
		BudgetCounter: perItemBudgetCounter{perMessage: 40},
		Config:        PipelineConfig{ContextWindow: 100, MaxTokens: 10},
		PruneMessages: func(msgs []providers.Message, budget int) ([]providers.Message, PruneStats) {
			gotBudget = budget
			return msgs, PruneStats{}
		},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{Content: "ok", FinishReason: "stop"}, nil
		},
	}
	state := defaultState()
	state.Messages.SetSystem(providers.Message{Role: "system", Content: "sys"})
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "q"}})

	_ = NewThinkStage(deps).Execute(context.Background(), state)

	// compact target 75 - tools 0 - fixed (system) 40 = 35 in BudgetCounter units.
	if gotBudget != 35 {
		t.Errorf("prune budget = %d, want 35 (fixed messages counted with BudgetCounter)", gotBudget)
	}
}
