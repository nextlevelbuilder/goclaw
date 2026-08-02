package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// chatMediaDebounceFloorMs is the minimum debounce window applied to Web Chat
// sends that carry media when the post-override delay would otherwise be 0.
// Mirrors cmd/gateway_consumer_debounce.go mediaDebounceFloorMs (Phase 1) —
// duplicated by value (1000ms) to keep gateway/methods decoupled from cmd.
const chatMediaDebounceFloorMs = 1000

// turnLifecycle is the per-logical-turn state for the Phase 7 Decision 4 chat
// turn lifecycle. A stable turnID is assigned before enqueue so a turn that is
// only queued or being classified — states that exist BEFORE any runID — still
// has an observable identity. terminal is a one-shot latch: it guarantees
// EXACTLY ONE terminal lifecycle event (completed | cancelled | failed) is
// emitted per turn, no matter which path resolves it (queue drain, cancel
// recheck, run success, run error, panic). It is DISTINCT from the request's
// respondedOnce latch: respondedOnce bounds RPC-response cardinality, terminal
// bounds lifecycle-event cardinality, and neither may suppress the other (Phase
// 7 Decision 4 point 8). A debounce/queue batch is one turn; its canonical turn
// is the primary (last) request, matching mergeChatSendRequests' primary rule,
// so every copy in the batch shares one turnID and one terminal latch by
// pointer.
type turnLifecycle struct {
	turnID   string
	terminal sync.Once
}

type chatSendRequest struct {
	ctx        context.Context
	client     *gateway.Client
	requestID  string
	params     chatSendParams
	loop       agent.Agent
	userID     string
	sessionKey string
	// turnLifecycle carries the stable turnID and the one-shot terminal-lifecycle
	// latch for the logical turn this request belongs to (Phase 7 Decision 4),
	// shared by pointer across every copy the request makes through the debouncer
	// and FIFO queue. nil disables turn lifecycle (direct/unit sends), so the
	// emitter and queued ack simply omit the turnId — preserving pre-Decision-4
	// behavior for callers that build requests inline.
	turnLifecycle *turnLifecycle
	// respondedOnce enforces exactly-one terminal RPC response per chat.send,
	// shared (by pointer) across every copy of this request as it moves through
	// the debouncer, the FIFO queue, and the serialized run. A busy follow-up is
	// acknowledged with a structural {queued:true} the moment it joins the FIFO
	// (Phase 7 review deviation #1); that ack claims the latch, so the batch's
	// later serialized run — whose assistant output is delivered out-of-band via
	// run/event/history — does NOT emit a second RPC response for the same ID.
	// The same latch makes a panic-path terminal error after a success send a
	// no-op (Phase 7 review trace item C: exactly-one-RPC-response). nil disables
	// the latch (direct send) so unit tests that build requests inline keep their
	// current single-send behavior.
	respondedOnce *sync.Once
}

type chatDebouncer struct {
	mu      sync.Mutex
	buffers map[string]*chatDebounceBuffer
	flushFn func([]chatSendRequest)
	// closed is set once by CloseAndDrain at graceful shutdown, under mu (Phase 7
	// closure item 4). Every Push branch checks it in the SAME critical section
	// that guards the buffer map, so a request that passed handleSend's early
	// fail-fast latch but reached Push only after the shutdown drain completed is
	// rejected (returns false) instead of creating a fresh buffer or dispatching
	// into an already-drained debouncer. Flush/Take also observe it so a timer
	// callback that fires after close cannot dispatch a stale buffer.
	closed bool
	// dispatching counts flushFn invocations that have already left the buffer map
	// (a timer Flush→take, or a Push sync-flush) but are still running OUTSIDE d.mu.
	// Each is registered under d.mu after observing !closed, so CloseAndDrain — which
	// sets closed under the same lock and then Waits — blocks until every in-flight
	// dispatch finishes. This closes the Take→flushFn window (Phase 7 closure item 4):
	// without it a batch taken from the buffer just before close could reach the FIFO
	// queue AFTER CloseAndDrain returned and the shutdown drain moved on, hanging with
	// no response. New dispatches cannot register once closed is set — they observe it
	// under the lock and return false (Push) / nil (take).
	dispatching sync.WaitGroup
}

