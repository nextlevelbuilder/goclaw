package pipeline

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// This file closes the last uncovered link in the anti-recompaction-loop chain.
//
// The chain has three hops:
//  1. prune_stage / final_request_guard compact mid-loop and set
//     state.Prune.MidLoopCompacted  — covered by
//     TestPruneStage_Compaction_PreservesPending + stages_test.go.
//  2. finalize_stage forwards that flag into deps.MaybeSummarize             — THIS FILE.
//  3. maybeSummarize lowers its threshold and PERSISTS the compaction        — covered by
//     internal/agent/loop_maybe_summarize_pressure_test.go.
//
// Hop 2 was effectively untested: TestFinalizeStage_MaybeSummarize_Called binds
// the flag as `_ bool` and never asserts it, so hardcoding `false` at the
// finalize call site would keep every existing test green while silently
// restoring the unbounded re-compaction loop (each turn recompacts from scratch
// because nothing is ever written back to the session store).
//
// These tests drive the REAL NewDefaultPipeline().Run() end to end — no stage is
// invoked directly — so the flag has to survive the actual run wiring.

// pressureE2EDeps builds deps for a full-pipeline run whose token math forces a
// mid-loop compaction, capturing what finalize hands to MaybeSummarize.
//
// Token math (mirrors TestPruneStage_Compaction_PreservesPending):
//
//	budget = ContextWindow(1000) - overhead(0) - MaxTokens(100) - ReserveTokens(0) = 900
//	history = 50 msgs * 100 tokens                                                = 5000 > 900
//
// PruneMessages deliberately reduces nothing, forcing fall-through to
// CompactMessages, which is the path that sets MidLoopCompacted.
func pressureE2EDeps(gotFlag *bool, gotCalls *int, compact bool) PipelineDeps {
	deps := PipelineDeps{
		Config: PipelineConfig{
			MaxIterations: 2,
			ContextWindow: 1000,
			MaxTokens:     100,
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 100},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			// No tool calls -> ThinkStage returns BreakLoop, so the run reaches
			// finalize after exactly one iteration.
			return &providers.ChatResponse{Content: "final answer", FinishReason: "stop"}, nil
		},
		MaybeSummarize: func(_ context.Context, _ string, midLoopCompacted bool) {
			*gotCalls++
			*gotFlag = midLoopCompacted
		},
	}
	if compact {
		deps.PruneMessages = func(msgs []providers.Message, _ int) ([]providers.Message, PruneStats) {
			return msgs, PruneStats{} // no reduction — force the compaction path
		}
		deps.CompactMessages = func(_ context.Context, _ []providers.Message, _ string) ([]providers.Message, error) {
			return []providers.Message{{Role: "user", Content: "[compacted summary]"}}, nil
		}
	}
	return deps
}

func pressureE2EState(historyMsgs int) *RunState {
	state := defaultState()
	history := make([]providers.Message, historyMsgs)
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)
	return state
}

// A full pipeline run that compacts mid-loop must tell post-turn summarization
// about it, so the compaction gets persisted to the session store instead of
// being thrown away and redone next turn.
func TestPipelineE2E_MidLoopCompaction_PropagatesPressureToMaybeSummarize(t *testing.T) {
	t.Parallel()
	var gotFlag bool
	var calls int
	deps := pressureE2EDeps(&gotFlag, &calls, true)
	state := pressureE2EState(50) // 5000 tokens >> budget 900 -> compaction fires

	if _, err := NewDefaultPipeline(deps).Run(context.Background(), state); err != nil {
		t.Fatalf("pipeline Run() error: %v", err)
	}

	// Precondition: the run really did compact mid-loop (otherwise this test
	// would vacuously pass on a false flag).
	if !state.Prune.MidLoopCompacted {
		t.Fatal("MidLoopCompacted = false, want true (pipeline should have compacted mid-loop)")
	}
	if calls != 1 {
		t.Fatalf("MaybeSummarize called %d times, want 1", calls)
	}
	// The assertion that TestFinalizeStage_MaybeSummarize_Called cannot make.
	if !gotFlag {
		t.Error("MaybeSummarize received midLoopCompacted=false, want true — " +
			"mid-loop compaction would not be persisted, re-introducing the re-compaction loop")
	}
}

// The mirror case: a run that never compacts must NOT claim pressure, otherwise
// every ordinary turn would truncate its session history early (over-compaction,
// which is what makes an agent lose context and "get dumber").
func TestPipelineE2E_NoCompaction_ReportsNoPressure(t *testing.T) {
	t.Parallel()
	var gotFlag bool
	var calls int
	// compact=false: no CompactMessages wired, and a tiny history that fits the
	// budget anyway, so no mid-loop compaction can occur.
	deps := pressureE2EDeps(&gotFlag, &calls, false)
	state := pressureE2EState(2) // 200 tokens < budget 900

	if _, err := NewDefaultPipeline(deps).Run(context.Background(), state); err != nil {
		t.Fatalf("pipeline Run() error: %v", err)
	}

	if state.Prune.MidLoopCompacted {
		t.Fatal("MidLoopCompacted = true, want false (history fits the budget)")
	}
	if calls != 1 {
		t.Fatalf("MaybeSummarize called %d times, want 1", calls)
	}
	if gotFlag {
		t.Error("MaybeSummarize received midLoopCompacted=true on a run that never compacted — " +
			"would lower the summarize threshold and over-compact healthy sessions")
	}
}
