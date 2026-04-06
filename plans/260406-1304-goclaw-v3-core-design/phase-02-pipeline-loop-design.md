# Phase 2: Pipeline Loop Design

## Context Links

- Brainstorm: `plans/reports/brainstorm-260406-1059-goclaw-v3-core-redesign.md` (decision 1)
- Loop deep dive: `plans/reports/Explore-260406-GoClaw-Agent-Loop-v3-Deep-Dive.md` (all sections)
- Phase 1 dependencies: `phase-01-foundation-interfaces.md` (TokenCounter, WorkspaceContext, EventBus, ProviderCapabilities)
- Current loop: `internal/agent/loop.go` (804 lines), `loop_types.go`, `loop_context.go`, `loop_history.go`, `loop_compact.go`, `pruning.go`, `loop_tools.go`, `loop_run.go`

## Overview

- **Priority:** P1 (core execution engine, blocks phases 3-6)
- **Status:** Complete
- **Effort:** 8h design
- **Depends on:** Phase 1 (all 4 foundation interfaces)

Replace monolithic `runLoop()` (804 lines, 20+ field `runState`) with a pipeline of pluggable stages. Each stage owns a substage of state, is independently testable, and communicates via a shared `RunState` struct.

## Key Insights

- Current loop has 7 exit conditions scattered across 800 lines. Pipeline model makes each exit condition a stage responsibility.
- `runState` has 20+ fields mixing concerns (loop control, output accumulation, crash safety, bootstrap detection, team task tracking, skill evolution). Split into typed substates.
- Mid-loop compaction (lines 352-411) interleaves with think phase. Pipeline separates this into `PruneStage` between think and tool.
- Parallel tool execution already well-isolated (goroutine per tool, results collected via channel). Wrapping in `ToolStage` is clean.
- Memory flush currently triggers inside `maybeSummarize()` which runs post-loop. Moving to `MemoryFlushStage` pre-compaction aligns with 3-tier memory design.

---

## A. Stage Interface

```go
// Package: internal/pipeline

// Stage is a single step in the agent pipeline.
// Stages are stateless -- all mutable state lives in RunState.
type Stage interface {
    // Name returns a human-readable identifier for logging/tracing.
    Name() string

    // Execute performs the stage's work. Returns error to abort pipeline.
    // Stages modify RunState in place.
    Execute(ctx context.Context, state *RunState) error
}

// StageResult signals how the pipeline should proceed after a stage.
type StageResult int

const (
    Continue    StageResult = iota // proceed to next stage
    BreakLoop                      // exit iteration loop (normal completion)
    AbortRun                       // abort entire run (error/kill)
)

// StageWithResult extends Stage to control pipeline flow.
// If a stage does not implement this, pipeline assumes Continue.
type StageWithResult interface {
    Stage
    Result() StageResult
}
```

## B. RunState Redesign

Split current 20+ field `runState` into typed substates. Each substate grouped by owning stage.

```go
// RunState is the shared mutable state for a single pipeline run.
// Passed by pointer through all stages.
type RunState struct {
    // Identity (set once at pipeline start, immutable during run)
    Request   *agent.RunRequest
    Workspace *workspace.WorkspaceContext
    Model     string
    Provider  providers.Provider

    // Message buffer (read/write by multiple stages)
    Messages  *MessageBuffer

    // Per-stage substates
    Context   ContextState
    Think     ThinkState
    Prune     PruneState
    Tool      ToolState
    Observe   ObserveState
    Compact   CompactState
    Evolution EvolutionState

    // Cross-cutting concerns
    Iteration int
    RunID     string
    ExitCode  StageResult
}

// MessageBuffer wraps the message list with append/replace semantics.
// Thread-safe for read; write only from owning stage (sequential pipeline).
type MessageBuffer struct {
    system   providers.Message   // system prompt (rebuilt by ContextStage)
    history  []providers.Message // conversation history
    pending  []providers.Message // new messages this run (flushed at checkpoint)
}

func (mb *MessageBuffer) All() []providers.Message { /* system + history + pending */ }
func (mb *MessageBuffer) AppendPending(msg providers.Message) { /* ... */ }
func (mb *MessageBuffer) FlushPending() []providers.Message { /* move pending to history */ }
func (mb *MessageBuffer) ReplaceHistory(msgs []providers.Message) { /* post-compaction */ }
```

