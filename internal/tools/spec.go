package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// ToolRetryPolicy controls whether and how a tool execution is retried after a
// transient failure. It is a per-tool execution contract (C1).
type ToolRetryPolicy int

const (
	// RetryNever disables retry entirely (default zero value).
	RetryNever ToolRetryPolicy = iota
	// RetryAuto retries on network timeouts, connection resets, HTTP 429 and
	// temporary 5xx responses, up to RetryMax retries.
	RetryAuto
	// RetryRepair performs a single retry after light argument repair
	// (trim/normalize string fields). No full repair pipeline.
	RetryRepair
)

// ToolExecutionSpec describes reliability behavior for a single tool. The zero
// value is the current behavior: no retry, no deadline (backward compat).
type ToolExecutionSpec struct {
	// Retry policy (RetryNever default). Retry is only attempted when the spec
	// opts in AND the tool is not Destructive (C2).
	Retry ToolRetryPolicy
	// RetryMax caps the number of retries. 0 means no retry.
	RetryMax int
	// Destructive tools are never retried.
	Destructive bool
	// SoftTimeout, when exceeded, only logs a warning. It never aborts the
	// tool; only HardTimeout can do that (C3).
	SoftTimeout time.Duration
	// HardTimeout, when > 0, bounds the whole execution via a context deadline.
	// 0 means unbounded. On expiry an error Result is returned.
	HardTimeout time.Duration
	// Progress opts into emitting tool.started / tool.progress / tool.log /
	// tool.completed agent-frame events (Phase 7 C4). Default false — progress
	// events are never emitted for tools that do not opt in.
	Progress bool
}

// SpecProvider is the optional per-tool interface that supplies an execution
// spec. Tools that do not implement it get the zero-value spec (no-op). This
// mirrors the existing XxxAware interface-assertion pattern in types.go.
type SpecProvider interface {
	Spec() ToolExecutionSpec
}

// SpecFor returns the execution spec for t. Tools that do not implement
// SpecProvider get the zero-value spec, which preserves current behavior.
func SpecFor(t Tool) ToolExecutionSpec {
	if sp, ok := t.(SpecProvider); ok {
		return sp.Spec()
	}
	return ToolExecutionSpec{}
}

// requiresExecutionSpec reports whether spec opts into a deadline, soft-timeout
// warning, or a retry. If false, ExecuteWithSpec behaves exactly like a plain
// tool.Execute call.
func (s ToolExecutionSpec) requiresExecutionSpec() bool {
	return s.HardTimeout > 0 || s.SoftTimeout > 0 || (s.Retry != RetryNever && s.RetryMax > 0)
}

// retryAllows implements the C2 gate: retry only for non-destructive tools that
// opted into a retry policy.
func (s ToolExecutionSpec) retryAllows() bool {
	return !s.Destructive && s.Retry != RetryNever && s.RetryMax > 0
}

// retryableResult reports whether a tool Result represents a transient failure,
// consulting Result.Err first and falling back to the ForLLM text so tools that
// only surface failures via their message still benefit from retry.
func retryableResult(res *Result) bool {
	if res == nil {
		return false
	}
	if res.Err != nil && isRetryableToolError(res.Err) {
		return true
	}
	return isRetryableText(res.ForLLM)
}

// isRetryableToolError classifies a tool error as transient: network timeouts,
// connection resets, HTTP 429 and temporary 5xx. It reuses the provider-layer
// retryable classifier so behavior stays consistent with provider retries.
func isRetryableToolError(err error) bool {
	if err == nil {
		return false
	}
	// A firing hard deadline is not a transient blip — the run's budget was
	// consumed. Retrying it would keep re-wrapping a spent context and fail
	// immediately; classify as non-retryable.
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if providers.IsRetryableError(err) {
		return true
	}
	return isRetryableText(err.Error())
}

// isRetryableText mirrors the transient marker set used by
// providers.IsRetryableError for tool surfaces that expose only a message.
func isRetryableText(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "429") ||
		strings.Contains(lower, "500") ||
		strings.Contains(lower, "502") ||
		strings.Contains(lower, "503") ||
		strings.Contains(lower, "504") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "temporarily unavailable") ||
		strings.Contains(lower, "while processing your request") ||
		strings.Contains(lower, "timeout")
}

