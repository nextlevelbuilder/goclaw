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

// TokenEstimateFunc returns token estimate and context window for a session.
// Used by adaptive throttle to reduce concurrency near the summary threshold.
type TokenEstimateFunc func(sessionKey string) (tokens int, contextWindow int)

// PendingRequest is a queued agent run awaiting execution.
type PendingRequest struct {
	Req        agent.RunRequest
	ResultCh   chan RunOutcome
	EnqueuedAt time.Time // timestamp when enqueued, used for stale message detection
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
	key      string
	config   QueueConfig
	runFn    RunFunc
	laneMgr *LaneManager
	lane     string

	mu              sync.Mutex
	queue           []*PendingRequest
	activeRuns      map[string]activeRunEntry // runID → entry (with generation)
	activeOrder     []string                  // FIFO order of active runIDs
	maxConcurrent   int                       // effective limit (from config or per-session override)
	timer           *time.Timer               // debounce timer
	parentCtx       context.Context           // stored from first Enqueue call
	abortCutoffTime time.Time                 // messages enqueued before this are stale
	generation      uint64                    // bumped on Reset() to ignore stale completions

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
func (sq *SessionQueue) Enqueue(ctx context.Context, req agent.RunRequest) <-chan RunOutcome {
	outcome := make(chan RunOutcome, 1)
	pending := &PendingRequest{Req: req, ResultCh: outcome, EnqueuedAt: time.Now()}

	sq.mu.Lock()
	defer sq.mu.Unlock()

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
			sq.scheduleNext(ctx)
		}

	default: // queue, followup
		if len(sq.queue) >= sq.config.Cap {
			sq.applyDropPolicy(pending)
		} else {
			sq.queue = append(sq.queue, pending)
		}

		if sq.hasCapacity() {
			sq.scheduleNext(ctx)
		}
	}

	return outcome
}

// scheduleNext starts the next queued request(s), applying debounce.
// Must be called with sq.mu held.
func (sq *SessionQueue) scheduleNext(ctx context.Context) {
	if len(sq.queue) == 0 {
		return
	}

	debounce := time.Duration(sq.config.DebounceMs) * time.Millisecond
	if debounce <= 0 {
		sq.startAvailable(ctx)
		return
	}

	// Reset debounce timer: collapses rapid messages
	if sq.timer != nil {
		sq.timer.Stop()
	}
	sq.timer = time.AfterFunc(debounce, func() {
		sq.mu.Lock()
		defer sq.mu.Unlock()
		if sq.hasCapacity() && len(sq.queue) > 0 {
			sq.startAvailable(ctx)
		}
	})
}

// startAvailable starts as many queued requests as capacity allows.
// Must be called with sq.mu held.
func (sq *SessionQueue) startAvailable(ctx context.Context) {
	for sq.hasCapacity() && len(sq.queue) > 0 {
		sq.startOne(ctx)
	}
}

// startOne picks the first queued request and runs it in the lane.
// Skips stale messages that were enqueued before the last abort cutoff.
// Must be called with sq.mu held.
func (sq *SessionQueue) startOne(ctx context.Context) {
	// Skip stale messages enqueued before the last /stopall abort cutoff.
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

	if len(sq.queue) == 0 {
		return
	}

	pending := sq.queue[0]
	sq.queue = sq.queue[1:]

	runID := pending.Req.RunID
	runCtx, cancel := context.WithCancel(ctx)
	sq.activeRuns[runID] = activeRunEntry{cancel: cancel, generation: sq.generation}
	sq.activeOrder = append(sq.activeOrder, runID)

	lane := sq.laneMgr.Get(sq.lane)
	if lane == nil {
		lane = sq.laneMgr.Get(LaneMain)
	}

	gen := sq.generation // capture generation under lock

	if lane == nil {
		// No lane available — run directly
		go sq.executeRun(runCtx, runID, gen, pending)
		return
	}

	err := lane.Submit(ctx, func() {
		sq.executeRun(runCtx, runID, gen, pending)
	})
	if err != nil {
		pending.ResultCh <- RunOutcome{Err: err}
		close(pending.ResultCh)
		// caller already holds sq.mu — clean up
		delete(sq.activeRuns, runID)
		sq.removeFromOrder(runID)
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
				sq.scheduleNext(sq.parentCtx)
			}
			sq.mu.Unlock()
		}
	}()

	result, err := sq.runFn(ctx, pending.Req)
	pending.ResultCh <- RunOutcome{Result: result, Err: err}
	close(pending.ResultCh)

	sq.mu.Lock()
	// Check generation: ignore stale completions from a previous generation.
	if entry, ok := sq.activeRuns[runID]; ok && entry.generation == sq.generation {
		delete(sq.activeRuns, runID)
		sq.removeFromOrder(runID)
	} else if runGeneration != sq.generation {
		// Stale completion from old generation — skip cleanup.
		sq.mu.Unlock()
		return
	}

	if sq.hasCapacity() && len(sq.queue) > 0 {
		// Use parentCtx (not the per-run ctx which may be cancelled)
		sq.scheduleNext(sq.parentCtx)
	}
	sq.mu.Unlock()
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
