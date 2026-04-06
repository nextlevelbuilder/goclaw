# Phase 1: Foundation Interfaces

## Context Links

- Brainstorm: `plans/reports/brainstorm-260406-1059-goclaw-v3-core-redesign.md` (decisions 5, 8, 14, 10)
- Architecture: `plans/reports/Explore-260406-thorough-v3-architecture.md` (sections 1, 2, 6, 7)
- Loop deep dive: `plans/reports/Explore-260406-GoClaw-Agent-Loop-v3-Deep-Dive.md` (section 2, 3)
- Workspace forcing: `plans/reports/Explore-260406-workspace-forcing.md`

## Overview

- **Priority:** P1 (all other phases depend on these)
- **Status:** Complete
- **Effort:** 8h design

4 foundational components with zero cross-dependencies. Can be designed and implemented in parallel. Every subsequent phase (pipeline, memory, integration) depends on at least one of these.

## Key Insights

- Token counting currently `chars/4` estimate -- drives ALL compaction/pruning decisions. Replacing with tiktoken-go is highest-ROI change
- Workspace resolution has 5 identified confusion sources (see workspace forcing report). Single immutable WorkspaceContext eliminates dual-key confusion
- EventBus currently `map[string]any` payloads, string topic constants. V3 needs typed events for consolidation pipeline
- Provider interface (`providers.Provider`) is functional but loop handles provider quirks directly. Adapter pattern isolates quirks

---

## A. TokenCounter Interface

### Interface Contract

```go
// Package: internal/tokencount

// TokenCounter provides accurate per-model token counting.
// Replaces chars/4 estimation used in compaction + pruning.
type TokenCounter interface {
    // Count returns token count for raw text using model's tokenizer.
    Count(model string, text string) int

    // CountMessages returns token count for a message list,
    // including per-message overhead (role tokens, separators).
    CountMessages(model string, msgs []providers.Message) int

    // ModelContextWindow returns max context tokens for a model.
    // Falls back to provider default if model unknown.
    ModelContextWindow(model string) int
}

// TokenizerID identifies which tokenizer a model uses.
type TokenizerID string

const (
    TokenizerCL100K TokenizerID = "cl100k_base" // Claude, GPT-3.5/4
    TokenizerO200K  TokenizerID = "o200k_base"  // GPT-4o, GPT-5
    TokenizerFallback TokenizerID = "fallback"   // chars/4
)

// ModelRegistry maps model name prefixes to tokenizer + context window.
type ModelInfo struct {
    TokenizerID   TokenizerID
    ContextWindow int // default context window for this model
}

// DefaultRegistry provides built-in model mappings.
// Extend at runtime via RegisterModel().
var DefaultRegistry = map[string]ModelInfo{
    "claude-":        {TokenizerCL100K, 200_000},
    "gpt-4o":         {TokenizerO200K, 128_000},
    "gpt-4":          {TokenizerCL100K, 128_000},
    "gpt-5":          {TokenizerO200K, 1_000_000},
    "qwen-":          {TokenizerCL100K, 128_000}, // approx
    "deepseek-":      {TokenizerCL100K, 128_000}, // approx
}
```

### Caching Strategy

```go
// cachedCounter wraps a tokenizer with per-message hash cache.
// Key: SHA-256(model + role + content + tool_call_ids)
// Value: token count (int)
// Eviction: LRU, 10K entries (typical session < 500 messages)
type cachedCounter struct {
    cache *lru.Cache[string, int] // hashicorp/golang-lru/v2
    mu    sync.RWMutex
}
```

Cache avoids re-counting unchanged messages during mid-loop re-estimation. Only new/modified messages hit tokenizer.

### Per-Message Overhead

```
Per message: +4 tokens (role marker + separators)
System message: +4 tokens additional
Tool calls: name + JSON args counted as text
Tool results: content text counted
Images: model-specific (Claude: tile-based, GPT: similar)
```

### Data Flow

```
Pipeline stage needs token count
  -> TokenCounter.CountMessages(model, messages)
     -> For each message:
        -> Hash(model, msg) -> check cache
        -> Cache miss: tokenizer.Encode(text) -> count -> store
     -> Sum + per-message overhead
     -> Return total
```

