# CP-03: Streaming Tool Execution

**Pattern**: #5 from Agentic OS analysis
**Priority**: HIGH — reduces latency by starting tool execution during LLM streaming
**Dependencies**: CP-02 (needs IsConcurrencySafe interface)
**Estimated effort**: 2 weeks
**Branch**: `feature/cp-03-streaming-tool-exec`

---

## Objective

Execute tools immediately as `tool_use` blocks arrive from the LLM stream,
instead of waiting for the full response. This overlaps LLM inference time
with tool execution time.

```
Current:  [===== LLM stream =====][=== tool 1 ===][=== tool 2 ===]
Target:   [===== LLM stream =====]
              [=== tool 1 ===]     ← starts during stream
                  [=== tool 2 ===] ← starts when tool 1 finishes (if exclusive)
```

---

## Step 1: StreamingToolExecutor

### 1.1 Create `internal/pipeline/streaming_tool_executor.go`

```go
package pipeline

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// toolStatus tracks lifecycle of a streaming tool execution.
type toolStatus string

const (
	toolQueued    toolStatus = "queued"
	toolExecuting toolStatus = "executing"
	toolCompleted toolStatus = "completed"
)

// streamingTool tracks one tool call through the streaming executor.
type streamingTool struct {
	call   ToolCall
	status atomic.Value // toolStatus
	safe   bool
	result *tools.Result
	err    error
}

// ToolUpdate is yielded by the executor when a tool completes.
type ToolUpdate struct {
	Call    ToolCall
	Result *tools.Result
	Err    error
}

// StreamingToolExecutor manages tool execution during LLM streaming.
//
// Concurrency rules (same as batch partitioning):
// - Concurrent-safe tools can start if ALL currently executing tools are also safe
// - Exclusive tools must wait until no tools are executing
// - If an exclusive tool is executing, all new tools wait
//
// Sibling abort: exec tool errors cancel all siblings (same policy as batch).
type StreamingToolExecutor struct {
	registry *tools.Registry
	execFn   func(ctx context.Context, tc ToolCall) (*tools.Result, error)

	mu       sync.Mutex
	tools    []*streamingTool
	allSafe  bool // true if all currently executing tools are concurrent-safe

	executing atomic.Int32

	sibCtx    context.Context
	sibCancel context.CancelFunc
	hasError  atomic.Bool

	results chan ToolUpdate
	closed  atomic.Bool

	wg sync.WaitGroup
}

// NewStreamingToolExecutor creates an executor bound to a parent context.
func NewStreamingToolExecutor(
	registry *tools.Registry,
	execFn func(ctx context.Context, tc ToolCall) (*tools.Result, error),
	parentCtx context.Context,
) *StreamingToolExecutor {
	sibCtx, sibCancel := context.WithCancel(parentCtx)
	return &StreamingToolExecutor{
		registry:  registry,
		execFn:    execFn,
		sibCtx:    sibCtx,
		sibCancel: sibCancel,
		results:   make(chan ToolUpdate, 32),
		allSafe:   true,
	}
}

// AddTool queues a tool for execution. Called by ThinkStage when a tool_use
// block arrives from the LLM stream. May start execution immediately if
// concurrency rules allow.
func (ste *StreamingToolExecutor) AddTool(tc ToolCall) {
	tool := ste.registry.Get(tc.Name)
	safe := tools.IsConcurrencySafeForTool(tool, tc.Args)

	st := &streamingTool{call: tc, safe: safe}
	st.status.Store(toolQueued)

	ste.mu.Lock()
	ste.tools = append(ste.tools, st)
	ste.mu.Unlock()

	ste.tryStartNext()
}

// canStart checks if a tool can begin executing given current state.
func (ste *StreamingToolExecutor) canStart(safe bool) bool {
	executing := ste.executing.Load()
	if executing == 0 {
		return true // nobody running
	}
	return safe && ste.allSafe // new tool safe AND all running are safe
}

// tryStartNext attempts to dequeue and start the next eligible tool(s).
func (ste *StreamingToolExecutor) tryStartNext() {
	ste.mu.Lock()
	defer ste.mu.Unlock()

	for _, st := range ste.tools {
		status := st.status.Load().(toolStatus)
		if status != toolQueued {
			continue
		}

		if !ste.canStart(st.safe) {
			break // can't start this one — stop looking
		}

		st.status.Store(toolExecuting)
		ste.executing.Add(1)
		if !st.safe {
			ste.allSafe = false
		}

		ste.wg.Add(1)
		go ste.executeTool(st)
	}
}

// executeTool runs a single tool and sends the result.
func (ste *StreamingToolExecutor) executeTool(st *streamingTool) {
	defer func() {
		ste.executing.Add(-1)
		st.status.Store(toolCompleted)
		ste.wg.Done()

		// Update allSafe: check if remaining executing tools are all safe
		ste.mu.Lock()
		allSafe := true
		for _, t := range ste.tools {
			if t.status.Load().(toolStatus) == toolExecuting && !t.safe {
				allSafe = false
				break
			}
		}
		ste.allSafe = allSafe
		ste.mu.Unlock()

		// Try to start queued tools now that this one finished
		ste.tryStartNext()
	}()

	result, err := ste.execFn(ste.sibCtx, st.call)
	st.result = result
	st.err = err

	// Sibling abort for exec tool errors
	if err != nil && isExecFamilyTool(st.call.Name) {
		if ste.hasError.CompareAndSwap(false, true) {
			slog.Warn("streaming exec error — aborting siblings",
				"tool", st.call.Name, "err", err)
			ste.sibCancel()
		}
	}

	// Send result (non-blocking — buffered channel)
	if !ste.closed.Load() {
		ste.results <- ToolUpdate{
			Call:   st.call,
			Result: result,
			Err:    err,
		}
	}
}

// Done signals that the LLM stream is complete and no more tools will be added.
// Returns a channel that yields results as tools complete.
// The channel closes when all tools have finished.
func (ste *StreamingToolExecutor) Done() <-chan ToolUpdate {
	go func() {
		ste.wg.Wait()
		ste.closed.Store(true)
		close(ste.results)
	}()
	return ste.results
}

// HasPending returns true if there are queued or executing tools.
func (ste *StreamingToolExecutor) HasPending() bool {
	ste.mu.Lock()
	defer ste.mu.Unlock()
	for _, st := range ste.tools {
		status := st.status.Load().(toolStatus)
		if status != toolCompleted {
			return true
		}
	}
	return false
}

// Cancel aborts all pending and executing tools.
func (ste *StreamingToolExecutor) Cancel() {
	ste.sibCancel()
	ste.closed.Store(true)
}
```

