package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Run executes the DAG. Steps run in dependency (topological) order;
// independent steps at the same level run concurrently when any of them is
// StepParallel, while sequential steps are serialized with respect to each
// other. Parallel error policy is FailFast: the first step error cancels the
// run, in-flight goroutines are waited for, and the first error is returned.
//
// The same RunCtx is shared by all steps so they can exchange data. Run is
// safe to call multiple times on the same DAG; the DAG itself is not mutated
// during execution.
func (d *DAG) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("workflow: nil context")
	}
	order, err := d.topoOrder(d.steps)
	if err != nil {
		return err
	}
	if len(order) == 0 {
		return nil // empty DAG is a no-op
	}

	rc := NewRunCtx()

	// runCtx is derived from the caller's context; the first step failure
	// cancels it so pending steps and in-flight goroutines observe FailFast.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// states tracks completion per step so dependents can wait on deps.
	type stepState struct {
		mu   sync.Mutex
		done bool
		cond *sync.Cond
	}
	states := make(map[string]*stepState, len(order))
	for _, id := range order {
		st := &stepState{}
		st.cond = sync.NewCond(&st.mu)
		states[id] = st
	}

	// firstErr records the first step error (the FailFast trigger).
	var (
		errMu    sync.Mutex
		firstErr error
	)
	failRun := func(err error) {
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
		cancel()
	}

	markDone := func(id string) {
		st := states[id]
		st.mu.Lock()
		st.done = true
		st.cond.Broadcast()
		st.mu.Unlock()
	}

	// waitDeps blocks until every dependency of id is done, or the run is
	// cancelled.
	waitDeps := func(id string, deps []string) error {
		for _, dep := range deps {
			ds := states[dep]
			ds.mu.Lock()
			for !ds.done {
				if runCtx.Err() != nil {
					ds.mu.Unlock()
					return runCtx.Err()
				}
				ds.cond.Wait()
			}
			ds.mu.Unlock()
		}
		return nil
	}

	// runStepBody executes the step body and records its outcome. The caller
	// is responsible for having already waited for dependencies and (for
	// sequential steps) holding seqMu.
	runStepBody := func(id string) {
		defer markDone(id)
		if runCtx.Err() != nil {
			// Run was cancelled after deps resolved; skip execution.
			return
		}
		if err := executeStep(runCtx, d.steps[id], rc); err != nil {
			failRun(&RunError{StepID: id, Err: err})
		}
	}

	var (
		seqMu sync.Mutex
		wg    sync.WaitGroup
	)
	for _, id := range order {
		id := id
		s := d.steps[id]
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Dependency waits happen WITHOUT holding the serialization lock,
			// so a sequential step can never block a step it depends on
			// (deadlock-free). Dependencies always precede dependents in topo
			// order, so their goroutines have already been launched.
			if err := waitDeps(id, s.Deps); err != nil {
				// Run cancelled; the step never executes. Mark done so
				// dependents do not block forever.
				markDone(id)
				return
			}
			if s.Type == StepParallel {
				runStepBody(id)
				return
			}
			// Non-parallel steps serialize against each other so execution
			// order is deterministic. The mutex is taken only after
			// dependencies are satisfied.
			seqMu.Lock()
			defer seqMu.Unlock()
			runStepBody(id)
		}()
	}
	wg.Wait()

	errMu.Lock()
	runErr := firstErr
	errMu.Unlock()
	if runErr == nil && runCtx.Err() != nil {
		// The run was cancelled externally (or by a step that cancelled the
		// context without failing). Surface the cancellation instead of a
		// misleading nil.
		return runCtx.Err()
	}
	return runErr
}

// executeStep applies the step's Type semantics: the conditional gate, retry
// policy (max attempts with linear backoff), per-attempt timeout, and the
// OnError/OnComplete hooks.
func executeStep(ctx context.Context, s *Step, rc *RunCtx) error {
	// Conditional gate: skipped steps are not errors.
	if s.Type == StepConditional && s.Cond != nil && !s.Cond(ctx, rc) {
		return nil
	}

	attempts := 1
	if s.Type == StepRetry && s.Retry != nil && s.Retry.MaxAttempts > 0 {
		attempts = s.Retry.MaxAttempts
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 && s.Retry != nil && s.Retry.Backoff > 0 {
			select {
			case <-time.After(s.Retry.Backoff * time.Duration(i)):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		aCtx := ctx
		var cancel context.CancelFunc
		if s.Timeout > 0 {
			aCtx, cancel = context.WithTimeout(ctx, s.Timeout)
		}
		err := s.Run(aCtx, rc)
		if cancel != nil {
			cancel()
		}
		if err == nil {
			// Success: run the completion hook if any.
			if s.OnComplete != nil {
				if onErr := s.OnComplete(ctx, rc); onErr != nil {
					return onErr
				}
			}
			return nil
		}
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			// The run was cancelled externally; do not retry.
			return err
		}
		lastErr = err
	}

	if s.OnError != nil {
		return s.OnError(ctx, rc, lastErr) // nil return recovers the step
	}
	return lastErr
}

// RunError reports a step failure during execution.
type RunError struct {
	StepID string
	Err    error
}

func (e *RunError) Error() string {
	return fmt.Sprintf("workflow: step %q failed: %v", e.StepID, e.Err)
}

// Unwrap supports errors.Is/As on the wrapped error.
func (e *RunError) Unwrap() error { return e.Err }
