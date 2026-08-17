package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// captureToolEvents collects emitted AgentEvents into a slice for assertions.
type captureToolEvents struct {
	events []AgentEvent
}

func (c *captureToolEvents) emit(ev AgentEvent) { c.events = append(c.events, ev) }

func (c *captureToolEvents) types() []string {
	var out []string
	for _, e := range c.events {
		out = append(out, e.Type)
	}
	return out
}

func (c *captureToolEvents) count(t string) int {
	n := 0
	for _, e := range c.events {
		if e.Type == t {
			n++
		}
	}
	return n
}

// buildTestEmitter creates a Loop + registry + capture sink for a single named
// tool with the given spec, and returns the emitter plus capture.
func buildTestEmitter(t *testing.T, toolName string, spec tools.ToolExecutionSpec) (*toolProgressEmitter, *captureToolEvents) {
	t.Helper()
	capture := &captureToolEvents{}
	l := &Loop{id: "prog-test", registry: tools.NewRegistry()}
	l.registry.Register(&specExecTool{name: toolName, spec: spec})
	e := buildToolProgressEmitter(l, capture.emit, "run-1", toolName, toolName, "tool-id-1", nil)
	return e, capture
}

func TestToolProgress_OptInEmitsStartedLogCompleted(t *testing.T) {
	e, capture := buildTestEmitter(t, "prog_tool", tools.ToolExecutionSpec{Progress: true})
	e.started()
	e.log("line one\nline two")
	e.completed(tools.NewResult("ok"))

	if capture.count(protocol.AgentEventToolStarted) != 1 {
		t.Fatalf("expected 1 tool.started, got %v", capture.types())
	}
	if capture.count(protocol.AgentEventToolLog) != 2 {
		t.Fatalf("expected 2 tool.log, got %v", capture.types())
	}
	if capture.count(protocol.AgentEventToolComplete) != 1 {
		t.Fatalf("expected 1 tool.completed, got %v", capture.types())
	}
	// Every event must carry runId + tool id for UI correlation (C4).
	for _, ev := range capture.events {
		if ev.RunID != "run-1" {
			t.Fatalf("event runId = %q, want run-1", ev.RunID)
		}
		payload, ok := ev.Payload.(map[string]any)
		if !ok {
			t.Fatalf("event payload not a map: %#v", ev.Payload)
		}
		if payload["id"] != "tool-id-1" {
			t.Fatalf("event id = %v, want tool-id-1", payload["id"])
		}
	}
}

func TestToolProgress_DefaultOffEmitsNothing(t *testing.T) {
	e, capture := buildTestEmitter(t, "quiet_tool", tools.ToolExecutionSpec{})
	e.started()
	e.log("should not appear")
	e.progress("should not appear")
	e.completed(tools.NewResult("ok"))

	if len(capture.events) != 0 {
		t.Fatalf("default spec must emit no progress events, got %v", capture.types())
	}
}

func TestToolProgress_LogCappedAtLimit(t *testing.T) {
	e, capture := buildTestEmitter(t, "chatty_tool", tools.ToolExecutionSpec{Progress: true})
	// More lines than the cap.
	var out strings.Builder
	for range toolProgressCap + 5 {
		out.WriteString("line\n")
	}
	e.log(out.String())
	if n := capture.count(protocol.AgentEventToolLog); n != toolProgressCap {
		t.Fatalf("tool.log chunks = %d, want cap %d", n, toolProgressCap)
	}
}

func TestToolProgress_CompletedCarriesErrorStatus(t *testing.T) {
	e, capture := buildTestEmitter(t, "fail_tool", tools.ToolExecutionSpec{Progress: true})
	e.completed(tools.ErrorResult("boom"))
	if n := capture.count(protocol.AgentEventToolComplete); n != 1 {
		t.Fatalf("expected 1 tool.completed, got %d", n)
	}
	payload, _ := capture.events[0].Payload.(map[string]any)
	if payload["is_error"] != true || payload["status"] != "error" {
		t.Fatalf("completed payload = %#v, want is_error=true status=error", payload)
	}
}
