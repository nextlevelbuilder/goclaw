package hooks

import "context"

// ScriptResult carries non-decision outputs from a script handler execution
// (reason text, updatedInput proposal, captured stdout). The dispatcher
// provisions one via context before calling Handler.Execute; the handler
// populates it on success. Phase 03 wires this into the dispatcher so that
// only builtin-source hooks can apply UpdatedInput to the pipeline state.
//
// Fields are read-write within a single Execute call; not concurrent-safe
// (each Execute gets its own instance via a context key).
type ScriptResult struct {
	// Reason is the human-readable explanation returned by the script.
	Reason string
	// UpdatedInput is a proposed replacement for Event.ToolInput. Dispatcher
	// applies only when cfg.Source == "builtin" (source-tier capability).
	UpdatedInput map[string]any
	// Stdout is the captured console.log / console.error output, bounded by
	// handlers.MaxStdoutBytes (truncated with a marker when exceeded).
	Stdout string
}

// ctxScriptResultKey is the private context-value key.
type ctxScriptResultKey struct{}

// WithScriptResult returns a ctx carrying r. The dispatcher calls this before
// invoking a script handler so the handler can write its non-decision outputs
// without widening the Handler interface.
func WithScriptResult(ctx context.Context, r *ScriptResult) context.Context {
	return context.WithValue(ctx, ctxScriptResultKey{}, r)
}

// ScriptResultFrom retrieves the result pointer from ctx, or nil when absent.
// Handler code must be nil-tolerant; non-script or Phase-02 standalone paths
// won't provision one.
func ScriptResultFrom(ctx context.Context) *ScriptResult {
	if v, ok := ctx.Value(ctxScriptResultKey{}).(*ScriptResult); ok {
		return v
	}
	return nil
}