type chatDebounceBuffer struct {
	items []chatSendRequest
	timer *time.Timer
}

// chatRunQueueMaxDepth caps the number of not-yet-started batches a single
// session reservation may hold before further sends are rejected with a
// backpressure error (Phase 7 review 7A-M2). A genuinely stuck run must not let
// batches — and the request contexts, clients and media they pin — accumulate
// without bound.
const chatRunQueueMaxDepth = 32

type chatRunQueue struct {
	mu       sync.Mutex
	queues   map[string]*chatRunQueueState
	shutdown bool

	// onTerminal emits the one-shot terminal chat.turn lifecycle event (Phase 7
	// Decision 4) for a batch the queue resolves itself — a drained/cancelled
	// batch (Cancel), a batch dropped at shutdown (Shutdown), or a batch whose
	// synchronous setup panicked (runBatch). The queue owns these terminal
	// transitions but has no event bus, so ChatMethods wires this callback at
	// construction. nil (unit wiring without an event bus) makes every terminal
	// emission a no-op. It is keyed on the batch's shared turnLifecycle latch, so
	// the RPC resolution (sendChatCancelled/sendChatError) and this lifecycle
	// terminal remain independent (Decision 4 point 8).
	onTerminal func(requests []chatSendRequest, state string)
}

type chatRunQueueState struct {
	batches [][]chatSendRequest
	run     func(cancelCtx context.Context, batch []chatSendRequest) <-chan struct{}

	// cancelCurrent cancels the batch the worker has popped and is currently
	// classifying/running before its run registers with the router. It is the
	// cancellation handle for the popped batch that is no longer in `batches` yet
	// not yet visible to the router — the window a plain Cancel (drains `batches`)
	// and AbortRunsForSession (router only) both miss (Phase 7 review mandatory
	// fix #5). Guarded by chatRunQueue.mu; nil when no batch is in flight.
	cancelCurrent context.CancelFunc
}

// submitResult reports how a Submit attempt resolved.
type submitResult int

const (
	submitJoined  submitResult = iota // appended to an existing reservation
	submitStarted                     // created a new reservation + started the worker (ran immediately)
	// submitStartedWaiting created a new reservation + started the worker, but the
	// worker's first batch must WAIT on an already-active external run's Done
	// before it can classify/run (Phase 7 closure item 3). The reservation is new
	// (not joined), yet the turn is not running — so, like submitJoined, it must
	// receive an immediate {queued:true} ack + queued lifecycle rather than the
	// deferred RPC an idle submitStarted turn gets. The decision is taken from the
	// admission-time router snapshot (initialWaitFn returning a non-nil channel),
	// never by probing whether that channel is already closed.
	submitStartedWaiting
	submitProbeMiss // no reservation existed and run was nil (nothing enqueued)
	submitRejected  // backpressure: reservation is at capacity
	submitShutdown  // queue is shutting down; nothing enqueued
)

func newChatRunQueue() *chatRunQueue {
	// onTerminal forwards to the package-level emitTurnLifecycle so a batch the
	// queue itself resolves (Cancel drain, Shutdown drain, runBatch panic) emits its
	// one-shot terminal chat.turn lifecycle event (Phase 7 Decision 4). The
	// exactly-one-terminal latch lives in the batch's shared turnLifecycle, so wiring
	// this unconditionally is safe: a batch without a turnLifecycle (direct/unit
	// send) is a no-op inside emitTurnLifecycle.
	return &chatRunQueue{
		queues:     make(map[string]*chatRunQueueState),
		onTerminal: func(requests []chatSendRequest, state string) { emitTurnLifecycle(requests, state, "") },
	}
}

