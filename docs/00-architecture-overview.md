# 00 - Architecture Overview

## 1. Overview

GoClaw is an AI agent gateway written in Go. It exposes a WebSocket RPC (v3) interface and an OpenAI-compatible HTTP API for orchestrating LLM-powered agents. The system uses PostgreSQL as its storage backend with full multi-tenant isolation, per-user context files, encrypted credentials, agent delegation, teams, and LLM call tracing.

## 2. Component Diagram

```mermaid
flowchart TD
    subgraph Clients
        WS[WebSocket Clients]
        HTTP[HTTP Clients]
        TG[Telegram]
        DC[Discord]
        FS[Feishu / Lark]
        ZL[Zalo OA]
        ZLP[Zalo Personal]
        WA[WhatsApp]
        SL[Slack]
    end

    subgraph Gateway["Gateway Server"]
        WSS[WebSocket Server]
        HTTPS[HTTP API Server]
        MR[Method Router]
        RL[Rate Limiter]
        RBAC[Permission Engine]
    end

    subgraph Channels["Channel Manager"]
        CM[Channel Manager]
        PA[Pairing Service]
    end

    subgraph Core["Core Engine"]
        BUS[Message Bus]
        SCHED[Scheduler -- 4 Lanes]
        AR[Agent Router]
        LOOP[Agent Loop -- Think / Act / Observe]
        HOOKS[Hook Dispatcher -- Lifecycle Events]
    end

    subgraph Providers["LLM Providers"]
        ANTH[Anthropic -- Native HTTP + SSE]
        OAI["OpenAI-Compatible -- HTTP + SSE<br/>(OpenAI, Gemini, DeepSeek, DashScope, +8)"]
        ACP["ACP -- JSON-RPC 2.0 stdio<br/>(Claude Code, Codex, Gemini CLI)"]
    end

    subgraph Tools["Tool Registry"]
        FS_T[Filesystem]
        EXEC[Exec / Shell]
        WEB[Web Search / Fetch]
        MEM[Memory]
        SUB[Subagent]
        DEL[Delegation]
        TEAM_T[Teams]
        TTS_T[TTS]
        BROW[Browser]
        SK[Skills]
        MCP_T[MCP Bridge]
        CT[Custom Tools]
    end

    subgraph Store["Store Layer"]
        SESS[SessionStore]
        AGENT_S[AgentStore]
        PROV_S[ProviderStore]
        CRON_S[CronStore]
        MEM_S[MemoryStore]
        SKILL_S[SkillStore]
        TRACE_S[TracingStore]
        MCP_S[MCPServerStore]
        CT_S[CustomToolStore]
        TM_S[TeamStore]
    end

    WS --> WSS
    HTTP --> HTTPS
    TG & DC & FS & ZL & ZLP & WA & SL --> CM

    WSS --> MR
    HTTPS --> MR
    MR --> RL --> RBAC --> AR

    CM --> BUS
    BUS --> SCHED
    SCHED --> AR
    AR --> LOOP

    LOOP --> Providers
    LOOP --> Tools
    Tools --> Store
    LOOP --> Store
```

## 3. Module Map

