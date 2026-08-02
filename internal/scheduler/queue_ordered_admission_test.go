package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

// recordingHook returns a PreExecuteHook that appends the request's RunID to the
// shared slice (guarded by mu) when it fires, so a test can assert the exact
// order in which the ordered admission pump invoked pre-execution hooks.
func recordingHook(mu *sync.Mutex, order *[]string) PreExecuteHook {
	return func(ctx context.Context, req *agent.RunRequest) (context.Context, error) {
		mu.Lock()
		*order = append(*order, req.RunID)
		mu.Unlock()
		return ctx, nil
	}
}

// TestSessionQueue_OrderedAdmissionFIFO proves Phase 7 Decision 1: the single
// ordered admission pump admits queued requests strictly in enqueue/seq order.
// The lane is saturated by a holder so A/B/C all pile up behind one slot; once
// released they are admitted — and therefore start their hook and runFn — in the
// exact order A, B, C. Before the fix each request launched its own independent
// lane-acquisition goroutine, so they raced for the token and FIFO could invert.
func TestSessionQueue_OrderedAdmissionFIFO(t *testing.T) {
	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 4}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 1}})
	release := saturateLane(t, laneMgr, cfg)
	defer release()

	var mu sync.Mutex
	var hookOrder []string
	runOrder := make(chan string, 3)
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		runOrder <- req.RunID
		return &agent.RunResult{RunID: req.RunID}, nil
	}
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	// Enqueue A, B, C in order. The lane is saturated, so all three sit behind the
	// holder: A is popped by the pump and blocks in lane.Submit; B and C wait in
	// the queue. None can be admitted until the holder releases the slot.
	hook := recordingHook(&mu, &hookOrder)
	chs := make([]<-chan RunOutcome, 3)
	for i, id := range []string{"A", "B", "C"} {
		chs[i] = sq.Enqueue(context.Background(), agent.RunRequest{RunID: id, SessionKey: "test"}, hook)
	}

	// Give A time to reach lane.Submit and block; B and C settle in the queue.
	time.Sleep(30 * time.Millisecond)

	// Release the holder: A acquires the freed slot first, runs, and releases it;
	// the pump — the sole admitter — then admits B, then C, in that order.
	release()

	for i, want := range []string{"A", "B", "C"} {
		select {
		case got := <-runOrder:
			if got != want {
				t.Fatalf("run #%d entered as %q, want %q (admission order not FIFO)", i, got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("run #%d (%q) never started", i, want)
		}
	}
	for _, ch := range chs {
		if out := <-ch; out.Err != nil {
			t.Fatalf("unexpected error outcome: %v", out.Err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(hookOrder) != 3 || hookOrder[0] != "A" || hookOrder[1] != "B" || hookOrder[2] != "C" {
		t.Fatalf("hook order = %v, want [A B C]", hookOrder)
	}
}

// TestSessionQueue_CancelledBeforeAdmissionSkippedInOrder proves Phase 7 Decision
// 1 point 5: a request cancelled before it receives the lane runs neither its
// PreExecuteHook nor its runFn, while its siblings are still admitted in order. B
// is cancelled (via its own enqueue context — the handle the WS/queue layer uses
// to cancel a specific not-yet-running turn) while all three wait behind the
// saturated lane; after release, A then C run in order and B is delivered a
// cancelled outcome without ever touching the hook or runFn.
func TestSessionQueue_CancelledBeforeAdmissionSkippedInOrder(t *testing.T) {
	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 4}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 1}})
	release := saturateLane(t, laneMgr, cfg)
	defer release()

	var mu sync.Mutex
	var hookOrder []string
	runOrder := make(chan string, 3)
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		runOrder <- req.RunID
		return &agent.RunResult{RunID: req.RunID}, nil
	}
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	hook := recordingHook(&mu, &hookOrder)
	ctxB, cancelB := context.WithCancel(context.Background())
	chA := sq.Enqueue(context.Background(), agent.RunRequest{RunID: "A", SessionKey: "test"}, hook)
	chB := sq.Enqueue(ctxB, agent.RunRequest{RunID: "B", SessionKey: "test"}, hook)
	chC := sq.Enqueue(context.Background(), agent.RunRequest{RunID: "C", SessionKey: "test"}, hook)

	// All three are waiting behind the saturated lane. Cancel B before anything is
	// admitted: the holder still holds the only slot, so B has not — and now can
	// not — reach its hook or runFn.
	time.Sleep(30 * time.Millisecond)
	cancelB()

	// B must be delivered a cancelled outcome (drained via its context by the pump
	// when it pops B and lane.Submit sees the dead context).
	release()

	// A then C run, in order; B never does.
	for i, want := range []string{"A", "C"} {
		select {
		case got := <-runOrder:
			if got != want {
				t.Fatalf("run #%d entered as %q, want %q", i, got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("run #%d (%q) never started", i, want)
		}
	}

	if out := <-chA; out.Err != nil {
		t.Fatalf("A unexpected error: %v", out.Err)
	}
	if out := <-chC; out.Err != nil {
		t.Fatalf("C unexpected error: %v", out.Err)
	}
	if out := <-chB; out.Err == nil {
		t.Fatal("B should have been cancelled with a non-nil error")
	}

	// No stray third run entered.
	select {
	case got := <-runOrder:
		t.Fatalf("a third run %q entered; the cancelled B must never run", got)
	case <-time.After(100 * time.Millisecond):
	}

	mu.Lock()
	defer mu.Unlock()
	for _, id := range hookOrder {
		if id == "B" {
			t.Fatalf("hook fired for cancelled request B; order=%v", hookOrder)
		}
	}
	if len(hookOrder) != 2 || hookOrder[0] != "A" || hookOrder[1] != "C" {
		t.Fatalf("hook order = %v, want [A C]", hookOrder)
	}
}

// TestSessionQueue_CancelAllWhileAwaitingAdmissionNoHookNoRun proves Phase 7
// Decision 1 point 4: CancelAll, arriving while A is blocked in lane.Submit and
// B/C wait in the queue, cancels the lane-waiting A and drains B/C — none of the
// three runs its hook or runFn, and each is delivered a cancelled outcome.
func TestSessionQueue_CancelAllWhileAwaitingAdmissionNoHookNoRun(t *testing.T) {
	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 4}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 1}})
	release := saturateLane(t, laneMgr, cfg)
	defer release()

	var mu sync.Mutex
	var hookOrder []string
	var fnRan atomic.Bool
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		fnRan.Store(true)
		return &agent.RunResult{RunID: req.RunID}, nil
	}
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	hook := recordingHook(&mu, &hookOrder)
	chs := make([]<-chan RunOutcome, 3)
	for i, id := range []string{"A", "B", "C"} {
		chs[i] = sq.Enqueue(context.Background(), agent.RunRequest{RunID: id, SessionKey: "test"}, hook)
	}
	time.Sleep(30 * time.Millisecond)

	done := make(chan bool, 1)
	go func() { done <- sq.CancelAll() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("CancelAll deadlocked while requests awaited admission")
	}

	for i, ch := range chs {
		select {
		case out := <-ch:
			if out.Err == nil {
				t.Fatalf("request #%d should have been cancelled with a non-nil error", i)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("cancelled request #%d never delivered an outcome", i)
		}
	}
	if fnRan.Load() {
		t.Fatal("runFn ran for a request cancelled before admission")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hookOrder) != 0 {
		t.Fatalf("hook fired for cancelled requests: %v", hookOrder)
	}
}

