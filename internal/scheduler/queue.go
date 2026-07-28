package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

// QueueMode determines how incoming messages are handled when an agent
// is already processing a message for the same session.
type QueueMode string

const (
	// QueueModeQueue is simple FIFO: new messages wait until current finishes.
	QueueModeQueue QueueMode = "queue"

	// QueueModeFollowup queues as a follow-up after the current run completes.
	QueueModeFollowup QueueMode = "followup"

	// QueueModeInterrupt cancels the current run and starts the new message.
	QueueModeInterrupt QueueMode = "interrupt"
)

// DropPolicy determines which messages to drop when the queue is full.
type DropPolicy string

const (
	DropOld DropPolicy = "old" // drop oldest message
	DropNew DropPolicy = "new" // reject incoming message
)

// QueueConfig configures per-session message queuing.
type QueueConfig struct {
	Mode          QueueMode  `json:"mode"`
	Cap           int        `json:"cap"`
	Drop          DropPolicy `json:"drop"`
	DebounceMs    int        `json:"debounce_ms"`
	MaxConcurrent int        `json:"max_concurrent"` // 0 or 1 = serial (default)
}

// DefaultQueueConfig returns sensible defaults.
func DefaultQueueConfig() QueueConfig {
	return QueueConfig{
		Mode:          QueueModeQueue,
		Cap:           10,
		Drop:          DropOld,
		DebounceMs:    800,
		MaxConcurrent: 1,
	}
}

// RunFunc is the callback that executes an agent run.
// The scheduler calls this when it's the request's turn.
type RunFunc func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error)

// PreExecuteHook is a scheduler-owned, per-request hook invoked at dequeue for
// the single request it was scheduled with. It runs AFTER lane acquisition and
// the post-lane cancellation recheck, and immediately BEFORE runFn — so for a
// serial session it observes the state the preceding run left behind (e.g. the
// latest session history). It may mutate the request in place and returns the
// context runFn executes under.
//
// The hook carries a result/error contract: a non-nil error ABORTS the run —
// runFn is not called and the error is delivered as the run's outcome. A nil
// returned context means "run under the context passed in" (the hook chose not
// to augment it). This is a scheduler concept, deliberately NOT a field on
// agent.RunRequest: only the scheduler invokes it, and only at dequeue.
//
// Contract on the returned context (Phase 7 Decision 2): it MUST derive from the
// input ctx — via context.WithValue/WithDeadline/WithCancel on ctx — and may only
// augment values or tighten the deadline. It MUST NOT detach from the scheduler's
// cancellation ownership by returning an unrelated or background context. The
// scheduler does not trust the hook to honour this: immediately before runFn it
// rechecks BOTH the input ctx and the returned context, so a hook that returns a
// detached live context can never revive a turn the scheduler already cancelled.
type PreExecuteHook func(ctx context.Context, req *agent.RunRequest) (context.Context, error)

// TokenEstimateFunc returns token estimate and context window for a session.
// Used by adaptive throttle to reduce concurrency near the summary threshold.
type TokenEstimateFunc func(sessionKey string) (tokens int, contextWindow int)

// PendingRequest is a queued agent run awaiting execution.
type PendingRequest struct {
	Req        agent.RunRequest
	ResultCh   chan RunOutcome
	EnqueuedAt time.Time // timestamp when enqueued, used for stale message detection

	// ctx is THIS request's own enqueue context. Each queued request keeps its
	// own context (user guidance #2) rather than sharing the session's
	// first-enqueue parentCtx: a busy follow-up's per-turn values (tenant scope,
	// cancellation) must bind to the turn that produced them, not to whichever
	// request happened to create the session queue. Cancellation of a preceding
	// run's context must not cancel a later queued run, so the pump derives each
	// run's lane-submit ctx from THIS field (falling back to parentCtx only when a
	// request was constructed without one); this per-request ctx is the base the
	// run itself executes under.
	ctx context.Context

	// preExec is the scheduler-owned pre-execution hook for this request, invoked
	// at dequeue after lane acquisition and the cancellation recheck (user
	// guidance #3/#4). Nil for protected internal runs.
	preExec PreExecuteHook

	// seq is an immutable, monotonically increasing admission-order identity
	// assigned under sq.mu at Enqueue (Phase 7 Decision 1). The single admission
	// pump pops and submits requests in ascending seq order, so seq is the stable
	// FIFO position a request keeps from enqueue through dequeue and lane
	// admission — the order identity the decision requires a request to retain
	// even while it waits for a lane token.
	seq uint64
}