| Module | Description |
|--------|-------------|
| `internal/gateway/` | WebSocket + HTTP server, client handling, method router. Decomposed: gateway_deps, gateway_http_wiring, gateway_events, gateway_lifecycle, gateway_tools_wiring |
| `internal/gateway/methods/` | RPC method handlers: chat, agents, teams, delegations, sessions, config, skills, cron, pairing, exec approval, usage, send |
| `internal/agent/` | Agent loop (think, act, observe), router, resolver, system prompt builder, sanitization, pruning, tracing, memory flush, DELEGATION.md + TEAM.md injection |
| `internal/providers/` | LLM providers: Anthropic (native HTTP + SSE), OpenAI-compatible (HTTP + SSE, 12+ providers), DashScope (Qwen), ACP (JSON-RPC 2.0 subprocess), Claude CLI, Codex, extended thinking support, retry logic. Shared SSEScanner in providers/sse_reader.go |
| `internal/providers/acp/` | ACP protocol implementation: ProcessPool (subprocess lifecycle), ToolBridge (fs/terminal), session management |
| `internal/tools/` | Tool registry, filesystem ops, exec/shell, policy engine, subagent, delegation manager, team tools, context file + memory interceptors, credential scrubbing, rate limiting, PathDenyable |
| `internal/tools/dynamic_loader.go` | Custom tool loader: LoadGlobal (startup), LoadForAgent (per-agent clone), ReloadGlobal (cache invalidation) |
| `internal/tools/dynamic_tool.go` | Custom tool executor: command template rendering, shell escaping, encrypted env vars |
| `internal/store/` | Store interfaces: SessionStore, AgentStore, ProviderStore, SkillStore, MemoryStore, CronStore, PairingStore, TracingStore, MCPServerStore, TeamStore, ChannelInstanceStore, ConfigSecretsStore. Dual-DB support via Dialect pattern |
| `internal/store/base/` | Shared store abstractions: Dialect interface, NilStr, BuildMapUpdate, BuildScopeClause, and other common helpers for both PostgreSQL and SQLite |
| `internal/store/pg/` | PostgreSQL implementations (`database/sql` + `pgx/v5`) |
| `internal/store/sqlitestore/` | SQLite implementations (`modernc.org/sqlite`) for desktop edition |
| `internal/bootstrap/` | System prompt files (AGENTS.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, BOOTSTRAP.md) + seeding + truncation |
| `internal/config/` | Config loading (JSON5) + env var overlay |
| `internal/skills/` | SKILL.md loader (5-tier hierarchy) + BM25 search + hot-reload via fsnotify |
| `internal/channels/` | Channel manager + adapters: Telegram (forum topics, STT, bot commands), Feishu/Lark (streaming cards, media), Zalo OA, Zalo Personal, Discord, WhatsApp, Slack |
| `internal/mcp/` | MCP server bridge (stdio, SSE, streamable-HTTP transports) |
| `internal/scheduler/` | Lane-based concurrency control (main, subagent, cron, team lanes) with per-session serialization. Per-edition rate limits (`MaxSubagentConcurrent`, `MaxSubagentDepth`) with tenant-scoped concurrency |
| `internal/memory/` | Memory system (pgvector hybrid search) |
| `internal/subagent/` | Subagent lifecycle: spawn, roster, task persistence (subagent_tasks table), announce queue (producer-consumer), auto-retry, per-edition rate limiting |
| `internal/permissions/` | RBAC policy engine (admin, operator, viewer roles) |
| `internal/store/pg/pairing.go` | DM/device pairing service (8-character codes, database-backed) |
| `internal/sandbox/` | Docker-based code execution sandbox |
| `internal/audio/` | Unified audio manager: 4 provider interfaces (TTS active; STT/Music/SFX stubbed/partial). Orchestrates ElevenLabs, OpenAI, Edge, MiniMax TTS providers. `internal/tts/` retained as backward-compat alias |
| `internal/tts/` | Backward-compat alias layer (24 symbols) — all pre-refactor callers compile unchanged |
| `internal/http/` | HTTP API handlers: /v1/chat/completions, /v1/agents, /v1/skills, /v1/traces, /v1/mcp, /v1/delegations, summoner |
| `internal/crypto/` | AES-256-GCM encryption for API keys |
| `internal/tracing/` | LLM call tracing (traces + spans), in-memory buffer with periodic store flush |
| `internal/tracing/otelexport/` | Optional OpenTelemetry OTLP exporter (opt-in via build tags; adds gRPC + protobuf) |
| `internal/cache/` | Caching layer for agent state and provider responses |
| `internal/bus/` | Event pub/sub message bus for inter-component communication |
| `internal/knowledgegraph/` | Knowledge graph storage and traversal |
| `internal/mcp/` | Model Context Protocol bridge/server (stdio, SSE, streamable-HTTP) |
| `internal/media/` | Media handling utilities |
| `internal/oauth/` | OAuth authentication integration |
| `internal/sessions/` | Session management and lifecycle |
| `internal/tasks/` | Task management system |
| `internal/upgrade/` | Database schema version tracking and migrations |
| `internal/pipeline/` | 8-stage pluggable agent pipeline (context → history → prompt → think → act → observe → memory → summarize) |
| `internal/orchestration/` | Orchestration primitives: BatchQueue[T] generic for result aggregation, ChildResult capture, media conversion helpers |
| `internal/eventbus/` | DomainEventBus: typed event publishing, worker pool, dedup, retry, used by consolidation workers |
| `internal/consolidation/` | Memory consolidation workers: episodic (recent facts), semantic (embeddings), dreaming (synthesis), dedup |
| `internal/tokencount/` | Token counting: tiktoken BPE counter with fallback, used by pipeline for context tracking |
| `internal/workspace/` | Workspace context resolver: 6 scenarios (agent default, team lead, team member, dispatch, subagent, cron) |
| `internal/vault/` | Knowledge Vault: wikilinks (semantic mesh), hybrid search (BM25+vector), filesystem sync, L0 auto-injection |
| `internal/channels/whatsapp/` | Native WhatsApp channel via whatsmeow (replaces WhatsApp API), QR auth, media handling |
| `internal/hooks/` | Agent lifecycle hooks: event dispatcher (sync/async), concrete handlers (command/http), matchers (regex + CEL), audit logging, edition gating, cost safeguards. Events: SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, Stop, SubagentStart/Stop. Handlers: CommandHandler (shell, Lite-only), HTTPHandler (SSRF-hardened, auth decrypt) |
| `internal/hooks/handlers/` | Concrete hook handler implementations: `CommandHandler` (exec wrapper, JSON I/O, env allowlist, edition recheck), `HTTPHandler` (SSRF-hardened HTTP client, retry-once on 5xx, no redirects, encrypted auth headers) |

