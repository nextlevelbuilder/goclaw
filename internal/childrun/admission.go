package childrun

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
)

var (
	ErrChildRunBusy                = errors.New("child_run_busy")
	ErrChildRunClosed              = errors.New("child_run_closed")
	ErrChildRunDrainTimeout        = errors.New("child_run_drain_timeout")
	ErrChildRunInvalidContinuation = errors.New("child_run_invalid_continuation")
	ErrChildRunContinuationDepth   = errors.New("child_run_continuation_depth")
)

const maxChildRunContinuationDepth = 64

type ChildRunState string

const (
	ChildRunNew          ChildRunState = "new"
	ChildRunQueued       ChildRunState = "queued"
	ChildRunRunning      ChildRunState = "running"
	ChildRunWaitingChild ChildRunState = "waiting_child"
	ChildRunCompleted    ChildRunState = "completed"
	ChildRunFailed       ChildRunState = "failed"
	ChildRunCancelled    ChildRunState = "cancelled"
)

// ChildRunConstraints are rechecked atomically whenever a run or suspended
// continuation is granted. Nil root identity means process-limit-only work.
//
// TaskID, Depth, and MaxDepth are optional for top-level runs, but TaskID and
// direct-child lineage are required for synchronous continuation handoff.
type ChildRunConstraints struct {
	TenantID     uuid.UUID
	RootAgentID  uuid.UUID
	RootLimit    int
	TaskID       string
	ParentTaskID string
	ParentFanout int
	Depth        int
	MaxDepth     int
}

type ChildRunStats struct {
	Active  int
	Pending int
	Live    int
	Closed  bool
}

type childRunRoot struct {
	tenant uuid.UUID
	agent  uuid.UUID
}

type childRunParent struct {
	root   childRunRoot
	parent string
}

// ChildRunAdmission owns process-wide child execution admission. It creates
// one dispatcher goroutine and stops it after Close has fully drained.
type ChildRunAdmission struct {
	mu                 sync.Mutex
	processLimit       int
	pendingLimit       int
	nextSequence       uint64
	active             int
	live               int
	pendingIndependent int
	pending            []*childRunRequest
	running            map[*ChildRunTicket]*childRunFrame
	liveTickets        map[*ChildRunTicket]struct{}
	rootActive         map[childRunRoot]int
	parentActive       map[childRunParent]int
	closed             bool
	wake               chan struct{}
	stop               chan struct{}
	dispatcherDone     chan struct{}
	drained            chan struct{}
	drainedOnce        sync.Once
	stopOnce           sync.Once
}

// ChildRunTicket represents one independent admission chain. Nested
// synchronous continuations reuse this chain and its original FIFO sequence.
type ChildRunTicket struct {
	controller *ChildRunAdmission
	ctx        context.Context
	sequence   uint64
	maxDepth   int
	state      ChildRunState
	err        error

	activated       bool
	cancelRequested bool
	finished        bool
	live            bool
	started         chan struct{}
	done            chan struct{}
	doneOnce        sync.Once
	stopCancel      func() bool

	top        *childRunFrame
	current    *childRunFrame
	stack      []*childRunFrame
	requests   map[*childRunRequest]struct{}
	topRequest *childRunRequest
}

// ChildRunLease is controller-issued execution authority. Continue validates
// that the lease is the chain's current owner before it can hand capacity to a
// direct synchronous child.
type ChildRunLease struct {
	controller       *ChildRunAdmission
	ticket           *ChildRunTicket
	frame            *childRunFrame
	continueMu       sync.Mutex
	releaseRequested atomic.Bool
}

// ContinuationParent exposes immutable structural lineage for a direct
// synchronous continuation. Agent-level spawn policy is enforced by the tool
// that owns the child; this lineage only protects admission handoff integrity.
func (l *ChildRunLease) ContinuationParent() (taskID string, depth int, ok bool) {
	if l == nil || l.frame == nil || l.frame.constraints.TaskID == "" {
		return "", 0, false
	}
	return l.frame.constraints.TaskID, l.frame.constraints.Depth, true
}

type childRunFrame struct {
	constraints     ChildRunConstraints
	ctx             context.Context
	cancel          context.CancelFunc
	run             func(context.Context, *ChildRunLease)
	lease           *ChildRunLease
	state           ChildRunState
	cancelRequested bool
	stopCancel      func() bool
}

