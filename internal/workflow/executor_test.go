package workflow

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

// TestRun_SequentialChain executes a linear dependency chain and verifies
// strict ordering. This test hangs if the executor has a sequential-step
// deadlock (the flagged bug), so it runs with a timeout context.
func TestRun_SequentialChain(t *testing.T) {
	var mu sync.Mutex
	var order []string

	d := NewDAG("seq")
	mustAdd(t, d, &Step{ID: "a", Run: func(context.Context, *RunCtx) error {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, "a")
		return nil
	}})
	mustAdd(t, d, &Step{ID: "b", Run: func(context.Context, *RunCtx) error {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, "b")
		return nil
	}, Deps: []string{"a"}})
	mustAdd(t, d, &Step{ID: "c", Run: func(context.Context, *RunCtx) error {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, "c")
		return nil
	}, Deps: []string{"b"}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := join(order); got != "a,b,c" {
		t.Errorf("execution order = %q, want %q", got, "a,b,c")
	}
}

// TestRun_SequentialSharesLevel verifies that independent sequential steps
// sharing a level are serialized (never run concurrently), without asserting a
// specific ordering between them (they have no dependency, so any order is
// valid).
func TestRun_SequentialSharesLevel(t *testing.T) {
	var mu sync.Mutex
	var order []string
	var active, maxActive int

	d := NewDAG("seq-same")
	for _, id := range []string{"x", "y", "z"} {
		id := id
		mustAdd(t, d, &Step{ID: id, Run: func(context.Context, *RunCtx) error {
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			order = append(order, id)
			mu.Unlock()

			// Hold a short slice to make overlap detectable.
			time.Sleep(10 * time.Millisecond)

			mu.Lock()
			active--
			mu.Unlock()
			return nil
		}})
	}
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(order) != 3 {
		t.Fatalf("execution order = %v, want exactly 3 steps", order)
	}
	seen := make(map[string]bool, 3)
	for _, id := range order {
		if seen[id] {
			t.Errorf("step %q ran more than once", id)
		}
		seen[id] = true
	}
	if maxActive != 1 {
		t.Errorf("max concurrent sequential steps = %d, want 1 (serialized)", maxActive)
	}
}

// TestRun_ParallelFanOut launches independent parallel steps concurrently and
// verifies all ran.
func TestRun_ParallelFanOut(t *testing.T) {
	var ran atomic.Int32
	d := NewDAG("par")
	for i := 0; i < 8; i++ {
		i := i
		mustAdd(t, d, &Step{ID: string(rune('a' + i)), Type: StepParallel, Run: func(context.Context, *RunCtx) error {
			time.Sleep(20 * time.Millisecond)
			ran.Add(1)
			return nil
		}})
	}
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ran.Load() != 8 {
		t.Errorf("ran = %d, want 8", ran.Load())
	}
}

// TestRun_ParallelDependencies verifies a fan-out with a root that must run
// first and a join that runs after both branches.
func TestRun_ParallelDependencies(t *testing.T) {
	var order []string
	var mu sync.Mutex
	record := func(id string) func(context.Context, *RunCtx) error {
		return func(context.Context, *RunCtx) error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, id)
			return nil
		}
	}

	d := NewDAG("par-dep")
	mustAdd(t, d, &Step{ID: "root", Run: record("root")})
	mustAdd(t, d, &Step{ID: "p1", Type: StepParallel, Run: record("p1"), Deps: []string{"root"}})
	mustAdd(t, d, &Step{ID: "p2", Type: StepParallel, Run: record("p2"), Deps: []string{"root"}})
	mustAdd(t, d, &Step{ID: "join", Run: record("join"), Deps: []string{"p1", "p2"}})

	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(order) != 4 {
		t.Fatalf("order = %v, want 4 entries", order)
	}
	if order[0] != "root" {
		t.Errorf("root must run first, got order %v", order)
	}
	if order[len(order)-1] != "join" {
		t.Errorf("join must run last, got order %v", order)
	}
}

// TestRun_ParallelFailFast asserts FailFast: the first failing parallel branch
// is reported and sibling branches stop early via context cancellation.
func TestRun_ParallelFailFast(t *testing.T) {
	d := NewDAG("failfast")
	mustAdd(t, d, &Step{ID: "boom", Type: StepParallel, Run: func(context.Context, *RunCtx) error {
		return errors.New("boom")
	}})
	// Slow sibling must observe cancellation rather than run to completion.
	var slowFinishedOK atomic.Bool
	var slowCancelled atomic.Bool
	mustAdd(t, d, &Step{ID: "slow", Type: StepParallel, Run: func(ctx context.Context, _ *RunCtx) error {
		select {
		case <-ctx.Done():
			slowCancelled.Store(true)
		case <-time.After(2 * time.Second):
			slowFinishedOK.Store(true)
		}
		return nil
	}})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := d.Run(ctx)
	if err == nil {
		t.Fatal("Run: expected error, got nil")
	}
	var re *RunError
	if !errors.As(err, &re) {
		t.Fatalf("error = %v, want *RunError", err)
	}
	if re.StepID != "boom" {
		t.Errorf("StepID = %q, want boom", re.StepID)
	}
	// The slow step either observed cancellation or never started (run was
	// cancelled before it was scheduled). It must NOT have run to completion.
	if slowFinishedOK.Load() {
		t.Error("slow sibling ran to completion; FailFast cancelled the run but it finished anyway")
	}
}

// TestRun_SequentialFailure verifies error propagation for a plain sequential
// failure (no parallelism involved).
func TestRun_SequentialFailure(t *testing.T) {
	d := NewDAG("seq-fail")
	mustAdd(t, d, &Step{ID: "ok", Run: noop()})
	mustAdd(t, d, &Step{ID: "bad", Run: func(context.Context, *RunCtx) error {
		return errors.New("kaboom")
	}, Deps: []string{"ok"}})

	err := d.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var re *RunError
	if !errors.As(err, &re) {
		t.Fatalf("error = %v, want *RunError", err)
	}
	if re.StepID != "bad" {
		t.Errorf("StepID = %q, want bad", re.StepID)
	}
}

// TestRun_ContextCancellation verifies an externally cancelled context stops
// the run.
func TestRun_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := NewDAG("cancel")
	mustAdd(t, d, &Step{ID: "gate", Run: func(context.Context, *RunCtx) error {
		cancel()
		return nil
	}})
	// Blocking step after the gate: must observe the cancelled context.
	blockerDone := make(chan struct{})
	mustAdd(t, d, &Step{ID: "blocker", Deps: []string{"gate"}, Run: func(ctx context.Context, _ *RunCtx) error {
		defer close(blockerDone)
		<-ctx.Done()
		return ctx.Err()
	}})

	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Run: expected cancellation error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}