---

## 3.5 Agent Hooks System

**Lifecycle hooks** allow agents to perform custom logic at key execution points. The system is event-driven (sync/async), integrated into the 8-stage pipeline, and includes audit logging, edition gating, and security safeguards.

### Event Types

| Event | Stage | Sync/Async | Purpose |
|-------|-------|-----------|---------|
| **SessionStart** | ContextStage | Async | Fires once per session (first turn); before history loading |
| **UserPromptSubmit** | ContextStage | Sync | Fires on user message arrival; blocks with hook reason or mutates input |
| **PreToolUse** | ToolStage | Sync | Fires before each tool execution; blocks tool or mutates arguments |
| **PostToolUse** | ToolStage | Async | Fires after tool result is processed; non-blocking |
| **Stop** | FinalizeStage | Async | Fires when session terminates (stop/complete/error) |
| **SubagentStart** | (Delegate tool) | Sync | Fires before delegated task spawns; blocks delegation |
| **SubagentStop** | (Domain events) | Async | Fires on delegation completion/failure; non-blocking |

### Handler Types

| Handler | Edition | Config | Semantics |
|---------|---------|--------|-----------|
| **Command** | Lite only | `cmd`, `allowedEnvVars` | Exec shell command; stdin=event JSON; stdout=decision JSON; exit 0→allow, exit 2→block; timeout→block (fail-closed) |
| **HTTP** | All | `url`, `headers` | POST event JSON to webhook URL; parse response for decision, additionalContext, updatedInput; 5xx retry once; 4xx error no-retry; SSRF-hardened with pinned IP |
| **Prompt** | Phase 3+ | TBD | Integrates custom prompting logic (deferred) |

### Sync vs Async

**Sync hooks** (UserPromptSubmit, PreToolUse, SubagentStart):
- Block pipeline until decision received
- Support Copy-on-Write (COW) staged mutations: `updatedInput` buffered, committed only if ALL sync hooks succeed
- Timeout ≤5s per hook; chain total ≤10s wall-time
- Rejection blocks the step (e.g., tool not executed, user message not processed)

