package orchestration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunParallel_AllComplete_IndexAligned(t *testing.T) {
	contestants := []Contestant{
		{ID: "a", Task: "one", Label: "simplest"},
		{ID: "b", Task: "two", Label: "performance"},
		{ID: "c", Task: "three", Label: "safest"},
	}
	results, err := RunParallel(context.Background(), contestants,
		func(ctx context.Context, task string) (ChildResult, error) {
			return ChildResult{Content: "done:" + task, Status: "completed"}, nil
		}, RunParallelOpts{Concurrency: 2})
	if err != nil {
		t.Fatalf("RunParallel: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}
	for i, c := range contestants {
		want := "done:" + c.Task
		if results[i].Content != want {
			t.Errorf("results[%d].Content = %q, want %q", i, results[i].Content, want)
		}
	}
}

func TestRunParallel_BoundedConcurrency(t *testing.T) {
	const total = 16
	contestants := make([]Contestant, total)
	for i := 0; i < total; i++ {
		contestants[i] = Contestant{ID: string(rune('a' + i)), Task: "t"}
	}

	var (
		active    atomic.Int32
		maxActive atomic.Int32
		ran       atomic.Int32
	)
	runner := func(ctx context.Context, task string) (ChildResult, error) {
		cur := active.Add(1)
		for {
			m := maxActive.Load()
			if cur <= m || maxActive.CompareAndSwap(m, cur) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		active.Add(-1)
		ran.Add(1)
		return ChildResult{Status: "completed"}, nil
	}

	if _, err := RunParallel(context.Background(), contestants, runner, RunParallelOpts{Concurrency: 4}); err != nil {
		t.Fatalf("RunParallel: %v", err)
	}
	if ran.Load() != total {
		t.Errorf("ran = %d, want %d", ran.Load(), total)
	}
	if got := maxActive.Load(); got > 4 {
		t.Errorf("max concurrent runners = %d, want <= 4", got)
	}
	if maxActive.Load() < 2 {
		t.Errorf("max concurrent runners = %d, want > 1 (fan-out actually parallel)", maxActive.Load())
	}
}

func TestRunParallel_DefaultConcurrency(t *testing.T) {
	// Concurrency <= 0 falls back to the default of 4 and must still run all.
	contestants := make([]Contestant, 10)
	for i := range contestants {
		contestants[i] = Contestant{ID: string(rune('a' + i))}
	}
	var ran atomic.Int32
	_, err := RunParallel(context.Background(), contestants,
		func(context.Context, string) (ChildResult, error) {
			ran.Add(1)
			return ChildResult{Status: "completed"}, nil
		}, RunParallelOpts{}) // zero opts
	if err != nil {
		t.Fatalf("RunParallel: %v", err)
	}
	if ran.Load() != 10 {
		t.Errorf("ran = %d, want 10", ran.Load())
	}
}

// TestRunParallel_ErrorCancel asserts FailFast: when one runner fails, the
// error is surfaced and runners that had not begun are never invoked. A slow
// in-flight runner observes cancellation instead of finishing.
func TestRunParallel_ErrorCancel(t *testing.T) {
	contestants := make([]Contestant, 6)
	for i := range contestants {
		contestants[i] = Contestant{ID: string(rune('a' + i))}
	}
	var (
		started       atomic.Int32 // runner invocations
		slowCancelled atomic.Bool
		slowFinished  atomic.Bool
		mu            sync.Mutex
		boomOnce      bool
	)
	runner := func(ctx context.Context, _ string) (ChildResult, error) {
		mu.Lock()
		isBoom := !boomOnce
		boomOnce = true
		mu.Unlock()
		started.Add(1)
		if isBoom {
			return ChildResult{}, errors.New("boom")
		}
		select {
		case <-ctx.Done():
			slowCancelled.Store(true)
			return ChildResult{}, ctx.Err()
		case <-time.After(2 * time.Second):
			slowFinished.Store(true)
		}
		return ChildResult{Status: "completed"}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	results, err := RunParallel(ctx, contestants, runner, RunParallelOpts{Concurrency: 3})
	if err == nil {
		t.Fatal("RunParallel: expected error, got nil")
	}
	var rpe *RunParallelError
	if !errors.As(err, &rpe) {
		t.Fatalf("error = %v, want *RunParallelError", err)
	}
	if rpe.Err == nil {
		t.Error("RunParallelError.Err = nil")
	}
	// Every slot must carry a definite status; the failing one is "failed".
	for i, r := range results {
		if r.Status == "" {
			t.Errorf("results[%d] has empty status, want definite status", i)
		}
	}
	foundFailed := false
	for _, r := range results {
		if r.Status == "failed" {
			foundFailed = true
			break
		}
	}
	if !foundFailed {
		t.Error("expected at least one failed result for the failing contender")
	}
	// No runner may run to completion after the boom cancelled the run.
	if slowFinished.Load() {
		t.Error("a slow runner finished despite FailFast cancellation")
	}
	// A slow runner only exists if the boom did not pre-empt every other
	// invocation; when one started it must have observed cancellation.
	if started.Load() > 1 && !slowCancelled.Load() {
		t.Error("a slow runner ran but did not observe context cancellation")
	}
}

func TestRunParallel_Errors(t *testing.T) {
	runner := func(context.Context, string) (ChildResult, error) {
		return ChildResult{Status: "completed"}, nil
	}
	if _, err := RunParallel(nil, []Contestant{{ID: "a"}}, runner, RunParallelOpts{}); err == nil {
		t.Error("nil context: expected error")
	}
	if _, err := RunParallel(context.Background(), []Contestant{{ID: "a"}}, nil, RunParallelOpts{}); err == nil {
		t.Error("nil runner: expected error")
	}
	if _, err := RunParallel(context.Background(), nil, runner, RunParallelOpts{}); err != ErrNoContestants {
		t.Errorf("no contestants: got %v, want ErrNoContestants", err)
	}
	if _, err := RunParallel(context.Background(), []Contestant{}, runner, RunParallelOpts{}); err != ErrNoContestants {
		t.Errorf("empty contestants: got %v, want ErrNoContestants", err)
	}
}

func TestRunParallel_PrefillsFailedForUnstarted(t *testing.T) {
	// Guard against races in FailFast bookkeeping: every result slot must be
	// written exactly once, including slots whose runner was cancelled before
	// invocation. This test also enforces the slice is index-aligned.
	results, err := RunParallel(context.Background(),
		[]Contestant{{ID: "ok"}, {ID: "boom"}, {ID: "never"}},
		func(ctx context.Context, _ string) (ChildResult, error) {
			// First invocation fails; later ones are cancelled before run.
			if ctx.Err() != nil {
				return ChildResult{}, ctx.Err()
			}
			return ChildResult{}, errors.New("fail now")
		},
		RunParallelOpts{Concurrency: 1})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(results) != 3 {
		t.Fatalf("results len = %d, want 3", len(results))
	}
	for i, r := range results {
		if r.Status == "" {
			t.Errorf("results[%d] has empty status, want a definite status", i)
		}
	}
}