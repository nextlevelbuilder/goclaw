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

// preExecCtxKey is a private context key used to prove the context the
// PreExecute hook returns is the one runFn executes under.
type preExecCtxKey struct{}

// TestSessionQueue_PreExecuteInvokedAtDequeueOncePerRun pins the core 7A-H1
// timing contract: on a serial (maxConcurrent=1) session, each run's PreExecute
// hook fires at dequeue — exactly once, before its own runFn — and run N+1's
// hook does not fire until run N's runFn has returned. That ordering is what
// lets the inbound Team Work gate classify against the history the preceding run
// just wrote, instead of the history at enqueue time.
func TestSessionQueue_PreExecuteInvokedAtDequeueOncePerRun(t *testing.T) {
	var mu sync.Mutex
	var events []string
	record := func(e string) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		record("run:" + req.RunID)
		return &agent.RunResult{Content: "ok", RunID: req.RunID}, nil
	}

	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 1}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	mkHook := func(id string) PreExecuteHook {
		return func(ctx context.Context, _ *agent.RunRequest) (context.Context, error) {
			record("pre:" + id)
			return ctx, nil
		}
	}

	ctx := context.Background()
	ch1 := sq.Enqueue(ctx, agent.RunRequest{RunID: "a", SessionKey: "test"}, mkHook("a"))
	ch2 := sq.Enqueue(ctx, agent.RunRequest{RunID: "b", SessionKey: "test"}, mkHook("b"))

	for i, ch := range []<-chan RunOutcome{ch1, ch2} {
		select {
		case out := <-ch:
			if out.Err != nil {
				t.Fatalf("run %d error: %v", i, out.Err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("run %d timed out", i)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	// Serial dequeue: a's hook and run must both precede b's hook and run.
	want := []string{"pre:a", "run:a", "pre:b", "run:b"}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events = %v, want %v", events, want)
		}
	}
}

// TestSessionQueue_PreExecuteMutationAndContextVisibleToRunFn proves the hook's
// two effects both reach runFn: an in-place mutation of the RunRequest (the gate
// setting Message/directive/blocked tools) and the augmented context it returns
// (the per-turn pending-dispatch tracker, audit ID, plan constraint the gate
// injects). Without both, the dequeue-time gate could not replace the
// enqueue-time context threading it removed.
func TestSessionQueue_PreExecuteMutationAndContextVisibleToRunFn(t *testing.T) {
	type observed struct {
		message string
		ctxVal  any
	}
	seen := make(chan observed, 1)
	runFn := func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		seen <- observed{message: req.Message, ctxVal: ctx.Value(preExecCtxKey{})}
		return &agent.RunResult{RunID: req.RunID}, nil
	}

	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 1}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	hook := func(ctx context.Context, req *agent.RunRequest) (context.Context, error) {
		req.Message = "rewritten-at-dequeue"
		return context.WithValue(ctx, preExecCtxKey{}, "per-turn-value"), nil
	}

	ch := sq.Enqueue(context.Background(), agent.RunRequest{
		RunID:      "r1",
		SessionKey: "test",
		Message:    "original-at-enqueue",
	}, hook)

	select {
	case got := <-seen:
		if got.message != "rewritten-at-dequeue" {
			t.Fatalf("runFn saw message %q, want the hook-mutated value", got.message)
		}
		if got.ctxVal != "per-turn-value" {
			t.Fatalf("runFn ctx value = %v, want the context the hook returned", got.ctxVal)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runFn never observed")
	}
	<-ch
}

