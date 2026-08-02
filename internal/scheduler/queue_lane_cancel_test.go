package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

// saturateLane fills a single-slot lane with a holder run and returns a release
// func. The returned SessionQueue's slot is occupied until release is called, so
// any other session sharing the lane must block in lane.Submit. The holder runs
// on its own session key so it does not interfere with the queue under test.
func saturateLane(t *testing.T, laneMgr *LaneManager, cfg QueueConfig) (release func()) {
	t.Helper()
	block := make(chan struct{})
	started := make(chan struct{})
	var once atomic.Bool
	holdFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		if once.CompareAndSwap(false, true) {
			close(started)
		}
		<-block
		return &agent.RunResult{RunID: req.RunID}, nil
	}
	sqHold := NewSessionQueue("holder", LaneMain, cfg, laneMgr, holdFn)
	chHold := sqHold.Enqueue(context.Background(), agent.RunRequest{RunID: "hold", SessionKey: "holder"}, nil)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("holder never occupied the lane slot")
	}
	// The returned release is idempotent: a test may call it explicitly to unblock
	// the holder mid-test AND still `defer release()` as a safety net without
	// double-closing block.
	var released sync.Once
	return func() {
		released.Do(func() {
			close(block)
			<-chHold
		})
	}
}

// TestSessionQueue_CancelAllWhileWaitingForLane proves mandatory fix #1: when a
// request is blocked in lane.Submit because the lane is saturated, CancelAll
// still acquires sq.mu and cancels the waiting run instead of deadlocking. Before
// the fix, startOne held sq.mu across the blocking lane.Submit, so CancelAll
// could never take the lock while the lane stayed full — a busy turn could
// neither be cancelled nor drained.
func TestSessionQueue_CancelAllWhileWaitingForLane(t *testing.T) {
	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 4}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 1}})
	release := saturateLane(t, laneMgr, cfg)
	defer release()

	var fnRan atomic.Bool
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		fnRan.Store(true)
		return &agent.RunResult{RunID: req.RunID}, nil
	}
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	ch := sq.Enqueue(context.Background(), agent.RunRequest{RunID: "r1", SessionKey: "test"}, nil)

	// Let r1 reach lane.Submit and block on the token.
	time.Sleep(20 * time.Millisecond)

	// CancelAll must not deadlock on sq.mu even though r1 is blocked in Submit.
	done := make(chan bool, 1)
	go func() { done <- sq.CancelAll() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("CancelAll deadlocked while a run waited for a saturated lane")
	}

	select {
	case out := <-ch:
		if out.Err == nil {
			t.Fatal("waiting run should have been cancelled with a non-nil error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled waiting run never delivered an outcome")
	}
	if fnRan.Load() {
		t.Fatal("runFn ran for a run cancelled while it waited for the lane")
	}
}

// TestSessionQueue_CancelOneWhileWaitingForLane proves CancelOne (the /stop
// single-run cancel) also works when the target run is blocked waiting for a
// saturated lane.
func TestSessionQueue_CancelOneWhileWaitingForLane(t *testing.T) {
	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 4}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 1}})
	release := saturateLane(t, laneMgr, cfg)
	defer release()

	var fnRan atomic.Bool
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		fnRan.Store(true)
		return &agent.RunResult{RunID: req.RunID}, nil
	}
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	ch := sq.Enqueue(context.Background(), agent.RunRequest{RunID: "r1", SessionKey: "test"}, nil)
	time.Sleep(20 * time.Millisecond)

	done := make(chan bool, 1)
	go func() { done <- sq.CancelOne() }()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("CancelOne returned false; the waiting run was not tracked as active")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CancelOne deadlocked while a run waited for a saturated lane")
	}

	select {
	case out := <-ch:
		if out.Err == nil {
			t.Fatal("waiting run should have been cancelled with a non-nil error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled waiting run never delivered an outcome")
	}
	if fnRan.Load() {
		t.Fatal("runFn ran for a run cancelled while it waited for the lane")
	}
}