### Substates

```go
// ContextState: owned by ContextStage, read by ThinkStage
type ContextState struct {
    ContextFiles  []bootstrap.ContextFile
    SkillsSummary string
    TeamContext   *TeamContextInfo // nil if not in team
    HadBootstrap  bool
    OverheadTokens int // system prompt + context files token count (accurate via TokenCounter)
}

// ThinkState: owned by ThinkStage
type ThinkState struct {
    LastResponse    *providers.ChatResponse
    TotalUsage      providers.Usage
    TruncRetries    int  // consecutive truncation retries (max 3)
    StreamingActive bool // true during active stream
}

// PruneState: owned by PruneStage
type PruneState struct {
    MidLoopCompacted bool // true after first in-loop compaction
    HistoryTokens    int  // last computed history token count
    HistoryBudget    int  // contextWindow * maxHistoryShare
}

// ToolState: owned by ToolStage
type ToolState struct {
    LoopDetector   *ToolLoopDetector
    TotalToolCalls int
    AsyncToolCalls []string       // tool names that executed async (spawn)
    MediaResults   []MediaResult  // files produced by tools
    Deliverables   []string       // content for team task results
    LoopKilled     bool           // set when loop detector triggers critical
}

// ObserveState: owned by ObserveStage
type ObserveState struct {
    FinalContent   string // accumulated response text
    FinalThinking  string // reasoning output
    BlockReplies   int
    LastBlockReply string
}

// CompactState: owned by CheckpointStage + MemoryFlushStage
type CompactState struct {
    CheckpointFlushedMsgs int
    MemoryFlushedThisCycle bool
    CompactionCount        int // tracks compaction cycles
}

// EvolutionState: owned by skill evolution nudge logic
type EvolutionState struct {
    Nudge70Sent     bool
    Nudge90Sent     bool
    PostscriptSent  bool
    BootstrapWrite  bool // BOOTSTRAP.md write detected
    TeamTaskCreates int
    TeamTaskSpawns  int
}
```

### Migration from current runState

| Current field | Moves to |
|--------------|----------|
| `loopDetector` | `ToolState.LoopDetector` |
| `totalUsage` | `ThinkState.TotalUsage` |
| `iteration` | `RunState.Iteration` |
| `totalToolCalls` | `ToolState.TotalToolCalls` |
| `finalContent` | `ObserveState.FinalContent` |
| `finalThinking` | `ObserveState.FinalThinking` |
| `asyncToolCalls` | `ToolState.AsyncToolCalls` |
| `mediaResults` | `ToolState.MediaResults` |
| `deliverables` | `ToolState.Deliverables` |
| `pendingMsgs` | `MessageBuffer.pending` |
| `blockReplies` | `ObserveState.BlockReplies` |
| `lastBlockReply` | `ObserveState.LastBlockReply` |
| `checkpointFlushedMsgs` | `CompactState.CheckpointFlushedMsgs` |
| `midLoopCompacted` | `PruneState.MidLoopCompacted` |
| `overheadTokens` | `ContextState.OverheadTokens` |
| `overheadCalibrated` | REMOVED (overhead now accurate upfront via TokenCounter) |
| `bootstrapWriteDetected` | `EvolutionState.BootstrapWrite` |
| `teamTaskCreates/Spawns` | `EvolutionState.TeamTaskCreates/Spawns` |
| `skillNudge*` | `EvolutionState.Nudge*` |
| `loopKilled` | `ToolState.LoopKilled` |
| `truncationRetries` | `ThinkState.TruncRetries` |

---

## C. Stage Catalog

### Setup Pipeline (runs once before iteration loop)

```
1. ContextStage
   - Resolve WorkspaceContext via Resolver
   - Load + filter context files (bootstrap, per-user, team)
   - Build skills summary (BM25)
   - Build system prompt via template
   - Compute overhead tokens (TokenCounter)
   - Enrich input media
   - Inject team task reminders
   
   Reads: RunState.Request, providers
   Writes: RunState.Workspace, ContextState, MessageBuffer.system
   
   Replaces: injectContext() + buildMessages() + enrichInputMedia() + injectTeamTaskReminders()
```

### Iteration Pipeline (runs per iteration, max N times)

