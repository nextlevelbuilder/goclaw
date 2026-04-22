package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dop251/goja"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// compile returns a cached compiled program; fresh compile on cache miss or
// version change. strict=true gives stronger semantics (assigning to an
// undeclared variable throws instead of silently creating a global).
func (h *ScriptHandler) compile(cfg hooks.HookConfig, source string) (*goja.Program, error) {
	key := progCacheKey{ID: cfg.ID, Version: cfg.Version}
	if prog, ok := h.progCache.Get(key); ok {
		return prog, nil
	}
	prog, err := goja.Compile(cfg.ID.String(), source, true)
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	h.progCache.Add(key, prog)
	return prog, nil
}

// bindEvent exposes a deep-frozen `event` global mirroring hooks.Event.
//
// The JSON round-trip disconnects the JS-side object from the Go map — goja
// would otherwise wrap ev.ToolInput as a live Go map view and script writes
// would propagate back, bypassing the source-tier capability check. One
// serialize/parse pair per exec is acceptable on the hook path.
//
// UUIDs serialize as strings; scripts observe `event.tenantId` / `event.agentId`
// as strings, not typed UUIDs.
func bindEvent(rt *goja.Runtime, ev hooks.Event) error {
	viewable := map[string]any{
		"hookEvent": string(ev.HookEvent),
		"sessionId": ev.SessionID,
		"tenantId":  ev.TenantID.String(),
		"agentId":   ev.AgentID.String(),
		"toolName":  ev.ToolName,
		"toolInput": ev.ToolInput,
		"rawInput":  ev.RawInput,
		"depth":     ev.Depth,
		"eventId":   ev.EventID,
	}
	b, err := json.Marshal(viewable)
	if err != nil {
		return fmt.Errorf("bindEvent marshal: %w", err)
	}

	// Parse in JS so nested objects become native JS objects (not Go wrappers),
	// then recursively Object.freeze every level to block in-place mutation.
	script := `
var event = ` + string(b) + `;
(function deepFreeze(o) {
  if (o === null || typeof o !== 'object') return;
  Object.freeze(o);
  for (var k in o) {
    if (Object.prototype.hasOwnProperty.call(o, k)) deepFreeze(o[k]);
  }
})(event);
`
	_, err = rt.RunString(script)
	return err
}

// captureStdout binds `console.log` / `console.error` to a truncating buffer.
// Once the buffer reaches MaxStdoutBytes, a single `... truncated` marker is
// written and further writes are dropped so a runaway print loop cannot
// exhaust memory.
func captureStdout(rt *goja.Runtime, buf *strings.Builder) {
	truncated := false
	write := func(level string, args ...any) {
		if truncated {
			return
		}
		parts := make([]string, 0, len(args)+1)
		parts = append(parts, level+":")
		for _, a := range args {
			parts = append(parts, fmt.Sprint(a))
		}
		line := strings.Join(parts, " ") + "\n"
		remaining := MaxStdoutBytes - buf.Len()
		if len(line) > remaining {
			if remaining > 0 {
				line = line[:remaining] + "... truncated\n"
			} else {
				line = "... truncated\n"
			}
			truncated = true
		}
		buf.WriteString(line)
	}
	console := rt.NewObject()
	_ = console.Set("log", func(call goja.FunctionCall) goja.Value {
		args := make([]any, len(call.Arguments))
		for i, a := range call.Arguments {
			args[i] = a.Export()
		}
		write("log", args...)
		return goja.Undefined()
	})
	_ = console.Set("error", func(call goja.FunctionCall) goja.Value {
		args := make([]any, len(call.Arguments))
		for i, a := range call.Arguments {
			args[i] = a.Export()
		}
		write("error", args...)
		return goja.Undefined()
	})
	_ = rt.Set("console", console)
}

// parseReturn enforces the `{decision, reason?, updatedInput?}` contract.
// Any deviation (non-object, missing field, unknown decision string) maps to
// DecisionError with a descriptive error.
func parseReturn(rt *goja.Runtime, v goja.Value) (hooks.Decision, string, map[string]any, error) {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return hooks.DecisionError, "", nil, errors.New("script return: must be object with decision")
	}
	obj := v.ToObject(rt)
	if obj == nil {
		return hooks.DecisionError, "", nil, errors.New("script return: must be object")
	}
	decRaw := obj.Get("decision")
	if decRaw == nil || goja.IsUndefined(decRaw) {
		return hooks.DecisionError, "", nil, errors.New("script return: missing decision field")
	}
	decStr, ok := decRaw.Export().(string)
	if !ok {
		return hooks.DecisionError, "", nil, errors.New("script return: decision must be string")
	}
	dec := hooks.Decision(decStr)
	switch dec {
	case hooks.DecisionAllow, hooks.DecisionBlock, hooks.DecisionAsk, hooks.DecisionDefer:
		// ok
	default:
		return hooks.DecisionError, "", nil, fmt.Errorf("script return: invalid decision %q", decStr)
	}
	var reason string
	if r := obj.Get("reason"); r != nil && !goja.IsUndefined(r) {
		if s, ok := r.Export().(string); ok {
			reason = s
		}
	}
	var updated map[string]any
	if u := obj.Get("updatedInput"); u != nil && !goja.IsUndefined(u) {
		if m, ok := u.Export().(map[string]any); ok {
			updated = m
		}
	}
	return dec, reason, updated, nil
}
