package methods

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
)

func TestMergeChatSendRequestsJoinsContentAndUsesLatestParams(t *testing.T) {
	items := []chatSendRequest{
		{params: chatSendParams{Message: "first", AgentID: "agent-a", SessionKey: "session-a", Stream: false}},
		{params: chatSendParams{Message: "", AgentID: "agent-a", SessionKey: "session-a", Stream: true}},
		{params: chatSendParams{Message: "second", AgentID: "agent-a", SessionKey: "session-a", Stream: true}},
	}

	got := mergeChatSendRequests(items)
	if got.Message != "first\nsecond" {
		t.Fatalf("merged message = %q, want %q", got.Message, "first\nsecond")
	}
	if !got.Stream {
		t.Fatal("latest params should win for stream flag")
	}
}

func TestChatDebouncerFlushesOnceAfterQuietWindow(t *testing.T) {
	out := make(chan []chatSendRequest, 1)
	d := newChatDebouncer(func(items []chatSendRequest) {
		out <- items
	})
	defer d.Stop()

	d.Push("u1:s1", 20*time.Millisecond, chatSendRequest{params: chatSendParams{Message: "one"}})
	d.Push("u1:s1", 20*time.Millisecond, chatSendRequest{params: chatSendParams{Message: "two"}})

	items := waitChatDebounce(t, out)
	if len(items) != 2 {
		t.Fatalf("flushed items = %d, want 2", len(items))
	}
	if got := mergeChatSendRequests(items).Message; got != "one\ntwo" {
		t.Fatalf("merged message = %q", got)
	}
}

func TestChatDebouncerTakeDrainsPendingBeforeBypass(t *testing.T) {
	out := make(chan []chatSendRequest, 1)
	d := newChatDebouncer(func(items []chatSendRequest) {
		out <- items
	})
	defer d.Stop()

	d.Push("u1:s1", time.Minute, chatSendRequest{params: chatSendParams{Message: "pending"}})

	items := d.Take("u1:s1")
	if len(items) != 1 || items[0].params.Message != "pending" {
		t.Fatalf("flushed items = %#v", items)
	}
	assertNoChatDebounceFlush(t, out)
}

func TestChatDebouncerDiscardDropsPendingBeforeCancel(t *testing.T) {
	out := make(chan []chatSendRequest, 1)
	d := newChatDebouncer(func(items []chatSendRequest) {
		out <- items
	})
	defer d.Stop()

	d.Push("u1:s1", 20*time.Millisecond, chatSendRequest{params: chatSendParams{Message: "pending"}})
	d.Discard("u1:s1")

	assertNoChatDebounceFlush(t, out)
}

// The FIFO reservation is the mechanism that serializes ANY busy-session
// follow-up behind the active run — not only workflow finalize/recovery. Since
// 7A, dispatchChatSendsInternal arms this same reservation for every active run
// instead of injecting ordinary follow-ups into the running loop. The queue
// mechanics are identical regardless of the active run's kind: a follow-up
// enqueued while the reservation exists joins it, and the serialized runner
// (which re-enters dispatch with serialized=true) classifies each turn at
// dequeue against the latest history. The initial "active-run done" channel here
// stands in for any active run's Done, not specifically a finalizer's.
func TestChatRunQueueSerializesFollowupsBehindActiveRun(t *testing.T) {
	queue := newChatRunQueue()
	finalizerDone := make(chan struct{})
	runDone := []chan struct{}{make(chan struct{}), make(chan struct{})}
	started := make(chan string, 2)
	runIndex := 0
	run := func(_ context.Context, items []chatSendRequest) <-chan struct{} {
		started <- mergeChatSendRequests(items).Message
		done := runDone[runIndex]
		runIndex++
		return done
	}

	first := []chatSendRequest{{params: chatSendParams{Message: "first follow-up"}}}
	second := []chatSendRequest{{params: chatSendParams{Message: "second follow-up"}}}
	// New reservation: initialWaitFn is evaluated under the queue lock and returns
	// the active run's Done; the worker waits on it before the first batch. Because
	// that wait channel is non-nil, Submit reports submitStartedWaiting (Phase 7
	// closure item 3): the reservation is new but the turn is queued behind an
	// active external run, so it takes the queued-ack contract.
	if res := queue.Submit("session-1", first, func() <-chan struct{} { return finalizerDone }, run); res != submitStartedWaiting {
		t.Fatalf("failed to create session reservation: got submitResult %d, want submitStartedWaiting", res)
	}
	// Reservation already exists → this joins it as a later batch; run/initialWaitFn
	// are ignored on a join.
	if res := queue.Submit("session-1", second, nil, nil); res != submitJoined {
		t.Fatalf("second follow-up did not join the existing reservation: got submitResult %d, want submitJoined", res)
	}

	select {
	case got := <-started:
		t.Fatalf("follow-up %q started before workflow finalizer completed", got)
	case <-time.After(30 * time.Millisecond):
	}
	close(finalizerDone)
	if got := waitChatRunStart(t, started); got != "first follow-up" {
		t.Fatalf("first run=%q", got)
	}
	select {
	case got := <-started:
		t.Fatalf("second follow-up %q started before first user turn completed", got)
	case <-time.After(30 * time.Millisecond):
	}
	close(runDone[0])
	if got := waitChatRunStart(t, started); got != "second follow-up" {
		t.Fatalf("second run=%q", got)
	}
	close(runDone[1])
}

