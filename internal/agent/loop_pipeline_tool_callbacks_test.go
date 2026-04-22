package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// stubExecutor implements tools.ToolExecutor with a canned successful Result.
// Used to isolate the tool-callback wrappers from real tool registry wiring.
type stubExecutor struct{}

func (s *stubExecutor) ExecuteWithContext(_ context.Context, _ string, _ map[string]any, _, _, _, _ string, _ tools.AsyncCallback) *tools.Result {
	return &tools.Result{ForLLM: "ok", IsError: false}
}
func (s *stubExecutor) TryActivateDeferred(string) bool         { return false }
func (s *stubExecutor) ProviderDefs() []providers.ToolDefinition { return nil }
func (s *stubExecutor) Get(string) (tools.Tool, bool)             { return nil, false }
func (s *stubExecutor) List() []string                            { return nil }
func (s *stubExecutor) Aliases() map[string]string                { return nil }

// eventCollector buffers AgentEvents for inspection in tests.
// Safe for concurrent appends from parallel goroutines.
type eventCollector struct {
	mu     sync.Mutex
	events []AgentEvent
}

func (c *eventCollector) onEvent(e AgentEvent) {
	c.mu.Lock()
	c.events = append(c.events, e)
	c.mu.Unlock()
}

func (c *eventCollector) filter(typ string) []AgentEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []AgentEvent
	for _, e := range c.events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// newTestLoopForToolCallbacks builds a minimal Loop instance sufficient to
// exercise makeExecuteToolCall / makeExecuteToolRaw. All optional subsystems
// (tracing, metrics, input guard) are left nil and hit early-return paths.
func newTestLoopForToolCallbacks(onEvent func(AgentEvent)) *Loop {
	return &Loop{
		id:      "test-agent",
		tools:   &stubExecutor{},
		onEvent: onEvent,
	}
}

// TestMakeExecuteToolCall_EmitsToolCallEvent verifies the sequential wrapper
// emits a tool.call event before running tool I/O.
func TestMakeExecuteToolCall_EmitsToolCallEvent(t *testing.T) {
	t.Parallel()
	col := &eventCollector{}
	l := newTestLoopForToolCallbacks(col.onEvent)

	req := &RunRequest{
		RunID:      "run-1",
		SessionKey: "sess-A",
		UserID:     "u-1",
		Channel:    "ws",
		RunKind:    "",
	}
	state := &pipeline.RunState{RunID: "run-1"}
	tc := providers.ToolCall{ID: "tc-1", Name: "read_file", Arguments: map[string]any{"path": "/tmp/x"}}

	_, err := l.makeExecuteToolCall(req, &runState{})(context.Background(), state, tc)
	if err != nil {
		t.Fatalf("makeExecuteToolCall returned error: %v", err)
	}

	calls := col.filter(protocol.AgentEventToolCall)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool.call event, got %d (all events: %+v)", len(calls), col.events)
	}
	assertToolCallPayload(t, calls[0], tc, req)
}

// TestMakeExecuteToolRaw_EmitsToolCallEvent is the PRIMARY regression guard.
// The original bug: parallel path (makeExecuteToolRaw) did not emit tool.call,
// so web UI and desktop UI silently dropped tool info during real-time streaming.
// Mutation-verify: remove emitRun(...) from makeExecuteToolRaw — this test must fail.
func TestMakeExecuteToolRaw_EmitsToolCallEvent(t *testing.T) {
	t.Parallel()
	col := &eventCollector{}
	l := newTestLoopForToolCallbacks(col.onEvent)

	req := &RunRequest{
		RunID:      "run-2",
		SessionKey: "sess-B",
		UserID:     "u-2",
		Channel:    "ws",
		RunKind:    "",
	}
	tc := providers.ToolCall{ID: "tc-2", Name: "write_file", Arguments: map[string]any{"path": "/tmp/y"}}

	msg, raw, err := l.makeExecuteToolRaw(req)(context.Background(), tc)
	if err != nil {
		t.Fatalf("makeExecuteToolRaw returned error: %v", err)
	}
	if msg.Role != "tool" || msg.ToolCallID != tc.ID {
		t.Errorf("unexpected tool message: %+v", msg)
	}
	if raw == nil {
		t.Error("expected non-nil raw data (toolRawResult)")
	}

	calls := col.filter(protocol.AgentEventToolCall)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool.call event, got %d (all events: %+v)", len(calls), col.events)
	}
	assertToolCallPayload(t, calls[0], tc, req)
}

// TestMakeExecuteToolRaw_ConcurrentCallsEmitAllEvents confirms the parallel
// wrapper is safe to invoke from multiple goroutines — mirrors the real
// executeParallel dispatch in pipeline/tool_stage.go.
func TestMakeExecuteToolRaw_ConcurrentCallsEmitAllEvents(t *testing.T) {
	t.Parallel()
	col := &eventCollector{}
	l := newTestLoopForToolCallbacks(col.onEvent)

	req := &RunRequest{RunID: "run-3", SessionKey: "sess-C", UserID: "u-3", Channel: "ws"}
	exec := l.makeExecuteToolRaw(req)

	const n = 5
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tc := providers.ToolCall{ID: "tc-" + string(rune('a'+idx)), Name: "t", Arguments: nil}
			if _, _, err := exec(context.Background(), tc); err != nil {
				t.Errorf("goroutine %d: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	calls := col.filter(protocol.AgentEventToolCall)
	if len(calls) != n {
		t.Fatalf("expected %d tool.call events, got %d", n, len(calls))
	}
}

// assertToolCallPayload verifies the event carries the expected tc identity
// and routing context from RunRequest.
func assertToolCallPayload(t *testing.T, ev AgentEvent, tc providers.ToolCall, req *RunRequest) {
	t.Helper()
	if ev.AgentID != "test-agent" {
		t.Errorf("AgentID: got %q, want test-agent", ev.AgentID)
	}
	if ev.RunID != req.RunID {
		t.Errorf("RunID: got %q, want %q", ev.RunID, req.RunID)
	}
	if ev.SessionKey != req.SessionKey {
		t.Errorf("SessionKey: got %q, want %q", ev.SessionKey, req.SessionKey)
	}
	if ev.Channel != req.Channel {
		t.Errorf("Channel: got %q, want %q", ev.Channel, req.Channel)
	}
	if ev.UserID != req.UserID {
		t.Errorf("UserID: got %q, want %q", ev.UserID, req.UserID)
	}
	payload, ok := ev.Payload.(map[string]any)
	if !ok {
		t.Fatalf("Payload is not map[string]any: %T", ev.Payload)
	}
	if payload["id"] != tc.ID {
		t.Errorf("payload.id: got %v, want %q", payload["id"], tc.ID)
	}
	if payload["name"] != tc.Name {
		t.Errorf("payload.name: got %v, want %q", payload["name"], tc.Name)
	}
}
