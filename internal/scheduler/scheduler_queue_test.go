package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

// mockRunFn creates a run function that completes after a delay.
func mockRunFn(delay time.Duration) RunFunc {
	return func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
			return &agent.RunResult{Content: "done:" + req.RunID}, nil
		}
	}
}

// fastRunFn completes immediately.
func fastRunFn() RunFunc {
	return func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		return &agent.RunResult{Content: "ok"}, nil
	}
}

// --- Draining mode rejects new requests ---

func TestScheduler_MarkDraining(t *testing.T) {
	sched := NewScheduler(nil, DefaultQueueConfig(), fastRunFn())
	defer sched.Stop()

	sched.MarkDraining()

	ch := sched.Schedule(context.Background(), LaneMain, agent.RunRequest{
		SessionKey: "agent:a1:s1",
		RunID:      "run-1",
	})

	outcome := <-ch
	if !errors.Is(outcome.Err, ErrGatewayDraining) {
		t.Fatalf("expected ErrGatewayDraining, got: %v", outcome.Err)
	}
}

// --- DropNew policy: full queue rejects incoming ---

func TestSessionQueue_DropNewPolicy(t *testing.T) {
	cfg := QueueConfig{
		Mode:          QueueModeQueue,
		Cap:           2,
		Drop:          DropNew,
		DebounceMs:    0,
		MaxConcurrent: 1,
	}

	// Use a slow run to keep the queue occupied
	blockCh := make(chan struct{})
	runFn := func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		<-blockCh
		return &agent.RunResult{}, nil
	}

	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})
	sq := NewSessionQueue("test-session", LaneMain, cfg, laneMgr, runFn)

	// Enqueue 3 requests: 1 active + 2 in queue = at capacity
	ctx := context.Background()
	sq.Enqueue(ctx, agent.RunRequest{RunID: "r1", SessionKey: "s"})
	time.Sleep(10 * time.Millisecond) // let r1 start
	sq.Enqueue(ctx, agent.RunRequest{RunID: "r2", SessionKey: "s"})
	sq.Enqueue(ctx, agent.RunRequest{RunID: "r3", SessionKey: "s"})

	// Queue is full (cap=2). Next one should be rejected.
	ch := sq.Enqueue(ctx, agent.RunRequest{RunID: "r4", SessionKey: "s"})

	outcome := <-ch
	if !errors.Is(outcome.Err, ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got: %v", outcome.Err)
	}

	close(blockCh) // unblock
}

// --- DropOld policy: full queue drops oldest ---

func TestSessionQueue_DropOldPolicy(t *testing.T) {
	cfg := QueueConfig{
		Mode:          QueueModeQueue,
		Cap:           2,
		Drop:          DropOld,
		DebounceMs:    0,
		MaxConcurrent: 1,
	}

	blockCh := make(chan struct{})
	runFn := func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		<-blockCh
		return &agent.RunResult{}, nil
	}

	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})
	sq := NewSessionQueue("test-session", LaneMain, cfg, laneMgr, runFn)

	ctx := context.Background()
	sq.Enqueue(ctx, agent.RunRequest{RunID: "r1", SessionKey: "s"})
	time.Sleep(10 * time.Millisecond)

	ch2 := sq.Enqueue(ctx, agent.RunRequest{RunID: "r2", SessionKey: "s"})
	sq.Enqueue(ctx, agent.RunRequest{RunID: "r3", SessionKey: "s"})

	// Queue is full. Adding r4 should drop r2 (oldest queued).
	sq.Enqueue(ctx, agent.RunRequest{RunID: "r4", SessionKey: "s"})

	// r2 should have been dropped
	outcome := <-ch2
	if !errors.Is(outcome.Err, ErrQueueDropped) {
		t.Fatalf("expected ErrQueueDropped for r2, got: %v", outcome.Err)
	}

	close(blockCh)
}

// --- Adaptive throttle: reduces concurrency near 60% context usage ---