**Async hooks** (SessionStart, PostToolUse, Stop, SubagentStop):
- Fire-and-forget via worker pool (default 16 workers, bounded queue 512)
- Non-blocking (pipeline continues immediately)
- Timeout per hook; chain budget enforced but no blocking

### Security Model

- **Edition gating**: `CommandHandler` only on Lite edition; attempts on Standard/other editions rejected (defense-in-depth)
- **SSRF hardening (HTTPHandler)**: Caller supplies net.Dialer pinning resolved IP, blocking loopback/link-local/private ranges; no HTTP redirects (CheckRedirect returns ErrUseLastResponse)
- **Auth header encryption**: `Authorization` + other sensitive fields in cfg.Config["headers"] encrypted at rest via AES-256-GCM; decrypted only at HTTP send-time
- **Audit logging**: All hook invocations logged to `hook_executions` table (encrypted, PII-redacted) with dedup_key for idempotency
- **Loop-depth guard (M5)**: SubagentStart checks recursion depth; max 3 levels prevents infinite delegation chains
- **Circuit breaker**: Auto-disables hook after 3 consecutive failures in recent window (C4 mitigation)

### Pipeline Integration

Dispatcher wired into `PipelineDeps.HookDispatcher` (nil-safe noop fallback). All 8 stages support hook firing with zero overhead when dispatcher not configured. Example sync hook flow:

```
1. ContextStage: UserPromptSubmit → Hooks.Fire(sync)
2. Sync hooks buffer mutations (updatedInput)
3. All hooks succeed? → Commit mutations, proceed
4. Any hook rejects? → Discard buffer, abort pipeline, user sees reason
```

---

## 4. Multi-Tenant Identity Model

GoClaw uses the **Identity Propagation** pattern (also known as **Trusted Subsystem**). It does not implement authentication or authorization — instead, it trusts the upstream service that authenticates with the gateway token to provide accurate user identity.

```mermaid
flowchart LR
    subgraph "Upstream Service (trusted)"
        AUTH["Authenticate end-user"]
        HDR["Set X-GoClaw-User-Id header<br/>or user_id in WS connect"]
    end

    subgraph "GoClaw Gateway"
        EXTRACT["Extract user_id<br/>(opaque, VARCHAR 255)"]
        CTX["store.WithUserID(ctx)"]
        SCOPE["Per-user scoping:<br/>sessions, context files,<br/>memory, traces, agent shares"]
    end

    AUTH --> HDR
    HDR --> EXTRACT
    EXTRACT --> CTX
    CTX --> SCOPE
```

### Identity Flow

| Entry Point | How user_id is provided | Enforcement |
|-------------|------------------------|-------------|
| HTTP API | `X-GoClaw-User-Id` header | Required |
| WebSocket | `user_id` field in `connect` handshake | Required |
| Channels | Derived from platform sender ID (e.g., Telegram user ID) | Automatic |

### Compound User ID Convention

The `user_id` field is **opaque** to GoClaw — it does not interpret or validate the format. For multi-tenant deployments, the recommended convention is:

```
tenant.{tenantId}.user.{userId}
```

This hierarchical format ensures natural isolation between tenants. Since `user_id` is used as a scoping key across all per-user tables (`user_context_files`, `user_agent_profiles`, `user_agent_overrides`, `agent_shares`, `sessions`, `traces`), the compound format guarantees that users from different tenants cannot access each other's data.

### Where user_id is used

| Component | Usage |
|-----------|-------|
| Session keys | `agent:{agentId}:{channel}:direct:{peerId}` — peerId derived from user_id |
| Context files | `user_context_files` table scoped by `(agent_id, user_id)` |
| User profiles | `user_agent_profiles` table — first/last seen, workspace |
| User overrides | `user_agent_overrides` — per-user provider/model preferences |
| Agent shares | `agent_shares` table — user-level access control |
| Memory | Per-user memory entries via context propagation |
| Traces | `traces` table includes `user_id` for filtering |
| MCP grants | `mcp_user_grants` — per-user MCP server access |
| Skills grants | `skill_user_grants` — per-user skill access |

