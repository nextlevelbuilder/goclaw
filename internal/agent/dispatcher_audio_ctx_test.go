package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ctxCapturingExecutor is a stubExecutor variant that captures the context
// passed to ExecuteWithContext so tests can assert on store.AgentAudioFromCtx.
type ctxCapturingExecutor struct {
	mu          sync.Mutex
	capturedCtx []context.Context
}

func (e *ctxCapturingExecutor) ExecuteWithContext(ctx context.Context, _ string, _ map[string]any, _, _, _, _ string, _ tools.AsyncCallback) *tools.Result {
	e.mu.Lock()
	e.capturedCtx = append(e.capturedCtx, ctx)
	e.mu.Unlock()
	return &tools.Result{ForLLM: "ok", IsError: false}
}
func (e *ctxCapturingExecutor) TryActivateDeferred(string) bool          { return false }
func (e *ctxCapturingExecutor) ProviderDefs() []providers.ToolDefinition  { return nil }
func (e *ctxCapturingExecutor) Get(string) (tools.Tool, bool)              { return nil, false }
func (e *ctxCapturingExecutor) List() []string                             { return nil }
func (e *ctxCapturingExecutor) Aliases() map[string]string                 { return nil }

func (e *ctxCapturingExecutor) lastCtx() context.Context {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.capturedCtx) == 0 {
		return nil
	}
	return e.capturedCtx[len(e.capturedCtx)-1]
}

func TestDispatcherAudioCtx_ToolCallInjectsSnapshot(t *testing.T) {
	t.Parallel()

	expectedID := uuid.New()
	otherCfg := json.RawMessage(`{"tts_voice_id":"V1"}`)

	cap := &ctxCapturingExecutor{}
	l := &Loop{
		id:               "audio-test-agent",
		agentUUID:        expectedID,
		agentOtherConfig: append([]byte(nil), otherCfg...),
		tools:            cap,
	}

	req := &RunRequest{RunID: "r1", SessionKey: "s1", Channel: "ws"}
	state := &pipeline.RunState{RunID: "r1"}
	tc := providers.ToolCall{ID: "tc-1", Name: "tts", Arguments: map[string]any{"text": "hello"}}

	_, err := l.makeExecuteToolCall(req, &runState{})(context.Background(), state, tc)
	if err != nil {
		t.Fatalf("makeExecuteToolCall error: %v", err)
	}

	captured := cap.lastCtx()
	if captured == nil {
		t.Fatal("ExecuteWithContext was not called")
	}
	snap, ok := store.AgentAudioFromCtx(captured)
	if !ok {
		t.Fatal("AgentAudioFromCtx returned ok=false — producer not wired in makeExecuteToolCall")
	}
	if snap.AgentID != expectedID {
		t.Errorf("AgentID: got %v, want %v", snap.AgentID, expectedID)
	}
	if !bytes.Equal(snap.OtherConfig, otherCfg) {
		t.Errorf("OtherConfig: got %q, want %q", snap.OtherConfig, otherCfg)
	}
}

func TestDispatcherAudioCtx_ToolRawInjectsSnapshot(t *testing.T) {
	t.Parallel()

	expectedID := uuid.New()
	otherCfg := json.RawMessage(`{"tts_voice_id":"V1"}`)

	cap := &ctxCapturingExecutor{}
	l := &Loop{
		id:               "audio-test-agent-raw",
		agentUUID:        expectedID,
		agentOtherConfig: append([]byte(nil), otherCfg...),
		tools:            cap,
	}

	req := &RunRequest{RunID: "r2", SessionKey: "s2", Channel: "ws"}
	tc := providers.ToolCall{ID: "tc-2", Name: "tts", Arguments: map[string]any{"text": "world"}}

	_, _, err := l.makeExecuteToolRaw(req)(context.Background(), tc)
	if err != nil {
		t.Fatalf("makeExecuteToolRaw error: %v", err)
	}

	captured := cap.lastCtx()
	if captured == nil {
		t.Fatal("ExecuteWithContext was not called")
	}
	snap, ok := store.AgentAudioFromCtx(captured)
	if !ok {
		t.Fatal("AgentAudioFromCtx returned ok=false — producer not wired in makeExecuteToolRaw")
	}
	if snap.AgentID != expectedID {
		t.Errorf("AgentID: got %v, want %v", snap.AgentID, expectedID)
	}
	if !bytes.Equal(snap.OtherConfig, otherCfg) {
		t.Errorf("OtherConfig: got %q, want %q", snap.OtherConfig, otherCfg)
	}
}