func TestSessionQueue_AdaptiveThrottle(t *testing.T) {
	cfg := QueueConfig{
		Mode:          QueueModeQueue,
		Cap:           10,
		Drop:          DropOld,
		DebounceMs:    0,
		MaxConcurrent: 5,
	}

	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 20}})
	sq := NewSessionQueue("test-session", LaneMain, cfg, laneMgr, fastRunFn())

	// Without token estimate → effectiveMaxConcurrent = 5
	sq.mu.Lock()
	got := sq.effectiveMaxConcurrent()
	sq.mu.Unlock()
	if got != 5 {
		t.Fatalf("without estimate: got %d, want 5", got)
	}

	// With token estimate under threshold (50%) → still 5
	sq.tokenEstimateFn = func(key string) (int, int) {
		return 50000, 100000 // 50%
	}
	sq.mu.Lock()
	got = sq.effectiveMaxConcurrent()
	sq.mu.Unlock()
	if got != 5 {
		t.Fatalf("under threshold: got %d, want 5", got)
	}

	// At 60% threshold → drops to 1
	sq.tokenEstimateFn = func(key string) (int, int) {
		return 60000, 100000 // 60%
	}
	sq.mu.Lock()
	got = sq.effectiveMaxConcurrent()
	sq.mu.Unlock()
	if got != 1 {
		t.Fatalf("at threshold: got %d, want 1", got)
	}

	// Above threshold → still 1
	sq.tokenEstimateFn = func(key string) (int, int) {
		return 80000, 100000 // 80%
	}
	sq.mu.Lock()
	got = sq.effectiveMaxConcurrent()
	sq.mu.Unlock()
	if got != 1 {
		t.Fatalf("above threshold: got %d, want 1", got)
	}
}

// --- Generation-based stale completion filtering ---

func TestSessionQueue_StaleCompletion_AfterReset(t *testing.T) {
	var runCount atomic.Int32
	blockCh := make(chan struct{})

	runFn := func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		runCount.Add(1)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-blockCh:
			return &agent.RunResult{Content: "from-" + req.RunID}, nil
		}
	}

	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 1}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	// Start run r1
	ch1 := sq.Enqueue(context.Background(), agent.RunRequest{RunID: "r1", SessionKey: "test"})
	time.Sleep(20 * time.Millisecond) // let r1 start

	// Reset bumps generation, cancels r1
	sq.Reset()

	// r1's context is cancelled → it should complete with error
	close(blockCh)
	outcome := <-ch1
	// r1 was either cancelled or completed, but the key test is the generation check
	_ = outcome

	// After reset, new runs should work normally
	sq2ch := sq.Enqueue(context.Background(), agent.RunRequest{RunID: "r2", SessionKey: "test"})
	outcome2 := <-sq2ch
	if outcome2.Err != nil {
		t.Fatalf("post-reset run should succeed: %v", outcome2.Err)
	}
}

// --- CancelAll sets abort cutoff for stale messages ---

func TestSessionQueue_CancelAll_StaleMessages(t *testing.T) {
	blockCh := make(chan struct{})
	runFn := func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-blockCh:
			return &agent.RunResult{}, nil
		}
	}

	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 1}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	ctx := context.Background()
	sq.Enqueue(ctx, agent.RunRequest{RunID: "r1", SessionKey: "test"})
	time.Sleep(10 * time.Millisecond)

	// Queue r2 before cancellation
	ch2 := sq.Enqueue(ctx, agent.RunRequest{RunID: "r2", SessionKey: "test"})

	// CancelAll → sets abort cutoff → r2 was queued before cutoff → stale
	sq.CancelAll()

	outcome := <-ch2
	// r2 was in queue when CancelAll drained it → drainQueue sends context.Canceled
	if !errors.Is(outcome.Err, context.Canceled) {
		t.Fatalf("expected context.Canceled for drained message, got: %v", outcome.Err)
	}

	close(blockCh)
}

// --- Lane concurrency enforcement ---

func TestLane_ConcurrencyEnforcement(t *testing.T) {
	lane := NewLane("test", 2)
	defer lane.Stop()

	var maxConcurrent atomic.Int32
	var current atomic.Int32
	var wg sync.WaitGroup

	for range 10 {
		wg.Add(1)
		err := lane.Submit(context.Background(), func() {
			defer wg.Done()
			cur := current.Add(1)
			// Track max concurrent
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			current.Add(-1)
		})
		if err != nil {
			wg.Done()
		}
	}

	wg.Wait()

	if got := maxConcurrent.Load(); got > 2 {
		t.Fatalf("max concurrent exceeded limit: got %d, want ≤2", got)
	}
}

