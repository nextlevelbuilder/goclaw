package tools

import (
	"context"
	"testing"
	"time"
)

// specTestTool is a minimal Tool whose Execute runs a user-supplied function,
// so tests can simulate transient errors, permission errors, and delays.
type specTestTool struct {
	name string
	fn   func(ctx context.Context, args map[string]any) *Result
}

func (s *specTestTool) Name() string        { return s.name }
func (s *specTestTool) Description() string { return "spec test tool" }
func (s *specTestTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (s *specTestTool) Execute(ctx context.Context, args map[string]any) *Result {
	if s.fn != nil {
		return s.fn(ctx, args)
	}
	return NewResult("ok")
}

// specTestToolWithSpec wraps specTestTool with a SpecProvider.
type specTestToolWithSpec struct {
	*specTestTool
	spec ToolExecutionSpec
}

func (s *specTestToolWithSpec) Spec() ToolExecutionSpec { return s.spec }

func TestSpecFor_ZeroValueForPlainTool(t *testing.T) {
	tool := &specTestTool{name: "plain"}
	spec := SpecFor(tool)
	if spec.Retry != RetryNever || spec.RetryMax != 0 || spec.Destructive ||
		spec.SoftTimeout != 0 || spec.HardTimeout != 0 {
		t.Fatalf("SpecFor on a non-SpecProvider tool should be zero value, got %+v", spec)
	}
}

func TestSpecFor_ReturnsDeclaredSpec(t *testing.T) {
	want := ToolExecutionSpec{Retry: RetryAuto, RetryMax: 2, HardTimeout: 5 * time.Second}
	tool := &specTestToolWithSpec{specTestTool: &specTestTool{name: "spec"}, spec: want}
	if got := SpecFor(tool); got != want {
		t.Fatalf("SpecFor = %+v, want %+v", got, want)
	}
}

func TestExecuteWithSpec_ZeroValuePassesThrough(t *testing.T) {
	calls := 0
	tool := &specTestTool{name: "zero", fn: func(ctx context.Context, args map[string]any) *Result {
		calls++
		return ErrorResult("permission denied")
	}}
	res := ExecuteWithSpec(context.Background(), tool, nil, ToolExecutionSpec{})
	if calls != 1 || res == nil || !res.IsError {
		t.Fatalf("zero-value spec should pass through untouched: calls=%d res=%+v", calls, res)
	}
}

func TestExecuteWithSpec_RetryAutoOnTransientError(t *testing.T) {
	// t.Parallel not used: mutates package-level retryDelay.
	calls := 0
	tool := &specTestTool{name: "flaky", fn: func(ctx context.Context, args map[string]any) *Result {
		calls++
		if calls == 1 {
			return ErrorResult("HTTP 429 too many requests")
		}
		return NewResult("ok")
	}}
	spec := ToolExecutionSpec{Retry: RetryAuto, RetryMax: 2}
	res := ExecuteWithSpec(context.Background(), tool, nil, spec)
	if res == nil || res.IsError {
		t.Fatalf("transient error should be retried: res=%+v", res)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (1 failure + 1 retry)", calls)
	}
}

func TestExecuteWithSpec_NoRetryOnPermissionError(t *testing.T) {
	calls := 0
	tool := &specTestTool{name: "deny", fn: func(ctx context.Context, args map[string]any) *Result {
		calls++
		return ErrorResult("permission denied")
	}}
	spec := ToolExecutionSpec{Retry: RetryAuto, RetryMax: 3}
	res := ExecuteWithSpec(context.Background(), tool, nil, spec)
	if calls != 1 || res == nil || !res.IsError {
		t.Fatalf("permission error must not retry: calls=%d res=%+v", calls, res)
	}
}

func TestExecuteWithSpec_NoRetryWhenDestructive(t *testing.T) {
	calls := 0
	tool := &specTestTool{name: "drop", fn: func(ctx context.Context, args map[string]any) *Result {
		calls++
		return ErrorResult("connection reset")
	}}
	spec := ToolExecutionSpec{Retry: RetryAuto, RetryMax: 3, Destructive: true}
	res := ExecuteWithSpec(context.Background(), tool, nil, spec)
	if calls != 1 || res == nil || !res.IsError {
		t.Fatalf("destructive tool must never retry: calls=%d res=%+v", calls, res)
	}
}

func TestExecuteWithSpec_RetryExhaustedReturnsError(t *testing.T) {
	calls := 0
	tool := &specTestTool{name: "doomed", fn: func(ctx context.Context, args map[string]any) *Result {
		calls++
		return ErrorResult("HTTP 503 service unavailable")
	}}
	spec := ToolExecutionSpec{Retry: RetryAuto, RetryMax: 2}
	res := ExecuteWithSpec(context.Background(), tool, nil, spec)
	// 1 original attempt + 2 retries = 3 calls, terminal error.
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if res == nil || !res.IsError {
		t.Fatalf("exhausted retries should return the error: res=%+v", res)
	}
}

func TestExecuteWithSpec_HardTimeoutReturnsErrorResult(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)
	tool := &specTestTool{name: "slow", fn: func(ctx context.Context, args map[string]any) *Result {
		<-ctx.Done()
		return ErrorResult(ctx.Err().Error())
	}}
	spec := ToolExecutionSpec{HardTimeout: 50 * time.Millisecond}
	res := ExecuteWithSpec(context.Background(), tool, nil, spec)
	if res == nil || !res.IsError {
		t.Fatalf("hard timeout should yield an error result: res=%+v", res)
	}
}