type childRunRequest struct {
	ticket      *ChildRunTicket
	frame       *childRunFrame
	sequence    uint64
	independent bool
	launch      bool
	activated   bool
	granted     chan struct{}
	cancelled   chan struct{}
	cancelErr   error
	resolved    bool
}

func NewChildRunAdmission(processLimit, pendingLimit int) *ChildRunAdmission {
	if processLimit < 1 {
		processLimit = 1
	}
	if pendingLimit < 0 {
		pendingLimit = 0
	}
	c := &ChildRunAdmission{
		processLimit:   processLimit,
		pendingLimit:   pendingLimit,
		running:        make(map[*ChildRunTicket]*childRunFrame),
		liveTickets:    make(map[*ChildRunTicket]struct{}),
		rootActive:     make(map[childRunRoot]int),
		parentActive:   make(map[childRunParent]int),
		wake:           make(chan struct{}, 1),
		stop:           make(chan struct{}),
		dispatcherDone: make(chan struct{}),
		drained:        make(chan struct{}),
	}
	go c.dispatch()
	return c
}

// Enqueue reserves one bounded independent pending record but leaves it
// dormant. Activate makes the run eligible after its owner installs visible
// queued task state.
func (c *ChildRunAdmission) Enqueue(
	ctx context.Context,
	constraints ChildRunConstraints,
	run func(context.Context, *ChildRunLease),
) (*ChildRunTicket, error) {
	if run == nil {
		return nil, fmt.Errorf("child run callback is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrChildRunClosed
	}
	if c.pendingIndependent >= c.pendingLimit {
		return nil, ErrChildRunBusy
	}
	maxDepth := constraints.MaxDepth
	if maxDepth <= 0 || maxDepth > maxChildRunContinuationDepth {
		maxDepth = maxChildRunContinuationDepth
	}
	if constraints.Depth < 0 || constraints.Depth > maxDepth {
		return nil, ErrChildRunContinuationDepth
	}
	constraints.MaxDepth = maxDepth

	c.nextSequence++
	ticket := &ChildRunTicket{
		controller: c,
		ctx:        ctx,
		sequence:   c.nextSequence,
		maxDepth:   maxDepth,
		state:      ChildRunNew,
		started:    make(chan struct{}),
		done:       make(chan struct{}),
		requests:   make(map[*childRunRequest]struct{}),
	}
	frame := c.newFrameLocked(ticket, ctx, constraints, run)
	request := &childRunRequest{
		ticket:      ticket,
		frame:       frame,
		sequence:    ticket.sequence,
		independent: true,
		launch:      true,
		granted:     make(chan struct{}),
		cancelled:   make(chan struct{}),
	}
	ticket.top = frame
	ticket.stack = []*childRunFrame{frame}
	ticket.topRequest = request
	ticket.requests[request] = struct{}{}
	c.pending = append(c.pending, request)
	c.pendingIndependent++
	ticket.stopCancel = context.AfterFunc(ctx, func() {
		ticket.Cancel()
	})
	return ticket, nil
}

func (t *ChildRunTicket) Activate() error {
	c := t.controller
	c.mu.Lock()
	defer c.mu.Unlock()

	if t.finished || t.cancelRequested {
		if t.err != nil {
			return t.err
		}
		return context.Canceled
	}
	if c.closed {
		c.cancelTicketLocked(t, ErrChildRunClosed)
		return ErrChildRunClosed
	}
	if t.state != ChildRunNew {
		return nil
	}
	t.activated = true
	t.state = ChildRunQueued
	t.top.state = ChildRunQueued
	t.topRequest.activated = true
	c.signalLocked()
	return nil
}

// Cancel decides cancel-versus-grant while holding the controller lock.
// Running work is marked cancelled immediately, but its capacity is retained
// until the callback actually returns.
func (t *ChildRunTicket) Cancel() bool {
	c := t.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.finished || t.cancelRequested {
		return false
	}
	c.cancelTicketLocked(t, context.Canceled)
	return true
}

func (t *ChildRunTicket) State() ChildRunState {
	t.controller.mu.Lock()
	defer t.controller.mu.Unlock()
	return t.state
}

func (t *ChildRunTicket) Err() error {
	t.controller.mu.Lock()
	defer t.controller.mu.Unlock()
	return t.err
}

func (t *ChildRunTicket) Started() <-chan struct{} { return t.started }
func (t *ChildRunTicket) Done() <-chan struct{}    { return t.done }

// Release returns execution capacity exactly once while keeping the ticket's
// lifecycle barrier open until the callback returns. Controller-owned wrappers
// call it only after execution and immutable result capture, before persistence
// and announcement side effects.
func (l *ChildRunLease) Release() {
	if l == nil || l.controller == nil || l.ticket == nil || l.frame == nil {
		return
	}
	c := l.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	if !l.releaseRequested.CompareAndSwap(false, true) {
		return
	}
	if c.running[l.ticket] == l.frame && l.ticket.current == l.frame {
		c.releaseActiveLocked(l.ticket, l.frame)
	}
}

// Continue synchronously transfers this lease's execution capacity to a direct
// child and reacquires the parent's original capacity before returning. The
// child executes on the caller's goroutine; no dispatcher execution goroutine
// is created for synchronous nesting.
func (l *ChildRunLease) Continue(
	ctx context.Context,
	constraints ChildRunConstraints,
	run func(context.Context, *ChildRunLease),
) error {
	if l == nil || l.controller == nil || l.ticket == nil || l.frame == nil {
		return ErrChildRunInvalidContinuation
	}
	if run == nil {
		return fmt.Errorf("child run callback is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// One executing parent owns one transferable continuation. Concurrent
	// synchronous calls serialize here; async children use Enqueue instead.
	l.continueMu.Lock()
	defer l.continueMu.Unlock()

	c := l.controller
	c.mu.Lock()
	constraints, err := c.validateContinuationLocked(l, constraints)
	if err != nil {
		c.mu.Unlock()
		return err
	}

	parent := l.frame
	child := c.newFrameLocked(l.ticket, ctx, constraints, run)
	l.ticket.stack = append(l.ticket.stack, child)
	parent.state = ChildRunWaitingChild
	l.ticket.state = ChildRunWaitingChild
	c.releaseActiveLocked(l.ticket, parent)

	request := c.newContinuationRequestLocked(l.ticket, child)
	sameRoot := sameRootConstraint(parent.constraints, child.constraints)
	if sameRoot && c.eligibleLocked(child.constraints) {
		c.grantLocked(request)
	} else {
		c.pending = append(c.pending, request)
		c.signalLocked()
	}
	c.mu.Unlock()

	if err := waitChildRunRequest(request); err != nil {
		resume := c.finishContinuation(child, err)
		if resume != nil {
			if resumeErr := waitChildRunRequest(resume); resumeErr != nil {
				return resumeErr
			}
		}
		return err
	}

	childErr := invokeChildRun(child)
	if childErr == nil && child.ctx.Err() != nil {
		childErr = child.ctx.Err()
	}
	resume := c.finishContinuation(child, childErr)
	if resume != nil {
		if err := waitChildRunRequest(resume); err != nil {
			return err
		}
	}
	return childErr
}

func (c *ChildRunAdmission) Stats() ChildRunStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ChildRunStats{
		Active:  c.active,
		Pending: len(c.pending),
		Live:    c.live,
		Closed:  c.closed,
	}
}

// Close is idempotent. The first call closes intake and cancels queued and
// active work. A timeout leaves the controller and dispatcher available for a
// later drain retry; the dispatcher stops only after every callback returns.
func (c *ChildRunAdmission) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	c.mu.Lock()
	if !c.closed {
		c.closed = true
		tickets := make(map[*ChildRunTicket]struct{})
		for _, request := range c.pending {
			tickets[request.ticket] = struct{}{}
		}
		for ticket := range c.running {
			tickets[ticket] = struct{}{}
		}
		for ticket := range c.liveTickets {
			tickets[ticket] = struct{}{}
		}
		for ticket := range tickets {
			c.cancelTicketLocked(ticket, ErrChildRunClosed)
		}
		c.maybeDrainedLocked()
		c.signalLocked()
	}
	drained := c.drained
	c.mu.Unlock()

	select {
	case <-drained:
		<-c.dispatcherDone
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%w: %v", ErrChildRunDrainTimeout, ctx.Err())
	}
}

func (c *ChildRunAdmission) dispatch() {
	defer close(c.dispatcherDone)
	for {
		select {
		case <-c.wake:
			c.dispatchAvailable()
		case <-c.stop:
			return
		}
	}
}

func (c *ChildRunAdmission) dispatchAvailable() {
	for {
		c.mu.Lock()
		request := c.oldestEligibleLocked()
		if request == nil {
			c.maybeDrainedLocked()
			c.mu.Unlock()
			return
		}
		launch := request.launch
		ticket := request.ticket
		frame := request.frame
		granted := c.grantLocked(request)
		c.mu.Unlock()

		if launch && granted {
			go c.executeTop(ticket, frame)
		}
	}
}

func (c *ChildRunAdmission) oldestEligibleLocked() *childRunRequest {
	if c.closed || c.active >= c.processLimit {
		return nil
	}
	var oldest *childRunRequest
	for _, request := range c.pending {
		if request.resolved || !request.activated {
			continue
		}
		if request.frame.ctx.Err() != nil || !c.eligibleLocked(request.frame.constraints) {
			continue
		}
		if oldest == nil || request.sequence < oldest.sequence {
			oldest = request
		}
	}
	return oldest
}

func (c *ChildRunAdmission) eligibleLocked(constraints ChildRunConstraints) bool {
	if c.active >= c.processLimit {
		return false
	}
	root := childRunRoot{tenant: constraints.TenantID, agent: constraints.RootAgentID}
	if constraints.RootAgentID != uuid.Nil && constraints.RootLimit > 0 &&
		c.rootActive[root] >= constraints.RootLimit {
		return false
	}
	parent := childRunParent{root: root, parent: constraints.ParentTaskID}
	return constraints.ParentTaskID == "" || constraints.ParentFanout <= 0 ||
		c.parentActive[parent] < constraints.ParentFanout
}

func (c *ChildRunAdmission) grantLocked(request *childRunRequest) bool {
	if request.resolved {
		return false
	}
	if err := request.frame.ctx.Err(); err != nil {
		c.cancelRequestLocked(request, err)
		return false
	}
	c.removePendingLocked(request)
	request.resolved = true
	request.frame.state = ChildRunRunning
	request.ticket.current = request.frame
	c.running[request.ticket] = request.frame
	c.incrementLocked(request.frame.constraints)

	if request.launch {
		request.ticket.state = ChildRunRunning
		request.ticket.live = true
		c.live++
		c.liveTickets[request.ticket] = struct{}{}
		close(request.ticket.started)
	} else if request.frame == request.ticket.top {
		request.ticket.state = ChildRunRunning
	} else {
		request.ticket.state = ChildRunWaitingChild
	}
	close(request.granted)
	return true
}

func (c *ChildRunAdmission) incrementLocked(constraints ChildRunConstraints) {
	c.active++
	root := childRunRoot{tenant: constraints.TenantID, agent: constraints.RootAgentID}
	if constraints.RootAgentID != uuid.Nil {
		c.rootActive[root]++
	}
	if constraints.ParentTaskID != "" {
		c.parentActive[childRunParent{root: root, parent: constraints.ParentTaskID}]++
	}
}

func (c *ChildRunAdmission) releaseActiveLocked(ticket *ChildRunTicket, frame *childRunFrame) {
	if c.running[ticket] != frame {
		return
	}
	delete(c.running, ticket)
	ticket.current = nil
	c.active--

	root := childRunRoot{
		tenant: frame.constraints.TenantID,
		agent:  frame.constraints.RootAgentID,
	}
	if frame.constraints.RootAgentID != uuid.Nil {
		decrementChildRunCount(c.rootActive, root)
	}
	if frame.constraints.ParentTaskID != "" {
		decrementChildRunCount(
			c.parentActive,
			childRunParent{root: root, parent: frame.constraints.ParentTaskID},
		)
	}
	c.signalLocked()
}

func (c *ChildRunAdmission) executeTop(ticket *ChildRunTicket, frame *childRunFrame) {
	var callbackErr error
	defer func() {
		if recovered := recover(); recovered != nil {
			callbackErr = fmt.Errorf("child run panic: %v", recovered)
		}
		c.finishTop(ticket, callbackErr)
	}()
	frame.run(frame.ctx, frame.lease)
}

func invokeChildRun(frame *childRunFrame) (callbackErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			callbackErr = fmt.Errorf("child run panic: %v", recovered)
		}
	}()
	frame.run(frame.ctx, frame.lease)
	return callbackErr
}