### Affected Files (current codebase)

| File | What changes |
|------|-------------|
| `internal/agent/pruning.go` | Replace `EstimateHistoryTokens()` (chars/4) with `TokenCounter.CountMessages()` |
| `internal/agent/loop_compact.go` | Replace token estimation in compaction trigger |
| `internal/agent/loop.go:352-411` | Mid-loop budget calculation uses token counter |
| `internal/agent/loop_history.go:624-699` | `maybeSummarize()` token estimation |
| `internal/agent/loop_types.go` | `runState.overheadTokens` recalibration logic |

### Breaking Changes

- `EstimateHistoryTokens()`, `EstimateTokensWithCalibration()` replaced
- `overheadCalibrated` field in `runState` removed (overhead now computed accurately upfront)
- `pruning.go` constants (`softTrimThreshold`, `hardClearThreshold`) stay but use accurate counts

---

## B. WorkspaceContext

### Interface Contract

```go
// Package: internal/workspace

// Scope defines workspace access boundary.
type Scope string

const (
    ScopePersonal Scope = "personal"  // single user, isolated
    ScopeTeam     Scope = "team"      // team context, shared or isolated
    ScopeDelegate Scope = "delegate"  // delegated task, scoped access
)

// WorkspaceContext is resolved ONCE at run start, immutable for the entire run.
// Eliminates dual ctxWorkspace + ctxTeamWorkspace confusion.
type WorkspaceContext struct {
    // ActivePath is THE path for all file operations (read/write/list/exec).
    // No other workspace path is accessible unless in ReadOnlyPaths.
    ActivePath string

    // Scope describes the access boundary type.
    Scope Scope

    // ReadOnlyPaths are additional paths the agent can read but NOT write.
    // Used for: delegator exports, shared reference dirs.
    ReadOnlyPaths []string

    // SharedPath is the shared delegate area (read/write by both delegator + delegatee).
    // nil when not in delegation context.
    SharedPath *string

    // TeamPath is the team workspace root (nil if not in team context).
    TeamPath *string

    // MemoryScope determines memory isolation.
    // Defaults to workspace scope. "shared" = all users in agent see same memory.
    MemoryScope string

    // KGScope determines knowledge graph isolation.
    // Defaults to workspace scope. "shared" = all users in agent see same KG.
    KGScope string

    // OwnerID identifies who owns this workspace context (user ID or chat ID).
    OwnerID string

    // EnforcementLabel is injected into system prompt verbatim.
    // e.g. "ENFORCED -- all file paths resolve here, absolute paths rejected"
    EnforcementLabel string
}

// Resolver produces a WorkspaceContext from request parameters.
// Called once at ContextStage. Result is immutable.
type Resolver interface {
    Resolve(ctx context.Context, params ResolveParams) (*WorkspaceContext, error)
}

// ResolveParams captures all inputs needed to determine workspace.
type ResolveParams struct {
    AgentID     string
    AgentType   string // "open" | "predefined"
    UserID      string
    ChatID      string
    TenantID    string
    PeerKind    string // "direct" | "group"
    TeamID      *string
    TeamConfig  *TeamWorkspaceConfig // from teams table
    DelegateCtx *DelegateContext     // from delegation task
    BaseDir     string              // global workspace root
    Sharing     *store.WorkspaceSharingConfig
}

// TeamWorkspaceConfig maps to team.settings JSON.
type TeamWorkspaceConfig struct {
    SharedWorkspace bool   `json:"shared_workspace"`
    WorkspacePath   string `json:"workspace_path,omitempty"`
}

// DelegateContext carries delegation-specific workspace overrides.
type DelegateContext struct {
    LinkID       string
    SharedPath   string
    ExportPaths  []string // read-only exports from delegator
}
```

### Resolution Rules