// Submit atomically joins an existing per-session reservation or creates a new
// one, deciding the worker's initial wait UNDER the queue lock.
//
// This single locked decision is what closes the reserve-before-classify race
// (Phase 7 review 7A-H2): two concurrent initial sends for the same idle session
// can no longer both observe "idle" and both classify+run. The first to acquire
// the lock creates the reservation and starts exactly one FIFO worker; the
// second necessarily sees the reservation and joins it as a later batch.
//
// initialWaitFn is invoked ONLY when a new reservation is created, while the
// queue lock is held. It returns the channel the worker must wait on before its
// first batch (e.g. an already-active run's Done when the session is busy with a
// run started outside this queue) or nil to start immediately. run re-enters the
// dispatch path with serialized=true so each dequeued batch is classified once
// against the latest history.
func (q *chatRunQueue) Submit(
	key string,
	items []chatSendRequest,
	initialWaitFn func() <-chan struct{},
	run func(cancelCtx context.Context, batch []chatSendRequest) <-chan struct{},
) submitResult {
	q.mu.Lock()
	if q.shutdown {
		q.mu.Unlock()
		return submitShutdown
	}
	if state, exists := q.queues[key]; exists {
		if len(state.batches) >= chatRunQueueMaxDepth {
			q.mu.Unlock()
			return submitRejected
		}
		state.batches = append(state.batches, items)
		q.mu.Unlock()
		return submitJoined
	}
	if run == nil {
		q.mu.Unlock()
		return submitProbeMiss
	}
	var initialWait <-chan struct{}
	if initialWaitFn != nil {
		initialWait = initialWaitFn()
	}
	state := &chatRunQueueState{batches: [][]chatSendRequest{items}, run: run}
	q.queues[key] = state
	q.mu.Unlock()

	go q.run(key, state, initialWait)
	// A non-nil initialWait means the worker will block on an already-active
	// external run (inbound/finalize/recovery) before this turn can classify/run,
	// so the turn is queued-behind-external even though its reservation is brand
	// new (Phase 7 closure item 3). Report submitStartedWaiting so chat.go acks it
	// {queued:true} immediately, matching submitJoined. An idle start (nil wait)
	// keeps the deferred-RPC submitStarted behavior.
	if initialWait != nil {
		return submitStartedWaiting
	}
	return submitStarted
}

// HasReservation reports whether a session currently holds a queue reservation —
// i.e. a batch is queued or being classified before its run registers with the
// router. Status/cancel use this so they observe queue transition states the
// router alone cannot see (Phase 7 review 7A-H4).
func (q *chatRunQueue) HasReservation(key string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := q.queues[key]
	return ok
}

// Cancel drains every not-yet-started batch for one session reservation,
// resolving each request with a cancelled response, and cancels the batch the
// worker has already popped and is classifying/running before its run registers.
// Returns the number of not-yet-started batches drained here.
//
// Two windows are covered. Not-yet-started batches (still in state.batches) are
// drained and answered with a cancelled response directly — the worker never
// touches them (Phase 7 review 7A-H5). The in-flight popped batch is cancelled
// through its armed handle (mandatory fix #5): between pop and RegisterRun it is
// invisible to both this queue's batch list and the router, so only the handle
// can stop it. Cancelling the handle makes the dispatch path resolve that batch's
// request IDs itself — either the pre-register cancellation check or the run
// goroutine's runCtx.Err() branch sends exactly one cancelled response — so we
// deliberately do NOT send a response for it here (that would double-respond).
// The router abort the caller issues alongside Cancel targets the same run once
// it has registered; context cancellation and AbortRun are both idempotent, so
// the overlap still yields exactly one terminal response.
func (q *chatRunQueue) Cancel(key string) int {
	q.mu.Lock()
	state, exists := q.queues[key]
	if !exists {
		q.mu.Unlock()
		return 0
	}
	drained := state.batches
	state.batches = nil
	cancelCurrent := state.cancelCurrent
	q.mu.Unlock()

	if cancelCurrent != nil {
		cancelCurrent()
	}
	for _, batch := range drained {
		sendChatCancelled(batch)
		q.emitTerminal(batch, protocol.ChatTurnCancelled)
	}
	return len(drained)
}