// TestSessionQueue_NilPreExecuteRunsUnchanged confirms a run without a hook
// (every protected internal run: finalize, recovery, announce, cron) executes
// normally under the passed context — the hook is strictly opt-in.
func TestSessionQueue_NilPreExecuteRunsUnchanged(t *testing.T) {
	ran := make(chan string, 1)
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		ran <- req.Message
		return &agent.RunResult{RunID: req.RunID}, nil
	}

	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 1}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	ch := sq.Enqueue(context.Background(), agent.RunRequest{RunID: "r1", SessionKey: "test", Message: "unchanged"}, nil)
	select {
	case got := <-ran:
		if got != "unchanged" {
			t.Fatalf("runFn message = %q, want unchanged", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runFn never ran with nil hook")
	}
	<-ch
}

// TestSessionQueue_StaleMessageSkipsPreExecute proves a message skipped as stale
// never runs its hook: the gate (and its LLM classify call + audit write) must
// not fire for a turn the scheduler discards. startOne drops stale messages with
// ErrMessageStale before executeRun, where the hook lives, so it is structurally
// unreachable for them — this guards that placement.
func TestSessionQueue_StaleMessageSkipsPreExecute(t *testing.T) {
	block := make(chan struct{})
	var hookCalls sync.Map // runID -> struct{}
	runFn := func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		if req.RunID == "r1" {
			<-block // hold the single capacity slot until we've armed the cutoff
		}
		return &agent.RunResult{RunID: req.RunID}, nil
	}
	mkHook := func(id string) PreExecuteHook {
		return func(ctx context.Context, _ *agent.RunRequest) (context.Context, error) {
			hookCalls.Store(id, struct{}{})
			return ctx, nil
		}
	}

	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 1}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	ctx := context.Background()
	ch1 := sq.Enqueue(ctx, agent.RunRequest{RunID: "r1", SessionKey: "test"}, mkHook("r1"))
	// r1 is now running and blocked. Queue r2 behind it.
	ch2 := sq.Enqueue(ctx, agent.RunRequest{RunID: "r2", SessionKey: "test"}, mkHook("r2"))

	// Arm the abort cutoff to a time strictly after r2 was enqueued so r2 is stale
	// when r1 completes and startOne next runs. (CancelAll would drain the queue;
	// we want r2 to survive to the stale-skip branch, so set the cutoff directly.)
	time.Sleep(10 * time.Millisecond)
	sq.mu.Lock()
	sq.abortCutoffTime = time.Now()
	sq.mu.Unlock()

	close(block) // let r1 finish → scheduleNext → startOne skips stale r2

	if out := <-ch2; out.Err == nil || !errIsStale(out.Err) {
		t.Fatalf("r2 outcome err = %v, want ErrMessageStale", out.Err)
	}
	<-ch1

	if _, ok := hookCalls.Load("r2"); ok {
		t.Fatal("hook fired for a stale-skipped message; the gate would classify a discarded turn")
	}
	if _, ok := hookCalls.Load("r1"); !ok {
		t.Fatal("hook did not fire for the run that actually executed")
	}
}