```
1. Solo DM, not shared:
   ActivePath = {baseDir}/{tenantID}/{agentID}/{userID}
   Scope = personal

2. Solo DM, shared workspace:
   ActivePath = {baseDir}/{tenantID}/{agentID}
   Scope = personal (shared files, memory still per-user unless overridden)

3. Team dispatch (member executing task):
   ActivePath = {baseDir}/{tenantID}/teams/{teamID}/{chatID}
   Scope = team
   TeamPath = &ActivePath

4. Team inbound (leader receiving message):
   ActivePath = {baseDir}/{tenantID}/{agentID}/{userID}
   Scope = personal
   TeamPath = &teamWorkspacePath (read-only reference)

5. Delegate:
   ActivePath = own workspace
   Scope = delegate
   SharedPath = &sharedDelegatePath
   ReadOnlyPaths = delegator exports

6. Group chat:
   ActivePath = {baseDir}/{tenantID}/{agentID}/{chatID}
   Scope = personal
```

### System Prompt Template Section

```
## Workspace

Active workspace: {{.ActivePath}} ({{.Scope}})
{{if .EnforcementLabel}}[{{.EnforcementLabel}}]{{end}}
{{if .TeamPath}}Team workspace: {{.TeamPath}} (accessible via team file tools){{end}}
{{if .ReadOnlyPaths}}Read-only paths: {{range .ReadOnlyPaths}}- {{.}}
{{end}}{{end}}

Context files (SOUL.md, USER.md, MEMORY.md): managed by system, read/write intercepted.
{{if eq .Scope "team"}}This is your active workspace. Personal workspace not accessible during this task.{{end}}
```

### Affected Files

| File | What changes |
|------|-------------|
| `internal/agent/loop_context.go:110-176` | Replace workspace resolution with `Resolver.Resolve()` |
| `internal/tools/context_keys.go` | Remove `ctxWorkspace`, `ctxTeamWorkspace`, `effectiveRestrict()` |
| `internal/tools/filesystem.go:308-340` | `resolvePathWithAllowed()` reads from `WorkspaceContext` |
| `internal/tools/filesystem_write.go` | Same |
| `internal/tools/shell.go:243-267` | Working dir from `WorkspaceContext.ActivePath` |
| `internal/agent/systemprompt.go:498-518` | Replace workspace section with template |
| `internal/agent/systemprompt_sections.go:503-520` | Merge team workspace section into workspace template |

### Breaking Changes

- `ctxWorkspace` / `ctxTeamWorkspace` context keys removed
- `effectiveRestrict()` removed (always true, now implicit via WorkspaceContext)
- `WithToolWorkspace()` / `WithToolTeamWorkspace()` replaced by single `WithWorkspaceContext()`
- `tools.WithRestrictToWorkspace()` removed (enforcement = default)

---

## C. EventBus Redesign

### Interface Contract

```go
// Package: internal/eventbus (new package, replaces internal/bus for domain events)
// Note: internal/bus MessageBus retained for channel message routing (InboundMessage/OutboundMessage).

// DomainEvent is a typed event with metadata for consolidation pipeline.
type DomainEvent struct {
    ID        string    // UUID v7 for ordering
    Type      EventType
    SourceID  string    // dedup key (e.g. session key, run ID)
    TenantID  string
    AgentID   string
    UserID    string
    Timestamp time.Time
    Payload   any       // typed per EventType (see below)
}

// EventType identifies the event category.
type EventType string

const (
    EventSessionCompleted EventType = "session.completed"
    EventEpisodicCreated  EventType = "episodic.created"
    EventEntityUpserted   EventType = "entity.upserted"
    EventMemoryLint       EventType = "memory.lint"
    EventRunCompleted     EventType = "run.completed"
    EventToolExecuted     EventType = "tool.executed"
)

// Typed payloads -- one per EventType.

type SessionCompletedPayload struct {
    SessionKey   string
    MessageCount int
    TokensUsed   int
    Summary      string // compaction summary if available
}

type EpisodicCreatedPayload struct {
    EpisodicID  string
    SessionKey  string
    Summary     string
    KeyEntities []string // entity names for downstream extraction
}

type EntityUpsertedPayload struct {
    EntityIDs []string // newly upserted entity UUIDs
}

type MemoryLintPayload struct {
    Scope string // "agent" or "user"
}

type RunCompletedPayload struct {
    RunID      string
    Iterations int
    TokensUsed int
    ToolCalls  int
    LoopKilled bool
}

type ToolExecutedPayload struct {
    ToolName  string
    Duration  time.Duration
    Success   bool
    ReadOnly  bool
}

// DomainEventBus manages typed event publishing + worker subscriptions.
type DomainEventBus interface {
    // Publish enqueues an event for async processing.
    // Non-blocking: returns immediately.
    Publish(event DomainEvent)

    // Subscribe registers a worker for a specific event type.
    // handler called in worker pool goroutine.
    // Returns unsubscribe function.
    Subscribe(eventType EventType, handler DomainEventHandler) func()

    // Start launches worker pool. Must be called before Publish.
    Start(ctx context.Context)

    // Drain waits for all queued events to be processed. For graceful shutdown.
    Drain(timeout time.Duration) error
}

type DomainEventHandler func(ctx context.Context, event DomainEvent) error
```

