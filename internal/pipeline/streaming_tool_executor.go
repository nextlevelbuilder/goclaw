// Package pipeline — StreamingToolExecutor (CP-03).
// Executes tools immediately as tool_use blocks arrive from LLM stream,
// overlapping LLM inference time with tool execution time.
package pipeline

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

type stToolStatus int32

const (
	stQueued stToolStatus = iota
	stExecuting
	stCompleted
)

// streamingTool tracks one tool call through the streaming executor.
type streamingTool struct {
	call   providers.ToolCall
	status atomic.Int32 // stToolStatus
	safe   bool
	result providers.Message
	raw    any
	err    error
}

// StreamToolUpdate is yielded by the executor when a tool completes.
type StreamToolUpdate struct {
	Call    providers.ToolCall
	RawMsg  providers.Message
	RawData any
	Err     error
}

// StreamingToolExecutor manages tool execution during LLM streaming.
//
// Concurrency rules:
//   - Concurrent-safe tools can start if ALL currently executing tools are also safe
//   - Exclusive tools must wait until no tools are executing
//   - If an exclusive tool is executing, all new tools wait
type StreamingToolExecutor struct {
	isSafeFn func(tc providers.ToolCall) bool
	execFn   func(ctx context.Context, tc providers.ToolCall) (providers.Message, any, error)

	mu            sync.Mutex
	tools         []*streamingTool
	allSafe       bool
	pending       int
	doneRequested bool
	resultsClosed bool

	executing atomic.Int32

	sibCtx    context.Context
	sibCancel context.CancelFunc
	hasError  atomic.Bool

	results chan StreamToolUpdate
	closed  atomic.Bool
}

// NewStreamingToolExecutor creates an executor for streaming tool execution.
//
// Parameters:
//   - isSafeFn: determines if a tool call can run concurrently
//   - execFn: executes tool I/O (should NOT mutate shared state)
//   - parentCtx: cancellation propagated to all tool executions
func NewStreamingToolExecutor(
	isSafeFn func(tc providers.ToolCall) bool,
	execFn func(ctx context.Context, tc providers.ToolCall) (providers.Message, any, error),
	parentCtx context.Context,
) *StreamingToolExecutor {
	sibCtx, sibCancel := context.WithCancel(parentCtx)
	return &StreamingToolExecutor{
		isSafeFn:  isSafeFn,
		execFn:    execFn,
		sibCtx:    sibCtx,
		sibCancel: sibCancel,
		results:   make(chan StreamToolUpdate, 32),
		allSafe:   true,
	}
}

// AddTool queues a tool for execution. Called by ThinkStage when a tool_use
// block arrives from the LLM stream. May start execution immediately.
func (ste *StreamingToolExecutor) AddTool(tc providers.ToolCall) bool {
	safe := false
	if ste.isSafeFn != nil {
		safe = ste.isSafeFn(tc)
	}

	st := &streamingTool{call: tc, safe: safe}
	st.status.Store(int32(stQueued))

	ste.mu.Lock()
	if ste.doneRequested || ste.resultsClosed {
		ste.mu.Unlock()
		return false
	}
	ste.tools = append(ste.tools, st)
	ste.pending++
	toStart := ste.tryStartNextLocked()
	ste.mu.Unlock()

	ste.startTools(toStart)
	return true
}

func (ste *StreamingToolExecutor) canStartLocked(safe bool) bool {
	executing := ste.executing.Load()
	if executing == 0 {
		return true
	}
	return safe && ste.allSafe
}

func (ste *StreamingToolExecutor) tryStartNextLocked() []*streamingTool {
	var toStart []*streamingTool
	for _, st := range ste.tools {
		if stToolStatus(st.status.Load()) != stQueued {
			continue
		}
		if !ste.canStartLocked(st.safe) {
			break
		}

		st.status.Store(int32(stExecuting))
		ste.executing.Add(1)
		if !st.safe {
			ste.allSafe = false
		}
		toStart = append(toStart, st)
	}
	return toStart
}

func (ste *StreamingToolExecutor) startTools(toStart []*streamingTool) {
	for _, st := range toStart {
		go ste.executeTool(st)
	}
}

func (ste *StreamingToolExecutor) maybeCloseResultsLocked() {
	if ste.resultsClosed || !ste.doneRequested || ste.pending != 0 {
		return
	}
	ste.resultsClosed = true
	ste.closed.Store(true)
	close(ste.results)
}

func (ste *StreamingToolExecutor) executeTool(st *streamingTool) {
	defer func() {
		ste.mu.Lock()
		ste.executing.Add(-1)
		st.status.Store(int32(stCompleted))
		ste.pending--

		// Recalculate allSafe.
		allSafe := true
		for _, t := range ste.tools {
			if stToolStatus(t.status.Load()) == stExecuting && !t.safe {
				allSafe = false
				break
			}
		}
		ste.allSafe = allSafe
		toStart := ste.tryStartNextLocked()
		ste.maybeCloseResultsLocked()
		ste.mu.Unlock()

		ste.startTools(toStart)
	}()

	msg, raw, err := ste.execFn(ste.sibCtx, st.call)
	st.result = msg
	st.raw = raw
	st.err = err

	// Sibling abort for exec tool errors
	if err != nil && isExecFamilyTool(st.call.Name) {
		if ste.hasError.CompareAndSwap(false, true) {
			slog.Warn("streaming exec error — aborting siblings",
				"tool", st.call.Name, "err", err)
			ste.sibCancel()
		}
	}

	if !ste.closed.Load() {
		ste.results <- StreamToolUpdate{
			Call:    st.call,
			RawMsg:  msg,
			RawData: raw,
			Err:     err,
		}
	}
}

// Done signals that the LLM stream is complete. Returns a channel that yields
// results as tools complete. Channel closes when all tools finish.
func (ste *StreamingToolExecutor) Done() <-chan StreamToolUpdate {
	ste.mu.Lock()
	ste.doneRequested = true
	ste.maybeCloseResultsLocked()
	ste.mu.Unlock()
	return ste.results
}

// HasPending returns true if there are queued or executing tools.
func (ste *StreamingToolExecutor) HasPending() bool {
	ste.mu.Lock()
	defer ste.mu.Unlock()
	for _, st := range ste.tools {
		if stToolStatus(st.status.Load()) != stCompleted {
			return true
		}
	}
	return false
}

// Cancel aborts all pending and executing tools.
func (ste *StreamingToolExecutor) Cancel() {
	ste.sibCancel()
	ste.closed.Store(true)
	ste.mu.Lock()
	ste.doneRequested = true
	ste.maybeCloseResultsLocked()
	ste.mu.Unlock()
}

// Count returns the total number of tools added.
func (ste *StreamingToolExecutor) Count() int {
	ste.mu.Lock()
	defer ste.mu.Unlock()
	return len(ste.tools)
}