```
2. ThinkStage
   - Inject iteration budget nudges (70%, 90%)
   - Build per-iteration tool definitions (policy + bootstrap filters)
   - Build ChatRequest from MessageBuffer
   - Call LLM (Chat or ChatStream based on capabilities)
   - Emit events: chunk, thinking
   - Accumulate token usage
   - Handle truncation retries
   
   Reads: MessageBuffer, ContextState, ProviderCapabilities
   Writes: ThinkState, MessageBuffer (append assistant message)
   Result: Continue (has tool calls) | BreakLoop (no tool calls, final content)
   
   Replaces: loop.go lines 217-446

3. PruneStage
   - Compute history tokens via TokenCounter
   - Phase 1 @ 70% budget: pruneContextMessages (soft-trim + hard-clear)
   - Phase 2 @ 100% budget: compactMessagesInPlace (LLM summarization)
   - If still over budget after Phase 2: AbortRun
   
   Reads: MessageBuffer, TokenCounter, context window
   Writes: PruneState, MessageBuffer (may replace history)
   Result: Continue | AbortRun (budget exceeded after compaction)
   
   Replaces: loop.go lines 352-411, loop_compact.go, pruning.go

4. ToolStage
   - Extract tool calls from ThinkState.LastResponse
   - Generate unique tool call IDs
   - Single tool: sequential execution
   - Multiple tools: parallel goroutines, collect via channel, sort by index
   - Per-tool: policy check, lazy MCP activate, execute, process result
   - Loop detection: same-tool, read-only streak, same-result
   - Tool budget check (totalToolCalls > maxToolCalls)
   - Emit events: tool.call, tool.result
   
   Reads: ThinkState.LastResponse, ToolExecutor, WorkspaceContext
   Writes: ToolState, MessageBuffer (append tool results)
   Result: Continue | BreakLoop (loop killed) | AbortRun (policy denied)
   
   Replaces: loop.go lines 450-738, loop_tools.go

5. ObserveStage
   - Process tool results: extract deliverables, media
   - Drain InjectCh (mid-run message injection, Point A + Point B)
   - Check if final content ready (no tool calls + no injected messages)
   - Sanitize final content
   - Skill evolution postscript
   - Publish domain events: tool.executed, run metrics
   
   Reads: ToolState, ThinkState, InjectCh
   Writes: ObserveState, MessageBuffer (may append injected messages)
   Result: Continue (more iterations needed) | BreakLoop (final content ready)
   
   Replaces: loop.go lines 450-466 (no-tool path), finalize portions

6. CheckpointStage
   - Every N iterations (default 5): flush pending messages to session store
   - Crash safety: intermediate state persisted
   - Conditional: skip if iteration % N != 0
   
   Reads: MessageBuffer.pending, SessionStore
   Writes: CompactState.CheckpointFlushedMsgs, MessageBuffer (clear pending)
   
   Replaces: loop.go lines 748-761

7. MemoryFlushStage
   - Runs before compaction (when PruneStage detects budget pressure)
   - Triggered by PruneStage setting a flag, NOT by iteration count
   - Two-stage fallback: LLM flush -> extractive regex
   - Dedup guard: once per compaction cycle
   - Publishes: session.completed event (for episodic pipeline)
   
   Reads: MessageBuffer, MemoryStore, compaction config
   Writes: CompactState.MemoryFlushedThisCycle
   
   Replaces: memoryflush.go, extractive_memory.go
```

### Post-Loop Pipeline (runs once after iteration loop exits)

```
8. FinalizeStage
   - Sanitize final content (HTML entities, markdown cleanup)
   - Media dedup + size population
   - Flush remaining pending messages to session
   - Token accumulation to session metadata
   - Bootstrap auto-cleanup (delete BOOTSTRAP.md if write detected)
   - Post-run summarization (maybeSummarize)
   - Publish run.completed event
   - Construct RunResult
   
   Reads: All substates
   Writes: RunResult (returned to caller)
   
   Replaces: finalizeRun(), maybeSummarize()
```

---

## D. Pipeline Orchestrator