// RunOutcome is the result of a scheduled agent run.
type RunOutcome struct {
	Result *agent.RunResult
	Err    error
}

// activeRunEntry tracks a running agent execution with its generation.
type activeRunEntry struct {
	cancel     context.CancelFunc
	generation uint64
}

// SessionQueue manages agent runs for a single session key.
// Supports configurable concurrency: 1 (serial) or N (concurrent).
type SessionQueue struct {
	key     string
	config  QueueConfig
	runFn   RunFunc
	laneMgr *LaneManager
	lane    string

	mu              sync.Mutex
	queue           []*PendingRequest
	activeRuns      map[string]activeRunEntry // runID → entry (with generation)
	activeOrder     []string                  // FIFO order of active runIDs
	maxConcurrent   int                       // effective limit (from config or per-session override)
	timer           *time.Timer               // debounce timer
	parentCtx       context.Context           // stored from first Enqueue call
	abortCutoffTime time.Time                 // messages enqueued before this are stale
	generation      uint64                    // bumped on Reset() to ignore stale completions

	// pumpRunning guards the single ordered admission goroutine (Phase 7
	// Decision 1). At most one pump runs at a time; it is started by scheduleNext
	// when there is schedulable work and clears the flag when it exits (no
	// capacity or empty queue). Enqueue (new work) and finishRun (freed capacity)
	// restart it through scheduleNext. Guarded by sq.mu.
	pumpRunning bool
	// seqCounter assigns each enqueued request an immutable admission-order seq
	// (Phase 7 Decision 1). Monotonic under sq.mu.
	seqCounter uint64

	tokenEstimateFn TokenEstimateFunc // optional: for adaptive throttle
}

// NewSessionQueue creates a queue for a specific session.
func NewSessionQueue(key, lane string, cfg QueueConfig, laneMgr *LaneManager, runFn RunFunc) *SessionQueue {
	maxC := cfg.MaxConcurrent
	if maxC <= 0 {
		maxC = 1
	}
	return &SessionQueue{
		key:           key,
		config:        cfg,
		runFn:         runFn,
		laneMgr:       laneMgr,
		lane:          lane,
		activeRuns:    make(map[string]activeRunEntry),
		maxConcurrent: maxC,
	}
}

// SetMaxConcurrent overrides the per-session max concurrent runs.
// Typically called from the consumer when it knows the peer kind (group vs DM).
func (sq *SessionQueue) SetMaxConcurrent(n int) {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	if n <= 0 {
		n = 1
	}
	sq.maxConcurrent = n
}

// effectiveMaxConcurrent returns the current concurrency limit,
// reduced to 1 when near the summary threshold (adaptive throttle).
// Must be called with sq.mu held.
func (sq *SessionQueue) effectiveMaxConcurrent() int {
	max := sq.maxConcurrent
	if max <= 0 {
		max = 1
	}
	if sq.tokenEstimateFn == nil {
		return max
	}
	tokens, contextWindow := sq.tokenEstimateFn(sq.key)
	if contextWindow > 0 && float64(tokens)/float64(contextWindow) >= 0.6 {
		return 1 // near summary threshold → serialize
	}
	return max
}

// hasCapacity returns whether a new run can start.
// Must be called with sq.mu held.
func (sq *SessionQueue) hasCapacity() bool {
	return len(sq.activeRuns) < sq.effectiveMaxConcurrent()
}

