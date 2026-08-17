package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// specExecTool is a tools.Tool used to test the executeToolForActor wrapper. It
// can be registered in a registry (shared path) or stored in mcpUserTools
// (per-user MCP path).
type specExecTool struct {
	name string
	fn   func(ctx context.Context, args map[string]any) *tools.Result
	spec tools.ToolExecutionSpec
}

func (s *specExecTool) Name() string        { return s.name }
func (s *specExecTool) Description() string { return "spec exec test tool" }
func (s *specExecTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (s *specExecTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	if s.fn != nil {
		return s.fn(ctx, args)
	}
	return tools.NewResult("ok")
}
func (s *specExecTool) Spec() tools.ToolExecutionSpec { return s.spec }

// specExecExecutor is a minimal tools.ToolExecutor backed by a registry.
type specExecExecutor struct {
	reg *tools.Registry
}

func (s *specExecExecutor) ExecuteWithContext(ctx context.Context, name string, args map[string]any, channel, chatID, peerKind, sessionKey string, asyncCB tools.AsyncCallback) *tools.Result {
	return s.reg.ExecuteWithContext(ctx, name, args, channel, chatID, peerKind, sessionKey, asyncCB)
}
func (s *specExecExecutor) TryActivateDeferred(string) bool          { return false }
func (s *specExecExecutor) ProviderDefs() []providers.ToolDefinition { return nil }
func (s *specExecExecutor) Get(string) (tools.Tool, bool)            { return nil, false }
func (s *specExecExecutor) List() []string                           { return nil }
func (s *specExecExecutor) Aliases() map[string]string               { return nil }

// newSpecExecLoop builds a Loop whose shared registry executes tools and a
// Registry's ExecuteWithContext path. The rate limiter is left unset so tool
// execution reaches safeExecute immediately.
func newSpecExecLoop(reg *tools.Registry) *Loop {
	return &Loop{
		id:          "spec-test",
		tools:       &specExecExecutor{reg: reg},
		registry:    reg,
		mcpUserTools: sync.Map{},
		onEvent:     func(AgentEvent) {},
	}
}

// TestExecuteToolForActor_RegistryRetriesTransient verifies the shared-registry
// path honors the retry spec at the executeToolForActor choke point.
func TestExecuteToolForActor_RegistryRetriesTransient(t *testing.T) {
	if err := overrideRetryDelay(t); err != nil {
		t.Fatal(err)
	}
	calls := 0
	reg := tools.NewRegistry()
	reg.Register(&specExecTool{name: "spec_retry", spec: tools.ToolExecutionSpec{Retry: tools.RetryAuto, RetryMax: 2},
		fn: func(ctx context.Context, args map[string]any) *tools.Result {
			calls++
			if calls == 1 {
				return tools.ErrorResult("HTTP 429 too many requests")
			}
			return tools.NewResult("ok")
		}})
	l := newSpecExecLoop(reg)

	res := l.executeToolForActor(context.Background(), "spec_retry", nil, "", "", "", "", "")
	if res == nil || res.IsError {
		t.Fatalf("transient error should have been retried: res=%+v", res)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

// TestExecuteToolForActor_NoRetryOnPermissionError verifies a non-transient
// error from a spec'd tool is returned without retry.
func TestExecuteToolForActor_NoRetryOnPermissionError(t *testing.T) {
	if err := overrideRetryDelay(t); err != nil {
		t.Fatal(err)
	}
	calls := 0
	reg := tools.NewRegistry()
	reg.Register(&specExecTool{name: "spec_deny", spec: tools.ToolExecutionSpec{Retry: tools.RetryAuto, RetryMax: 3},
		fn: func(ctx context.Context, args map[string]any) *tools.Result {
			calls++
			return tools.ErrorResult("permission denied")
		}})
	l := newSpecExecLoop(reg)

	res := l.executeToolForActor(context.Background(), "spec_deny", nil, "", "", "", "", "")
	if calls != 1 || res == nil || !res.IsError {
		t.Fatalf("permission error must not retry: calls=%d res=%+v", calls, res)
	}
}

// TestExecuteToolForActor_NoRetryWhenDestructive verifies destructive tools
// never retry even when the spec opts in.
func TestExecuteToolForActor_NoRetryWhenDestructive(t *testing.T) {
	if err := overrideRetryDelay(t); err != nil {
		t.Fatal(err)
	}
	calls := 0
	reg := tools.NewRegistry()
	reg.Register(&specExecTool{name: "spec_drop", spec: tools.ToolExecutionSpec{Retry: tools.RetryAuto, RetryMax: 3, Destructive: true},
		fn: func(ctx context.Context, args map[string]any) *tools.Result {
			calls++
			return tools.ErrorResult("connection reset")
		}})
	l := newSpecExecLoop(reg)

	res := l.executeToolForActor(context.Background(), "spec_drop", nil, "", "", "", "", "")
	if calls != 1 || res == nil || !res.IsError {
		t.Fatalf("destructive tool must not retry: calls=%d res=%+v", calls, res)
	}
}

// TestExecuteToolForActor_HardTimeoutDeadline verifies the hard deadline
// translates into an ErrorResult at the choke point.
func TestExecuteToolForActor_HardTimeoutDeadline(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&specExecTool{name: "spec_slow", spec: tools.ToolExecutionSpec{HardTimeout: 50 * time.Millisecond},
		fn: func(ctx context.Context, args map[string]any) *tools.Result {
			<-ctx.Done()
			return tools.ErrorResult(ctx.Err().Error())
		}})
	l := newSpecExecLoop(reg)

	res := l.executeToolForActor(context.Background(), "spec_slow", nil, "", "", "", "", "")
	if res == nil || !res.IsError {
		t.Fatalf("hard timeout should yield error result: res=%+v", res)
	}
}

// TestExecuteToolForActor_ZeroValueSpecUntouched verifies tools without a spec
// execute exactly once with no deadline attached.
func TestExecuteToolForActor_ZeroValueSpecUntouched(t *testing.T) {
	calls := 0
	reg := tools.NewRegistry()
	reg.Register(&specExecTool{
		name: "spec_plain",
		fn: func(ctx context.Context, args map[string]any) *tools.Result {
			calls++
			return tools.NewResult("plain")
		}})
	l := newSpecExecLoop(reg)

	res := l.executeToolForActor(context.Background(), "spec_plain", nil, "", "", "", "", "")
	if calls != 1 || res == nil || res.IsError {
		t.Fatalf("zero-value spec must pass through: calls=%d res=%+v", calls, res)
	}
}

// TestExecuteToolForActor_PerUserMCPRetriesTransient verifies the per-user MCP
// direct-Execute path honors the retry spec.
func TestExecuteToolForActor_PerUserMCPRetriesTransient(t *testing.T) {
	if err := overrideRetryDelay(t); err != nil {
		t.Fatal(err)
	}
	calls := 0
	mcpTool := &specExecTool{name: "mcp_flaky", spec: tools.ToolExecutionSpec{Retry: tools.RetryAuto, RetryMax: 2},
		fn: func(ctx context.Context, args map[string]any) *tools.Result {
			calls++
			if calls == 1 {
				return tools.ErrorResult("HTTP 502 bad gateway")
			}
			return tools.NewResult("mcp ok")
		}}

	reg := tools.NewRegistry()
	l := newSpecExecLoop(reg)
	// Simulate a per-user tool cache populated with this user's MCP BridgeTool.
	l.mcpUserTools.Store("user-1", []tools.Tool{mcpTool})

	res := l.executeToolForActor(context.Background(), "mcp_flaky", nil, "ws", "chat-1", "direct", "sess-1", "user-1")
	if res == nil || res.IsError {
		t.Fatalf("per-user MCP transient error should retry: res=%+v", res)
	}
	if calls != 2 {
		t.Fatalf("mcp calls = %d, want 2", calls)
	}
}

// TestExecuteToolForActor_ErrBackedTransient verifies registration through a
// Result.Err (not just ForLLM text) still classifies as retryable.
func TestExecuteToolForActor_ErrBackedTransient(t *testing.T) {
	if err := overrideRetryDelay(t); err != nil {
		t.Fatal(err)
	}
	calls := 0
	reg := tools.NewRegistry()
	retryErr := errors.New("connection reset by peer")
	reg.Register(&specExecTool{name: "spec_errcase", spec: tools.ToolExecutionSpec{Retry: tools.RetryAuto, RetryMax: 2},
		fn: func(ctx context.Context, args map[string]any) *tools.Result {
			calls++
			if calls == 1 {
				return tools.ErrorResult("boom").WithError(retryErr)
			}
			return tools.NewResult("ok")
		}})
	l := newSpecExecLoop(reg)

	res := l.executeToolForActor(context.Background(), "spec_errcase", nil, "", "", "", "", "")
	if calls != 2 || res == nil || res.IsError {
		t.Fatalf("Err-backed transient must be retried: calls=%d res=%+v", calls, res)
	}
}

// overrideRetryDelay shrinks the package-level retry backoff so tests run in
// wall-clock time instead of hundreds of milliseconds per backoff.
func overrideRetryDelay(t *testing.T) error {
	t.Helper()
	orig := tools.RetryDelay
	tools.RetryDelay = func(attempt int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { tools.RetryDelay = orig })
	return nil
}