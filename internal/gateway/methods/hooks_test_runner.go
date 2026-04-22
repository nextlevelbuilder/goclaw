package methods

import (
	"context"
	"fmt"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// DispatcherTestRunner is a HookTestRunner that invokes a registered handler
// one-shot without writing to hook_executions. Used by `hooks.test` WS method
// to power the Web UI test panel. dryRun-by-construction: no store writes.
//
// Handlers is expected to be the same map passed to the production dispatcher
// (command + http + prompt). Missing handler → DecisionError result.
type DispatcherTestRunner struct {
	Handlers map[hooks.HandlerType]hooks.Handler
}

// NewDispatcherTestRunner returns a test runner sharing handlers with the
// production dispatcher. Panics if handlers is nil.
func NewDispatcherTestRunner(handlers map[hooks.HandlerType]hooks.Handler) *DispatcherTestRunner {
	if handlers == nil {
		handlers = map[hooks.HandlerType]hooks.Handler{}
	}
	return &DispatcherTestRunner{Handlers: handlers}
}

// RunTest implements HookTestRunner. Enforces the per-hook timeout, captures
// duration, and maps handler return values into HookTestResult. Does NOT
// call the audit writer.
func (r *DispatcherTestRunner) RunTest(ctx context.Context, cfg hooks.HookConfig, ev hooks.Event) HookTestResult {
	h, ok := r.Handlers[cfg.HandlerType]
	if !ok {
		return HookTestResult{
			Decision: hooks.DecisionError,
			Error:    fmt.Sprintf("no handler registered for %q", cfg.HandlerType),
		}
	}

	timeout := 5 * time.Second
	if cfg.TimeoutMS > 0 {
		timeout = time.Duration(cfg.TimeoutMS) * time.Millisecond
	}
	hctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	dec, err := h.Execute(hctx, cfg, ev)
	durationMS := int(time.Since(start) / time.Millisecond)

	res := HookTestResult{
		Decision:   dec,
		DurationMS: durationMS,
	}
	if err != nil {
		res.Error = err.Error()
	}
	return res
}