---

## Step 2: Integrate into ThinkStage

### 2.1 Add streaming executor to ThinkStage

**File**: `internal/pipeline/think_stage.go`

```go
type ThinkStage struct {
	deps              *PipelineDeps
	result            StageResult
	reactiveCompactor *ReactiveCompactor
	streamingExec     *StreamingToolExecutor // NEW — created per-iteration
}
```

### 2.2 In Execute(), feed tool blocks to executor during stream

```go
func (s *ThinkStage) Execute(ctx context.Context, state *RunState) error {
	// Create streaming executor if feature enabled
	var streamExec *StreamingToolExecutor
	if s.deps.Config.StreamingToolExec {
		streamExec = NewStreamingToolExecutor(
			s.deps.ToolRegistry,
			func(ctx context.Context, tc ToolCall) (*tools.Result, error) {
				return s.deps.ToolRegistry.ExecuteWithContext(ctx, tc.Name, tc.Args,
					/* channel, chatID, etc. from state */)
			},
			ctx,
		)
		state.Tool.StreamExecutor = streamExec
	}

	// Stream LLM response
	resp, err := s.deps.ChatStream(ctx, req, func(chunk StreamChunk) {
		// ... existing chunk handling ...

		// NEW: Feed tool calls to streaming executor as they arrive
		if streamExec != nil && chunk.ToolCall != nil {
			streamExec.AddTool(*chunk.ToolCall)
		}
	})

	// Signal to executor that LLM is done streaming
	if streamExec != nil {
		// Don't close yet — ToolStage will drain results
	}

	// ... rest of existing logic ...
}
```