// TestSessionQueue_ResetWhileWaitingForLane proves Reset (in-process restart)
// works when a run is blocked waiting for a saturated lane: it bumps the
// generation, cancels the waiting run, and the stale completion that follows
// does not schedule work under the new generation.
func TestSessionQueue_ResetWhileWaitingForLane(t *testing.T) {
	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 4}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 1}})
	release := saturateLane(t, laneMgr, cfg)
	defer release()

	var fnRan atomic.Bool
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		fnRan.Store(true)
		return &agent.RunResult{RunID: req.RunID}, nil
	}
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	ch := sq.Enqueue(context.Background(), agent.RunRequest{RunID: "r1", SessionKey: "test"}, nil)
	time.Sleep(20 * time.Millisecond)

	done := make(chan struct{})
	go func() { sq.Reset(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Reset deadlocked while a run waited for a saturated lane")
	}

	select {
	case out := <-ch:
		if out.Err == nil {
			t.Fatal("waiting run should have been cancelled by Reset")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reset waiting run never delivered an outcome")
	}
	if fnRan.Load() {
		t.Fatal("runFn ran for a run cancelled by Reset while it waited for the lane")
	}
}

// TestSessionQueue_CancelInHookAbortsBeforeRun proves mandatory fix #2: a turn
// cancelled WHILE its pre-execute hook runs does not proceed to runFn. The hook
// (Team Work classify + audit) can run arbitrarily long; if the user cancels
// during it, the recheck of the execution context after the hook must stop the
// run. Without the recheck the agent would start for an already-cancelled turn.
func TestSessionQueue_CancelInHookAbortsBeforeRun(t *testing.T) {
	var fnRan atomic.Bool
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		fnRan.Store(true)
		return &agent.RunResult{RunID: req.RunID}, nil
	}

	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 1}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	cctx, cancel := context.WithCancel(context.Background())
	inHook := make(chan struct{})
	// The hook signals it has started, then blocks until it is cancelled. It
	// returns the (now-cancelled) context so the post-hook recheck sees the
	// cancellation on exactly the context runFn would run under.
	hook := func(ctx context.Context, _ *agent.RunRequest) (context.Context, error) {
		close(inHook)
		<-ctx.Done()
		return ctx, nil
	}

	ch := sq.Enqueue(cctx, agent.RunRequest{RunID: "r1", SessionKey: "test"}, hook)

	select {
	case <-inHook:
	case <-time.After(5 * time.Second):
		t.Fatal("hook never started")
	}
	cancel() // cancel the turn while its hook is running

	select {
	case out := <-ch:
		if out.Err == nil {
			t.Fatal("run cancelled during its hook should deliver a non-nil error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled-in-hook run never delivered an outcome")
	}
	if fnRan.Load() {
		t.Fatal("runFn ran despite the turn being cancelled during its pre-execute hook")
	}
}

// TestSessionQueue_CancelInHookDoesNotBlockNextRun proves the fix-#2 abort path
// still releases the session's capacity slot through finishRun, so a following
// queued run proceeds. A cancelled-in-hook run must not orphan the serial slot.
func TestSessionQueue_CancelInHookDoesNotBlockNextRun(t *testing.T) {
	ran := make(chan string, 2)
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		ran <- req.RunID
		return &agent.RunResult{RunID: req.RunID}, nil
	}

	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 1}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	cctx, cancel := context.WithCancel(context.Background())
	inHook := make(chan struct{})
	cancelHook := func(ctx context.Context, _ *agent.RunRequest) (context.Context, error) {
		close(inHook)
		<-ctx.Done()
		return ctx, nil
	}

	// r1 is cancelled during its hook; r2 (nil hook, own context) must still run.
	ch1 := sq.Enqueue(cctx, agent.RunRequest{RunID: "r1", SessionKey: "test"}, cancelHook)
	ch2 := sq.Enqueue(context.Background(), agent.RunRequest{RunID: "r2", SessionKey: "test"}, nil)

	select {
	case <-inHook:
	case <-time.After(5 * time.Second):
		t.Fatal("r1 hook never started")
	}
	cancel()

	if out := <-ch1; out.Err == nil {
		t.Fatal("r1 should have aborted after being cancelled in its hook")
	}
	select {
	case got := <-ran:
		if got != "r2" {
			t.Fatalf("ran %q, want r2", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("r2 never ran; the cancelled-in-hook r1 orphaned the capacity slot")
	}
	<-ch2
}