func (c *ChildRunAdmission) finishTop(ticket *ChildRunTicket, callbackErr error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ticket.finished {
		return
	}

	if current := c.running[ticket]; current != nil {
		c.releaseActiveLocked(ticket, current)
	}
	for request := range ticket.requests {
		c.cancelRequestLocked(request, terminalChildRunError(ticket))
	}
	for _, frame := range ticket.stack {
		if frame.stopCancel != nil {
			frame.stopCancel()
		}
		frame.cancel()
	}
	if ticket.stopCancel != nil {
		ticket.stopCancel()
	}

	switch {
	case ticket.cancelRequested:
		ticket.state = ChildRunCancelled
		if ticket.err == nil {
			ticket.err = context.Canceled
		}
	case callbackErr != nil:
		ticket.state = ChildRunFailed
		ticket.err = callbackErr
	default:
		ticket.state = ChildRunCompleted
	}
	ticket.top.state = ticket.state
	ticket.finished = true
	if ticket.live {
		ticket.live = false
		c.live--
		delete(c.liveTickets, ticket)
	}
	ticket.doneOnce.Do(func() { close(ticket.done) })
	c.maybeDrainedLocked()
	c.signalLocked()
}

func (c *ChildRunAdmission) finishContinuation(
	child *childRunFrame,
	callbackErr error,
) *childRunRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	ticket := child.lease.ticket

	if c.running[ticket] == child {
		c.releaseActiveLocked(ticket, child)
	}
	if child.stopCancel != nil {
		child.stopCancel()
	}
	wasCancelled := child.cancelRequested || child.ctx.Err() != nil
	contextErr := child.ctx.Err()
	child.cancel()

	switch {
	case wasCancelled:
		child.state = ChildRunCancelled
		if callbackErr == nil {
			callbackErr = contextErr
		}
	case callbackErr != nil:
		child.state = ChildRunFailed
	default:
		child.state = ChildRunCompleted
	}

	if len(ticket.stack) > 1 && ticket.stack[len(ticket.stack)-1] == child {
		ticket.stack = ticket.stack[:len(ticket.stack)-1]
	}
	if ticket.cancelRequested || ticket.finished || c.closed {
		return nil
	}
	parent := ticket.stack[len(ticket.stack)-1]
	if parent.ctx.Err() != nil {
		c.cancelTicketLocked(ticket, parent.ctx.Err())
		return nil
	}
	parent.state = ChildRunQueued
	ticket.state = ChildRunWaitingChild
	request := c.newContinuationRequestLocked(ticket, parent)
	c.pending = append(c.pending, request)
	c.signalLocked()
	return request
}

