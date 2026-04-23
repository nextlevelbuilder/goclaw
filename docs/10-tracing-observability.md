# 10 - Tracing & Observability

Records agent run activities asynchronously. Spans are buffered in memory and flushed to the TracingStore in batches, with optional export to external OpenTelemetry backends.

> Tracing uses PostgreSQL. The `traces` and `spans` tables store all tracing data. Optional OTel export sends spans to external backends (Jaeger, Grafana Tempo, Datadog) in addition to PostgreSQL.

---

## 1. Collector -- Buffer-Flush Architecture

```mermaid
flowchart TD
    EMIT["EmitSpan(span)"] --> BUF["spanCh<br/>(buffered channel, cap = 1000)"]
    BUF --> FLUSH["flushLoop() -- every 5s"]
    FLUSH --> DRAIN["Drain all spans from channel"]
    DRAIN --> BATCH["BatchCreateSpans() to PostgreSQL"]
    DRAIN --> OTEL["OTelExporter.ExportSpans()<br/>to OTLP backend (if configured)"]
    DRAIN --> AGG["Update aggregates<br/>for dirty traces"]

    FULL{"Buffer full?"} -.->|"Drop + warning log"| BUF
```

### Trace Lifecycle

```mermaid
flowchart LR
    CT["CreateTrace()<br/>(synchronous, 1 per run)"] --> ES["EmitSpan()<br/>(async, buffered)"]
    ES --> FT["FinishTrace()<br/>(status, error, output preview)"]
```

### Cancel Handling

When a run is cancelled via `chat.abort` (WS) or manual stop, the router atomically transitions to "aborting" state, cancels the request context, and waits 3 seconds for the agent goroutine to exit. If it doesn't, the trace is force-marked `cancelled` to prevent orphaned traces.

During cancellation:
- HTTP provider streams close immediately (context-aware transport with `ResponseHeaderTimeout`; SSE body closes via goroutine wrapper to unblock socket read)
- Tool execution terminates (shell: process-group kill; browser: page close)
- Agent loop receives `ctx.Done()` and propagates cancellation up

Trace finalization persists independently with a detached context (`context.WithoutCancel`), 5-second timeout, and exponential backoff retry (3 tries, max 10 total via retry queue). This ensures trace status writes don't fail silently due to cancellation.

Stale recovery is **currently disabled**. The implementation exists but the background loop is not started in `Collector.Start()`. Reason: the sweep condition is `start_time < NOW() - threshold`, which measures trace age rather than inactivity. Any threshold low enough to be useful (2–10 min) would kill healthy long-running agent runs (research chains, large code generation, extended shell commands). Re-enable only after adding a `last_span_at` column so recovery can gate on "no activity for N minutes" instead of "started > N min ago". Until then, zombie traces from gateway crashes may remain `running` in the DB — an accepted safety-net gap traded against false kills. The primary abort path (router 2-phase abort + `trace.status` WS event) handles the common case. Context values (traceID, collector) survive cancellation — only `ctx.Done()` and `ctx.Err()` change.

---

## 2. Span Types & Hierarchy

| Type | Description | OTel Kind |
|------|-------------|-----------|
| `llm_call` | LLM provider call | Client |
| `tool_call` | Tool execution | Internal |
| `agent` | Root agent span (parents all child spans) | Internal |
| `embedding` | Embedding generation (vector store operations) | Internal |
| `event` | Discrete event marker (no duration) | Internal |

```mermaid
flowchart TD
    AGENT["Agent Span (root)<br/>parents all child spans"] --> LLM1["LLM Call Span 1<br/>(model, tokens, finish reason)"]
    AGENT --> TOOL1["Tool Span: exec<br/>(tool_name, duration)"]
    AGENT --> LLM2["LLM Call Span 2"]
    AGENT --> TOOL2["Tool Span: read_file"]
    AGENT --> EMB["Embedding Span<br/>(vector store operation)"]
    AGENT --> LLM3["LLM Call Span 3"]
```

### Token Aggregation