### Worker Pool Design

```go
// Config for the domain event bus.
type Config struct {
    QueueSize     int           // buffered channel capacity (default 1000)
    WorkerCount   int           // goroutines per event type (default 2)
    RetryAttempts int           // retry on handler error (default 3)
    RetryDelay    time.Duration // backoff base (default 1s, exponential)
}
```

Lane-based concurrency: each EventType gets its own worker pool. Events of different types processed in parallel. Events of same type processed sequentially per-worker (but multiple workers can process different events of same type concurrently).

### Idempotency

Dedup by `SourceID`: events with same SourceID + EventType processed at most once. Implementation: in-memory set with TTL (1h). If event bus restarts, re-processing is safe because all handlers are idempotent.

```go
// dedup check before handler dispatch
func (b *bus) dispatch(event DomainEvent) {
    key := event.Type + ":" + event.SourceID
    if b.seen.Has(key) {
        return // already processed
    }
    b.seen.Set(key, time.Now().Add(b.dedupTTL))
    // dispatch to handler...
}
```

### Integration Points

| Producer | Event | Consumer |
|----------|-------|----------|
| Pipeline ObserveStage | `run.completed` | Metrics, tracing |
| Pipeline ObserveStage | `tool.executed` | Self-evolution metrics |
| Session close / compaction | `session.completed` | Episodic summary worker |
| Episodic worker | `episodic.created` | Semantic extraction worker |
| KG ingest | `entity.upserted` | Dedup check worker |
| Cron scheduler | `memory.lint` | Memory lint worker |

### Affected Files

| File | What changes |
|------|-------------|
| `internal/bus/types.go` | Retained for channel routing. DomainEvent types in new package |
| `internal/bus/bus.go` | Retained. New `internal/eventbus/` package alongside |
| `internal/agent/loop_run.go` | Publish `run.completed` after finalize |
| `internal/agent/loop_tools.go` | Publish `tool.executed` per tool call |
| `internal/agent/loop_history.go:624-699` | Publish `session.completed` in `maybeSummarize()` |

### Breaking Changes

- None for existing `bus.MessageBus` (retained)
- New `internal/eventbus` package added
- Agent loop gains `DomainEventBus` dependency (injected via `Loop` struct)

---

## D. ProviderAdapter Interface

### Interface Contract

```go
// Package: internal/providers (extends existing package)

// ProviderCapabilities declares what a provider supports.
// Queried by pipeline to choose code paths (streaming vs non-streaming, etc.)
type ProviderCapabilities struct {
    Streaming       bool   // supports ChatStream()
    ToolCalling     bool   // supports tools in request
    StreamWithTools bool   // can stream while tool calls are in-flight (false for DashScope)
    Thinking        bool   // supports extended thinking / reasoning
    Vision          bool   // supports image inputs
    CacheControl    bool   // supports cache_control blocks (Anthropic)
    MaxContextWindow int   // default context window for default model
    TokenizerID     string // for tokencount package mapping
}

// ProviderAdapter transforms between internal wire format and provider-specific format.
// Each provider implements this to isolate quirks from the pipeline loop.
type ProviderAdapter interface {
    // ToRequest converts internal ChatRequest to provider-specific wire bytes.
    // Handles: schema transforms, thinking passback, cache control injection.
    ToRequest(req ChatRequest) ([]byte, http.Header, error)

    // FromResponse converts provider-specific response bytes to internal ChatResponse.
    // Handles: thinking extraction, usage normalization, finish reason mapping.
    FromResponse(data []byte) (*ChatResponse, error)

    // FromStreamChunk converts a single SSE chunk to internal StreamChunk.
    // Returns nil if chunk should be skipped (keep-alive, metadata).
    FromStreamChunk(data []byte) (*StreamChunk, error)

    // Capabilities returns static capability declaration.
    Capabilities() ProviderCapabilities

    // Name returns provider identifier.
    Name() string
}

// CapabilitiesAware is optionally implemented by Provider.
// Pipeline checks this to choose code path.
type CapabilitiesAware interface {
    Capabilities() ProviderCapabilities
}
```

