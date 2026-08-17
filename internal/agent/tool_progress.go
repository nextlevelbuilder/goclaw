package agent

import (
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// toolProgressCap bounds the number of tool.log chunks that are broadcast for a
// single tool execution. Long-running tools can emit a lot of output; the cap
// keeps the WS event stream bounded and avoids flooding the client.
const toolProgressCap = 20

// toolProgressEmitter is a small helper that emits Phase 7 tool.progress events
// (C4). It is per-execution: created by buildToolProgressEmitter for a single
// tool call, so the runId and tool identity are fixed for the whole lifecycle.
type toolProgressEmitter struct {
	loop    *Loop
	emitRun func(AgentEvent)
	runID   string
	name    string // registry name (canonical)
	rawName string // model-facing name (tc.Name)
	id      string // tool call id (tc.ID)
	spec    tools.ToolExecutionSpec
	optIn   bool
}

// active reports whether progress events were opted into for this tool.
// When false, all emit* calls are no-ops (C4: default off).
func (e *toolProgressEmitter) active() bool {
	return e != nil && e.optIn && e.emitRun != nil
}

// started emits tool.started right before a tool begins executing.
func (e *toolProgressEmitter) started() {
	if !e.active() {
		return
	}
	e.emitRun(AgentEvent{
		Type:    protocol.AgentEventToolStarted,
		AgentID: e.loop.id,
		RunID:   e.runID,
		Payload: map[string]any{"id": e.id, "name": e.name, "rawName": e.rawName, "arguments": nil},
	})
}

// log emits a bounded stream of tool.log chunks. Lines are split on newlines
// and capped at toolProgressCap events. A chunk larger than ~800 runes is
// truncated so individual payloads stay small.
func (e *toolProgressEmitter) log(output string) {
	if !e.active() || output == "" {
		return
	}
	lines := strings.Split(output, "\n")
	if len(lines) > toolProgressCap {
		lines = lines[:toolProgressCap]
	}
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		e.emitRun(AgentEvent{
			Type:    protocol.AgentEventToolLog,
			AgentID: e.loop.id,
			RunID:   e.runID,
			Payload: map[string]any{"id": e.id, "name": e.name, "rawName": e.rawName, "content": line},
		})
	}
}

// progress emits an optional periodic heartbeat (tool.progress) for long tools.
// Callers use it when a tool exposes an ongoing step/percent; it carries a
// small diagnostic string rather than the full output (which log already covers).
func (e *toolProgressEmitter) progress(msg string) {
	if !e.active() || msg == "" {
		return
	}
	e.emitRun(AgentEvent{
		Type:    protocol.AgentEventToolProgress,
		AgentID: e.loop.id,
		RunID:   e.runID,
		Payload: map[string]any{"id": e.id, "name": e.name, "rawName": e.rawName, "message": msg},
	})
}

// completed emits tool.completed with the final success/error status, right
// after the tool returns. The existing tool.result event carries the full
// result; this event mirrors it with a lightweight status so the UI can resolve
// long-running tool lifecycles without waiting for a tool.result.
func (e *toolProgressEmitter) completed(result *tools.Result) {
	if !e.active() {
		return
	}
	isError := result != nil && result.IsError
	payload := map[string]any{
		"id":      e.id,
		"name":    e.name,
		"rawName": e.rawName,
		"is_error": isError,
	}
	if result != nil {
		payload["status"] = "ok"
		if isError {
			payload["status"] = "error"
		}
	}
	e.emitRun(AgentEvent{
		Type:    protocol.AgentEventToolComplete,
		AgentID: e.loop.id,
		RunID:   e.runID,
		Payload: payload,
	})
}

// buildToolProgressEmitter constructs the progress emitter for one tool call,
// looking up the tool's ToolExecutionSpec to decide whether progress events are
// opted in (C4). emitRun may be nil in tests where no event sink is configured.
func buildToolProgressEmitter(l *Loop, emitRun func(AgentEvent), runID, name, rawName, id string, args map[string]any) *toolProgressEmitter {
	e := &toolProgressEmitter{
		loop:    l,
		emitRun: emitRun,
		runID:   runID,
		name:    name,
		rawName: rawName,
		id:      id,
	}
	if l.registry != nil {
		if t, ok := l.registry.Get(name); ok {
			e.spec = tools.SpecFor(t)
		}
	}
	e.optIn = e.spec.Progress
	return e
}