// A media-bearing follow-up must serialize through the SAME reservation as a
// text follow-up. The pre-7A code dropped busy media turns to a fresh concurrent
// run (the InjectMessage branch was text-only); now every busy follow-up joins
// the FIFO regardless of media, so the reservation queue must carry a
// media-bearing batch through unchanged and start it only after the active run
// completes.
func TestChatRunQueueSerializesMediaFollowupBehindActiveRun(t *testing.T) {
	queue := newChatRunQueue()
	activeDone := make(chan struct{})
	runDone := make(chan struct{})
	started := make(chan string, 1)
	run := func(_ context.Context, items []chatSendRequest) <-chan struct{} {
		merged := mergeChatSendRequests(items)
		if len(merged.parseMedia()) == 0 {
			t.Errorf("media follow-up lost its media in the reservation: %+v", merged)
		}
		started <- merged.Message
		return runDone
	}

	mediaFollowup := []chatSendRequest{{params: chatSendParams{Message: "look at this", Media: json.RawMessage(`["/tmp/pic.png"]`)}}}
	// Non-nil initialWaitFn → the reservation is new but waits behind the active
	// run, so Submit reports submitStartedWaiting (Phase 7 closure item 3).
	if res := queue.Submit("session-media", mediaFollowup, func() <-chan struct{} { return activeDone }, run); res != submitStartedWaiting {
		t.Fatalf("failed to create the active-run reservation for a media follow-up: got submitResult %d, want submitStartedWaiting", res)
	}
	// Must not start while the active run is still in flight.
	select {
	case got := <-started:
		t.Fatalf("media follow-up %q started before the active run completed", got)
	case <-time.After(30 * time.Millisecond):
	}
	close(activeDone)
	if got := waitChatRunStart(t, started); got != "look at this" {
		t.Fatalf("media run=%q", got)
	}
	close(runDone)
}

// Submit on an idle session with a nil run is a pure probe: it must not create a
// reservation (submitProbeMiss) so a caller can cheaply ask "is anyone home?"
// without arming a worker. The production path always passes a non-nil run, so
// this only guards the probe contract HasReservation-style callers rely on.
func TestChatRunQueueSubmitProbeMissOnIdle(t *testing.T) {
	queue := newChatRunQueue()
	if res := queue.Submit("idle", nil, nil, nil); res != submitProbeMiss {
		t.Fatalf("probe on idle session = submitResult %d, want submitProbeMiss", res)
	}
	if queue.HasReservation("idle") {
		t.Fatal("probe must not create a reservation")
	}
}