```go
// Pipeline orchestrates stage execution for a single agent run.
type Pipeline struct {
    setup     []Stage // runs once before loop
    iteration []Stage // runs per iteration
    finalize  []Stage // runs once after loop

    maxIterations int
    tokenCounter  tokencount.TokenCounter
    eventBus      eventbus.DomainEventBus
}

// NewDefaultPipeline creates the standard agent pipeline.
func NewDefaultPipeline(deps PipelineDeps) *Pipeline {
    return &Pipeline{
        setup: []Stage{
            NewContextStage(deps),
        },
        iteration: []Stage{
            NewThinkStage(deps),
            NewPruneStage(deps),
            NewToolStage(deps),
            NewObserveStage(deps),
            NewCheckpointStage(deps),
        },
        finalize: []Stage{
            NewFinalizeStage(deps),
        },
        maxIterations: deps.Config.MaxIterations,
        tokenCounter:  deps.TokenCounter,
        eventBus:      deps.EventBus,
    }
}

// PipelineDeps bundles all dependencies injected into stages.
type PipelineDeps struct {
    Provider      providers.Provider
    TokenCounter  tokencount.TokenCounter
    EventBus      eventbus.DomainEventBus
    SessionStore  store.SessionStore
    ToolExecutor  tools.ToolExecutor
    ToolPolicy    *tools.PolicyEngine
    SkillsLoader  *skills.Loader
    MemoryStore   store.MemoryStore
    WorkspaceResolver workspace.Resolver
    Config        PipelineConfig
}

type PipelineConfig struct {
    MaxIterations      int
    MaxToolCalls       int
    CheckpointInterval int // flush every N iterations (default 5)
    ContextWindow      int
    MaxTokens          int
    Compaction         *config.CompactionConfig
    ContextPruning     *config.ContextPruningConfig
}
```

### Execution Flow

```go
// Run executes the full pipeline for a single agent run.
func (p *Pipeline) Run(ctx context.Context, req *agent.RunRequest) (*agent.RunResult, error) {
    state := NewRunState(req)

    // 1. Setup pipeline (once)
    for _, stage := range p.setup {
        if err := stage.Execute(ctx, state); err != nil {
            return nil, fmt.Errorf("setup %s: %w", stage.Name(), err)
        }
    }

    // 2. Iteration loop
    for state.Iteration = 0; state.Iteration < p.maxIterations; state.Iteration++ {
        for _, stage := range p.iteration {
            if err := stage.Execute(ctx, state); err != nil {
                return nil, fmt.Errorf("iter %d %s: %w", state.Iteration, stage.Name(), err)
            }

            // Check if stage wants to control flow
            if swr, ok := stage.(StageWithResult); ok {
                switch swr.Result() {
                case BreakLoop:
                    goto finalize
                case AbortRun:
                    goto finalize
                }
            }
        }

        // Check context cancellation (/stop)
        if ctx.Err() != nil {
            break
        }
    }

finalize:
    // 3. Finalize pipeline (once)
    for _, stage := range p.finalize {
        if err := stage.Execute(ctx, state); err != nil {
            slog.Warn("finalize stage error", "stage", stage.Name(), "err", err)
            // finalize errors are logged, not fatal
        }
    }

    return state.BuildResult(), nil
}
```

### Error Propagation

```
Stage returns error:
  - Setup stage error -> abort run, return error
  - Iteration stage error -> abort run, still run finalize
  - Finalize stage error -> log warning, continue other finalize stages

Stage returns StageResult:
  - Continue -> next stage in iteration
  - BreakLoop -> skip remaining iteration stages, jump to finalize
  - AbortRun -> skip remaining stages, jump to finalize (with error flag)
```

---

## Data Flow Between Stages

```
ContextStage
  writes: Workspace, ContextState, MessageBuffer.system
     |
     v
ThinkStage
  reads: MessageBuffer.All(), ContextState.OverheadTokens
  writes: ThinkState.LastResponse, ThinkState.TotalUsage
  writes: MessageBuffer.AppendPending(assistant msg)
     |
     v
PruneStage
  reads: MessageBuffer, TokenCounter, PruneState
  writes: PruneState.HistoryTokens
  may trigger: MemoryFlushStage (inline, before compaction)
  may write: MessageBuffer.ReplaceHistory(compacted)
     |
     v
ToolStage
  reads: ThinkState.LastResponse.ToolCalls
  writes: ToolState.*, MessageBuffer.AppendPending(tool results)
     |
     v
ObserveStage
  reads: ToolState, ThinkState, InjectCh
  writes: ObserveState.FinalContent (if done)
  may write: MessageBuffer.AppendPending(injected msgs)
     |
     v
CheckpointStage
  reads: MessageBuffer.pending
  writes: SessionStore, CompactState.CheckpointFlushedMsgs
     |
     v (loop back to ThinkStage or exit to FinalizeStage)

FinalizeStage
  reads: All substates
  writes: RunResult
```

