package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Spec is a declarative, struct-based workflow description used by ParseSpec.
// It avoids a YAML dependency while still letting callers define a workflow
// from data rather than imperative AddStep calls.
type Spec struct {
	// Name of the resulting DAG.
	Name string
	// Steps in the workflow. Order is preserved; IDs must be unique.
	Steps []SpecStep
}

// SpecStep mirrors Step but with a stable, data-friendly shape.
type SpecStep struct {
	ID    string
	Name  string
	Type  StepType
	Run   SpecFunc
	Deps  []string
	Cond  SpecCond
	Retry *RetryPolicy
	// TimeoutSeconds is the per-attempt timeout in seconds. Zero disables it.
	TimeoutSeconds int
}

// SpecFunc is the step body for a SpecStep. Steps that need richer hooks
// (OnError/OnComplete) or custom timeouts should construct Step directly.
type SpecFunc func(ctx context.Context, rc *RunCtx) error

// SpecCond is the conditional gate for a SpecStep.
type SpecCond func(ctx context.Context, rc *RunCtx) bool

// ParseSpec builds a DAG from a Spec. It validates every step and registers
// them in order. Run and Cond are optional: a step with no Run is a no-op
// (useful for data-passing intermediate nodes); a conditional step with no
// Cond defaults to always-true.
func ParseSpec(spec Spec) (*DAG, error) {
	if len(spec.Steps) == 0 {
		return nil, errEmptySpec
	}
	d := NewDAG(spec.Name)
	for _, ss := range spec.Steps {
		var run func(ctx context.Context, rc *RunCtx) error
		if ss.Run != nil {
			run = ss.Run
		} else {
			// No-op body keeps intermediate steps valid.
			run = func(context.Context, *RunCtx) error { return nil }
		}

		st := &Step{
			ID:      ss.ID,
			Name:    ss.Name,
			Type:    ss.Type,
			Deps:    append([]string(nil), ss.Deps...),
			Retry:   ss.Retry,
			Timeout: time.Duration(ss.TimeoutSeconds) * time.Second,
			Run:     run,
		}
		if ss.Type == StepConditional {
			if ss.Cond == nil {
				return nil, fmt.Errorf("workflow: spec step %q: conditional step requires Cond", ss.ID)
			}
			st.Cond = func(ctx context.Context, rc *RunCtx) bool { return ss.Cond(ctx, rc) }
		}
		if err := d.AddStep(st); err != nil {
			return nil, fmt.Errorf("workflow: parse spec: %w", err)
		}
	}
	// Validate the graph up front (cycle / unknown deps) so a bad spec fails
	// at parse time rather than at run time.
	if _, err := d.TopoOrder(); err != nil {
		return nil, err
	}
	return d, nil
}

var errEmptySpec = errors.New("workflow: parse spec: at least one step required")