// Backpressure: once a reservation holds chatRunQueueMaxDepth queued batches, the
// next Submit is rejected instead of growing the slice without bound (Phase 7
// review 7A-M2). The batch that the worker has already popped and is running does
// not count against the depth — only not-yet-started batches do.
func TestChatRunQueueSubmitRejectsAtCapacity(t *testing.T) {
	queue := newChatRunQueue()
	block := make(chan struct{})
	runDone := make(chan struct{})
	var startedOnce bool
	run := func(_ context.Context, items []chatSendRequest) <-chan struct{} {
		if !startedOnce {
			startedOnce = true
			close(block) // signal: first batch is now running
		}
		return runDone
	}

	// First Submit starts the worker; its batch is popped and blocks on runDone.
	if res := queue.Submit("cap", []chatSendRequest{{params: chatSendParams{Message: "run-0"}}}, nil, run); res != submitStarted {
		t.Fatalf("first submit = submitResult %d, want submitStarted", res)
	}
	<-block // ensure the first batch left the queue before we fill it

	// Fill the queue to exactly chatRunQueueMaxDepth pending batches.
	for i := 0; i < chatRunQueueMaxDepth; i++ {
		if res := queue.Submit("cap", []chatSendRequest{{params: chatSendParams{Message: "queued"}}}, nil, nil); res != submitJoined {
			t.Fatalf("submit %d = submitResult %d, want submitJoined", i, res)
		}
	}
	// One more must be rejected with backpressure.
	if res := queue.Submit("cap", []chatSendRequest{{params: chatSendParams{Message: "overflow"}}}, nil, nil); res != submitRejected {
		t.Fatalf("over-capacity submit = submitResult %d, want submitRejected", res)
	}
	close(runDone)
	// Let the worker drain so the goroutine exits before the test ends.
	waitChatReservationCleared(t, queue, "cap")
}

// A panic in the serialized runner (the synchronous classify/setup that happens
// before the async run goroutine is spawned) must be recovered by the FIFO worker
// so the reservation is not stranded and later batches still run (Phase 7 review
// 7A-C1, sync arm). We enqueue a second batch behind a first that panics and
// assert the second still starts.
func TestChatRunQueueRecoversRunnerPanic(t *testing.T) {
	queue := newChatRunQueue()
	started := make(chan string, 2)
	runDone := make(chan struct{})
	// proceed gates the first (panicking) batch: the worker pops batch 1 and blocks
	// here until the test has issued Submit #2, so #2 always joins the still-live
	// reservation before the panic recovery drains and deletes it. Without this the
	// worker can win the race, drain the reservation, and Submit #2 lands as a
	// probe-miss (submitProbeMiss) rather than submitJoined — a ~3% flake under -race
	// that is purely test-ordering, not a production behavior change (the post-panic
	// reservation delete is correct so the next send starts fresh).
	proceed := make(chan struct{})
	var calls int
	run := func(_ context.Context, items []chatSendRequest) <-chan struct{} {
		calls++
		msg := mergeChatSendRequests(items).Message
		if calls == 1 {
			<-proceed
			panic("boom in classify/setup")
		}
		started <- msg
		return runDone
	}

	// The panicking batch needs a real client so the worker's recovery path can
	// resolve its request with a terminal error (Phase 7 review 7A-C1) instead of
	// nil-dereferencing — in production every chatSendRequest carries a client.
	pc, pch := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "p", 4)
	if res := queue.Submit("panic", []chatSendRequest{{ctx: context.Background(), client: pc, requestID: "p1", params: chatSendParams{Message: "will-panic"}}}, nil, run); res != submitStarted {
		t.Fatalf("first submit = submitResult %d, want submitStarted", res)
	}
	if res := queue.Submit("panic", []chatSendRequest{{params: chatSendParams{Message: "survivor"}}}, nil, nil); res != submitJoined {
		t.Fatalf("second submit = submitResult %d, want submitJoined", res)
	}
	// Batch 2 has joined the live reservation; release batch 1 to panic now.
	close(proceed)
	// The panicked batch's request must be resolved with a terminal error, not left
	// hanging.
	if resp := readResponse(t, pch); resp.ID != "p1" || resp.Error == nil {
		t.Fatalf("panicked batch response = %+v, want error response for p1", resp)
	}
	// The worker must recover from the first batch's panic and proceed to the second.
	if got := waitChatRunStart(t, started); got != "survivor" {
		t.Fatalf("post-panic run=%q, want survivor (worker did not survive the panic)", got)
	}
	close(runDone)
	waitChatReservationCleared(t, queue, "panic")
}