---

## 6. Gateway Startup Sequence

```mermaid
sequenceDiagram
    participant CLI as CLI (cmd/root.go)
    participant GW as runGateway()
    participant PG as PostgreSQL
    participant Engine as Core Engine

    CLI->>GW: 1. Parse CLI flags + load config
    GW->>GW: 2. Resolve workspace + data dirs
    GW->>GW: 3. Create Message Bus

    GW->>PG: 4. Connect to Postgres (pg.NewPGStores)
    PG-->>GW: PG stores created
    GW->>GW: 5. Start tracing collector
    GW->>PG: 6. Register providers from DB
    GW->>PG: 7. Wire embedding provider to PGMemoryStore
    GW->>PG: 8. Backfill memory embeddings (background)

    GW->>GW: 9. Register config-based providers
    GW->>GW: 10. Create tool registry (filesystem, exec, web, memory, browser, TTS, subagent, MCP)
    GW->>GW: 11. Load bootstrap files (DB)
    GW->>GW: 12. Create skills loader + register skill_search tool
    GW->>GW: 13. Wire skill embeddings

    GW->>GW: 14. Create agents lazily (set ManagedResolver)
    GW->>GW: 15. wireManagedExtras (interceptors, cache subscribers)
    GW->>GW: 16. Wire managed HTTP handlers (agents, skills, traces, MCP)

    GW->>Engine: 17. Create gateway server (WS + HTTP)
    GW->>Engine: 18. Register RPC methods
    GW->>Engine: 19. Register + start channels (Telegram, Discord, Feishu, Zalo, WhatsApp)
    GW->>Engine: 20. Start cron, scheduler (4 lanes)
    GW->>Engine: 21. Start skills watcher + inbound consumer
    GW->>Engine: 22. Listen on host:port
```

---

## 7. Database Wiring

The `wireManagedExtras()` function in `cmd/gateway_managed.go` wires multi-tenant components:

```mermaid
flowchart TD
    W1["1. ContextFileInterceptor<br/>Routes read_file / write_file to DB"] --> W2
    W2["2. User Seeding Callback<br/>Seeds per-user context files on first chat"] --> W3
    W3["3. Context File Loader<br/>Loads per-user vs agent-level files by agent_type"] --> W4
    W4["4. ManagedResolver<br/>Lazy-creates agent Loops from DB on cache miss"] --> W5
    W5["5. Virtual FS Interceptors<br/>Wire interceptors on read_file + write_file + memory tools"] --> W6
    W6["6. Memory Store Wiring<br/>Wire PGMemoryStore on memory_search + memory_get tools"] --> W7
    W7["7. Cache Invalidation Subscribers<br/>Subscribe to MessageBus events"] --> W8
    W8["8. Delegation Tools<br/>DelegateManager + agent links"] --> W9
    W9["9. Team Tools<br/>team_tasks + team_message + team auto-linking"] --> W10
    W10["10. Team Mailbox<br/>team_message tool for peer communication"]
```

### Cache Invalidation Events

| Event | Subscriber | Action |
|-------|-----------|--------|
| `cache:bootstrap` | ContextFileInterceptor | `InvalidateAgent()` or `InvalidateAll()` |
| `cache:agent` | AgentRouter | `InvalidateAgent()` -- forces re-resolve from DB |
| `cache:skills` | SkillStore | `BumpVersion()` |
| `cache:cron` | CronStore | `InvalidateCache()` |
| `cache:custom_tools` | DynamicToolLoader | `ReloadGlobal()` + `AgentRouter.InvalidateAll()` |

---

## 8. Scheduler Lanes

The scheduler uses a lane-based concurrency model. Each lane is a named worker pool with a bounded semaphore. Per-session queues control concurrency within each session.