### Adapter Registry

```go
// AdapterRegistry maps provider names to adapter factories.
// Enables plugin-style provider registration.
type AdapterRegistry struct {
    mu       sync.RWMutex
    adapters map[string]AdapterFactory
}

type AdapterFactory func(cfg ProviderConfig) (ProviderAdapter, error)

// Register adds a provider adapter. Called at init() or runtime.
func (r *AdapterRegistry) Register(name string, factory AdapterFactory) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.adapters[name] = factory
}

// Get returns adapter for provider name. Error if not registered.
func (r *AdapterRegistry) Get(name string, cfg ProviderConfig) (ProviderAdapter, error) {
    r.mu.RLock()
    factory, ok := r.adapters[name]
    r.mu.RUnlock()
    if !ok {
        return nil, fmt.Errorf("unknown provider: %s", name)
    }
    return factory(cfg)
}
```

### What Moves to Adapter

| Currently in loop/provider | Moves to adapter |
|---------------------------|-----------------|
| `RawAssistantContent` passback (Anthropic thinking blocks) | `AnthropicAdapter.ToRequest()` |
| `ThinkingSignature` accumulation | `AnthropicAdapter.FromStreamChunk()` |
| DashScope non-streaming fallback for tool calls | `DashScopeAdapter.Capabilities().StreamWithTools = false` |
| Codex `Phase` field injection | `CodexAdapter.ToRequest()` / `FromResponse()` |
| Schema normalization (tool definitions → provider format) | Each adapter's `ToRequest()` |
| Usage normalization (different field names) | Each adapter's `FromResponse()` |

### Pipeline Usage

```
ThinkStage:
  1. caps := provider.Capabilities() // if CapabilitiesAware
  2. If caps.StreamWithTools == false && len(tools) > 0:
     -> use Chat() not ChatStream()
  3. If caps.Thinking:
     -> inject thinking options
  4. Call Chat/ChatStream as before (Provider interface unchanged)
  5. Provider internally uses its adapter for wire transforms
```

Important: `providers.Provider` interface UNCHANGED. `ProviderAdapter` is internal to each provider implementation. Pipeline queries capabilities via optional `CapabilitiesAware` interface on the provider.

### Affected Files

| File | What changes |
|------|-------------|
| `internal/providers/types.go` | Add `ProviderCapabilities`, `CapabilitiesAware` |
| `internal/providers/anthropic.go` | Extract wire transforms to `AnthropicAdapter` |
| `internal/providers/openai.go` | Extract wire transforms to `OpenAIAdapter` |
| `internal/providers/dashscope.go` | `StreamWithTools = false` in capabilities |
| `internal/providers/codex.go` | Phase handling in adapter |
| `internal/agent/loop.go:217-323` | ThinkStage queries capabilities instead of provider-type checks |

### Breaking Changes

- `ThinkingCapable` interface superseded by `CapabilitiesAware` (backward compatible -- both can coexist during transition)
- Provider-specific branching in loop.go removed (replaced by capability checks)
- No external API changes

---

## Requirements

### Functional
- F1: Token counting accurate within 5% of provider-reported usage
- F2: WorkspaceContext resolves all 6 scenarios correctly (solo DM, shared, team dispatch, team inbound, delegate, group)
- F3: DomainEventBus processes events within 100ms p99 (in-process)
- F4: Provider capabilities queryable without LLM call

