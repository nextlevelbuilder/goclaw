package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func runBudgetPipeline(t *testing.T, deps PipelineDeps, state *RunState, iteration func(*PipelineDeps) []Stage) *RunResult {
	t.Helper()
	d := &deps
	p := NewPipeline(nil, iteration(d), []Stage{NewFinalizeStage(d)}, deps)
	result, err := p.Run(context.Background(), state)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	return result
}

// Regression for trace 01a0ec56: the chars/2 heuristic said "over budget" while
// the real request fitted, and the run ended with the empty-reply fallback.
func TestPipeline_HeuristicOverBudget_StillCallsModel(t *testing.T) {
	t.Parallel()
	called := false
	deps := PipelineDeps{
		Config:        PipelineConfig{ContextWindow: 1000, MaxTokens: 100, MaxIterations: 3},
		TokenCounter:  &mockTokenCounter{countPerMessage: 1000},
		BudgetCounter: perItemBudgetCounter{perMessage: 1},
		CompactMessages: func(_ context.Context, _ []providers.Message, _ string) ([]providers.Message, error) {
			return nil, ErrNotCompacted
		},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			called = true
			return &providers.ChatResponse{Content: "answer", FinishReason: "stop"}, nil
		},
	}
	state := defaultState()
	state.Messages.SetHistory([]providers.Message{
		{Role: "user", Content: "q1"}, {Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"}, {Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"},
	})

	result := runBudgetPipeline(t, deps, state, func(d *PipelineDeps) []Stage {
		return []Stage{NewPruneStage(d, NewMemoryFlushStage(d)), NewThinkStage(d), NewObserveStage(d)}
	})

	if !called {
		t.Fatal("CallLLM was not called; the heuristic estimate must not stop the run")
	}
	if result.Content != "answer" {
		t.Errorf("Content = %q, want %q", result.Content, "answer")
	}
	if result.StopReason != "" {
		t.Errorf("StopReason = %q, want empty", result.StopReason)
	}
}

func TestPipeline_GuardExhausted_DeliversContextBudgetNotice(t *testing.T) {
	t.Parallel()
	deps := PipelineDeps{
		TokenCounter: finalRequestBudgetCounter{},
		Config: PipelineConfig{
			ContextWindow: 100, MaxTokens: 10, MaxIterations: 3,
			Compaction: &config.CompactionConfig{MaxRequestShare: 0.85},
		},
		CompactMessages: func(_ context.Context, _ []providers.Message, _ string) ([]providers.Message, error) {
			return nil, ErrNotCompacted
		},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			t.Error("CallLLM must not run while the request exceeds the context budget")
			return &providers.ChatResponse{Content: "unexpected", FinishReason: "stop"}, nil
		},
	}
	state := defaultState()
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: strings.Repeat("long-history", 10)}})

	result := runBudgetPipeline(t, deps, state, func(d *PipelineDeps) []Stage {
		return []Stage{NewThinkStage(d), NewObserveStage(d)}
	})

	if want := i18n.T(store.LocaleFromContext(context.Background()), i18n.MsgContextBudgetExceeded); result.Content != want {
		t.Errorf("Content = %q, want the context budget notice %q", result.Content, want)
	}
	if !strings.Contains(result.StopReason, "context budget exceeded") {
		t.Errorf("StopReason = %q, want the guard error", result.StopReason)
	}
}

// A "save this as a skill?" postscript makes no sense under a stop notice.
func TestFinalizeStage_StoppedRun_SkipsSkillPostscript(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		SkillPostscript: func(_ context.Context, content string, _ int) string {
			return content + "\n\nsave as skill?"
		},
	}
	state := defaultState()
	state.Observe.FinalContent = "context budget notice"
	state.StopReason = "final request context budget exceeded"

	if err := NewFinalizeStage(deps).Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if state.Observe.FinalContent != "context budget notice" {
		t.Errorf("FinalContent = %q, want the notice without a postscript", state.Observe.FinalContent)
	}
}