// emitTerminal invokes the queue's one-shot terminal chat.turn lifecycle callback
// for a batch the queue resolves itself (drain/cancel/shutdown/panic). It is a
// no-op when onTerminal is unwired (unit queues without an event bus). The latch
// that guarantees exactly-one terminal per turn lives in the batch's shared
// turnLifecycle, so the callback — not this method — is responsible for claiming
// it; emitTerminal only forwards. Kept separate from the RPC resolution helpers
// (sendChatCancelled/sendChatError) so the RPC latch and the lifecycle latch stay
// independent (Decision 4 point 8).
func (q *chatRunQueue) emitTerminal(batch []chatSendRequest, state string) {
	if q.onTerminal == nil {
		return
	}
	q.onTerminal(batch, state)
}

// Shutdown blocks new Submits and drains every still-queued batch with a
// terminal error so no pending chat.send hangs at process shutdown (Phase 7
// review 7A-M2, Decision 6). It also cancels each reservation's currently
// classifying batch — the one popped by the worker but not yet registered with
// the router (Decision 6 point 5): cancelling its armed handle makes the
// dispatch path resolve that batch cancelled at its pre/post-classify recheck
// instead of registering a run into a shutting-down gateway. A batch whose run
// has already registered is left to finish/abort against the router (the
// lifecycle goroutine drains active runs through the router's bounded abort),
// not here.
func (q *chatRunQueue) Shutdown() {
	q.mu.Lock()
	q.shutdown = true
	var pending [][]chatSendRequest
	var cancels []context.CancelFunc
	for _, state := range q.queues {
		pending = append(pending, state.batches...)
		state.batches = nil
		if state.cancelCurrent != nil {
			cancels = append(cancels, state.cancelCurrent)
		}
	}
	q.mu.Unlock()

	// Cancel currently classifying batches first so their pre/post-classify
	// recheck resolves them cancelled rather than proceeding to RegisterRun.
	for _, cancel := range cancels {
		cancel()
	}
	for _, batch := range pending {
		locale := ""
		if len(batch) > 0 {
			locale = store.LocaleFromContext(batch[0].ctx)
		}
		sendChatError(batch, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "gateway shutting down"))
		q.emitTerminal(batch, protocol.ChatTurnFailed)
	}
}

func (q *chatRunQueue) run(key string, state *chatRunQueueState, initialWait <-chan struct{}) {
	if initialWait != nil {
		<-initialWait
	}
	for {
		q.mu.Lock()
		current, exists := q.queues[key]
		if !exists || current != state || len(state.batches) == 0 {
			if exists && current == state {
				delete(q.queues, key)
			}
			q.mu.Unlock()
			return
		}
		batch := state.batches[0]
		state.batches = state.batches[1:]
		// Arm a cancellation handle for this popped batch while still under the
		// queue lock, BEFORE releasing it (Phase 7 review mandatory fix #5). Between
		// pop and RegisterRun the batch is invisible to both the reservation queue
		// (it left state.batches) and the router (no run yet); without this handle a
		// cancel arriving in that window could neither drain nor abort it, and the
		// batch would still register and run after the user cancelled. Cancel closes
		// over this ctx so a cancel in the classify window stops the run before it
		// registers; the post-lane/post-hook rechecks and the run-goroutine's own
		// runCtx (derived from this ctx downstream) turn the cancellation into a
		// terminal cancelled response.
		batchCtx, cancelBatch := context.WithCancel(context.Background())
		state.cancelCurrent = cancelBatch
		q.mu.Unlock()

		q.runBatch(state, batchCtx, batch)

		// Disarm: the batch has completed (or its run registered and finished). Clear
		// the handle so a later Cancel does not cancel an already-finished batch.
		q.mu.Lock()
		if state.cancelCurrent != nil {
			state.cancelCurrent = nil
		}
		q.mu.Unlock()
		cancelBatch()
	}
}