func TestExecuteWithSpec_DeadlineExceededNotRetried(t *testing.T) {
	calls := 0
	tool := &specTestTool{name: "timeout-flaky", fn: func(ctx context.Context, args map[string]any) *Result {
		calls++
		<-ctx.Done()
		return ErrorResult(ctx.Err().Error())
	}}
	spec := ToolExecutionSpec{HardTimeout: 50 * time.Millisecond, Retry: RetryAuto, RetryMax: 3, SoftTimeout: 0}
	res := ExecuteWithSpec(context.Background(), tool, nil, spec)
	// DeadlineExceeded must not be retried.
	if calls != 1 {
		t.Fatalf("deadline exceeded must not be retried: calls=%d", calls)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected terminal error result: res=%+v", res)
	}
}

func TestExecuteWithSpec_RetryRepairTrimsArgsOnce(t *testing.T) {
	calls := 0
	var seenFirst, seenSecond map[string]any
	tool := &specTestTool{name: "repair", fn: func(ctx context.Context, args map[string]any) *Result {
		calls++
		if calls == 1 {
			seenFirst = args
			return ErrorResult("HTTP 500 internal error")
		}
		seenSecond = args
		if s, _ := args["query"].(string); s != "hello world" {
			return ErrorResult("args not repaired")
		}
		return NewResult("ok")
	}}
	spec := ToolExecutionSpec{Retry: RetryRepair, RetryMax: 3}
	res := ExecuteWithSpec(context.Background(), tool, map[string]any{"query": "  hello world  "}, spec)
	if res == nil || res.IsError {
		t.Fatalf("repaired retry should succeed: res=%+v", res)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if got, _ := seenFirst["query"].(string); got != "  hello world  " {
		t.Fatalf("first attempt should get original args, got %q", got)
	}
	if got, _ := seenSecond["query"].(string); got != "hello world" {
		t.Fatalf("repaired attempt should get trimmed args, got %q", got)
	}
}

func TestExecuteWithSpec_RetryRepairStopsAfterOneRepair(t *testing.T) {
	calls := 0
	tool := &specTestTool{name: "repair-loop", fn: func(ctx context.Context, args map[string]any) *Result {
		calls++
		return ErrorResult("HTTP 500 internal error")
	}}
	spec := ToolExecutionSpec{Retry: RetryRepair, RetryMax: 5}
	res := ExecuteWithSpec(context.Background(), tool, map[string]any{"query": " x "}, spec)
	// 1 original + 1 repaired retry, then stop.
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected error after one repaired retry: res=%+v", res)
	}
}

func TestExecuteWithSpec_SoftTimeoutWarnsOnly(t *testing.T) {
	tool := &specTestTool{name: "soft", fn: func(ctx context.Context, args map[string]any) *Result {
		time.Sleep(30 * time.Millisecond)
		return NewResult("slow but ok")
	}}
	spec := ToolExecutionSpec{SoftTimeout: 5 * time.Millisecond}
	res := ExecuteWithSpec(context.Background(), tool, nil, spec)
	// Soft timeout must not abort — the result is returned as-is.
	if res == nil || res.IsError {
		t.Fatalf("soft timeout should not abort: res=%+v", res)
	}
}

func TestRetryableResult_ErrBacked(t *testing.T) {
	// Confirm a fields-only Result surfaces a retryable class without strings.
	res := &Result{IsError: true, ForLLM: "boom"}
	if retryableResult(res) {
		t.Fatal("plain text without a transient marker must not be retryable")
	}
	res = &Result{IsError: true, ForLLM: "HTTP 429 too many requests"}
	if !retryableResult(res) {
		t.Fatal("429 text should be retryable")
	}
}

func TestExecuteWithSpec_ForLLMTextOnlyTransient(t *testing.T) {
	calls := 0
	tool := &specTestTool{name: "text-flaky", fn: func(ctx context.Context, args map[string]any) *Result {
		calls++
		if calls == 1 {
			return &Result{IsError: true, ForLLM: "rate limit exceeded"}
		}
		return NewResult("ok")
	}}
	spec := ToolExecutionSpec{Retry: RetryAuto, RetryMax: 2}
	res := ExecuteWithSpec(context.Background(), tool, nil, spec)
	if calls != 2 || res == nil || res.IsError {
		t.Fatalf("text-only transient should be retried: calls=%d res=%+v", calls, res)
	}
}