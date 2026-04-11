# CP-00: Current State Analysis

**Status**: Reference only — no code changes required.

This document maps the existing GoClaw architecture to the 18 patterns
from "Giai phau mot Agentic Operating System" so you know exactly
where each upgrade hooks in.

---

## Pipeline Architecture (internal/pipeline/)

### Stage Interface (stage.go)
```go
type StageResult int
const (
    Continue  StageResult = iota  // proceed to next stage
    BreakLoop                     // exit iteration (normal)
    AbortRun                      // abort entire run (error)
)

type Stage interface {
    Name() string
    Execute(ctx context.Context, state *RunState) error
}

type StageWithResult interface {
    Stage
    Result() StageResult
}
```

### Default Pipeline (pipeline.go:NewDefaultPipeline)
```
Setup:     [ContextStage]
Iteration: [ThinkStage → PruneStage → ToolStage → ObserveStage → CheckpointStage]
Finalize:  [FinalizeStage]
```

### Shared State (run_state.go + substates.go)
All stages share `*RunState`:
- `Messages` — MessageBuffer (history + pending)
- `Context` — EffectiveContextWindow, OverheadTokens
- `Think` — LastResponse, usage tracking
- `Prune` — HistoryBudget, HistoryTokens, flags
- `Tool` — TotalToolCalls, LoopKilled, ReadOnlyStreak
- `Observe` — BlockReplies, InjectedMessages
- `Compact` — MemoryFlushedThisCycle
- `Evolution` — SelfEvolve tracking

---

## Tool System (internal/tools/)

### Tool Interface (not in a single types.go — spread across files)
```go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]any
    Execute(ctx context.Context, args map[string]any) *Result
}
```

### Key files
| File | Purpose |
|------|---------|
| `capability.go` | ToolCapability enum: CapReadOnly, CapMutating, CapAsync, CapMCPBridged |
| `context_keys.go` | Context injection: WithToolChannel, WithToolSandboxKey, etc. |
| `context_file_interceptor.go` | Wraps reads/writes to agent context files |
| `announce_queue.go` | Async announcement queue for tools |

### Concurrency Model
- Tools are **immutable singletons** — thread-safe via context injection
- `Registry.ExecuteWithContext()` injects per-call values into context
- ToolStage parallel path: `ExecuteToolRaw` (parallel I/O) → `ProcessToolResult` (sequential mutation)
- **Gap**: No per-invocation `IsConcurrencySafe(args)` — only static `CapReadOnly` metadata

### Tool Metadata (capability.go:inferMetadata)
```
read-only:  read_file, list_files, read_image, memory_search, web_search, web_fetch, ...
mutating:   everything else (default)
async:      spawn
```

---

## Context Defense (internal/pipeline/prune_stage.go)

### Current 2-phase pruning
```
Phase 1 (70% budget): PruneMessages callback — soft trim old messages
Phase 2 (100% budget): Memory flush + LLM compaction
AbortRun if still over budget after both phases
```

### Budget calculation
```go
budget = contextWindow - overheadTokens - maxTokens - reserveTokens
```

### Gaps vs Claude Code's 5 layers
| Layer | Claude Code | GoClaw |
|-------|------------|--------|
| 1. Tool result truncation | Per-tool maxResultSizeChars | `tool_result_truncation.go` in agent/ — partial |
| 2. Microcompact | Remove stale tool results by ID | Not present |
| 3. Auto-compact | LLM summarize → boundary message | PruneStage Phase 2 — partial (no boundary msg) |
| 4. Reactive compact | On 413 error, emergency compact | Not present |
| 5. Context collapse | Read-time projection | Not present |

---

## Agent System (internal/agent/)

### Key files
| File | Purpose |
|------|---------|
| `loop.go` | V3 pipeline adapter (all agents use pipeline) |
| `loop_run.go` | Main `Run()` entrypoint |
| `loop_pipeline_adapter.go` | Bridges old loop callbacks to pipeline deps |
| `loop_pipeline_callbacks.go` | Pipeline callback implementations |
| `loop_pipeline_tool_callbacks.go` | Tool execution callbacks |
| `router.go` | Agent registry with TTL cache (10 min) |
| `systemprompt.go` | 4-mode system prompt builder |
| `tool_result_truncation.go` | Basic truncation (agent-level) |
| `pruning.go` | History pruning helpers |
| `orchestration_mode.go` | Coordinator mode detection |