// runBatch executes one dequeued batch and waits for its run to complete.
//
// The synchronous classification/setup that state.run performs before the run
// goroutine is registered can panic (nil provider, malformed classifier
// response, etc.). Without recovery that panic would kill the FIFO worker,
// leaving the reservation in the map forever and stranding every request in the
// batch — and every later send that joins the dead reservation — with no
// response (Phase 7 review 7A-C1). We recover, resolve the batch's requests with
// a terminal error, and let the worker proceed to the next batch. The panic
// happens before RegisterRun, so no router run is leaked.
func (q *chatRunQueue) runBatch(state *chatRunQueueState, batchCtx context.Context, batch []chatSendRequest) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("chat run queue: batch runner panicked", "panic", fmt.Sprint(r), "batch_size", len(batch))
			locale := ""
			if len(batch) > 0 {
				locale = store.LocaleFromContext(batch[0].ctx)
			}
			sendChatError(batch, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "chat run failed"))
			q.emitTerminal(batch, protocol.ChatTurnFailed)
		}
	}()
	if done := state.run(batchCtx, batch); done != nil {
		<-done
	}
}

func newChatDebouncer(flushFn func([]chatSendRequest)) *chatDebouncer {
	return &chatDebouncer{
		buffers: make(map[string]*chatDebounceBuffer),
		flushFn: flushFn,
	}
}

// Push appends an item to the per-key buffer.
//
// Behavior:
//   - delay > 0: append + (re)set the silence timer (existing buffered path).
//   - delay <= 0 AND a buffer already exists with items: append the incoming
//     item to the buffer and flush immediately (merge-then-flush). Required so
//     a no-media follow-up cannot bypass a buffered media chat-send and trigger
//     a duplicate dispatch (Phase 1.5 Rule #4, mirrors bus debouncer Rule #1).
//   - delay <= 0 AND no buffer exists: dispatch immediately (passthrough).
//
// Push returns false iff the debouncer has been closed by CloseAndDrain (Phase 7
// closure item 4): the item was neither buffered nor flushed and the caller must
// settle it with a shutdown error. It returns true when the item was accepted
// into a buffer or synchronously flushed. The closed check is taken under mu in
// the SAME critical section that mutates the buffer, so admission is atomic with
// respect to shutdown drain — a request cannot slip past a completed
// CloseAndDrain and create a new buffer or dispatch.
func (d *chatDebouncer) Push(key string, delay time.Duration, item chatSendRequest) bool {
	if delay <= 0 {
		d.mu.Lock()
		if d.closed {
			d.mu.Unlock()
			return false
		}
		buf, exists := d.buffers[key]
		if exists && len(buf.items) > 0 {
			buf.items = append(buf.items, item)
			if buf.timer != nil {
				buf.timer.Stop()
			}
			items := buf.items
			delete(d.buffers, key)
			// Register the sync-flush on the dispatching WaitGroup UNDER the lock (as
			// takeForFlush does for the timer path) so CloseAndDrain's Wait blocks until
			// this flushFn returns — a merge-then-flush that starts just before close
			// cannot push a batch into the FIFO queue after the shutdown drain moved past
			// the debouncer (Phase 7 closure item 4).
			d.dispatching.Add(1)
			d.mu.Unlock()
			defer d.dispatching.Done()
			d.flushFn(items)
			return true
		}
		d.dispatching.Add(1)
		d.mu.Unlock()
		defer d.dispatching.Done()
		d.flushFn([]chatSendRequest{item})
		return true
	}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return false
	}
	buf, exists := d.buffers[key]
	if !exists {
		buf = &chatDebounceBuffer{}
		d.buffers[key] = buf
	}
	buf.items = append(buf.items, item)
	if buf.timer != nil {
		buf.timer.Stop()
	}
	buf.timer = time.AfterFunc(delay, func() {
		d.Flush(key)
	})
	d.mu.Unlock()
	return true
}