```mermaid
flowchart TD
    subgraph Main["Lane: main (concurrency 2)"]
        M1[Channel messages]
        M2[WebSocket requests]
    end

    subgraph Sub["Lane: subagent (concurrency 4)"]
        S1[Subagent executions]
    end

    subgraph Del["Lane: team (concurrency 100)"]
        D1[Delegation executions]
    end

    subgraph Cron["Lane: cron (concurrency 1)"]
        C1[Cron job executions]
    end

    Main --> SEM1[Semaphore]
    Sub --> SEM2[Semaphore]
    Del --> SEM3[Semaphore]
    Cron --> SEM4[Semaphore]

    SEM1 --> Q[Per-Session Queue]
    SEM2 --> Q
    SEM3 --> Q
    SEM4 --> Q

    Q --> AGENT[Agent Loop]
```

### Lane Defaults

| Lane | Concurrency | Env Override | Purpose |
|------|:-----------:|-------------|---------|
| `main` | 30 | `GOCLAW_LANE_MAIN` | Primary user chat sessions |
| `subagent` | 50 | `GOCLAW_LANE_SUBAGENT` | Spawned subagents |
| `team` | 100 | `GOCLAW_LANE_TEAM` | Agent team/delegation executions |
| `cron` | 30 | `GOCLAW_LANE_CRON` | Scheduled cron jobs |

### Session Queue Concurrency

Per-session queues now support configurable `maxConcurrent`:
- **DMs**: `maxConcurrent = 1` (single-threaded per user)
- **Groups**: `maxConcurrent = 3` (multiple concurrent responses)
- **Adaptive throttle**: When session history exceeds 60% of context window, concurrency drops to 1

### Queue Modes

| Mode | Behavior |
|------|----------|
| `queue` | FIFO -- new messages wait until the current run completes |
| `followup` | Merges incoming message into the pending queue as a follow-up |
| `interrupt` | Cancels the active run and replaces it with the new message |

Default queue config: capacity 10, drop policy `old` (drops oldest on overflow), debounce 800ms.

### /stop and /stopall

- `/stop` -- Cancel the oldest running task (others keep going)
- `/stopall` -- Cancel all running tasks + drain the queue

Both are intercepted before the debouncer to avoid being merged with normal messages.

---

## 9. Graceful Shutdown

When the process receives SIGINT or SIGTERM:

1. Broadcast `shutdown` event to all connected WebSocket clients.
2. `channelMgr.StopAll()` -- stop all channel adapters.
3. `cronStore.Stop()` -- stop cron scheduler.
4. `sandboxMgr.Stop()` + `ReleaseAll()` -- release Docker containers.
6. `cancel()` -- cancel root context, propagating to consumer + scheduler.
7. Deferred cleanup: flush tracing collector, close memory store, close browser manager, stop scheduler lanes.
8. HTTP server shutdown with a **5-second timeout** (`context.WithTimeout`).

---

## 10. Config System

Configuration is loaded from a JSON5 file with environment variable overlay. Secrets are never persisted to the config file.

```mermaid
flowchart TD
    A{Config path?} -->|--config flag| B[CLI flag path]
    A -->|GOCLAW_CONFIG env| C[Env var path]
    A -->|default| D["config.json"]

    B & C & D --> LOAD["config.Load()"]
    LOAD --> S1["1. Set defaults"]
    S1 --> S2["2. Parse JSON5"]
    S2 --> S3["3. Env var overlay<br/>(GOCLAW_*_API_KEY)"]
    S3 --> S4["4. Apply computed defaults<br/>(context pruning, etc.)"]
    S4 --> READY[Config ready]
```

### Key Config Sections

| Section | Purpose |
|---------|---------|
| `gateway` | host, port, token, allowed_origins, rate_limit_rpm, max_message_chars |
| `agents` | defaults (provider, model, context_window) + list (per-agent overrides) |
| `tools` | profile, allow/deny lists, exec_approval, web, browser, mcp_servers, rate_limit_per_hour |
| `channels` | Per-channel: enabled, token, dm_policy, group_policy, allow_from |
| `database` | postgres_dsn read only from env var |

### Secret Handling