// Enqueue adds a request to the session queue.
// If capacity is available, it starts immediately (after debounce).
// Returns a channel that receives the result when the run completes.
//
// preExec is the scheduler-owned pre-execution hook for THIS request (nil for
// protected internal runs). It fires at dequeue, after lane acquisition and the
// cancellation recheck, immediately before runFn.
func (sq *SessionQueue) Enqueue(ctx context.Context, req agent.RunRequest, preExec PreExecuteHook) <-chan RunOutcome {
	outcome := make(chan RunOutcome, 1)
	pending := &PendingRequest{Req: req, ResultCh: outcome, EnqueuedAt: time.Now(), ctx: ctx, preExec: preExec}

	sq.mu.Lock()
	defer sq.mu.Unlock()

	// Assign the immutable admission-order identity under the lock (Phase 7
	// Decision 1). The single pump admits in ascending seq, so this is the FIFO
	// position the request keeps from here through lane admission.
	pending.seq = sq.seqCounter
	sq.seqCounter++

	// Store parent context for spawning future runs
	if sq.parentCtx == nil {
		sq.parentCtx = ctx
	}

	switch sq.config.Mode {
	case QueueModeInterrupt:
		// Cancel all active runs
		for runID, entry := range sq.activeRuns {
			entry.cancel()
			delete(sq.activeRuns, runID)
		}
		sq.activeOrder = nil
		// Clear existing queue and enqueue this one
		sq.drainQueue(RunOutcome{Err: context.Canceled})
		sq.queue = append(sq.queue, pending)
		if sq.hasCapacity() {
			sq.scheduleNext()
		}

	default: // queue, followup
		if len(sq.queue) >= sq.config.Cap {
			sq.applyDropPolicy(pending)
		} else {
			sq.queue = append(sq.queue, pending)
		}

		if sq.hasCapacity() {
			sq.scheduleNext()
		}
	}

	return outcome
}

// scheduleNext ensures the ordered admission pump is running (or is armed to run
// after the debounce window) whenever there is capacity and queued work. Must be
// called with sq.mu held.
//
// It no longer starts runs directly: the single pump goroutine is the ONLY code
// that pops the queue and submits to the lane (Phase 7 Decision 1). scheduleNext
// just (re)arms it. Debounce still collapses rapid enqueues by delaying the pump
// start; with debounce disabled the pump starts synchronously.
func (sq *SessionQueue) scheduleNext() {
	if len(sq.queue) == 0 {
		return
	}

	debounce := time.Duration(sq.config.DebounceMs) * time.Millisecond
	if debounce <= 0 {
		sq.startPump()
		return
	}

	// Reset debounce timer: collapses rapid messages
	if sq.timer != nil {
		sq.timer.Stop()
	}
	sq.timer = time.AfterFunc(debounce, func() {
		sq.mu.Lock()
		defer sq.mu.Unlock()
		sq.startPump()
	})
}

// startPump starts the single ordered admission goroutine if one is not already
// running and there is schedulable work. Must be called with sq.mu held.
//
// At most one pump runs per session (Phase 7 Decision 1): pumpRunning is the
// single-writer guard. Because the pump is the only code that pops the queue and
// calls lane.Submit, requests are admitted strictly in enqueue/seq order and no
// per-request acquisition goroutine can race for a lane token and invert FIFO.
func (sq *SessionQueue) startPump() {
	if sq.pumpRunning {
		return
	}
	if !sq.hasCapacity() || len(sq.queue) == 0 {
		return
	}
	sq.pumpRunning = true
	go sq.pump()
}

// skipStaleHead drops queued requests enqueued before the last /stopall abort
// cutoff, answering each with ErrMessageStale, until a fresh head remains or the
// queue empties. A request skipped here never reaches the lane, so its
// PreExecuteHook and runFn never run (Phase 7 Decision 1 point 5). Must be called
// with sq.mu held.
func (sq *SessionQueue) skipStaleHead() {
	for len(sq.queue) > 0 {
		head := sq.queue[0]
		if !sq.abortCutoffTime.IsZero() && head.EnqueuedAt.Before(sq.abortCutoffTime) {
			sq.queue = sq.queue[1:]
			head.ResultCh <- RunOutcome{Err: ErrMessageStale}
			close(head.ResultCh)
			slog.Debug("scheduler: skipped stale message",
				"session", sq.key,
				"enqueued", head.EnqueuedAt,
				"cutoff", sq.abortCutoffTime,
			)
			continue
		}
		// Clear cutoff once a non-stale message is found
		sq.abortCutoffTime = time.Time{}
		break
	}
}