// TestSessionQueue_PreExecuteErrorAbortsRun proves the hook's error contract
// (user guidance #3): a non-nil error ABORTS the run — runFn is never called and
// the error is delivered as the run's outcome. This is what lets a hook that
// genuinely cannot proceed (e.g. a required audit write that failed and must not
// be swallowed) refuse the turn instead of running it degraded.
func TestSessionQueue_PreExecuteErrorAbortsRun(t *testing.T) {
	var ranFn atomic.Bool
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		ranFn.Store(true)
		return &agent.RunResult{RunID: req.RunID}, nil
	}

	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 1}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	wantErr := errors.New("hook refused")
	hook := func(ctx context.Context, _ *agent.RunRequest) (context.Context, error) {
		return nil, wantErr
	}

	ch := sq.Enqueue(context.Background(), agent.RunRequest{RunID: "r1", SessionKey: "test"}, hook)
	select {
	case out := <-ch:
		if !errors.Is(out.Err, wantErr) {
			t.Fatalf("outcome err = %v, want %v", out.Err, wantErr)
		}
		if out.Result != nil {
			t.Fatalf("outcome result = %v, want nil (runFn must not have run)", out.Result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("aborted run never delivered an outcome")
	}
	if ranFn.Load() {
		t.Fatal("runFn ran despite the hook returning an error")
	}
}

// TestSessionQueue_PreExecuteErrorDoesNotBlockNextRun proves an aborted run still
// releases its capacity slot so the next queued run proceeds. The error path must
// go through the same finishRun cleanup as a normal completion — otherwise a hook
// error would orphan the session's single slot and wedge the queue.
func TestSessionQueue_PreExecuteErrorDoesNotBlockNextRun(t *testing.T) {
	ran := make(chan string, 2)
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		ran <- req.RunID
		return &agent.RunResult{RunID: req.RunID}, nil
	}

	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 1}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 10}})
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	failHook := func(ctx context.Context, _ *agent.RunRequest) (context.Context, error) {
		return nil, errors.New("first run refused")
	}

	ctx := context.Background()
	// r1 aborts in its hook; r2 has a nil hook and must still run.
	ch1 := sq.Enqueue(ctx, agent.RunRequest{RunID: "r1", SessionKey: "test"}, failHook)
	ch2 := sq.Enqueue(ctx, agent.RunRequest{RunID: "r2", SessionKey: "test"}, nil)

	if out := <-ch1; out.Err == nil {
		t.Fatal("r1 should have aborted with the hook error")
	}
	select {
	case got := <-ran:
		if got != "r2" {
			t.Fatalf("ran %q, want r2", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("r2 never ran; the aborted r1 orphaned the capacity slot")
	}
	<-ch2
}

// TestSessionQueue_PostLaneCancellationSkipsHookAndRun proves the post-lane
// cancellation recheck (user guidance #4): a run whose context is cancelled while
// it waited for a lane slot bails BEFORE the hook fires — so the Team Work
// classifier (an LLM call + durable audit write) never runs for an already-dead
// turn — and runFn is likewise never called. The outcome carries the context
// error.
func TestSessionQueue_PostLaneCancellationSkipsHookAndRun(t *testing.T) {
	var hookRan, fnRan atomic.Bool
	runFn := func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		fnRan.Store(true)
		return &agent.RunResult{RunID: req.RunID}, nil
	}
	hook := func(ctx context.Context, _ *agent.RunRequest) (context.Context, error) {
		hookRan.Store(true)
		return ctx, nil
	}

	// A single-slot lane so the waiting request must block behind a holder.
	cfg := QueueConfig{Mode: QueueModeQueue, Cap: 10, DebounceMs: 0, MaxConcurrent: 2}
	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 1}})
	sq := NewSessionQueue("test", LaneMain, cfg, laneMgr, runFn)

	// Saturate the lane's single slot with a holder that is guaranteed to be IN the
	// slot before we proceed (the helper waits on a started barrier). Under the
	// ordered pump the holder and r2 are admitted asynchronously, so without this
	// barrier r2 could win the token race and run — the helper removes that race.
	release := saturateLane(t, laneMgr, cfg)
	defer release()

	// Under the pump, Enqueue returns immediately; the pump pops r2, registers it
	// active, and blocks in lane.Submit on the saturated lane's token. r2 keeps its
	// own enqueue context, so cancelling it makes Submit return ctx.Err before the
	// hook or runFn ever fire.
	cctx, cancel := context.WithCancel(context.Background())
	ch2 := sq.Enqueue(cctx, agent.RunRequest{RunID: "r2", SessionKey: "test"}, hook)

	time.Sleep(20 * time.Millisecond) // let the pump reach lane.Submit and block on the token
	cancel()                          // cancel r2 while it waits for the slot → Submit returns ctx.Err

	select {
	case out := <-ch2:
		if out.Err == nil {
			t.Fatal("cancelled run should have delivered a non-nil error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled run never delivered an outcome")
	}

	release() // release the holder so its goroutine doesn't leak

	if hookRan.Load() {
		t.Fatal("hook ran for a run cancelled before execution; the gate would classify a dead turn")
	}
	if fnRan.Load() {
		t.Fatal("runFn ran for a cancelled run")
	}
}

func errIsStale(err error) bool {
	return err == ErrMessageStale
}