- Secrets exist only in env vars or `.env.local` -- never in `config.json`.
- `GOCLAW_POSTGRES_DSN` is tagged `json:"-"` and cannot be read from the config file.
- `MaskedCopy()` replaces API keys with `"***"` when returning config over WebSocket.
- `StripSecrets()` removes secrets before writing config to disk.
- Config hot-reload via `fsnotify` watcher with 300ms debounce.

---

## 11. File Reference

| Module | Path | Purpose |
|---|---|---|
| CLI & startup | `cmd/` | Cobra entry point, gateway orchestrator, DB wiring, provider registration, RPC method registration |
| Gateway server | `internal/gateway/` | WS + HTTP server, client lifecycle, method router, rate limiter |
| Config | `internal/config/` | JSON5 config loading, env overlay, channel config structs |
| Store layer | `internal/store/` | `Stores` container, `BaseModel`, `StoreConfig`, `GenNewID()` |

Use `grep` or your editor's symbol search for specific files.

---

## V3 Architecture (Wave 1 & Wave 2 - dev-v3 branch)

### Overview

V3 introduces a **pluggable 8-stage pipeline** (replacing the monolithic `runLoop`), an event-driven architecture via `DomainEventBus`, and advanced memory consolidation. The system maintains backward compatibility via a **dual-mode gate** at the loop level: agents can opt into v3 pipeline or stay on v2 monolithic loop per-agent via `other_config` JSONB.

### 8-Stage Pipeline

| Stage | Phase | Responsibility |
|-------|-------|-----------------|
| **ContextStage** | Setup (once) | Inject agent/user/workspace context, compute per-user files |
| **ThinkStage** | Iteration | Build system prompt, filter tools by policy, call LLM |
| **PruneStage** | Iteration | Context pruning (2-pass: soft trim → hard clear), run memory flush if compaction triggered |
| **ToolStage** | Iteration | Execute tool calls (parallel goroutines for multiple calls) |
| **ObserveStage** | Iteration | Process tool results, append to messages |
| **CheckpointStage** | Iteration | Track iteration state, check for loop exit conditions |
| **FinalizeStage** | Finalize (once) | Sanitize output, flush messages, update session metadata |

### Feature Flags (in `agents.other_config` JSONB)

| Flag | Key | Type | Default | Purpose |
|------|-----|------|---------|---------|
| Pipeline | `v3_pipeline_enabled` | bool | false | Use v3 pipeline instead of v2 monolithic loop |
| Memory | `v3_memory_enabled` | bool | false | Enable episodic/semantic consolidation workers via DomainEventBus |
| Retrieval | `v3_retrieval_enabled` | bool | false | Enable Knowledge Vault with wikilinks + hybrid search, L0 auto-injection |
| Evolution Metrics | `self_evolution_metrics` | bool | false | Track agent metrics for evolution suggestions (tool usage, retrieval patterns) |
| Evolution Suggestions | `self_evolution_suggestions` | bool | false | Generate and apply evolution suggestions (auto-adapt prompt/tools) |

### Memory Consolidation System

**DomainEventBus** drives asynchronous consolidation:

- **Episodic Worker** — Extracts facts from recent runs, clusters by topic, stores in `episodic_memory` table with embeddings
- **Semantic Worker** — Reprocesses episodic clusters, generates abstracted summaries, produces `semantic_memory` entries
- **Dreaming Worker** — Synthesizes novel insights from memory clusters, cross-links related memories, drives self-evolution
- **Dedup Worker** — Prevents duplicate memory entries, maintains consistency across consolidation cycles

### Workspace Context Resolver

Six distinct workspace scenarios:

1. **Agent default** — Agent workspace from config, sandbox environment
2. **Team lead** — Team workspace as default (agent coordinates tasks)
3. **Team member** — Agent workspace with team workspace accessible via `WithToolTeamWorkspace()`
4. **Dispatch** — Temporary workspace from `req.TeamWorkspace` (one-off delegated task)
5. **Subagent** — Inherited workspace from parent agent via context propagation
6. **Cron** — Workspace resolved from agent + timezone context at cron trigger time

### Knowledge Vault (Wikilinks + Hybrid Search)