// RetryDelay is the fixed backoff between tool retries. It is intentionally
// short and non-exponential — tool retries are a safety net, not the provider
// retry loop. A var so tests (including the agent package) can shrink
// real-time delays.
var RetryDelay = func(attempt int) time.Duration {
	if attempt <= 1 {
		return 100 * time.Millisecond
	}
	return time.Duration(attempt) * 100 * time.Millisecond
}

// ExecuteWithSpec runs a tool with the reliability behavior described by spec.
// It is the shared execution choke point used by both the registry layer
// (safeExecute) and the per-user MCP path (executeToolForActor).
//
// Order of operations (C3):
//  1. HardTimeout > 0 → wrap ctx with context.WithTimeout per attempt.
//  2. Execute.
//  3. SoftTimeout warning (advisory only, never aborts).
//  4. Retry (per spec) when the tool returns a transient error.
//
// A HardTimeout expiry returns an error Result — no side-effect retry is
// attempted. When the spec is the zero value, execution is passed through
// untouched.
func ExecuteWithSpec(ctx context.Context, tool Tool, args map[string]any, spec ToolExecutionSpec) *Result {
	if !spec.requiresExecutionSpec() {
		return tool.Execute(ctx, args)
	}

	start := time.Now()
	attemptArgs := args
	retriesDone := 0

	for {
		result, timedOut := executeOnce(ctx, tool, attemptArgs, spec)
		if timedOut {
			return deadlineResult(tool, spec, result)
		}
		if result == nil {
			result = ErrorResult("tool returned nil result")
		}

		// Soft timeout is advisory only — never aborts (C3).
		if spec.SoftTimeout > 0 && time.Since(start) > spec.SoftTimeout {
			slog.Warn("tools.spec.soft_timeout",
				"tool", tool.Name(),
				"soft_timeout_ms", spec.SoftTimeout.Milliseconds(),
				"elapsed_ms", time.Since(start).Milliseconds())
		}

		if !result.IsError || !spec.retryAllows() || retriesDone >= spec.RetryMax {
			return result
		}

		// RetryRepair: repair arguments once on the first retry, then stop
		// (C2 — one repaired retry, no full repair pipeline).
		nextArgs := attemptArgs
		if spec.Retry == RetryRepair {
			if retriesDone == 0 {
				nextArgs = repairArgsForRetry(attemptArgs)
			} else {
				return result
			}
		}

		if !retryableResult(result) {
			return result
		}

		slog.Debug("tools.spec.retry",
			"tool", tool.Name(),
			"retry", retriesDone+1,
			"max_retries", spec.RetryMax,
			"error", resultErrorSummary(result))

		select {
		case <-ctx.Done():
			// Outer context cancelled (e.g. run teardown) — stop retrying.
			return result
		case <-time.After(RetryDelay(retriesDone + 1)):
		}

		retriesDone++
		attemptArgs = nextArgs
	}
}

// executeOnce runs one attempt under the spec's hard deadline. It captures
// whether the deadline fired on this attempt *before* releasing the sub-context
// (after cancel(), the error would read as context.Canceled, not
// DeadlineExceeded).
func executeOnce(ctx context.Context, tool Tool, args map[string]any, spec ToolExecutionSpec) (result *Result, timedOut bool) {
	if spec.HardTimeout <= 0 {
		return tool.Execute(ctx, args), false
	}
	runCtx, cancel := context.WithTimeout(ctx, spec.HardTimeout)
	result = tool.Execute(runCtx, args)
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		timedOut = true
	}
	cancel()
	return result, timedOut
}

// deadlineResult builds the terminal Result for a hard-timeout expiry. A tool
// that honored ctx and surfaced its own error message keeps that message;
// otherwise a synthesized timeout error is returned (C3).
func deadlineResult(tool Tool, spec ToolExecutionSpec, result *Result) *Result {
	if result != nil && result.IsError {
		return result
	}
	return ErrorResult(fmt.Sprintf("tool %q exceeded hard timeout of %s", tool.Name(), spec.HardTimeout))
}

// resultErrorSummary picks the best diagnostic for the retry log line.
func resultErrorSummary(result *Result) string {
	if result == nil {
		return ""
	}
	if result.Err != nil {
		return result.Err.Error()
	}
	return result.ForLLM
}

// repairArgsForRetry trims/normalizes string fields in args for a RetryRepair
// retry. It is deliberately minimal — no full repair pipeline (C2).
func repairArgsForRetry(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	repaired := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok && s != strings.TrimSpace(s) {
			repaired[k] = strings.TrimSpace(s)
			continue
		}
		repaired[k] = v
	}
	return repaired
}