---

## Step 3: Update ToolStage for streaming path

**File**: `internal/pipeline/tool_stage.go`

```go
func (s *ToolStage) Execute(ctx context.Context, state *RunState) error {
	s.result = Continue

	// === NEW: Streaming path ===
	if streamExec := state.Tool.StreamExecutor; streamExec != nil {
		return s.executeStreaming(ctx, state, streamExec)
	}

	// === Existing: Batch path (unchanged) ===
	resp := state.Think.LastResponse
	if resp == nil || len(resp.ToolCalls) == 0 {
		return nil
	}
	// ... existing partition + execute logic ...
}

// executeStreaming drains results from the StreamingToolExecutor.
// Tools may already be completed (started during ThinkStage stream).
func (s *ToolStage) executeStreaming(
	ctx context.Context,
	state *RunState,
	exec *StreamingToolExecutor,
) error {
	for update := range exec.Done() {
		if update.Err != nil {
			slog.Warn("streaming tool error",
				"tool", update.Call.Name,
				"err", update.Err)
			// Inject error result into messages
			state.Messages.AppendPending(errorMessage(update.Call, update.Err))
		} else {
			// Process result — sequential mutation
			msgs, err := s.deps.ProcessToolResult(ctx, state, update.Call,
				resultToMessage(update.Result), update.Result)
			if err != nil {
				return err
			}
			for _, msg := range msgs {
				state.Messages.AppendPending(msg)
			}
		}
		state.Tool.TotalToolCalls++

		if state.Tool.LoopKilled {
			exec.Cancel()
			s.result = BreakLoop
			return nil
		}
	}

	return s.checkExitConditions(state)
}
```

---

## Step 4: Add Substate

**File**: `internal/pipeline/substates.go`

```go
type ToolSubstate struct {
	// ... existing fields ...
	StreamExecutor *StreamingToolExecutor // NEW: set by ThinkStage when streaming enabled
}
```

---

## Step 5: Feature Flag

**File**: `internal/pipeline/deps.go`

```go
type PipelineConfig struct {
	// ... existing ...
	StreamingToolExec bool // Default: false. Env: GOCLAW_STREAMING_TOOL_EXEC
}
```

**File**: `cmd/gateway.go` or config loading:
```go
cfg.StreamingToolExec = os.Getenv("GOCLAW_STREAMING_TOOL_EXEC") == "true"
```

---

## Verification Checklist

- [ ] Feature flag off → existing batch behavior unchanged
- [ ] Feature flag on → tools start during LLM stream
- [ ] Concurrent-safe tools start in parallel during stream
- [ ] Exclusive tool waits until previous tools finish
- [ ] Exec tool error cancels sibling tools
- [ ] Read tool error does NOT cancel siblings
- [ ] All tool results collected in ToolStage after stream ends
- [ ] Sequential mutation order preserved
- [ ] Streaming executor properly cleaned up on cancel/abort
- [ ] Performance: measurable latency reduction with 3+ read-only tools

## Test File

Create `internal/pipeline/streaming_tool_executor_test.go`:
```go
func TestStreamingExecutor_ConcurrentSafe(t *testing.T) { ... }
func TestStreamingExecutor_ExclusiveWaits(t *testing.T) { ... }
func TestStreamingExecutor_MixedBatches(t *testing.T) { ... }
func TestStreamingExecutor_SiblingAbort(t *testing.T) { ... }
func TestStreamingExecutor_Cancel(t *testing.T) { ... }
func TestStreamingExecutor_EmptyNoBlock(t *testing.T) { ... }
```
