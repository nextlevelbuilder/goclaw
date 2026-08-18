package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestRunStateMarshalRestoreRoundtrip proves MarshalCheckpoint captures identity,
// substates, and messages (including Images + RawAssistantContent), and that
// RestoreCheckpoint rebuilds the state with Resuming()=true and the caller can
// re-attach Input/Model/Provider.
func TestRunStateMarshalRestoreRoundtrip(t *testing.T) {
	state := defaultState()
	state.Iteration = 7
	state.RunID = "run-checkpoint-1"
	state.Model = "claude-3"
	state.Think.TotalUsage = providers.Usage{PromptTokens: 10, CompletionTokens: 20}
	state.Tool.TotalToolCalls = 3
	state.Observe.FinalContent = "partial answer"
	state.Context.OverheadTokens = 42

	state.Messages.SetSystem(providers.Message{Role: "system", Content: "sys"})
	state.Messages.SetHistory([]providers.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world",
			Images:              []providers.ImageContent{{MimeType: "image/png", Data: "base64"}},
			RawAssistantContent: json.RawMessage(`[{"type":"thinking","thinking":"..."}]`),
			ToolCallID:          "call-9"},
	})

	raw, err := state.MarshalCheckpoint()
	if err != nil {
		t.Fatalf("MarshalCheckpoint: %v", err)
	}

	restored, err := RestoreCheckpoint(raw)
	if err != nil {
		t.Fatalf("RestoreCheckpoint: %v", err)
	}
	if !restored.Resuming() {
		t.Fatal("restored.Resuming() = false, want true")
	}
	// Caller re-attaches identity after restore.
	restored.Input = state.Input
	restored.Model = state.Model

	if restored.Iteration != 7 {
		t.Fatalf("Iteration = %d, want 7", restored.Iteration)
	}
	if restored.RunID != "run-checkpoint-1" {
		t.Fatalf("RunID = %s", restored.RunID)
	}
	if restored.Model != "claude-3" {
		t.Fatalf("Model = %s", restored.Model)
	}
	if restored.Think.TotalUsage != state.Think.TotalUsage {
		t.Fatalf("Think.TotalUsage = %+v", restored.Think.TotalUsage)
	}
	if restored.Tool.TotalToolCalls != 3 {
		t.Fatalf("Tool.TotalToolCalls = %d", restored.Tool.TotalToolCalls)
	}
	if restored.Observe.FinalContent != "partial answer" {
		t.Fatalf("Observe.FinalContent = %q", restored.Observe.FinalContent)
	}
	if restored.Context.OverheadTokens != 42 {
		t.Fatalf("Context.OverheadTokens = %d", restored.Context.OverheadTokens)
	}

	all := restored.Messages.All()
	if len(all) != 3 {
		t.Fatalf("restored messages len = %d, want 3 (system + 2 history)", len(all))
	}
	if all[0].Content != "sys" {
		t.Fatalf("restored system = %q", all[0].Content)
	}
	restoredAssistant := all[2]
	if len(restoredAssistant.Images) != 1 || restoredAssistant.Images[0].MimeType != "image/png" {
		t.Fatalf("Images not preserved: %+v", restoredAssistant.Images)
	}
	if string(restoredAssistant.RawAssistantContent) != `[{"type":"thinking","thinking":"..."}]` {
		t.Fatalf("RawAssistantContent = %s", restoredAssistant.RawAssistantContent)
	}
	if restoredAssistant.ToolCallID != "call-9" {
		t.Fatalf("ToolCallID = %q", restoredAssistant.ToolCallID)
	}
}

// TestRunStateCheckpointOmitsRuntimeValues proves runtime-only values (Provider,
// Ctx) are not serialized — they cannot round-trip and are re-attached by the
// caller.
func TestRunStateCheckpointOmitsRuntimeValues(t *testing.T) {
	state := defaultState()
	state.Provider = &testProvider{}
	raw, err := state.MarshalCheckpoint()
	if err != nil {
		t.Fatalf("MarshalCheckpoint: %v", err)
	}
	restored, err := RestoreCheckpoint(raw)
	if err != nil {
		t.Fatalf("RestoreCheckpoint: %v", err)
	}
	if restored.Provider != nil {
		t.Fatal("Provider must not be restored (caller re-attaches)")
	}
	if restored.Ctx != nil {
		t.Fatal("Ctx must not be restored (ContextStage re-derives on run start)")
	}
}

// TestRunStateCheckpointMessageCapTrimsHistory proves the message count cap
// keeps the system prompt plus the most recent entries.
func TestRunStateCheckpointMessageCapTrimsHistory(t *testing.T) {
	state := defaultState()
	// System prompt lives in the buffer's system slot (set by ContextStage in a
	// real run), not in history — mirror that here.
	state.Messages.SetSystem(providers.Message{Role: "system", Content: "sys"})
	msgs := make([]providers.Message, 0, maxCheckpointMessages+10)
	for i := 0; i < maxCheckpointMessages+5; i++ {
		msgs = append(msgs, providers.Message{Role: "user", Content: "m"})
	}
	state.Messages.SetHistory(msgs)

	raw, err := state.MarshalCheckpoint()
	if err != nil {
		t.Fatalf("MarshalCheckpoint: %v", err)
	}
	var cp runStateCheckpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cp.Messages) != maxCheckpointMessages {
		t.Fatalf("checkpoint messages = %d, want %d", len(cp.Messages), maxCheckpointMessages)
	}
	if cp.Messages[0].Content != "sys" {
		t.Fatalf("system prompt lost: %q", cp.Messages[0].Content)
	}
}

// TestRunStateRestoreCheckpointInvalidJSON proves an unparseable checkpoint
// returns an error (caller falls back to a fresh run).
func TestRunStateRestoreCheckpointInvalidJSON(t *testing.T) {
	if _, err := RestoreCheckpoint(json.RawMessage(`{not valid`)); err == nil {
		t.Fatal("RestoreCheckpoint on invalid JSON succeeded, want error")
	}
}

// TestMessageBufferRestoreRebuildsShape proves Restore accepts the checkpoint
// flat layout [system, ...history] and drops pending.
func TestMessageBufferRestoreRebuildsShape(t *testing.T) {
	mb := NewMessageBuffer(providers.Message{Role: "system", Content: "sys"})
	mb.SetHistory([]providers.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
	})
	mb.AppendPending(providers.Message{Role: "assistant", Content: "pending-dropped"})

	mb.Restore([]providers.Message{
		{Role: "system", Content: "new-sys"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
	})

	if mb.System().Content != "new-sys" {
		t.Fatalf("system = %q", mb.System().Content)
	}
	hist := mb.History()
	if len(hist) != 2 || hist[0].Content != "u2" || hist[1].Content != "a2" {
		t.Fatalf("history = %+v", hist)
	}
	if len(mb.Pending()) != 0 {
		t.Fatalf("pending = %+v, want empty", mb.Pending())
	}
}

// testProvider is a minimal providers.Provider satisfying the interface for the
// runtime-value test above.
type testProvider struct{}

func (m *testProvider) Chat(ctx context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	return nil, nil
}
func (m *testProvider) ChatStream(ctx context.Context, req providers.ChatRequest, onChunk func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, nil
}
func (m *testProvider) DefaultModel() string { return "mock" }
func (m *testProvider) Name() string         { return "mock" }

// Compile-time guard: testProvider must satisfy providers.Provider.
var _ providers.Provider = (*testProvider)(nil)
