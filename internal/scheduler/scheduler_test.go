package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

func TestLane_ConcurrencyLimit(t *testing.T) {
	lane := NewLane("test", 2)
	defer lane.Stop()

	var active atomic.Int32
	var maxActive atomic.Int32
	var wg sync.WaitGroup

	for range 6 {
		wg.Add(1)
		err := lane.Submit(context.Background(), func() {
			defer wg.Done()
			cur := active.Add(1)

			// Track the max concurrency observed
			for {
				old := maxActive.Load()
				if cur <= old || maxActive.CompareAndSwap(old, cur) {
					break
				}
			}

			time.Sleep(50 * time.Millisecond)
			active.Add(-1)
		})
		if err != nil {
			t.Fatalf("submit failed: %v", err)
		}
	}

	wg.Wait()

	if m := maxActive.Load(); m > 2 {
		t.Errorf("max active = %d, want <= 2", m)
	}
	if m := maxActive.Load(); m < 2 {
		t.Errorf("max active = %d, want >= 2 (should use full concurrency)", m)
	}
}

func TestLane_Stats(t *testing.T) {
	lane := NewLane("test", 3)
	defer lane.Stop()

	stats := lane.Stats()
	if stats.Name != "test" {
		t.Errorf("name = %q, want %q", stats.Name, "test")
	}
	if stats.Concurrency != 3 {
		t.Errorf("concurrency = %d, want 3", stats.Concurrency)
	}
	if stats.Active != 0 {
		t.Errorf("active = %d, want 0", stats.Active)
	}
}

func TestLaneManager_GetFallback(t *testing.T) {
	lm := NewLaneManager([]LaneConfig{
		{Name: "main", Concurrency: 2},
		{Name: "subagent", Concurrency: 4},
	})
	defer lm.StopAll()

	// Known lane
	if l := lm.Get("subagent"); l == nil {
		t.Error("Get('subagent') returned nil")
	}

	// Unknown lane → fallback to main
	if l := lm.Get("nonexistent"); l == nil {
		t.Error("Get('nonexistent') should fallback to main")
	} else if l.name != "main" {
		t.Errorf("fallback lane name = %q, want 'main'", l.name)
	}
}

func TestLaneManager_GetOrCreate(t *testing.T) {
	lm := NewLaneManager([]LaneConfig{
		{Name: "main", Concurrency: 2},
	})
	defer lm.StopAll()

	l := lm.GetOrCreate("custom", 8)
	if l == nil {
		t.Fatal("GetOrCreate returned nil")
	}
	if l.concurrency != 8 {
		t.Errorf("concurrency = %d, want 8", l.concurrency)
	}

	// Second call returns existing
	l2 := lm.GetOrCreate("custom", 16)
	if l2.concurrency != 8 {
		t.Errorf("second call should return existing lane with concurrency 8, got %d", l2.concurrency)
	}
}