Token counts are aggregated **only from `llm_call` spans** (not `agent` spans) to avoid double-counting. The `BatchUpdateTraceAggregates()` method sums `input_tokens` and `output_tokens` from spans where `span_type = 'llm_call'` and writes the totals to the parent trace record.

---

## 3. Verbose Mode

| Mode | InputPreview | OutputPreview |
|------|:---:|:---:|
| Normal | Not recorded | 500 characters max |
| Verbose (`GOCLAW_TRACE_VERBOSE=1`) | Up to 200KB | Up to 200KB |

Verbose mode is useful for debugging LLM conversations. When enabled via `GOCLAW_TRACE_VERBOSE=1`:

- **LLM spans**: Full input messages (including system prompt, history, and tool results) are serialized as JSON and stored in `InputPreview` (truncated at 200KB). LLM response content is stored in `OutputPreview` (truncated at 200KB, includes `<thinking>` tag if present).
- **Tool spans**: Tool input and output are both recorded up to 200KB.
- **Agent span**: Input message and output are both recorded up to 200KB.

In normal mode, previews are truncated to 500 characters max to minimize storage overhead.

---

## 4. OTel Export

Optional OpenTelemetry OTLP exporter that sends spans to external observability backends.

```mermaid
flowchart TD
    COLLECTOR["Collector flush cycle"] --> CHECK{"SpanExporter set?"}
    CHECK -->|No| PG_ONLY["Write to PostgreSQL only"]
    CHECK -->|Yes| BOTH["Write to PostgreSQL<br/>+ ExportSpans() to OTLP backend"]
    BOTH --> BACKEND["Jaeger / Tempo / Datadog"]
```

### OTel Configuration

| Parameter | Description |
|-----------|-------------|
| `endpoint` | OTLP endpoint (e.g., `localhost:4317` for gRPC, `localhost:4318` for HTTP) |
| `protocol` | `grpc` (default) or `http` |
| `insecure` | Skip TLS for local development |
| `service_name` | OTel service name (default: `goclaw-gateway`) |
| `headers` | Extra headers (auth tokens, etc.) |

### Batch Processing

| Parameter | Value |
|-----------|-------|
| Max batch size | 100 spans |
| Batch timeout | 5 seconds |

The exporter lives in a separate sub-package (`internal/tracing/otelexport/`) so its gRPC and protobuf dependencies are isolated. Commenting out the import and wiring removes approximately 15-20MB from the binary. The exporter is attached to the Collector via `SetExporter()`.

---

## 5. Cost Calculation

Per-span cost is calculated using the `CalculateCost()` function in `internal/tracing/cost.go`. For each LLM call span:

```
Cost = (PromptTokens × InputCostPerMillion) / 1,000,000
      + (CompletionTokens × OutputCostPerMillion) / 1,000,000
      + (CacheReadTokens × CacheReadCostPerMillion) / 1,000,000
      + (CacheCreationTokens × CacheCreateCostPerMillion) / 1,000,000
```

Model pricing is loaded from `config.ModelPricing` and keyed by `provider/model` (with fallback to `model` only). Cost is stored in the `total_cost` field of each LLM call span. The trace aggregation sums costs from all child `llm_call` spans to compute the trace-level `total_cost`.

Cache token costs (read + create) are optional and only applied if the pricing config specifies non-zero values.

---

## 6. Snapshot Worker -- Realtime Usage Aggregation

The `SnapshotWorker` periodically aggregates trace and span data into hourly `usage_snapshots` for realtime analytics and dashboard displays.

### Operation

- **Schedule**: Ticks every hour at HH:05:00 UTC (5 minutes past the hour)
- **Catch-up**: On startup and after each tick, computes snapshots for all missed hours
- **Backfill**: `Backfill()` method populates historical snapshots from the earliest trace to now

### Snapshot Dimensions

For each hour `[00:00, 01:00)`, the worker creates two types of snapshot rows:

1. **Totals Row** (`provider=""`, `model=""`) — Aggregated from traces:
   - `request_count` — Count of root traces
   - `error_count` — Count of failed traces
   - `unique_users` — Distinct `user_id` in traces
   - `input_tokens`, `output_tokens` — Sum from all child `llm_call` spans
   - `total_cost` — Sum of costs from all child `llm_call` spans
   - `tool_call_count` — Sum from traces
   - `avg_duration_ms` — Average trace duration
   - `memory_docs`, `memory_chunks` — Point-in-time count (attached to agent's totals row only)
   - `kg_entities`, `kg_relations` — Point-in-time count (attached to agent's totals row only)

2. **Detail Rows** (`provider` + `model` specified) — Aggregated from `llm_call` spans:
   - `llm_call_count` — Count of LLM calls for this provider/model
   - `input_tokens`, `output_tokens` — Sum of tokens
   - `total_cost` — Sum of per-call costs
   - `cache_read_tokens`, `cache_create_tokens`, `thinking_tokens` — Sum from span metadata

Grouping: by `(agent_id, channel)` for totals; by `(agent_id, channel, provider, model)` for details.

### Usage

```go
worker := tracing.NewSnapshotWorker(db, snapshotStore)
worker.Start()

// Later:
hoursBackfilled, err := worker.Backfill(ctx)
worker.Stop()
```

---

## 7. Trace HTTP API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/traces` | List traces with pagination and filters |
| GET | `/v1/traces/{id}` | Get trace details with all spans |

### Query Filters

| Parameter | Type | Description |
|-----------|------|-------------|
| `agent_id` | UUID | Filter by agent |
| `user_id` | string | Filter by user |
| `status` | string | Filter by status (running, success, error, cancelled) |
| `from` / `to` | timestamp | Date range filter |
| `limit` | int | Page size (default 50) |
| `offset` | int | Pagination offset |

---

## 8. Delegation History

Delegation history records are stored in the `delegation_history` table and exposed alongside traces for cross-referencing agent interactions.

| Channel | Endpoint | Details |
|---------|----------|---------|
| WebSocket RPC | `delegations.list` / `delegations.get` | Results truncated (500 runes for list, 8000 for detail) |
| HTTP API | `GET /v1/delegations` / `GET /v1/delegations/{id}` | Full records |
| Agent tool | `delegate(action="history")` | Agent self-checking past delegations |

Delegation history is automatically recorded by `DelegateManager.saveDelegationHistory()` for every delegation (sync/async). Each record includes source agent, target agent, input, result, duration, and status.

---

## File Reference

| Module | Path | Purpose |
|---|---|---|
| Tracing engine | `internal/tracing/` | Collector (buffer-flush, EmitSpan, FinishTrace), context propagation, cost calculation, OTel OTLP exporter |
| Store & snapshots | `internal/store/tracing_store.go`, `internal/store/pg/tracing.go`, `internal/tracing/snapshot_worker.go` | TracingStore interface, PostgreSQL persistence + aggregation, hourly usage snapshots |
| Agent & pipeline integration | `internal/agent/loop_tracing.go`, `internal/pipeline/` | Span emission from agent loop (LLM, tool, agent spans), pipeline stage tracing |
| HTTP & RPC handlers | `internal/http/traces.go`, `internal/http/delegations.go`, `internal/gateway/methods/delegations.go` | GET /v1/traces, delegation history HTTP + RPC handlers |

Use `grep` or your editor's symbol search for specific files.

---

## Cross-References

| Document | Relevant Content |
|----------|-----------------|
| [01-agent-loop.md](./01-agent-loop.md) | Span emission during agent execution, cancel handling |
| [03-tools-system.md](./03-tools-system.md) | Delegation system, delegation history via agent tool |
| [06-store-data-model.md](./06-store-data-model.md) | traces/spans tables schema, delegation_history table |
| [08-scheduling-cron.md](./08-scheduling-cron.md) | Scheduler lanes, /stop and /stopall commands |
| [09-security.md](./09-security.md) | Rate limiting, RBAC access control |