### Agent Router
- Cache key includes tenant ID → multi-tenant isolation
- TTL 10 minutes, lazy resolver from DB
- ActiveRuns + SessionRuns tracking via sync.Map

---

## Scheduler (internal/scheduler/)

### Lane Architecture
```
LaneMain     = "main"      (concurrency 30)
LaneSubagent = "subagent"  (concurrency 50)
LaneTeam     = "team"      (concurrency 100)
LaneCron     = "cron"      (concurrency 30)
```
- Per-session serialization via SessionQueue
- Semaphore-based concurrency control

---

## Permission System (internal/permissions/policy.go)

### 5-Layer Permission
```
1. Gateway Auth (token/password)
2. Global Tool Policy (tools.allow/deny/profile)
3. Per-Agent Policy (agents.list[].tools)
4. Per-Channel/Group Policy
5. Owner-Only Tools (senderIsOwner)
```

### Roles
```
owner > admin > operator > viewer
```

### Gaps vs Claude Code
- No bash command AST analysis
- No dangerous patterns detection (curl | bash, etc.)
- No denial tracking (anti permission-fatigue)
- No 7 permission modes (only RBAC roles)

---

## Memory System (internal/memory/ + internal/consolidation/)

### 3-Tier Memory
```
L0 (Working)  — AutoInjector: inject relevant memories into system prompt
L1 (Episodic) — EpisodicWorker: session summaries via domain event bus
L2 (Semantic) — SemanticWorker → DreamingWorker → KnowledgeGraph
```

### AutoInjector
- Max 5 entries, 200 token budget, 0.3 threshold
- Hybrid search: FTS + pgvector (standard) or FTS5 (lite)
- Injected once at start of each run

---

## Skills System (internal/skills/)

### Current Architecture
- 5-tier hierarchy: workspace > project agent > personal > global > builtin
- BM25 search + optional vector embeddings
- SKILL.md format with frontmatter
- DB-managed versioned skills

### Gaps
- No path-based conditional activation (`paths: ["**/migrations/**"]`)
- No shell-in-prompt (`!`backtick commands``)
- No directory walk discovery (walk up tree for `.goclaw/skills/`)

---

## Provider System (internal/providers/)

### Supported Providers
Anthropic, OpenAI (compatible), DashScope, Claude CLI, ACP, Codex

### Streaming
- SSE scanner shared across providers
- `ChatStream(ctx, req, onChunk func(StreamChunk))`
- Streaming is provider-level, not tool-level

### Retry
- `RetryDo[T]()` with exponential backoff
- Retryable: 429, 500, 502, 503, 504, connection errors
- **Gap**: Single-tier only. No escalate output → inject message → fallback model

---

## Files Summary for Quick Reference

```
internal/pipeline/
  stage.go              ← Stage/StageWithResult interfaces
  pipeline.go           ← NewDefaultPipeline orchestrator
  run_state.go          ← RunState shared mutable container
  substates.go          ← Per-stage substates
  think_stage.go        ← LLM invocation
  prune_stage.go        ← 2-phase context pruning
  tool_stage.go         ← Tool execution (parallel+serial)
  observe_stage.go      ← Result aggregation
  context_stage.go      ← Context injection
  checkpoint_stage.go   ← State persistence
  finalize_stage.go     ← Post-run cleanup
  memory_flush_stage.go ← Memory extraction
  message_buffer.go     ← Message container
  deps.go               ← PipelineDeps callback struct

internal/tools/
  capability.go         ← ToolCapability metadata
  context_keys.go       ← Per-call context injection

internal/agent/
  loop_run.go           ← Agent.Run() entrypoint
  router.go             ← Agent registry
  orchestration_mode.go ← Coordinator mode
  tool_result_truncation.go ← Basic truncation

internal/permissions/
  policy.go             ← PolicyEngine + RBAC

internal/skills/
  loader.go             ← Skill discovery
  search.go             ← BM25 search

internal/memory/
  auto_injector.go      ← Memory injection interface
  auto_injector_impl.go ← Implementation

internal/consolidation/
  episodic_worker.go    ← Session summaries
  semantic_worker.go    ← KG extraction
  dreaming_worker.go    ← Background synthesis

internal/providers/
  retry.go              ← RetryDo with backoff
  middleware.go         ← Request middleware chain
```