func (d *chatDebouncer) Flush(key string) {
	items, registered := d.takeForFlush(key)
	if len(items) == 0 {
		return
	}
	// registered == true means takeForFlush added this dispatch to the dispatching
	// WaitGroup UNDER d.mu (before releasing the lock), so CloseAndDrain blocks in
	// Wait() until this flushFn returns. Balance it here (Phase 7 closure item 4).
	if registered {
		defer d.dispatching.Done()
	}
	d.flushFn(items)
}

// takeForFlush removes key's buffer and, when the batch is non-empty, registers
// the impending flushFn on the dispatching WaitGroup in the SAME critical section
// that checks closed and deletes the buffer. This makes the take→flushFn hand-off
// atomic with respect to CloseAndDrain (Phase 7 closure item 4): once close is
// observed the batch is never handed out, and a batch handed out immediately
// before close keeps CloseAndDrain's Wait blocked until flushFn returns — so no
// debounce dispatch can push a batch into the FIFO queue after the shutdown drain
// has moved past the debouncer. Returns (items, registered); registered is true
// iff the caller must call d.dispatching.Done() after flushFn. Distinct from Take,
// which is used by the cancel path that resolves items itself and never flushes.
func (d *chatDebouncer) takeForFlush(key string) ([]chatSendRequest, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, false
	}
	buf, ok := d.buffers[key]
	if !ok || len(buf.items) == 0 {
		return nil, false
	}
	if buf.timer != nil {
		buf.timer.Stop()
	}
	items := buf.items
	delete(d.buffers, key)
	d.dispatching.Add(1)
	return items, true
}

func (d *chatDebouncer) Take(key string) []chatSendRequest {
	d.mu.Lock()
	// A timer callback (Flush → Take) may fire concurrently with CloseAndDrain.
	// CloseAndDrain stops timers and removes every buffer under this same lock, so
	// a post-close Take finds no buffer and returns nil; the explicit closed guard
	// makes that intent unmistakable and covers a timer that had already entered
	// AfterFunc before the stop. Either way, nothing is dispatched after close
	// (Phase 7 closure item 4).
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	buf, ok := d.buffers[key]
	if !ok || len(buf.items) == 0 {
		d.mu.Unlock()
		return nil
	}
	if buf.timer != nil {
		buf.timer.Stop()
	}
	items := buf.items
	delete(d.buffers, key)
	d.mu.Unlock()

	return items
}

// CloseAndDrain atomically closes the debouncer and drains it in one critical
// section (Phase 7 closure item 4): it sets closed=true, stops every buffer's
// timer, removes all buffers, and returns the buffered items grouped by buffer
// (one slice per debounce window) in no particular order. Used by graceful
// shutdown (Phase 7 Decision 6): the caller resolves each still-buffered
// request — which never left the debounce window, so it has neither a queued
// ack nor a started run — with a terminal shutdown response instead of flushing
// it into a queue that is already shutting down. Unlike Stop(), it does NOT
// invoke flushFn, so nothing is dispatched.
//
// Setting closed and removing the buffers in the SAME locked section is what
// closes the shutdown-admission race: a Push that already passed handleSend's
// early fail-fast latch but had not yet acquired d.mu observes closed==true (or
// finds its buffer gone) and returns false, so it can neither re-buffer nor
// synchronously flush into the just-drained debouncer. A timer callback that
// races close (Flush → Take) finds closed and dispatches nothing.
//
// After closing under the lock, CloseAndDrain Waits (outside the lock) on the
// dispatching WaitGroup so it also blocks until every flushFn that had ALREADY
// left the buffer map but was still running outside d.mu returns. This closes
// the take→flushFn window (Phase 7 closure item 4): a batch handed to flushFn
// microseconds before close could otherwise reach the FIFO queue after this
// drain returned and Shutdown moved on, hanging with no response. Because closed
// is set under the lock and every dispatch registers on the WaitGroup under the
// lock only after observing !closed, no new dispatch can start once close is
// observed — so the counter is frozen at unlock and can only decrement, making a
// post-unlock Wait correct and free of any lock held across it.
func (d *chatDebouncer) CloseAndDrain() [][]chatSendRequest {
	d.mu.Lock()
	d.closed = true
	var out [][]chatSendRequest
	for key, buf := range d.buffers {
		if buf.timer != nil {
			buf.timer.Stop()
		}
		if len(buf.items) > 0 {
			out = append(out, buf.items)
		}
		delete(d.buffers, key)
	}
	d.mu.Unlock()

	// Block until in-flight dispatches (take→flushFn / Push sync-flush that
	// escaped the lock before close) have finished handing their batch to the
	// queue, so the caller's shutdown drain sees a fully quiesced dispatch path.
	d.dispatching.Wait()
	return out
}

