package pipeline

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestFinalizeStage_UpdateMetadataReceivesPersistedMessageCount(t *testing.T) {
	t.Parallel()

	var gotUsage providers.Usage
	var gotLastUsage providers.Usage
	var gotMsgCount int
	deps := &PipelineDeps{
		FlushMessages: func(_ context.Context, _ string, _ []providers.Message) error {
			return nil
		},
		UpdateMetadata: func(_ context.Context, _ string, usage, lastUsage providers.Usage, msgCount int) error {
			gotUsage = usage
			gotLastUsage = lastUsage
			gotMsgCount = msgCount
			return nil
		},
	}

	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Messages.SetHistory([]providers.Message{
		{Role: "user", Content: "old question"},
		{Role: "assistant", Content: "old answer"},
	})
	state.Messages.AppendPending(providers.Message{Role: "assistant", Content: "tool result"})
	state.Messages.AppendPending(providers.Message{Role: "assistant", Content: "transient", Transient: true})
	state.Observe.FinalContent = "final answer"
	state.Think.TotalUsage = providers.Usage{PromptTokens: 1234, CompletionTokens: 56}
	state.Think.LastUsage = providers.Usage{PromptTokens: 700, CompletionTokens: 30}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if gotUsage.PromptTokens != 1234 || gotUsage.CompletionTokens != 56 {
		t.Fatalf("UpdateMetadata usage = %+v, want prompt=1234 completion=56", gotUsage)
	}
	// Regression guard: the last-call usage (current context size) must reach
	// UpdateMetadata separately from the run-cumulative total — the sessions
	// "context used" bar and compaction calibration read it via
	// SetLastPromptTokens. Passing the total inflated it by the iteration count.
	if gotLastUsage.PromptTokens != 700 {
		t.Fatalf("UpdateMetadata lastUsage.PromptTokens = %d, want 700 (final iteration only)", gotLastUsage.PromptTokens)
	}
	if gotMsgCount != 4 {
		t.Fatalf("UpdateMetadata msgCount = %d, want 4 persisted messages", gotMsgCount)
	}
}