func TestScheduler_SessionSerialization(t *testing.T) {
	var active atomic.Int32
	var maxActive atomic.Int32

	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		cur := active.Add(1)
		for {
			old := maxActive.Load()
			if cur <= old || maxActive.CompareAndSwap(old, cur) {
				break
			}
		}

		time.Sleep(30 * time.Millisecond)
		active.Add(-1)

		return &agent.RunResult{Content: "ok", RunID: req.RunID}, nil
	}

	sched := NewScheduler(DefaultLanes(), QueueConfig{
		Mode:       QueueModeQueue,
		Cap:        10,
		Drop:       DropOld,
		DebounceMs: 0, // no debounce for test speed
	}, runFn)
	defer sched.Stop()

	// Submit 3 requests to the same session
	ctx := context.Background()
	sessionKey := "agent:default:test-session"

	var outcomes []<-chan RunOutcome
	for i := range 3 {
		ch := sched.Schedule(ctx, "main", agent.RunRequest{
			SessionKey: sessionKey,
			Message:    "hello",
			RunID:      "run-" + string(rune('a'+i)),
		})
		outcomes = append(outcomes, ch)
	}

	// Collect all results
	for i, ch := range outcomes {
		select {
		case out := <-ch:
			if out.Err != nil {
				t.Errorf("run %d error: %v", i, out.Err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("run %d timed out", i)
		}
	}

	// For the same session, max concurrent should be 1
	if m := maxActive.Load(); m > 1 {
		t.Errorf("same session max active = %d, want 1 (should serialize)", m)
	}
}

func TestScheduler_DifferentSessionsParallel(t *testing.T) {
	var active atomic.Int32
	var maxActive atomic.Int32

	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		cur := active.Add(1)
		for {
			old := maxActive.Load()
			if cur <= old || maxActive.CompareAndSwap(old, cur) {
				break
			}
		}

		time.Sleep(80 * time.Millisecond)
		active.Add(-1)

		return &agent.RunResult{Content: "ok", RunID: req.RunID}, nil
	}

	sched := NewScheduler(DefaultLanes(), QueueConfig{
		Mode:       QueueModeQueue,
		Cap:        10,
		Drop:       DropOld,
		DebounceMs: 0,
	}, runFn)
	defer sched.Stop()

	ctx := context.Background()

	// Submit to 2 different sessions — should run in parallel
	ch1 := sched.Schedule(ctx, "main", agent.RunRequest{
		SessionKey: "agent:default:session-1",
		Message:    "hello 1",
		RunID:      "run-1",
	})
	ch2 := sched.Schedule(ctx, "main", agent.RunRequest{
		SessionKey: "agent:default:session-2",
		Message:    "hello 2",
		RunID:      "run-2",
	})

	for _, ch := range []<-chan RunOutcome{ch1, ch2} {
		select {
		case out := <-ch:
			if out.Err != nil {
				t.Errorf("error: %v", out.Err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out")
		}
	}

	// Different sessions should have run in parallel
	if m := maxActive.Load(); m < 2 {
		t.Errorf("different sessions max active = %d, want >= 2 (should parallelize)", m)
	}
}

func TestScheduler_DropOldPolicy(t *testing.T) {
	// Use a blocking run function
	started := make(chan struct{})
	blockCh := make(chan struct{})

	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-blockCh
		return &agent.RunResult{Content: "ok", RunID: req.RunID}, nil
	}

	sched := NewScheduler(DefaultLanes(), QueueConfig{
		Mode:       QueueModeQueue,
		Cap:        2,
		Drop:       DropOld,
		DebounceMs: 0,
	}, runFn)
	defer sched.Stop()
	// Close blockCh before Stop() (LIFO) so goroutines unblock and Stop() doesn't hang.
	defer func() { select { case <-blockCh: default: close(blockCh) } }()

	ctx := context.Background()
	session := "agent:default:drop-test"

	// First request starts running
	_ = sched.Schedule(ctx, "main", agent.RunRequest{
		SessionKey: session, RunID: "run-1", Message: "msg1",
	})

	// Wait for first run to start
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first run didn't start")
	}

	// Queue 2 more (fills cap=2)
	ch2 := sched.Schedule(ctx, "main", agent.RunRequest{
		SessionKey: session, RunID: "run-2", Message: "msg2",
	})
	ch3 := sched.Schedule(ctx, "main", agent.RunRequest{
		SessionKey: session, RunID: "run-3", Message: "msg3",
	})

	// Queue a 3rd — should drop oldest (run-2)
	_ = sched.Schedule(ctx, "main", agent.RunRequest{
		SessionKey: session, RunID: "run-4", Message: "msg4",
	})

	// run-2 should have been dropped
	select {
	case out := <-ch2:
		if out.Err != ErrQueueDropped {
			t.Errorf("expected ErrQueueDropped, got %v", out.Err)
		}
	case <-time.After(2 * time.Second):
		t.Error("dropped message notification timed out")
	}

	// run-3 should still be queued (not dropped)
	select {
	case <-ch3:
		t.Error("run-3 should still be queued, not completed")
	default:
		// OK, still pending
	}

	// Unblock everything and drain queued runs before Stop().
	// Without draining, Stop() races with scheduleNext() causing wg.Wait() to hang.
	close(blockCh)

	select {
	case <-ch3:
	case <-time.After(5 * time.Second):
		t.Fatal("queued run didn't complete after unblock")
	}
}

func TestScheduler_InterruptMode(t *testing.T) {
	blockCh := make(chan struct{})
	started := make(chan struct{}, 2)

	runFn := func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		started <- struct{}{}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-blockCh:
			return &agent.RunResult{Content: "ok", RunID: req.RunID}, nil
		}
	}

	sched := NewScheduler(DefaultLanes(), QueueConfig{
		Mode:       QueueModeInterrupt,
		Cap:        10,
		Drop:       DropOld,
		DebounceMs: 0,
	}, runFn)
	defer sched.Stop()
	// Close blockCh before Stop() (LIFO) so goroutines unblock and Stop() doesn't hang.
	defer func() { select { case <-blockCh: default: close(blockCh) } }()

	ctx := context.Background()
	session := "agent:default:interrupt-test"

	// Start first request
	ch1 := sched.Schedule(ctx, "main", agent.RunRequest{
		SessionKey: session, RunID: "run-1", Message: "msg1",
	})

	// Wait for it to start
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first run didn't start")
	}

	// Send interrupt — should cancel first run
	ch2 := sched.Schedule(ctx, "main", agent.RunRequest{
		SessionKey: session, RunID: "run-2", Message: "msg2",
	})

	// First run should be cancelled
	select {
	case out := <-ch1:
		if out.Err == nil {
			t.Error("first run should have been cancelled")
		}
	case <-time.After(3 * time.Second):
		t.Error("first run cancellation timed out")
	}

	// Let second run complete
	close(blockCh)

	select {
	case out := <-ch2:
		if out.Err != nil {
			t.Errorf("second run error: %v", out.Err)
		}
	case <-time.After(3 * time.Second):
		t.Error("second run timed out")
	}
}