// TestSessionQueue_ResetWhileAwaitingAdmissionNoHookNoRun proves Phase 7 Decision
// 1 point 4 for Reset (in-process restart): a Reset arriving while A waits in
// lane.Submit and B/C wait in the queue cancels the lane-waiting run, drains the
// queue, and no hook or runFn fires. The stale completion that follows A's
// released Submit does not schedule work under the bumped generation.
func TestSessionQueue_ResetWhileAwaitingAdmissionNoHookNoRun(t *testing.T) {
	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 4}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 1}})
	release := saturateLane(t, laneMgr, cfg)
	defer release()

	var mu sync.Mutex
	var hookOrder []string
	var fnRan atomic.Bool
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		fnRan.Store(true)
		return &agent.RunResult{RunID: req.RunID}, nil
	}
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	hook := recordingHook(&mu, &hookOrder)
	chs := make([]<-chan RunOutcome, 3)
	for i, id := range []string{"A", "B", "C"} {
		chs[i] = sq.Enqueue(context.Background(), agent.RunRequest{RunID: id, SessionKey: "test"}, hook)
	}
	time.Sleep(30 * time.Millisecond)

	done := make(chan struct{})
	go func() { sq.Reset(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Reset deadlocked while requests awaited admission")
	}

	for i, ch := range chs {
		select {
		case out := <-ch:
			if out.Err == nil {
				t.Fatalf("request #%d should have been cancelled by Reset", i)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("reset request #%d never delivered an outcome", i)
		}
	}
	if fnRan.Load() {
		t.Fatal("runFn ran for a request cancelled by Reset before admission")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hookOrder) != 0 {
		t.Fatalf("hook fired for reset requests: %v", hookOrder)
	}
}

// TestSessionQueue_StaleCutoffSkipsQueuedBeforeAdmission proves Phase 7 Decision 1
// point 5 for the stale cutoff: queued requests enqueued before an abort cutoff
// are skipped by the pump before they reach the lane, so neither their hook nor
// runFn fires. A occupies the serial slot (its runFn blocks); B and C queue
// behind it; a cutoff is stamped after they enqueue; when A completes the pump's
// skipStaleHead drops B and C with ErrMessageStale without admitting them.
func TestSessionQueue_StaleCutoffSkipsQueuedBeforeAdmission(t *testing.T) {
	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 1}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})

	var mu sync.Mutex
	var hookOrder []string
	releaseA := make(chan struct{})
	var bcRan atomic.Bool
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		if req.RunID == "A" {
			<-releaseA // hold the serial slot so B/C queue behind A
			return &agent.RunResult{RunID: req.RunID}, nil
		}
		bcRan.Store(true)
		return &agent.RunResult{RunID: req.RunID}, nil
	}
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	hook := recordingHook(&mu, &hookOrder)
	chA := sq.Enqueue(context.Background(), agent.RunRequest{RunID: "A", SessionKey: "test"}, hook)
	// Let A be admitted and enter its (blocking) runFn, holding the only slot.
	time.Sleep(30 * time.Millisecond)
	chB := sq.Enqueue(context.Background(), agent.RunRequest{RunID: "B", SessionKey: "test"}, hook)
	chC := sq.Enqueue(context.Background(), agent.RunRequest{RunID: "C", SessionKey: "test"}, hook)

	// Stamp the abort cutoff AFTER B and C enqueued so both are stale. (White-box:
	// this is exactly the state CancelAll leaves, without also cancelling active A.)
	sq.mu.Lock()
	sq.abortCutoffTime = time.Now()
	sq.mu.Unlock()

	// Release A. It completes normally; the pump then runs skipStaleHead and drops
	// B and C before admitting them.
	close(releaseA)

	if out := <-chA; out.Err != nil {
		t.Fatalf("A should have completed normally, got %v", out.Err)
	}
	for _, ch := range []struct {
		id string
		c  <-chan RunOutcome
	}{{"B", chB}, {"C", chC}} {
		select {
		case out := <-ch.c:
			if out.Err != ErrMessageStale {
				t.Fatalf("%s outcome = %v, want ErrMessageStale", ch.id, out.Err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("stale %s never delivered an outcome", ch.id)
		}
	}
	if bcRan.Load() {
		t.Fatal("runFn ran for a stale queued request")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, id := range hookOrder {
		if id == "B" || id == "C" {
			t.Fatalf("hook fired for stale request %s; order=%v", id, hookOrder)
		}
	}
}

// TestSessionQueue_DualContextCheckDetachedHook proves Phase 7 Decision 2: the
// scheduler rechecks BOTH the scheduler-owned input context AND the context the
// PreExecuteHook returned before calling runFn. Here the hook violates its
// contract — it returns a detached, never-cancelled context (context.Background)
// even though the input context has been cancelled. execCtx.Err() would be nil,
// so a scheduler that trusted only the returned context would run the agent for a
// turn the user cancelled. Because the scheduler also checks the input ctx, runFn
// is never called and the turn is resolved cancelled.
func TestSessionQueue_DualContextCheckDetachedHook(t *testing.T) {
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
	// The hook waits for the input ctx to be cancelled, then returns a DETACHED
	// live context (never cancelled) — the exact contract violation Decision 2
	// defends against.
	hook := func(ctx context.Context, _ *agent.RunRequest) (context.Context, error) {
		close(inHook)
		<-ctx.Done()
		return context.Background(), nil
	}

	ch := sq.Enqueue(cctx, agent.RunRequest{RunID: "r1", SessionKey: "test"}, hook)

	select {
	case <-inHook:
	case <-time.After(5 * time.Second):
		t.Fatal("hook never started")
	}
	cancel() // cancel the turn; the hook will still hand back a live context

	select {
	case out := <-ch:
		if out.Err == nil {
			t.Fatal("run cancelled during its hook should deliver a non-nil error even when the hook returns a detached live context")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled-in-hook run never delivered an outcome")
	}
	if fnRan.Load() {
		t.Fatal("runFn ran despite input-context cancellation; the dual-context check failed to catch a detached hook context")
	}
}
