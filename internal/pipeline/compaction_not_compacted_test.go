package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestPruneStage_NotCompacted_LeavesHistoryAndCountersUnchanged(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config:       PipelineConfig{ContextWindow: 1000, MaxTokens: 100},
		TokenCounter: &mockTokenCounter{countPerMessage: 100},
		PruneMessages: func(msgs []providers.Message, _ int) ([]providers.Message, PruneStats) {
			return msgs, PruneStats{}
		},
		CompactMessages: func(_ context.Context, _ []providers.Message, _ string) ([]providers.Message, error) {
			return nil, ErrNotCompacted
		},
	}
	stage := NewPruneStage(deps, NewMemoryFlushStage(deps))
	state := defaultState()
	history := make([]providers.Message, 50)
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)
	state.Messages.AppendPending(providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "c1", Name: "read_file"}}})
	state.Messages.AppendPending(providers.Message{Role: "tool", ToolCallID: "c1", Content: "result"})

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if got := len(state.Messages.History()); got != 50 {
		t.Errorf("history len = %d, want 50 (unchanged)", got)
	}
	if got := len(state.Messages.Pending()); got != 2 {
		t.Errorf("pending len = %d, want 2 (preserved)", got)
	}
	if state.Prune.MidLoopCompacted {
		t.Error("MidLoopCompacted = true, want false when nothing was compacted")
	}
	if state.Compact.CompactionCount != 0 {
		t.Errorf("CompactionCount = %d, want 0", state.Compact.CompactionCount)
	}
}

func TestThinkStage_FinalRequestGuard_NotCompactedFallsThroughToShrinkMemory(t *testing.T) {
	t.Parallel()
	called := false
	memory := strings.Repeat("m", 80)

	deps := &PipelineDeps{
		TokenCounter: finalRequestBudgetCounter{},
		Config:       PipelineConfig{ContextWindow: 100, MaxTokens: 10, Compaction: &config.CompactionConfig{MaxRequestShare: 0.85}},
		CompactMessages: func(_ context.Context, _ []providers.Message, _ string) ([]providers.Message, error) {
			return nil, ErrNotCompacted
		},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			called = true
			return &providers.ChatResponse{Content: "ok", FinishReason: "stop"}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()
	state.Messages.SetSystem(providers.Message{Role: "system", Content: "sys\n\n" + memory})
	state.Context.MemorySection = memory
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "short"}})

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !called {
		t.Fatal("expected CallLLM after shrink_memory brought the request under the limit")
	}
	if state.Prune.MidLoopCompacted {
		t.Error("MidLoopCompacted = true, want false when nothing was compacted")
	}
}

// A failed compaction (no split point, summarizer error/timeout) is not retried
// every iteration; the request guard remains the authority for this run.
func TestPruneStage_NotCompacted_DoesNotRetryInSameRun(t *testing.T) {
	t.Parallel()
	compactCalls, flushCalls := 0, 0
	deps := &PipelineDeps{
		Config:       PipelineConfig{ContextWindow: 1000, MaxTokens: 100},
		TokenCounter: &mockTokenCounter{countPerMessage: 100},
		RunMemoryFlush: func(context.Context, *RunState) error {
			flushCalls++
			return nil
		},
		CompactMessages: func(_ context.Context, _ []providers.Message, _ string) ([]providers.Message, error) {
			compactCalls++
			return nil, ErrNotCompacted
		},
	}
	stage := NewPruneStage(deps, NewMemoryFlushStage(deps))
	state := defaultState()
	history := make([]providers.Message, 50)
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)

	for range 3 {
		if err := stage.Execute(context.Background(), state); err != nil {
			t.Fatalf("Execute() error: %v", err)
		}
	}
	if compactCalls != 1 || flushCalls != 1 {
		t.Errorf("compact calls = %d, flush calls = %d; want 1 each", compactCalls, flushCalls)
	}
	if !state.Compact.Unavailable {
		t.Error("Compact.Unavailable = false, want true after ErrNotCompacted")
	}
}

func TestThinkStage_FinalRequestGuard_SkipsCompactionWhenUnavailable(t *testing.T) {
	t.Parallel()
	compactCalls := 0
	memory := strings.Repeat("m", 80)
	deps := &PipelineDeps{
		TokenCounter: finalRequestBudgetCounter{},
		Config:       PipelineConfig{ContextWindow: 100, MaxTokens: 10, Compaction: &config.CompactionConfig{MaxRequestShare: 0.85}},
		CompactMessages: func(_ context.Context, _ []providers.Message, _ string) ([]providers.Message, error) {
			compactCalls++
			return nil, ErrNotCompacted
		},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			return &providers.ChatResponse{Content: "ok", FinishReason: "stop"}, nil
		},
	}
	state := defaultState()
	state.Compact.Unavailable = true
	state.Messages.SetSystem(providers.Message{Role: "system", Content: "sys\n\n" + memory})
	state.Context.MemorySection = memory
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "short"}})

	if err := NewThinkStage(deps).Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if compactCalls != 0 {
		t.Errorf("CompactMessages calls = %d, want 0 once compaction is unavailable", compactCalls)
	}
}

// Post-run summarization must still see context pressure when mid-loop
// compaction was needed but could not compact anything.
func TestFinalizeStage_PassesPressureWhenCompactionUnavailable(t *testing.T) {
	t.Parallel()
	var gotPressure bool
	deps := &PipelineDeps{
		MaybeSummarize: func(_ context.Context, _ string, pressure bool) { gotPressure = pressure },
	}
	state := defaultState()
	state.Observe.FinalContent = "answer"
	state.Compact.Unavailable = true

	if err := NewFinalizeStage(deps).Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if !gotPressure {
		t.Error("MaybeSummarize pressure = false, want true when compaction was unavailable")
	}
}
