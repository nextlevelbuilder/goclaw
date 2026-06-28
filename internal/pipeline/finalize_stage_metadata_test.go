package pipeline

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestFinalizeStage_UpdateMetadataReceivesPersistedMessageCount(t *testing.T) {
	t.Parallel()

	var gotUsage providers.Usage
	var gotMsgCount int
	deps := &PipelineDeps{
		FlushMessages: func(_ context.Context, _ string, _ []providers.Message) error {
			return nil
		},
		UpdateMetadata: func(_ context.Context, _ string, usage providers.Usage, msgCount int) error {
			gotUsage = usage
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

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if gotUsage.PromptTokens != 1234 || gotUsage.CompletionTokens != 56 {
		t.Fatalf("UpdateMetadata usage = %+v, want prompt=1234 completion=56", gotUsage)
	}
	if gotMsgCount != 4 {
		t.Fatalf("UpdateMetadata msgCount = %d, want 4 persisted messages", gotMsgCount)
	}
}