---

## MemoryFlushStage Integration with PruneStage

MemoryFlushStage is NOT a regular iteration stage. It is invoked inline by PruneStage when compaction is needed.

```go
// PruneStage.Execute
func (s *PruneStage) Execute(ctx context.Context, state *RunState) error {
    tokens := s.tokenCounter.CountMessages(state.Model, state.Messages.All())
    state.Prune.HistoryTokens = tokens

    budget := s.contextWindow * 80 / 100 // 80% for history

    // Phase 1: soft pruning at 70%
    if tokens > budget*70/100 {
        pruneContextMessages(state.Messages, s.config)
        tokens = s.tokenCounter.CountMessages(state.Model, state.Messages.All())
    }

    // Phase 2: full compaction at 100%
    if tokens > budget {
        // Memory flush BEFORE compaction (saves memories before they're summarized away)
        if !state.Compact.MemoryFlushedThisCycle && s.memoryFlush != nil {
            s.memoryFlush.Execute(ctx, state) // inline call, not pipeline stage
            state.Compact.MemoryFlushedThisCycle = true
        }

        state.Prune.MidLoopCompacted = true
        compactMessagesInPlace(state.Messages, s.compactionConfig)

        // Re-check after compaction
        tokens = s.tokenCounter.CountMessages(state.Model, state.Messages.All())
        if tokens > budget {
            state.ExitCode = AbortRun
        }
    }
    return nil
}
```

---

## Requirements

### Functional
- F1: Pipeline produces identical `RunResult` as current monolithic loop for same inputs
- F2: All 7 exit conditions preserved: no-tool-calls, max iterations, truncation limit, loop kill, read-only streak, tool budget, ctx.Done
- F3: Parallel tool execution behavior unchanged (goroutine per tool, sorted results)
- F4: Mid-run injection (InjectCh) works at both Point A (after tools) and Point B (no tools)
- F5: Checkpoint flush every N iterations, crash safety preserved

### Non-Functional
- NF1: No measurable latency increase from stage dispatch overhead (stages are coarse-grained)
- NF2: Each stage independently testable with mock RunState
- NF3: Adding new stage requires only: implement Stage interface + insert in pipeline constructor
- NF4: RunState substates compile-time typed (no `map[string]any`)

## Related Code Files

### Files to decompose

| Current file | Lines | Maps to stages |
|-------------|-------|---------------|
| `internal/agent/loop.go` | 804 | ThinkStage, ToolStage, ObserveStage, orchestrator |
| `internal/agent/loop_run.go` | 245 | Pipeline.Run() entry point |
| `internal/agent/loop_context.go` | 275 | ContextStage |
| `internal/agent/loop_history.go` | 859 | ContextStage (buildMessages), FinalizeStage (maybeSummarize) |
| `internal/agent/loop_compact.go` | 119 | PruneStage |
| `internal/agent/pruning.go` | 327 | PruneStage |
| `internal/agent/loop_tools.go` | ~180 | ToolStage |
| `internal/agent/loop_tool_filter.go` | ~100 | ThinkStage (buildFilteredTools) |
| `internal/agent/memoryflush.go` | ~150 | MemoryFlushStage |
| `internal/agent/extractive_memory.go` | ~150 | MemoryFlushStage (fallback) |
| `internal/agent/loop_types.go` | ~150 | RunState, substates |
| `internal/agent/loop_input_media.go` | ~100 | ContextStage |
| `internal/agent/loop_team_reminders.go` | ~60 | ContextStage |

### New files to create

| File | Purpose |
|------|---------|
| `internal/pipeline/stage.go` | Stage interface, StageResult, StageWithResult |
| `internal/pipeline/run_state.go` | RunState + MessageBuffer + all substates |
| `internal/pipeline/pipeline.go` | Pipeline orchestrator |
| `internal/pipeline/context_stage.go` | ContextStage implementation |
| `internal/pipeline/think_stage.go` | ThinkStage implementation |
| `internal/pipeline/prune_stage.go` | PruneStage + inline MemoryFlush |
| `internal/pipeline/tool_stage.go` | ToolStage implementation |
| `internal/pipeline/observe_stage.go` | ObserveStage implementation |
| `internal/pipeline/checkpoint_stage.go` | CheckpointStage implementation |
| `internal/pipeline/finalize_stage.go` | FinalizeStage implementation |

