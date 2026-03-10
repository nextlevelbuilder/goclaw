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

When a run is cancelled via `/stop` or `/stopall`, the run context is cancelled but trace finalization still needs to persist. `FinishTrace()` detects `ctx.Err() != nil` and switches to `context.Background()` for the final database write. The trace status is set to `"cancelled"` instead of `"error"`.

Context values (traceID, collector) survive cancellation -- only `ctx.Done()` and `ctx.Err()` change. This allows trace finalization to find everything it needs with a fresh context for the DB call.

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
| Verbose (`GOCLAW_TRACE_VERBOSE=1`) | Up to 50KB | 500 characters max |

Verbose mode is useful for debugging LLM conversations. Full input messages (including system prompt, history, and tool results) are serialized as JSON and stored in the span's `InputPreview` field, truncated at 50,000 characters.

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

## 5. Trace HTTP API

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

## 6. Delegation History

Delegation history records are stored in the `delegation_history` table and exposed alongside traces for cross-referencing agent interactions.

| Channel | Endpoint | Details |
|---------|----------|---------|
| WebSocket RPC | `delegations.list` / `delegations.get` | Results truncated (500 runes for list, 8000 for detail) |
| HTTP API | `GET /v1/delegations` / `GET /v1/delegations/{id}` | Full records |
| Agent tool | `delegate(action="history")` | Agent self-checking past delegations |

Delegation history is automatically recorded by `DelegateManager.saveDelegationHistory()` for every delegation (sync/async). Each record includes source agent, target agent, input, result, duration, and status.

---

## File Reference

| File | Description |
|------|-------------|
| `internal/tracing/collector.go` | Collector buffer-flush, EmitSpan, FinishTrace |
| `internal/tracing/context.go` | Trace context propagation (TraceID, ParentSpanID) |
| `internal/tracing/otelexport/exporter.go` | OTel OTLP exporter (gRPC + HTTP) |
| `internal/store/tracing_store.go` | TracingStore interface |
| `internal/store/pg/tracing.go` | PostgreSQL trace/span persistence + aggregation |
| `internal/http/traces.go` | Trace HTTP API handler (GET /v1/traces) |
| `internal/agent/loop_tracing.go` | Span emission from agent loop (LLM, tool, agent spans) |
| `internal/http/delegations.go` | Delegation history HTTP API handler |
| `internal/gateway/methods/delegations.go` | Delegation history RPC handlers |

---

## Cross-References

| Document | Relevant Content |
|----------|-----------------|
| [01-agent-loop.md](./01-agent-loop.md) | Span emission during agent execution, cancel handling |
| [03-tools-system.md](./03-tools-system.md) | Delegation system, delegation history via agent tool |
| [06-store-data-model.md](./06-store-data-model.md) | traces/spans tables schema, delegation_history table |
| [08-scheduling-cron.md](./08-scheduling-cron.md) | Scheduler lanes, /stop and /stopall commands |
| [09-security.md](./09-security.md) | Rate limiting, RBAC access control |