func (c *ChildRunAdmission) validateContinuationLocked(
	lease *ChildRunLease,
	child ChildRunConstraints,
) (ChildRunConstraints, error) {
	ticket := lease.ticket
	if c.closed {
		return child, ErrChildRunClosed
	}
	if ticket.finished || ticket.cancelRequested {
		return child, terminalChildRunError(ticket)
	}
	if c.running[ticket] != lease.frame || ticket.current != lease.frame ||
		lease.frame.state != ChildRunRunning {
		return child, ErrChildRunInvalidContinuation
	}
	parent := lease.frame.constraints
	if parent.TaskID == "" || child.TaskID == "" || child.ParentTaskID != parent.TaskID {
		return child, ErrChildRunInvalidContinuation
	}
	if len(ticket.stack) >= maxChildRunContinuationDepth {
		return child, ErrChildRunContinuationDepth
	}
	if child.TenantID != parent.TenantID {
		return child, ErrChildRunInvalidContinuation
	}
	if child.Depth != parent.Depth+1 {
		return child, ErrChildRunInvalidContinuation
	}
	if child.Depth > ticket.maxDepth {
		return child, ErrChildRunContinuationDepth
	}
	child.MaxDepth = ticket.maxDepth
	if parent.RootAgentID != uuid.Nil && child.RootAgentID == parent.RootAgentID {
		child.RootLimit = parent.RootLimit
	}
	return child, nil
}