## Design Steps

1. Define Stage interface + StageResult enum
2. Design RunState with typed substates (map current fields)
3. Define MessageBuffer with append/replace/flush semantics
4. Design each stage's Execute signature and data flow
5. Design Pipeline orchestrator with setup/iteration/finalize phases
6. Map error propagation rules (setup fatal, iteration -> finalize, finalize logged)
7. Design MemoryFlushStage inline integration with PruneStage
8. Verify all 7 exit conditions map to StageResult values

## Todo List

- [x] A1: Stage interface + StageResult + StageWithResult — `internal/pipeline/stage.go`
- [x] B1: RunState top-level struct — `internal/pipeline/run_state.go`
- [x] B2: MessageBuffer with thread-safe read, owning-stage write — `internal/pipeline/run_state.go`
- [x] B3: ContextState substate — `internal/pipeline/substates.go`
- [x] B4: ThinkState substate — `internal/pipeline/substates.go`
- [x] B5: PruneState substate — `internal/pipeline/substates.go`
- [x] B6: ToolState substate — `internal/pipeline/substates.go`
- [x] B7: ObserveState substate — `internal/pipeline/substates.go`
- [x] B8: CompactState substate — `internal/pipeline/substates.go`
- [x] B9: EvolutionState substate — `internal/pipeline/substates.go`
- [x] C1: ContextStage design — documented in phase (impl deferred)
- [x] C2: ThinkStage design — documented in phase (impl deferred)
- [x] C3: PruneStage design — documented in phase (impl deferred)
- [x] C4: ToolStage design — documented in phase (impl deferred)
- [x] C5: ObserveStage design — documented in phase (impl deferred)
- [x] C6: CheckpointStage design — documented in phase (impl deferred)
- [x] C7: MemoryFlushStage design — documented in phase (impl deferred)
- [x] C8: FinalizeStage design — documented in phase (impl deferred)
- [x] D1: Pipeline orchestrator with 3-phase execution — `internal/pipeline/pipeline.go`
- [x] D2: PipelineDeps dependency bundle — via NewPipeline() params
- [x] D3: Error propagation rules — setup fatal, iteration fatal+finalize, finalize logged
- [x] E1: Verify all 7 exit conditions preserved — documented in phase design
- [x] E2: Map current runState fields to substates — documented in phase design

## Success Criteria

- Every line of current `loop.go` maps to exactly one stage
- `runState` fields fully accounted for in substates (no orphans)
- Pipeline.Run() produces same RunResult semantics as current Loop.Run()
- Each stage can be tested with a constructed RunState (no Loop struct dependency)
- MemoryFlushStage integrated with PruneStage without circular dependency
- InjectCh drain preserved at both injection points

## Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|------------|
| Subtle behavior change from stage ordering | HIGH | Integration tests comparing v2 vs v3 output for same inputs. Run existing test suite. |
| InjectCh timing sensitivity (Point A vs Point B) | MEDIUM | ObserveStage handles both points explicitly. Document injection semantics. |
| MemoryFlushStage inline call breaks stage isolation | LOW | Acceptable trade-off: flush MUST happen before compaction. Document as intentional coupling. |
| RunState too large (sum of substates) | LOW | Substates are small structs. Total < 500 bytes. No heap pressure. |
| Stage interface too simple for complex stages | LOW | StageWithResult extension handles flow control. Further extension possible via StageWithCleanup etc. |

## Security Considerations

- ToolStage must preserve all existing security checks: policy engine, shell deny patterns, workspace restriction
- ContextStage must preserve input guard scanning (injection pattern detection)
- CheckpointStage flushes to session store with existing tenant scoping
- No new attack surface introduced by pipeline refactor

## Next Steps

- Phase 3 (Memory & KG) adds MemoryFlushStage consumer for `session.completed` events
- Phase 4 (System Integration) adds template-based system prompt in ContextStage
- Phase 5 (Orchestration) may add DelegateStage or modify ToolStage for delegation