// Cancel drains every not-yet-started batch, resolving each request as cancelled,
// and reports how many batches it drained (Phase 7 review 7A-H5). The batch the
// worker already popped is left running; only queued batches are drained.
func TestChatRunQueueCancelDrainsQueuedBatches(t *testing.T) {
	queue := newChatRunQueue()
	runDone := make(chan struct{})
	popped := make(chan struct{})
	var startedOnce bool
	run := func(_ context.Context, items []chatSendRequest) <-chan struct{} {
		if !startedOnce {
			startedOnce = true
			close(popped)
		}
		return runDone
	}

	if res := queue.Submit("cancel", []chatSendRequest{{params: chatSendParams{Message: "running"}}}, nil, run); res != submitStarted {
		t.Fatalf("first submit = submitResult %d, want submitStarted", res)
	}
	<-popped // first batch is now running, off the queue

	// Two queued batches, each with a request whose cancelled response we can observe.
	c1, ch1 := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u1", 4)
	c2, ch2 := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u2", 4)
	queue.Submit("cancel", []chatSendRequest{{client: c1, requestID: "q1", params: chatSendParams{Message: "queued-1"}}}, nil, nil)
	queue.Submit("cancel", []chatSendRequest{{client: c2, requestID: "q2", params: chatSendParams{Message: "queued-2"}}}, nil, nil)

	if drained := queue.Cancel("cancel"); drained != 2 {
		t.Fatalf("Cancel drained %d batches, want 2", drained)
	}
	// Both queued requests must have received a cancelled response.
	assertChatCancelled(t, ch1, "q1")
	assertChatCancelled(t, ch2, "q2")

	close(runDone)
	waitChatReservationCleared(t, queue, "cancel")
}

// Shutdown blocks new Submits and drains still-queued batches with a terminal
// error so nothing hangs at process exit (Phase 7 review 7A-M2).
func TestChatRunQueueShutdownDrainsAndBlocks(t *testing.T) {
	queue := newChatRunQueue()
	runDone := make(chan struct{})
	popped := make(chan struct{})
	var startedOnce bool
	run := func(_ context.Context, items []chatSendRequest) <-chan struct{} {
		if !startedOnce {
			startedOnce = true
			close(popped)
		}
		return runDone
	}
	if res := queue.Submit("shut", []chatSendRequest{{params: chatSendParams{Message: "running"}}}, nil, run); res != submitStarted {
		t.Fatalf("first submit = submitResult %d, want submitStarted", res)
	}
	<-popped

	c, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 4)
	queue.Submit("shut", []chatSendRequest{{ctx: context.Background(), client: c, requestID: "q", params: chatSendParams{Message: "queued"}}}, nil, nil)

	queue.Shutdown()
	// Queued batch drained with a response (terminal error), so it does not hang.
	if resp := readResponse(t, ch); resp.ID != "q" {
		t.Fatalf("shutdown-drained response ID = %q, want q", resp.ID)
	}
	// New submits are refused after shutdown.
	if res := queue.Submit("shut", []chatSendRequest{{params: chatSendParams{Message: "late"}}}, nil, run); res != submitShutdown {
		t.Fatalf("post-shutdown submit = submitResult %d, want submitShutdown", res)
	}
	close(runDone)
}

// TestChatDebouncerPushAfterCloseReturnsFalse proves the atomic shutdown-admission
// contract (Phase 7 closure item 4): once CloseAndDrain has run, a later Push in
// EITHER branch (delay>0 and delay<=0) is rejected — it neither buffers, arms a
// timer, nor synchronously flushes — and returns false so the caller settles the
// item with a shutdown error instead of dispatching into a closed debouncer.
func TestChatDebouncerPushAfterCloseReturnsFalse(t *testing.T) {
	out := make(chan []chatSendRequest, 1)
	d := newChatDebouncer(func(items []chatSendRequest) { out <- items })
	defer d.Stop()

	d.CloseAndDrain()

	if d.Push("u1:s1", 20*time.Millisecond, chatSendRequest{params: chatSendParams{Message: "late-delayed"}}) {
		t.Fatal("delayed Push after close returned true; must be rejected")
	}
	if d.Push("u1:s1", 0, chatSendRequest{params: chatSendParams{Message: "late-immediate"}}) {
		t.Fatal("immediate Push after close returned true; must be rejected")
	}
	assertNoChatDebounceFlush(t, out)
}