### Non-Functional
- NF1: TokenCounter cache hit rate >90% in typical session (messages rarely change)
- NF2: WorkspaceContext immutable after creation (no data races)
- NF3: EventBus handles 1000 events/sec sustained (consolidation pipeline load)
- NF4: All 4 components have zero cross-dependencies (can implement in parallel)

## Design Steps

1. Define `TokenCounter` interface + `ModelRegistry` mapping table
2. Implement `cachedCounter` with tiktoken-go + LRU cache
3. Define `WorkspaceContext` struct + `Resolver` interface
4. Implement resolver with resolution rules table
5. Define `DomainEvent` types + `DomainEventBus` interface
6. Implement worker pool with lane-based concurrency + dedup
7. Define `ProviderCapabilities` + `CapabilitiesAware` interface
8. Define `AdapterRegistry` for plugin registration

## Todo List

- [x] A1: TokenCounter interface definition — `internal/tokencount/token_counter.go`
- [x] A2: ModelRegistry with prefix-based mapping — `internal/tokencount/token_counter.go`
- [x] A3: Per-message hash cache design — documented in phase, cache impl deferred to implementation
- [x] A4: Fallback strategy for unknown models — `internal/tokencount/fallback_counter.go`
- [x] B1: WorkspaceContext struct definition — `internal/workspace/workspace_context.go`
- [x] B2: Resolver interface + ResolveParams — `internal/workspace/workspace_context.go`
- [x] B3: Resolution rules for all 6 scenarios — documented in phase design
- [x] B4: System prompt template section for workspace — documented in phase design
- [x] B5: Enforcement label design — EnforcementLabel field in WorkspaceContext
- [x] C1: DomainEvent type definitions — `internal/eventbus/event_types.go`
- [x] C2: Typed payload structs for each event — `internal/eventbus/event_types.go`
- [x] C3: DomainEventBus interface — `internal/eventbus/domain_event_bus.go`
- [x] C4: Worker pool config + lane-based concurrency design — `internal/eventbus/domain_event_bus.go`
- [x] C5: Idempotency / dedup strategy — documented in phase design (SourceID-based)
- [x] D1: ProviderCapabilities struct — `internal/providers/capabilities.go`
- [x] D2: CapabilitiesAware optional interface — `internal/providers/capabilities.go`
- [x] D3: AdapterRegistry for plugin registration — `internal/providers/adapter_registry.go`
- [x] D4: Document what moves from loop to each adapter — documented in phase design

## Success Criteria

- All 4 interfaces defined with complete method signatures
- Each interface has zero imports from the other 3
- WorkspaceContext resolution rules cover all 6 scenarios
- Token counting accuracy validated against Anthropic/OpenAI reported usage
- EventBus worker pool supports lane-based concurrency with configurable workers
- Provider capabilities capture all known provider quirks (DashScope, Codex, Anthropic)

## Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|------------|
| tiktoken-go tokenizer not exact match for Claude models | MEDIUM | Claude uses custom tokenizer, cl100k_base is approximation. Validate against API usage. Accept 5% error. |
| WorkspaceContext missing edge case for new workspace scenario | LOW | Resolution rules table is explicit. Add new row when new scenario appears. |
| EventBus in-process limitation (single instance, no persistence) | LOW | Sufficient for v3.0. External bus (Redis streams) can implement same interface later. |
| ProviderAdapter abstraction leaks (provider needs loop context) | MEDIUM | Keep adapter stateless. Pass all needed data via `ChatRequest` fields. |

## Security Considerations

- WorkspaceContext enforcement is security-critical: `ActivePath` must be validated (no path traversal)
- Existing `resolvePath()` security (symlink, hardlink checks) preserved -- WorkspaceContext provides the boundary, not the validation
- EventBus handlers must not leak tenant data across boundaries (TenantID in every event)
- TokenCounter is read-only, no security concerns

## Next Steps

- Phase 2 (Pipeline Loop) consumes all 4 interfaces
- Phase 3 (Memory & KG) consumes TokenCounter + DomainEventBus
- Phase 4 (System Integration) consumes WorkspaceContext + ProviderCapabilities
