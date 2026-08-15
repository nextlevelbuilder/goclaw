package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestPruneStage_Compaction_PreservesPending is the regression guard for the
// mid-loop compaction tool-pairing bug. ThinkStage appends the current
// iteration's assistant(tool_calls) message to the pending buffer, then
// PruneStage runs next. If compaction fires, CompactMessages only receives
// History() — pending is NOT part of the compaction input — yet ReplaceHistory
// clears pending. Without preservation, the assistant(tool_calls) message is
// dropped, ToolStage later appends orphaned tool_result messages, and the next
// provider call rejects the unpaired tool_result (OpenAI/DeepSeek 400).
//
// This test asserts the pending assistant(tool_calls) message survives a
// mid-loop compaction so tool_calls -> tool_result pairing stays intact.
func TestPruneStage_Compaction_PreservesPending(t *testing.T) {
	t.Parallel()
	// budget = ContextWindow(1000) - overhead(0) - MaxTokens(100) = 900.
	// History of 50 msgs * 100 tokens = 5000 > 900 forces the compaction path.
	deps := &PipelineDeps{
		Config: PipelineConfig{
			ContextWindow: 1000,
			MaxTokens:     100,
		},
		TokenCounter: &mockTokenCounter{countPerMessage: 100},
		PruneMessages: func(msgs []providers.Message, _ int) ([]providers.Message, PruneStats) {
			return msgs, PruneStats{} // no reduction — forces fall-through to compaction
		},
		CompactMessages: func(_ context.Context, msgs []providers.Message, _ string) ([]providers.Message, error) {
			// Compaction summarizes history into a single message. Crucially it
			// only ever sees History(), never the pending assistant(tool_calls).
			return []providers.Message{{Role: "user", Content: "[compacted summary]"}}, nil
		},
	}
	stage := NewPruneStage(deps, nil)
	state := defaultState()

	history := make([]providers.Message, 50)
	for i := range history {
		history[i] = providers.Message{Role: "user", Content: "msg"}
	}
	state.Messages.SetHistory(history)

	// The in-flight assistant(tool_calls) message ThinkStage placed in pending
	// this iteration — it must outlive ReplaceHistory.
	assistantToolCall := providers.Message{
		Role:      "assistant",
		ToolCalls: []providers.ToolCall{{ID: "tc-keep", Name: "read_file"}},
	}
	state.Messages.AppendPending(assistantToolCall)

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if !state.Prune.MidLoopCompacted {
		t.Fatal("MidLoopCompacted = false, want true (compaction should have fired)")
	}
	if state.Compact.CompactionCount != 1 {
		t.Errorf("CompactionCount = %d, want 1", state.Compact.CompactionCount)
	}

	// The assistant(tool_calls) message must still be pending after compaction.
	pending := state.Messages.Pending()
	if len(pending) != 1 {
		t.Fatalf("Pending() len = %d, want 1 (assistant tool_calls preserved across ReplaceHistory)", len(pending))
	}
	if pending[0].Role != "assistant" || len(pending[0].ToolCalls) != 1 || pending[0].ToolCalls[0].ID != "tc-keep" {
		t.Errorf("pending[0] = %+v, want the preserved assistant tool_calls message (id tc-keep)", pending[0])
	}

	// History should be the compacted summary, not carrying a duplicate of the
	// preserved pending message.
	hist := state.Messages.History()
	if len(hist) != 1 || hist[0].Content != "[compacted summary]" {
		t.Errorf("History() = %+v, want single compacted summary message", hist)
	}
}

// TestThinkStage_EmergencyCompaction_PreservesPending guards the second
// ReplaceHistory call site. When CallLLM reports a context-overflow error,
// ThinkStage runs an emergency compaction and retries the iteration. Pending
// messages already staged this iteration (e.g. iteration-budget nudges from
// maybeInjectNudge) must survive the ReplaceHistory that emergency compaction
// performs, for the same reason as the prune path.
func TestThinkStage_EmergencyCompaction_PreservesPending(t *testing.T) {
	t.Parallel()
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			// "context length exceeded" is matched by IsContextOverflowMessage.
			return nil, errors.New("context length exceeded for this request")
		},
		CompactMessages: func(_ context.Context, msgs []providers.Message, _ string) ([]providers.Message, error) {
			return []providers.Message{{Role: "user", Content: "[compacted summary]"}}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: "old turn"}})

	// A pending message staged earlier in this iteration (an iteration nudge).
	nudge := providers.Message{Role: "user", Content: "[System] wrap up your work"}
	state.Messages.AppendPending(nudge)

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// Emergency compaction retries the iteration (Continue) and records one retry.
	if stage.Result() != Continue {
		t.Errorf("Result() = %v, want Continue (retry after emergency compaction)", stage.Result())
	}
	if state.Think.OverflowRetries != 1 {
		t.Errorf("OverflowRetries = %d, want 1", state.Think.OverflowRetries)
	}

	pending := state.Messages.Pending()
	if len(pending) != 1 {
		t.Fatalf("Pending() len = %d, want 1 (nudge preserved across emergency ReplaceHistory)", len(pending))
	}
	if pending[0].Content != "[System] wrap up your work" {
		t.Errorf("pending[0] = %+v, want the preserved nudge message", pending[0])
	}
}
