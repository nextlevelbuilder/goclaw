// Package workflow provides a minimal native DAG executor for agent
// workflows. It supports sequential, parallel, conditional, and retry step
// semantics over plain Go functions, with an in-memory RunCtx used to pass
// values between steps. No database and no external dependencies.
package workflow

import (
	"context"
	"errors"
	"sync"
	"time"
)

// StepType enumerates the execution semantics of a step.
type StepType int

const (
	// StepSequential runs the step after all its dependencies complete,
	// respecting topological order.
	StepSequential StepType = iota
	// StepParallel fans the step out as a goroutine alongside its sibling
	// steps that are ready at the same level. Parallel error policy is
	// FailFast: the first error cancels the run.
	StepParallel
	// StepConditional runs the step only when Cond returns true. The step is
	// skipped (no error) when Cond returns false or Cond is nil.
	StepConditional
	// StepRetry wraps the step's Run func with MaxAttempts and linear
	// Backoff. Timeout applies per attempt via a child context.
	StepRetry
)

// String returns a human-readable name for the step type.
func (s StepType) String() string {
	switch s {
	case StepSequential:
		return "sequential"
	case StepParallel:
		return "parallel"
	case StepConditional:
		return "conditional"
	case StepRetry:
		return "retry"
	default:
		return "unknown"
	}
}

// RunCtx carries data between steps during a single DAG run. Access is safe
// for concurrent use by parallel steps.
type RunCtx struct {
	mu     sync.RWMutex
	values map[string]any
}

// NewRunCtx returns an empty RunCtx ready for use.
func NewRunCtx() *RunCtx {
	return &RunCtx{values: make(map[string]any)}
}

// SetOutput stores a value under key.
func (rc *RunCtx) SetOutput(key string, val any) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.values == nil {
		rc.values = make(map[string]any)
	}
	rc.values[key] = val
}

// GetOutput returns the value stored under key.
func (rc *RunCtx) GetOutput(key string) (any, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	v, ok := rc.values[key]
	return v, ok
}

// Outputs returns a shallow copy of the current values map.
func (rc *RunCtx) Outputs() map[string]any {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	out := make(map[string]any, len(rc.values))
	for k, v := range rc.values {
		out[k] = v
	}
	return out
}

// CondFunc gates whether a conditional step executes.
type CondFunc func(ctx context.Context, rc *RunCtx) bool

// RetryPolicy configures retry behavior for a StepRetry step.
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts (>= 1). Zero defaults to 1.
	MaxAttempts int
	// Backoff is the base sleep between attempts in a linear series:
	// Backoff * attempt. Zero means no sleep between attempts.
	Backoff time.Duration
}

// Step is a single node in the workflow DAG.
type Step struct {
	// ID is a unique identifier within the DAG.
	ID string
	// Name is a human-readable label, optional.
	Name string
	// Type controls execution semantics.
	Type StepType
	// Run is the step body. It receives a per-attempt context (for timeout /
	// cancellation) and the shared RunCtx.
	Run func(ctx context.Context, rc *RunCtx) error
	// Deps lists IDs of steps that must complete before this one.
	Deps []string
	// Cond gates execution for StepConditional steps.
	Cond CondFunc
	// Retry configures retries for StepRetry steps.
	Retry *RetryPolicy
	// Timeout bounds each attempt. Zero means no per-step timeout.
	Timeout time.Duration
	// OnError runs when Run (after retries) fails. A non-nil error returned by
	// OnError replaces the step error.
	OnError func(ctx context.Context, rc *RunCtx, err error) error
	// OnComplete runs after a successful step execution.
	OnComplete func(ctx context.Context, rc *RunCtx) error
}

// Validate reports structural problems with the step. It is called by
// AddStep and by ParseSpec.
func (s *Step) Validate() error {
	if s == nil {
		return errors.New("workflow: nil step")
	}
	if s.ID == "" {
		return errors.New("workflow: step ID must not be empty")
	}
	if s.Run == nil {
		return errors.New("workflow: step " + s.ID + ": Run func must not be nil")
	}
	if s.Type == StepConditional && s.Cond == nil {
		return errors.New("workflow: step " + s.ID + ": conditional step requires Cond")
	}
	if s.Type == StepRetry {
		if s.Retry == nil {
			return errors.New("workflow: step " + s.ID + ": retry step requires Retry policy")
		}
		if s.Retry.MaxAttempts < 0 {
			return errors.New("workflow: step " + s.ID + ": Retry.MaxAttempts must be >= 0 (0 means 1 attempt)")
		}
	}
	return nil
}