func (c *ChildRunAdmission) newFrameLocked(
	ticket *ChildRunTicket,
	ctx context.Context,
	constraints ChildRunConstraints,
	run func(context.Context, *ChildRunLease),
) *childRunFrame {
	runCtx, cancel := context.WithCancel(ctx)
	frame := &childRunFrame{
		constraints: constraints,
		ctx:         runCtx,
		cancel:      cancel,
		run:         run,
		state:       ChildRunNew,
	}
	frame.lease = &ChildRunLease{controller: c, ticket: ticket, frame: frame}
	if ticket.top != nil {
		frame.stopCancel = context.AfterFunc(runCtx, func() {
			c.cancelContinuationFrame(ticket, frame)
		})
	}
	return frame
}

func (c *ChildRunAdmission) newContinuationRequestLocked(
	ticket *ChildRunTicket,
	frame *childRunFrame,
) *childRunRequest {
	request := &childRunRequest{
		ticket:    ticket,
		frame:     frame,
		sequence:  ticket.sequence,
		activated: true,
		granted:   make(chan struct{}),
		cancelled: make(chan struct{}),
	}
	ticket.requests[request] = struct{}{}
	return request
}

func (c *ChildRunAdmission) cancelContinuationFrame(
	ticket *ChildRunTicket,
	frame *childRunFrame,
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ticket.finished || frame.state == ChildRunCompleted ||
		frame.state == ChildRunFailed || frame.state == ChildRunCancelled {
		return
	}
	frame.cancelRequested = true
	frame.cancel()
	for request := range ticket.requests {
		if request.frame == frame {
			c.cancelRequestLocked(request, frame.ctx.Err())
		}
	}
	c.signalLocked()
}