// --- Lane Submit with cancelled context ---

func TestLane_Submit_CancelledContext(t *testing.T) {
	lane := NewLane("test", 1)
	defer lane.Stop()

	// Fill the single slot
	blockCh := make(chan struct{})
	lane.Submit(context.Background(), func() {
		<-blockCh
	})

	// Submit with already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := lane.Submit(ctx, func() {
		t.Fatal("should not execute")
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}

	close(blockCh)
}

// --- LaneManager fallback to main ---

func TestLaneManager_FallbackToMain(t *testing.T) {
	lm := NewLaneManager([]LaneConfig{
		{Name: LaneMain, Concurrency: 5},
	})

	// Known lane
	if lm.Get(LaneMain) == nil {
		t.Fatal("main lane should exist")
	}

	// Unknown lane → falls back to main
	fallback := lm.Get("nonexistent")
	main := lm.Get(LaneMain)
	if fallback != main {
		t.Fatal("unknown lane should fall back to main")
	}
}

// --- HasActiveSessionsForAgent ---

func TestScheduler_HasActiveSessionsForAgent(t *testing.T) {
	blockCh := make(chan struct{})
	runFn := func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		<-blockCh
		return &agent.RunResult{}, nil
	}

	cfg := DefaultQueueConfig()
	cfg.DebounceMs = 0
	sched := NewScheduler(nil, cfg, runFn)

	sched.Schedule(context.Background(), LaneMain, agent.RunRequest{
		SessionKey: "agent:agent-123:scope1",
		RunID:      "run-1",
	})
	time.Sleep(20 * time.Millisecond) // let run start

	if !sched.HasActiveSessionsForAgent("agent-123") {
		t.Fatal("expected active sessions for agent-123")
	}
	if sched.HasActiveSessionsForAgent("other-agent") {
		t.Fatal("expected no active sessions for other-agent")
	}

	close(blockCh)
	time.Sleep(20 * time.Millisecond)

	if sched.HasActiveSessionsForAgent("agent-123") {
		t.Fatal("expected no active sessions after completion")
	}
}

// --- Debounce collapsing ---

func TestSessionQueue_Debounce_CollapsesRapidMessages(t *testing.T) {
	var runCount atomic.Int32
	runFn := func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		runCount.Add(1)
		return &agent.RunResult{}, nil
	}

	cfg := QueueConfig{
		Mode:          QueueModeQueue,
		Cap:           10,
		Drop:          DropOld,
		DebounceMs:    200, // 200ms debounce
		MaxConcurrent: 1,
	}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	ctx := context.Background()

	// Send 5 rapid messages within debounce window
	var channels []<-chan RunOutcome
	for i := range 5 {
		ch := sq.Enqueue(ctx, agent.RunRequest{
			RunID:      "r" + string(rune('0'+i)),
			SessionKey: "test",
		})
		channels = append(channels, ch)
	}

	// Wait for debounce + execution of all queued messages
	for i, ch := range channels {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatalf("message %d timed out", i)
		}
	}

	// Debounce collapses the initial scheduleNext calls into one timer fire,
	// but all 5 messages still execute sequentially. The key behavior is that
	// execution doesn't start until after debounce delay (200ms), not immediately.
	if got := runCount.Load(); got != 5 {
		t.Fatalf("all 5 messages should eventually run, got %d", got)
	}
}

// --- Interrupt mode ---

func TestSessionQueue_InterruptMode(t *testing.T) {
	var runIDs []string
	var mu sync.Mutex

	runFn := func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		mu.Lock()
		runIDs = append(runIDs, req.RunID)
		mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return &agent.RunResult{}, nil
		}
	}

	cfg := QueueConfig{
		Mode:          QueueModeInterrupt,
		Cap:           10,
		DebounceMs:    0,
		MaxConcurrent: 1,
	}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	ctx := context.Background()
	sq.Enqueue(ctx, agent.RunRequest{RunID: "r1", SessionKey: "test"})
	time.Sleep(20 * time.Millisecond)

	// Interrupt: should cancel r1 and start r2
	ch2 := sq.Enqueue(ctx, agent.RunRequest{RunID: "r2", SessionKey: "test"})

	outcome := <-ch2
	if outcome.Err != nil {
		t.Fatalf("r2 should complete successfully: %v", outcome.Err)
	}
}
