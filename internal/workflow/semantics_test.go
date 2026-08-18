package workflow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestConditional_Skip verifies a conditional step whose gate is false is
// skipped without error and its Run never executes.
func TestConditional_Skip(t *testing.T) {
	var ran atomic.Bool
	d := NewDAG("cond-skip")
	mustAdd(t, d, &Step{ID: "cond", Type: StepConditional, Cond: func(context.Context, *RunCtx) bool {
		return false
	}, Run: func(context.Context, *RunCtx) error {
		ran.Store(true)
		return nil
	}})
	// Dependent of the skipped step must still run.
	var depRan atomic.Bool
	mustAdd(t, d, &Step{ID: "dep", Deps: []string{"cond"}, Run: func(context.Context, *RunCtx) error {
		depRan.Store(true)
		return nil
	}})

	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ran.Load() {
		t.Error("conditional step ran despite false gate")
	}
	if !depRan.Load() {
		t.Error("dependent of skipped conditional step did not run")
	}
}

// TestConditional_Run verifies a conditional step with a true gate executes.
func TestConditional_Run(t *testing.T) {
	var ran atomic.Bool
	d := NewDAG("cond-run")
	mustAdd(t, d, &Step{ID: "cond", Type: StepConditional, Cond: func(context.Context, *RunCtx) bool {
		return true
	}, Run: func(context.Context, *RunCtx) error {
		ran.Store(true)
		return nil
	}})
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !ran.Load() {
		t.Error("conditional step with true gate did not run")
	}
}

// TestRetry_SucceedsOnRetry verifies a StepRetry step retries and finally
// succeeds. Backoff is kept tiny so the test stays fast.
func TestRetry_SucceedsOnRetry(t *testing.T) {
	var attempts atomic.Int32
	d := NewDAG("retry-ok")
	mustAdd(t, d, &Step{ID: "r", Type: StepRetry, Retry: &RetryPolicy{MaxAttempts: 3, Backoff: time.Millisecond},
		Run: func(context.Context, *RunCtx) error {
			n := attempts.Add(1)
			if n < 3 {
				return errors.New("transient")
			}
			return nil
		}})
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if attempts.Load() != 3 {
		t.Errorf("attempts = %d, want 3", attempts.Load())
	}
}

// TestRetry_Exhausted verifies a StepRetry step that always fails exhausts its
// attempts and returns the underlying error.
func TestRetry_Exhausted(t *testing.T) {
	var attempts atomic.Int32
	d := NewDAG("retry-fail")
	mustAdd(t, d, &Step{ID: "r", Type: StepRetry, Retry: &RetryPolicy{MaxAttempts: 2},
		Run: func(context.Context, *RunCtx) error {
			attempts.Add(1)
			return errors.New("always-fails")
		}})
	err := d.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts.Load() != 2 {
		t.Errorf("attempts = %d, want 2", attempts.Load())
	}
	var re *RunError
	if !errors.As(err, &re) {
		t.Fatalf("error = %v, want *RunError", err)
	}
	if re.StepID != "r" {
		t.Errorf("StepID = %q, want r", re.StepID)
	}
}

// TestRetry_DefaultAttempts verifies a retry step with no policy defaults to a
// single attempt (non-nil policy validated at build time requires >= 1).
func TestRetry_SingleAttemptByDefault(t *testing.T) {
	var attempts atomic.Int32
	d := NewDAG("retry-default")
	mustAdd(t, d, &Step{ID: "r", Type: StepRetry, Retry: &RetryPolicy{},
		Run: func(context.Context, *RunCtx) error {
			attempts.Add(1)
			return errors.New("nope")
		}})
	err := d.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1", attempts.Load())
	}
}

// TestTimeout_StepFailsOnTimeout verifies a step whose Run exceeds Timeout is
// cancelled by the per-attempt context.
func TestTimeout_StepFailsOnTimeout(t *testing.T) {
	d := NewDAG("timeout")
	var sawTimeout atomic.Bool
	mustAdd(t, d, &Step{ID: "slow", Timeout: 30 * time.Millisecond,
		Run: func(ctx context.Context, _ *RunCtx) error {
			select {
			case <-time.After(2 * time.Second):
				return nil // would be a bug if reached
			case <-ctx.Done():
				sawTimeout.Store(true)
				return ctx.Err()
			}
		}})
	err := d.Run(context.Background())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !sawTimeout.Load() {
		t.Fatal("expected step to observe the timeout cancellation")
	}
}

// TestOnError_Hook verifies OnError runs after a step failure and can recover
// the step by returning nil.
func TestOnError_Hook(t *testing.T) {
	var hookRan atomic.Bool
	d := NewDAG("onerror")
	mustAdd(t, d, &Step{ID: "bad", Run: func(context.Context, *RunCtx) error {
		return errors.New("bad")
	}, OnError: func(context.Context, *RunCtx, error) error {
		hookRan.Store(true)
		return nil // recover
	}})
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run (recovered): %v", err)
	}
	if !hookRan.Load() {
		t.Error("OnError hook did not run")
	}
}

// TestOnComplete_Hook verifies OnComplete runs after success.
func TestOnComplete_Hook(t *testing.T) {
	var hookRan atomic.Bool
	d := NewDAG("oncomplete")
	mustAdd(t, d, &Step{ID: "ok", Run: noop(), OnComplete: func(context.Context, *RunCtx) error {
		hookRan.Store(true)
		return nil
	}})
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !hookRan.Load() {
		t.Error("OnComplete hook did not run")
	}
}

// TestRunCtx_ValueExchange verifies data flows between steps via RunCtx.
func TestRunCtx_ValueExchange(t *testing.T) {
	d := NewDAG("ctx")
	mustAdd(t, d, &Step{ID: "set", Run: func(_ context.Context, rc *RunCtx) error {
		rc.SetOutput("token", 42)
		rc.SetOutput("name", "hello")
		return nil
	}})
	var gotV, gotK any
	mustAdd(t, d, &Step{ID: "get", Deps: []string{"set"}, Run: func(_ context.Context, rc *RunCtx) error {
		gotV, _ = rc.GetOutput("token")
		gotK, _ = rc.GetOutput("name")
		return nil
	}})
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotV != 42 || gotK != "hello" {
		t.Errorf("gotV = %v, gotK = %v", gotV, gotK)
	}
}

// TestRunCtx_ParallelSafe verifies concurrent SetOutput/GetOutput from parallel
// steps does not race.
func TestRunCtx_ParallelSafe(t *testing.T) {
	d := NewDAG("ctx-par")
	for i := 0; i < 16; i++ {
		i := i
		mustAdd(t, d, &Step{ID: string(rune('a' + i)), Type: StepParallel,
			Run: func(_ context.Context, rc *RunCtx) error {
				rc.SetOutput("k", i)
				rc.SetOutput(string(rune('a'+i)), i)
				_, _ = rc.GetOutput("k")
				return nil
			}})
	}
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Run with -race would fail here if RunCtx races.
}

func stringFromOutput(rc *RunCtx, key string) string {
	if v, ok := rc.GetOutput(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}