// TestChatDebouncerCloseAndDrainReturnsBuffersWithoutFlushing proves CloseAndDrain
// is a pure drain barrier (Phase 7 closure item 4): still-buffered items are
// returned to the caller grouped by window, and flushFn is NOT invoked — shutdown
// settles them with a terminal error rather than dispatching them into a
// shutting-down queue.
func TestChatDebouncerCloseAndDrainReturnsBuffersWithoutFlushing(t *testing.T) {
	out := make(chan []chatSendRequest, 4)
	d := newChatDebouncer(func(items []chatSendRequest) { out <- items })
	defer d.Stop()

	d.Push("u1:s1", time.Minute, chatSendRequest{params: chatSendParams{Message: "a"}})
	d.Push("u1:s1", time.Minute, chatSendRequest{params: chatSendParams{Message: "b"}})
	d.Push("u2:s2", time.Minute, chatSendRequest{params: chatSendParams{Message: "c"}})

	batches := d.CloseAndDrain()
	total := 0
	for _, b := range batches {
		total += len(b)
	}
	if total != 3 {
		t.Fatalf("CloseAndDrain returned %d items across %d batches, want 3", total, len(batches))
	}
	// Nothing may be dispatched — flushFn must never run during a drain.
	assertNoChatDebounceFlush(t, out)
}

// TestChatDebouncerTimerAfterCloseDoesNotDispatch proves a debounce timer that
// fires AFTER CloseAndDrain (the timer-race window) dispatches nothing: Flush→Take
// observes closed and returns nil (Phase 7 closure item 4). We arm a short timer,
// close before it fires, then wait past its deadline and assert no flush.
func TestChatDebouncerTimerAfterCloseDoesNotDispatch(t *testing.T) {
	out := make(chan []chatSendRequest, 1)
	d := newChatDebouncer(func(items []chatSendRequest) { out <- items })
	defer d.Stop()

	d.Push("u1:s1", 30*time.Millisecond, chatSendRequest{params: chatSendParams{Message: "buffered"}})
	// Close (and drain) before the timer fires; CloseAndDrain stops the timer under
	// the same lock, but even a timer already inside AfterFunc must find closed.
	d.CloseAndDrain()

	// Wait well past the original timer deadline; a leaked dispatch would land here.
	assertNoChatDebounceFlush(t, out)
}

// TestChatDebouncerCloseAndDrainWaitsForInFlightTimerFlush proves the Take→flushFn
// barrier (Phase 7 closure item 4). A debounce timer that has ALREADY fired and
// entered flushFn — having removed its batch from the buffer map under the lock
// just before close — must keep CloseAndDrain blocked until that flushFn returns.
// Without the dispatching WaitGroup, CloseAndDrain would return while the escaped
// batch was still being handed to the FIFO queue: the shutdown drain would move
// past the debouncer and that batch would reach an already-shutting-down queue
// with no response (a hung chat.send). The pre-fix item-4 tests never forced this
// interleaving — they only closed BEFORE the timer fired. Here we drive the exact
// window deterministically: flushFn signals entry then blocks; we assert
// CloseAndDrain has not returned, release flushFn, then assert it returns.
func TestChatDebouncerCloseAndDrainWaitsForInFlightTimerFlush(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	d := newChatDebouncer(func(items []chatSendRequest) {
		close(entered)
		<-release
	})

	// Arm a short timer; when it fires, Flush→takeForFlush removes the batch under
	// the lock (registering the dispatch on the WaitGroup) and calls flushFn, which
	// blocks holding the WaitGroup counter at 1.
	d.Push("u1:s1", 5*time.Millisecond, chatSendRequest{params: chatSendParams{Message: "buffered"}})

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("timer flush never entered flushFn")
	}

	// CloseAndDrain must block on the WaitGroup until the in-flight flushFn returns.
	drainReturned := make(chan struct{})
	go func() {
		d.CloseAndDrain()
		close(drainReturned)
	}()

	select {
	case <-drainReturned:
		t.Fatal("CloseAndDrain returned while an in-flight timer flushFn was still running; the take→flushFn barrier is missing")
	case <-time.After(100 * time.Millisecond):
	}

	// Release flushFn; CloseAndDrain's Wait must now unblock.
	close(release)
	select {
	case <-drainReturned:
	case <-time.After(time.Second):
		t.Fatal("CloseAndDrain did not return after the in-flight timer flushFn finished")
	}
}

