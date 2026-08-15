package pipeline

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestThinkStage_TracksLastUsagePerIteration guards the per-iteration usage
// snapshot used for the sessions "context used" display and compaction
// calibration. Regression: SetLastPromptTokens received the RUN-CUMULATIVE
// TotalUsage.PromptTokens (sum over all think→act→observe iterations), so a
// tool-heavy run reported e.g. 901K "context" against a 258K window and
// over-triggered compaction. LastUsage must hold only the FINAL iteration's
// own usage — the actual size of the last prompt sent to the model.
func TestThinkStage_TracksLastUsagePerIteration(t *testing.T) {
	t.Parallel()

	usages := []providers.Usage{
		{PromptTokens: 100000, CompletionTokens: 500, TotalTokens: 100500},
		{PromptTokens: 120000, CompletionTokens: 300, TotalTokens: 120300, CacheReadTokens: 90000},
	}
	call := 0
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			u := usages[call]
			call++
			return &providers.ChatResponse{FinishReason: "stop", Content: "done", Usage: &u}, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() 1: %v", err)
	}
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() 2: %v", err)
	}

	// TotalUsage accumulates (unchanged behavior).
	if state.Think.TotalUsage.PromptTokens != 220000 {
		t.Errorf("TotalUsage.PromptTokens = %d, want 220000", state.Think.TotalUsage.PromptTokens)
	}
	// LastUsage holds only the final iteration's usage — NOT the sum.
	if state.Think.LastUsage.PromptTokens != 120000 {
		t.Errorf("LastUsage.PromptTokens = %d, want 120000 (final iteration only)", state.Think.LastUsage.PromptTokens)
	}
	if state.Think.LastUsage.CacheReadTokens != 90000 {
		t.Errorf("LastUsage.CacheReadTokens = %d, want 90000", state.Think.LastUsage.CacheReadTokens)
	}
}

// TestThinkStage_LastUsageKeptWhenFinalResponseLacksUsage: a final iteration
// whose response carries no usage (nil or zero prompt tokens) must not wipe the
// last usable snapshot.
func TestThinkStage_LastUsageKeptWhenFinalResponseLacksUsage(t *testing.T) {
	t.Parallel()

	responses := []*providers.ChatResponse{
		{FinishReason: "stop", Content: "a", Usage: &providers.Usage{PromptTokens: 50000, CompletionTokens: 10}},
		{FinishReason: "stop", Content: "b", Usage: nil},
	}
	call := 0
	deps := &PipelineDeps{
		Config: PipelineConfig{MaxIterations: 10, MaxTokens: 1000},
		CallLLM: func(_ context.Context, _ *RunState, _ providers.ChatRequest) (*providers.ChatResponse, error) {
			r := responses[call]
			call++
			return r, nil
		},
	}
	stage := NewThinkStage(deps)
	state := defaultState()

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() 1: %v", err)
	}
	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() 2: %v", err)
	}

	if state.Think.LastUsage.PromptTokens != 50000 {
		t.Errorf("LastUsage.PromptTokens = %d, want 50000 (kept from iteration 1)", state.Think.LastUsage.PromptTokens)
	}
}