func (c *ChildRunAdmission) cancelTicketLocked(ticket *ChildRunTicket, err error) {
	if ticket.finished || ticket.cancelRequested {
		return
	}
	ticket.cancelRequested = true
	ticket.err = err
	ticket.state = ChildRunCancelled
	for _, frame := range ticket.stack {
		frame.cancelRequested = true
		frame.cancel()
	}
	for request := range ticket.requests {
		c.cancelRequestLocked(request, err)
	}

	if !ticket.live {
		ticket.finished = true
		ticket.top.state = ChildRunCancelled
		if ticket.stopCancel != nil {
			ticket.stopCancel()
		}
		ticket.doneOnce.Do(func() { close(ticket.done) })
	}
	c.maybeDrainedLocked()
	c.signalLocked()
}

func (c *ChildRunAdmission) cancelRequestLocked(request *childRunRequest, err error) {
	if request.resolved {
		return
	}
	c.removePendingLocked(request)
	request.resolved = true
	request.cancelErr = err
	request.frame.cancelRequested = true
	request.frame.state = ChildRunCancelled
	close(request.cancelled)
}

func (c *ChildRunAdmission) removePendingLocked(request *childRunRequest) {
	for index, pending := range c.pending {
		if pending != request {
			continue
		}
		c.pending = append(c.pending[:index], c.pending[index+1:]...)
		if request.independent {
			c.pendingIndependent--
		}
		break
	}
	delete(request.ticket.requests, request)
}

func (c *ChildRunAdmission) maybeDrainedLocked() {
	if !c.closed || c.active != 0 || c.live != 0 || len(c.pending) != 0 {
		return
	}
	c.drainedOnce.Do(func() { close(c.drained) })
	c.stopOnce.Do(func() { close(c.stop) })
}

func (c *ChildRunAdmission) signalLocked() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func waitChildRunRequest(request *childRunRequest) error {
	select {
	case <-request.granted:
		return nil
	case <-request.cancelled:
		if request.cancelErr != nil {
			return request.cancelErr
		}
		return context.Canceled
	}
}

func terminalChildRunError(ticket *ChildRunTicket) error {
	if ticket.err != nil {
		return ticket.err
	}
	if ticket.cancelRequested {
		return context.Canceled
	}
	return ErrChildRunClosed
}

func sameRootConstraint(left, right ChildRunConstraints) bool {
	return left.TenantID == right.TenantID &&
		left.RootAgentID != uuid.Nil &&
		left.RootAgentID == right.RootAgentID &&
		left.RootLimit == right.RootLimit
}

func decrementChildRunCount[K comparable](counts map[K]int, key K) {
	if counts[key] <= 1 {
		delete(counts, key)
		return
	}
	counts[key]--
}