- **Wikilinks**: Bidirectional semantic links (`[[related-concept]]`) automatically extracted from memories
- **Hybrid Search**: BM25 keyword search + vector similarity (pgvector) combined via RRF (reciprocal rank fusion)
- **L0 Auto-Injection**: Top-K vault entries injected into system prompt as "relevant context from vault"
- **Filesystem Sync**: Vault entries exported as `.md` files for manual editing, re-imported with change tracking

### Audio & Voice System (ElevenLabs + Streaming TTS)

**Provider architecture** (`internal/audio/`):
- **`Manager`**: Central orchestrator dispatching TTS/STT/Music/SFX requests to pluggable providers
- **`TTSProvider` interface**: Core text-to-speech contract (blocking, buffered response)
- **`StreamingTTSProvider` interface**: Optional interface for ElevenLabs `/stream` endpoint (chunked audio via `io.ReadCloser`)
- **Implementations**: ElevenLabs, OpenAI, Edge, MiniMax (phase-gated; STT/Music/SFX partial)

**Voice discovery & caching**:
- **Voice cache** (`internal/audio/voice_cache.go`): In-memory LRU (cap 1000 tenants, TTL 1h) shared by HTTP `/v1/voices` + WS `voices.list` handlers. Thread-safe with `sync.Mutex` (LRU updates require write lock)
- **Cache miss recovery**: HTTP handler auto-fetches from ElevenLabs; WS handler requires prior cache warm (provider resolution deferred to Phase 3)
- **Agent audio context** (`store.WithAgentAudio` / `AgentAudioFromCtx`): Immutable snapshot bundle (AgentID + OtherConfig JSONB) injected by dispatcher before tool dispatch; consumed by `TtsTool.Execute` for voice/model resolution

**Agent-level configuration** (`agents.other_config` JSONB):
- `tts_voice_id`: ElevenLabs voice ID (e.g., "pMsXgVXv3BLzUgSXRplE")
- `tts_model_id`: Model choice (eleven_v3, eleven_flash_v2_5, eleven_multilingual_v2, eleven_turbo_v2_5)
- Resolution precedence: CLI args → agent config → tenant override → provider default

**Web UI voice picker** (`ui/web/src/components/voice-picker.tsx`):
- Combobox with BM25 search, preview playback button (HTML `<audio>`)
- Handles preview CDN 403 (expiry) via `onError` → auto-refresh cache
- Embedded in PromptSettingsSection, bound to `other_config.tts_voice_id`

---

## Cross-References

| Document | Content |
|----------|---------|
| [01-agent-loop.md](./01-agent-loop.md) | Agent loop detail, v3 pipeline stages, sanitization pipeline, history management, orchestration modes, self-evolution |
| [02-providers.md](./02-providers.md) | LLM providers, retry logic, schema cleaning |
| [03-tools-system.md](./03-tools-system.md) | Tool registry, policy engine, interceptors, custom tools, MCP grants |
| [04-gateway-protocol.md](./04-gateway-protocol.md) | WebSocket protocol v3, HTTP API, RBAC, identity propagation |
| [05-channels-messaging.md](./05-channels-messaging.md) | Channel adapters, Telegram formatting, pairing, per-user scoping |
| [06-store-data-model.md](./06-store-data-model.md) | Store interfaces, PostgreSQL schema, session caching, custom tool store |
| [07-bootstrap-skills-memory.md](./07-bootstrap-skills-memory.md) | Bootstrap files, skills system, memory, skills grants |
| [08-scheduling-cron.md](./08-scheduling-cron.md) | Scheduler lanes, cron lifecycle |
| [09-security.md](./09-security.md) | Defense layers, encryption, rate limiting, RBAC, sandbox |
| [10-tracing-observability.md](./10-tracing-observability.md) | Tracing collector, span hierarchy, OTel export, trace API |
| [11-agent-teams.md](./11-agent-teams.md) | Agent teams, task board, mailbox, delegation integration |
| [12-extended-thinking.md](./12-extended-thinking.md) | Extended thinking, per-provider support, streaming |