// pump is the single ordered admission goroutine (Phase 7 Decision 1). It
// repeatedly pops the head of the queue (in seq order), registers it active under
// the lock, then submits it to the lane OFF the lock and IN ORDER.
//
// lane.Submit blocks only until a worker token is acquired, then launches the run
// in its own goroutine and returns; because the pump calls Submit synchronously
// before popping the next request, admission order is deterministic — request A
// acquires its slot before B is even considered. Executions may still overlap and
// complete out of order when MaxConcurrent>1, but the admission barrier is strict
// FIFO. For a serial session (MaxConcurrent=1) the pump admits one run, sees no
// capacity, and exits; finishRun restarts it once the run completes.
//
// The lock is NEVER held across the blocking lane.Submit (mandatory fix #1), so
// CancelAll/CancelOne/Reset and the stale cutoff can always take sq.mu and cancel
// a request that is waiting for a lane token: they cancel the entry's runCtx, and
// lane.Submit returns that ctx's error without ever running the request — so a
// request cancelled or made stale before it receives the lane runs neither its
// PreExecuteHook nor runFn.
func (sq *SessionQueue) pump() {
	for {
		sq.mu.Lock()
		sq.skipStaleHead()
		if !sq.hasCapacity() || len(sq.queue) == 0 {
			// No schedulable work right now. Clear the guard and exit atomically
			// under the lock: any concurrent Enqueue/finishRun that appended work or
			// freed capacity is serialized behind this lock, so it will observe
			// pumpRunning==false and start a fresh pump — no lost wakeup.
			sq.pumpRunning = false
			sq.mu.Unlock()
			return
		}

		pending := sq.queue[0]
		sq.queue = sq.queue[1:]

		// Each request runs under ITS OWN enqueue context (user guidance #2), not
		// the session's first-enqueue parentCtx. A busy follow-up's per-turn values
		// and cancellation must bind to the turn that produced them. Fall back to
		// parentCtx (then Background) only if a request was constructed without one
		// (defensive; Enqueue always sets it).
		baseCtx := pending.ctx
		if baseCtx == nil {
			baseCtx = sq.parentCtx
		}
		if baseCtx == nil {
			baseCtx = context.Background()
		}

		runID := pending.Req.RunID
		runCtx, cancel := context.WithCancel(baseCtx)
		sq.activeRuns[runID] = activeRunEntry{cancel: cancel, generation: sq.generation}
		sq.activeOrder = append(sq.activeOrder, runID)
		gen := sq.generation // capture generation under lock

		lane := sq.laneMgr.Get(sq.lane)
		if lane == nil {
			lane = sq.laneMgr.Get(LaneMain)
		}
		sq.mu.Unlock()

		if lane == nil {
			// No lane available — run directly. Still ordered: the pump has already
			// committed this run active and only loops back for the next after the
			// goroutine is launched.
			go sq.executeRun(runCtx, runID, gen, pending)
			continue
		}

		// Submit synchronously and IN ORDER, off the lock (mandatory fix #1). Submit
		// waits on runCtx: CancelAll/CancelOne/Reset cancel the entry's runCtx, so a
		// queue-level cancellation aborts a still-pending acquisition. Both exit
		// paths (slot acquired → executeRun, or acquisition failed → here) release
		// bookkeeping through the shared finishRun path, which reschedules so a
		// failed acquisition never wedges the queue.
		if err := lane.Submit(runCtx, func() {
			sq.executeRun(runCtx, runID, gen, pending)
		}); err != nil {
			pending.ResultCh <- RunOutcome{Err: err}
			close(pending.ResultCh)
			sq.finishRun(runID, gen)
		}
	}
}