// TestChatDebouncerCloseAndDrainWaitsForInFlightSyncFlush proves the same barrier
// for the Push merge-then-flush path (delay<=0 with an existing buffer): the
// sync-flush registers on the dispatching WaitGroup under the lock, so a
// CloseAndDrain racing it blocks until flushFn returns rather than letting the
// merged batch escape into a shutting-down queue (Phase 7 closure item 4).
func TestChatDebouncerCloseAndDrainWaitsForInFlightSyncFlush(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	d := newChatDebouncer(func(items []chatSendRequest) {
		close(entered)
		<-release
	})

	// Buffer one item with a long timer so it stays put, then a delay<=0 Push merges
	// and flushes synchronously — flushFn blocks holding the WaitGroup counter.
	d.Push("u1:s1", time.Hour, chatSendRequest{params: chatSendParams{Message: "first"}})
	go d.Push("u1:s1", 0, chatSendRequest{params: chatSendParams{Message: "second"}})

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("sync-flush never entered flushFn")
	}

	drainReturned := make(chan struct{})
	go func() {
		d.CloseAndDrain()
		close(drainReturned)
	}()

	select {
	case <-drainReturned:
		t.Fatal("CloseAndDrain returned while an in-flight sync-flush was still running; the barrier is missing")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case <-drainReturned:
	case <-time.After(time.Second):
		t.Fatal("CloseAndDrain did not return after the in-flight sync-flush finished")
	}
}

func TestChatDebounceDelayGlobalAndAgentOverride(t *testing.T) {
	// hasMedia=false: legacy behavior preserved (no floor applied).
	if got := chatDebounceDelay(&config.Config{}, nil, false); got != 0 {
		t.Fatalf("default debounce = %s, want disabled", got)
	}
	cfg := &config.Config{}
	cfg.Gateway.InboundDebounceMs = 250
	if got := chatDebounceDelay(cfg, nil, false); got != 250*time.Millisecond {
		t.Fatalf("global debounce = %s, want 250ms", got)
	}
	if got := chatDebounceDelay(cfg, []byte(`{"inbound_debounce_ms":0}`), false); got != 0 {
		t.Fatalf("agent disabled debounce = %s, want disabled", got)
	}
	if got := chatDebounceDelay(cfg, []byte(`{"inbound_debounce_ms":500}`), false); got != 500*time.Millisecond {
		t.Fatalf("agent custom debounce = %s, want 500ms", got)
	}
	if got := chatDebounceDelay(cfg, []byte(`{"other":true}`), false); got != 250*time.Millisecond {
		t.Fatalf("agent inherit debounce = %s, want 250ms", got)
	}
}

func waitChatDebounce(t *testing.T, ch <-chan []chatSendRequest) []chatSendRequest {
	t.Helper()
	select {
	case items := <-ch:
		return items
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for chat debounce flush")
		return nil
	}
}

func waitChatRunStart(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case message := <-ch:
		return message
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for serialized chat run")
		return ""
	}
}

func assertNoChatDebounceFlush(t *testing.T, ch <-chan []chatSendRequest) {
	t.Helper()
	select {
	case items := <-ch:
		t.Fatalf("unexpected flush: %#v", items)
	case <-time.After(50 * time.Millisecond):
	}
}

// waitChatReservationCleared polls until the FIFO worker has drained its queue
// and removed the session reservation, so a test that closed the run's Done does
// not leak the worker goroutine past the test body.
func waitChatReservationCleared(t *testing.T, q *chatRunQueue, key string) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !q.HasReservation(key) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("reservation for %q was not cleared; worker goroutine leaked", key)
}

// assertChatCancelled asserts the given request ID received the {cancelled:true}
// response the drain path sends (Phase 7 review 7A-H5).
func assertChatCancelled(t *testing.T, ch <-chan []byte, wantID string) {
	t.Helper()
	resp := readResponse(t, ch)
	if resp.ID != wantID {
		t.Fatalf("cancelled response ID = %q, want %q", resp.ID, wantID)
	}
	if resp.Error != nil {
		t.Fatalf("cancelled response for %q carried an error: %+v", wantID, resp.Error)
	}
	payload, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("cancelled response for %q has non-object payload: %#v", wantID, resp.Payload)
	}
	if cancelled, _ := payload["cancelled"].(bool); !cancelled {
		t.Fatalf("cancelled response for %q missing cancelled=true: %#v", wantID, payload)
	}
}