func (d *chatDebouncer) Discard(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	buf, ok := d.buffers[key]
	if !ok {
		return
	}
	if buf.timer != nil {
		buf.timer.Stop()
	}
	delete(d.buffers, key)
}

func (d *chatDebouncer) Stop() {
	d.mu.Lock()
	keys := make([]string, 0, len(d.buffers))
	for key := range d.buffers {
		keys = append(keys, key)
	}
	d.mu.Unlock()

	for _, key := range keys {
		d.Flush(key)
	}
}

// mergeChatSendRequests coalesces one debounce/queue batch into a single turn.
//
// Batch-is-one-turn semantics (Phase 7 review 7A-H3/M1): a debounce window may
// hold several sends (text and/or media). They are classified and run exactly
// once as one turn, so ALL of their media must survive the merge, in arrival
// order, not just the last send's. The pre-fix code took the last item's params
// verbatim and joined only text — silently dropping media attached to earlier
// sends (media-to-text, media-to-media, multi-attachment bursts). Metadata other
// than message/media follows the latest send (stream flag, agent/session, which
// are batch-invariant in practice since the debounce key is per user+session).
func mergeChatSendRequests(items []chatSendRequest) chatSendParams {
	if len(items) == 0 {
		return chatSendParams{}
	}
	merged := items[len(items)-1].params
	parts := make([]string, 0, len(items))
	var mediaItems []chatMediaItem
	for _, item := range items {
		if item.params.Message != "" {
			parts = append(parts, item.params.Message)
		}
		mediaItems = append(mediaItems, item.params.parseMedia()...)
	}
	merged.Message = strings.Join(parts, "\n")
	// Re-encode the concatenated media as the canonical {path,filename} form so a
	// single downstream parseMedia() sees every attachment from the batch. When
	// the batch carried no media at all, leave Media nil (parseMedia stays a
	// no-op) rather than emitting an empty JSON array.
	if len(mediaItems) > 0 {
		if encoded, err := json.Marshal(mediaItems); err == nil {
			merged.Media = json.RawMessage(encoded)
		} else {
			// Marshaling []chatMediaItem cannot realistically fail; keep the latest
			// send's media rather than dropping everything if it somehow does.
			slog.Warn("chat merge: failed to re-encode batch media; keeping latest send only", "error", err)
		}
	}
	return merged
}

// chatDebounceDelay computes the per-send debounce window.
//
// Precedence: agent override (when set) overrides the global config. The media
// floor fires ONLY when the post-override delay is exactly 0 AND the message
// carries media — a non-zero agent override (even below the floor) is honored
// verbatim. Mirrors Phase 1's resolveInboundDebounceDelay + applyMediaFloor.
func chatDebounceDelay(cfg *config.Config, agentOtherConfig json.RawMessage, hasMedia bool) time.Duration {
	debounceMs := 0
	if cfg != nil {
		debounceMs = cfg.Gateway.InboundDebounceMs
	}
	if overrideMs, ok := store.ParseInboundDebounceMsFromOtherConfig(agentOtherConfig); ok {
		debounceMs = overrideMs
	}
	if debounceMs <= 0 && hasMedia {
		debounceMs = chatMediaDebounceFloorMs
	}
	if debounceMs <= 0 {
		return 0
	}
	return time.Duration(debounceMs) * time.Millisecond
}

func chatDebounceKey(userID, sessionKey string) string {
	return userID + ":" + sessionKey
}