// executeRun runs the agent and then starts the next queued message(s) if capacity allows.
func (sq *SessionQueue) executeRun(ctx context.Context, runID string, runGeneration uint64, pending *PendingRequest) {
	// Defense-in-depth: if runFn panics despite agent-level recovery,
	// ensure cleanup still runs so the session queue doesn't orphan this run.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("scheduler: executeRun panicked", "run_id", runID, "panic", fmt.Sprint(r))
			pending.ResultCh <- RunOutcome{Err: fmt.Errorf("run panic: %v", r)}
			close(pending.ResultCh)
			sq.mu.Lock()
			delete(sq.activeRuns, runID)
			sq.removeFromOrder(runID)
			if sq.hasCapacity() && len(sq.queue) > 0 {
				sq.scheduleNext()
			}
			sq.mu.Unlock()
		}
	}()

	// Recheck cancellation AFTER lane acquisition (user guidance #4). A request
	// can wait arbitrarily long for a lane slot behind other sessions' work; by
	// the time we get here it may have been cancelled (/stop, /stopall, restart)
	// or its own context expired. Bail before the pre-execute hook so we never run
	// the Team Work classifier (an LLM call + durable audit write) for a turn that
	// is already dead.
	req := pending.Req
	if err := ctx.Err(); err != nil {
		pending.ResultCh <- RunOutcome{Err: err}
		close(pending.ResultCh)
		sq.finishRun(runID, runGeneration)
		return
	}

	// Scheduler-owned pre-execution hook (Phase 7 review 7A-H1, user guidance
	// #3): invoked at the moment the run actually starts — after any preceding
	// run for this serial session has completed and immediately before runFn. The
	// inbound Team Work gate uses this so classification reads the LATEST session
	// history (the history left by the preceding run) rather than the history at
	// enqueue, and so its durable audit is still written before the run begins.
	// Invoked exactly once per dequeued run; a stale message skipped by
	// skipStaleHead in the pump never reaches here, so its gate never runs. The hook may mutate the request
	// in place; a non-nil error ABORTS the run (runFn is not called); a non-nil
	// returned context replaces the execution context.
	execCtx := ctx
	if pending.preExec != nil {
		augmented, hookErr := pending.preExec(ctx, &req)
		if hookErr != nil {
			pending.ResultCh <- RunOutcome{Err: hookErr}
			close(pending.ResultCh)
			sq.finishRun(runID, runGeneration)
			return
		}
		if augmented != nil {
			execCtx = augmented
		}
	}

	// Recheck cancellation AFTER the pre-execute hook and immediately before
	// runFn (Phase 7 Decision 2). The hook can run arbitrarily long — the Team
	// Work classifier is an LLM call plus a durable audit write — and the turn may
	// be cancelled (/stop, /stopall, restart) or its context expire while the hook
	// runs. Without this recheck the scheduler would start the agent for a turn the
	// user already cancelled, making the control path unreliable.
	//
	// Check BOTH contexts: the scheduler-owned input ctx AND the context the hook
	// returned. The hook contract says the returned context must derive from ctx
	// and must not detach cancellation ownership — but the scheduler does not trust
	// it to. A buggy or hostile hook that returns a detached live context (e.g.
	// context.Background()) would leave execCtx.Err()==nil even though the turn was
	// cancelled; checking ctx.Err() too means such a hook can never revive a run the
	// scheduler already cancelled. Resolve the outcome once through finishRun so the
	// next queued request still proceeds.
	if err := ctx.Err(); err != nil {
		pending.ResultCh <- RunOutcome{Err: err}
		close(pending.ResultCh)
		sq.finishRun(runID, runGeneration)
		return
	}
	if err := execCtx.Err(); err != nil {
		pending.ResultCh <- RunOutcome{Err: err}
		close(pending.ResultCh)
		sq.finishRun(runID, runGeneration)
		return
	}

	result, err := sq.runFn(execCtx, req)
	pending.ResultCh <- RunOutcome{Result: result, Err: err}
	close(pending.ResultCh)

	sq.finishRun(runID, runGeneration)
}

// finishRun performs generation-aware cleanup after a run leaves executeRun for
// any reason (normal completion, post-lane cancellation, or a pre-execute hook
// error) and schedules the next queued request if capacity allows. Shared by all
// non-panic exit paths so they agree exactly on generation and scheduling
// semantics; the panic path keeps its own inline cleanup inside the recover.
func (sq *SessionQueue) finishRun(runID string, runGeneration uint64) {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	// Check generation: ignore stale completions from a previous generation.
	if entry, ok := sq.activeRuns[runID]; ok && entry.generation == sq.generation {
		delete(sq.activeRuns, runID)
		sq.removeFromOrder(runID)
	} else if runGeneration != sq.generation {
		// Stale completion from an old generation (Reset bumped it) — the reset
		// already drained/rescheduled, so skip scheduling next here.
		return
	}

	if sq.hasCapacity() && len(sq.queue) > 0 {
		sq.scheduleNext()
	}
}

