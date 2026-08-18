// Package orchestration provides primitives for orchestrating multi-agent
// runs: bounded parallel fan-out, judge-driven best-result selection, and a
// bounded negotiation round model. Results travel through the canonical
// ChildResult shape so downstream aggregation code has a single view of a
// child agent's outcome.
package orchestration

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// RunnerFunc executes a single contender for a given task and returns its
// outcome as a ChildResult. The signature mirrors the DelegateRunFunc shape so
// a gateway-injected delegate runner can be adapted directly.
type RunnerFunc func(ctx context.Context, task string) (ChildResult, error)

// Contestant describes one competitor in a parallel fan-out round.
type Contestant struct {
	// ID uniquely identifies the contender within the round.
	ID string
	// Task is the work handed to this contender (may be a full contract task
	// or a strategy-specific prompt).
	Task string
	// Label is a human tag used by scoring criteria, e.g. "simplest",
	// "performance", or "safest".
	Label string
}

// RunParallelOpts configures a parallel fan-out round.
type RunParallelOpts struct {
	// Concurrency bounds the number of runners executing simultaneously.
	// Values <= 0 fall back to a default of 4.
	Concurrency int
}

// defaultParallelism bounds fan-out when RunParallelOpts.Concurrency is unset.
const defaultParallelism = 4

// ErrNoContestants reports a fan-out round with nothing to run.
var ErrNoContestants = errors.New("orchestration: RunParallel: no contestants")

// RunParallel executes every contestant concurrently under a bounded worker
// pool. The returned slice is index-aligned with contestants (results[i]
// corresponds to contestants[i]). The first runner error cancels the remaining
// runners (FailFast): in-flight runners observe the cancelled context,
// not-yet-started contestants are recorded with Status "failed" without
// invoking the runner, and the first error is returned once the pool drains so
// no goroutine is left behind. A runner panic is converted to an error; an
// external context cancellation is propagated as the returned error when no
// runner error preceded it.
func RunParallel(ctx context.Context, contestants []Contestant, runner RunnerFunc, opts RunParallelOpts) ([]ChildResult, error) {
	if ctx == nil {
		return nil, errors.New("orchestration: RunParallel: nil context")
	}
	if runner == nil {
		return nil, errors.New("orchestration: RunParallel: nil runner")
	}
	if len(contestants) == 0 {
		return nil, ErrNoContestants
	}

	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = defaultParallelism
	}
	if concurrency > len(contestants) {
		concurrency = len(contestants)
	}

	// Preseed every slot as failed so a contestant that is never claimed by a
	// worker (the channel closed before the index was delivered under FailFast)
	// still carries a definite status in the aligned result slice.
	results := make([]ChildResult, len(contestants))
	for i := range results {
		results[i] = ChildResult{Status: "failed"}
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// jobs carries contestant indices to the worker pool; each worker writes
	// its result back by index for deterministic contestant-aligned output.
	jobs := make(chan int)
	runErrCh := make(chan error, 1)

	var wg sync.WaitGroup
	wg.Add(concurrency)
	for w := 0; w < concurrency; w++ {
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if runCtx.Err() != nil {
					// A prior failure cancelled the run; never invoke the
					// runner again, just record the queued contestant failed.
					results[idx] = ChildResult{Status: "failed"}
					continue
				}
				c := contestants[idx]
				res, err := runSafely(runner, runCtx, c.Task)
				if err != nil {
					results[idx] = ChildResult{Status: "failed"}
					select {
					case runErrCh <- &RunParallelError{ContestantID: c.ID, Err: err}:
						cancel() // FailFast: stop further runner invocations.
					default:
						// A prior error already cancelled the run.
					}
					continue
				}
				if res.Status == "" {
					res.Status = "completed"
				}
				results[idx] = res
			}
		}()
	}

	// Dispatch every index. Stop scheduling early if the run was cancelled
	// (externally or by a runner error); workers drain the channel and mark
	// the skipped slots failed.
dispatch:
	for i := range contestants {
		select {
		case jobs <- i:
		case <-runCtx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-runErrCh:
		return results, err
	default:
		if err := runCtx.Err(); err != nil {
			// External cancellation with no runner error.
			return results, err
		}
		return results, nil
	}
}

// runSafely invokes a runner and converts a panic into a returned error so a
// misbehaving runner cannot kill the whole fan-out pool or strand its
// goroutines.
func runSafely(runner RunnerFunc, ctx context.Context, task string) (res ChildResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("orchestration: runner panicked: %v", r)
		}
	}()
	return runner(ctx, task)
}

// RunParallelError wraps the first runner failure with the offending
// contestant ID so callers can identify which contender failed.
type RunParallelError struct {
	ContestantID string
	Err          error
}

func (e *RunParallelError) Error() string {
	return fmt.Sprintf("orchestration: contestant %q failed: %v", e.ContestantID, e.Err)
}

// Unwrap supports errors.Is/As on the wrapped runner error.
func (e *RunParallelError) Unwrap() error { return e.Err }