// removeFromOrder removes a runID from the activeOrder slice.
// Must be called with sq.mu held.
func (sq *SessionQueue) removeFromOrder(runID string) {
	for i, id := range sq.activeOrder {
		if id == runID {
			sq.activeOrder = append(sq.activeOrder[:i], sq.activeOrder[i+1:]...)
			return
		}
	}
}

// applyDropPolicy handles a full queue.
// Must be called with sq.mu held.
func (sq *SessionQueue) applyDropPolicy(incoming *PendingRequest) {
	switch sq.config.Drop {
	case DropOld:
		// Drop the oldest queued message
		if len(sq.queue) > 0 {
			old := sq.queue[0]
			old.ResultCh <- RunOutcome{Err: ErrQueueDropped}
			close(old.ResultCh)
			sq.queue = sq.queue[1:]
		}
		sq.queue = append(sq.queue, incoming)

	case DropNew:
		// Reject the incoming message
		incoming.ResultCh <- RunOutcome{Err: ErrQueueFull}
		close(incoming.ResultCh)

	default:
		// Default to drop old
		if len(sq.queue) > 0 {
			old := sq.queue[0]
			old.ResultCh <- RunOutcome{Err: ErrQueueDropped}
			close(old.ResultCh)
			sq.queue = sq.queue[1:]
		}
		sq.queue = append(sq.queue, incoming)
	}
}

// drainQueue cancels all pending requests with the given outcome.
// Must be called with sq.mu held.
func (sq *SessionQueue) drainQueue(outcome RunOutcome) {
	for _, p := range sq.queue {
		p.ResultCh <- outcome
		close(p.ResultCh)
	}
	sq.queue = nil
}

// CancelOne stops the oldest active run (FIFO).
// Does NOT drain the pending queue or set abort cutoff. Used by /stop command.
// Returns true if an active run was actually cancelled.
func (sq *SessionQueue) CancelOne() bool {
	sq.mu.Lock()
	defer sq.mu.Unlock()

	if len(sq.activeOrder) == 0 {
		return false
	}

	// Cancel the oldest active run
	runID := sq.activeOrder[0]
	if entry, ok := sq.activeRuns[runID]; ok {
		entry.cancel()
		delete(sq.activeRuns, runID)
		sq.activeOrder = sq.activeOrder[1:]
		return true
	}
	return false
}

// CancelAll stops all active runs and drains all pending requests.
// Sets abort cutoff so stale queued messages are skipped on next schedule.
// Used by /stopall command.
// Returns true if any active run was actually cancelled.
func (sq *SessionQueue) CancelAll() bool {
	sq.mu.Lock()
	defer sq.mu.Unlock()

	sq.abortCutoffTime = time.Now() // mark cutoff for stale message skipping

	cancelled := false
	for runID, entry := range sq.activeRuns {
		entry.cancel()
		delete(sq.activeRuns, runID)
		cancelled = true
	}
	sq.activeOrder = nil
	sq.drainQueue(RunOutcome{Err: context.Canceled})
	return cancelled
}

// Cancel is an alias for CancelAll (backward compat with /stop command).
func (sq *SessionQueue) Cancel() bool {
	return sq.CancelAll()
}

// IsActive returns whether any run is currently executing.
func (sq *SessionQueue) IsActive() bool {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	return len(sq.activeRuns) > 0
}

// ActiveCount returns the number of currently executing runs.
func (sq *SessionQueue) ActiveCount() int {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	return len(sq.activeRuns)
}

// QueueLen returns the number of pending messages.
func (sq *SessionQueue) QueueLen() int {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	return len(sq.queue)
}

// Reset bumps the generation counter, cancels all active runs, and drains
// the pending queue. Stale completions from the old generation are ignored.
// Used during in-process restart (e.g. SIGUSR1).
func (sq *SessionQueue) Reset() {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	sq.generation++
	for _, entry := range sq.activeRuns {
		entry.cancel()
	}
	sq.activeRuns = make(map[string]activeRunEntry)
	sq.activeOrder = nil
	sq.drainQueue(RunOutcome{Err: ErrLaneCleared})
}
