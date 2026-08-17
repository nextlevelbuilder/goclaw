# Execution Plan — P0 Durable Agent Run (§7 cluster)

**Date:** 2026-08-16 03:30 (Asia/Saigon)
**Branch:** `feat/reliability-upgrade`
**Informed by:** `plans/reports/gap-analysis-260816-0313-goclaw-upgrade-plan-report.md` (user chose **Full P0 durable run**).
**Plan status:** REVIEWED ✅ (audit 2026-08-16 03:24, 14 claims: 8 VERIFIED, 2 wrong claims corrected below, 2 convention divergences fixed, 0 fabricated identifiers). → ready to implement.

> **Audit corrections applied (from code-reviewer a5544518e72c86a1a):**
> 1. Migration numbers: **`000097_agent_runs`** (highest is `000096`).
> 2. UUID: repos dùng **`uuid_generate_v7()`** (18 migrations, `GenNewID()` = v7) — KHÔNG v4.
> 3. D8 RunID claim: `ticker.go:288` heartbeat runs use `run_id = "heartbeat:<agentKey>"` (NOT `uuid.NewString()`). `run_id TEXT` handles it, nhưng nếu heartbeat runs ghi `agent_runs` thì UNIQUE + replay phải chấp nhận non-UUID run_id.
> 4. Schema cần **`updated_at`** column — `BuildMapUpdate` auto-appends nó cho tables trong `TablesWithUpdatedAt` (query_builder.go:54). Thiếu sẽ fail update.

---

## 1. Mục tiêu

Biến agent run từ "in-memory goroutine lifetime" thành **durable state machine** mà:
- Có một run record bền vững (`agent_runs` table) theo dõi state qua mọi phase (queued → running → compacting → completed/failed/cancelled).
- Có **heartbeat + stale-recovery**: run treo (hung/thrash) bị phát hiện và đánh dấu `failed` sau khi vượt `extension_budget`.
- Có **sequence** mỗi event trên WS (`EventFrame.Seq`) để client **resync** sau reconnect bằng cursor.
- Có API chuẩn để inspect/replay: `GET /runs/{id}` + `GET /runs/{id}/events?after=cursor` (+ RPC `runs.get`).
- Có CLI debug: `goclaw run list|get|events`.
- Có config block `reliability.runs.*`.

**KHÔNG làm trong phase này** (out of scope, note để tránh scope creep):
- Checkpoint/resume thực sự (resume run từ iteration đã dừng) — cần `agent_run_checkpoints` + workspace snapshot; plan §17 là phase sau. Phase này add cột `checkpoint` placeholders nhưng chưa có resume.
- Weak-model repair (§10), completion verifier (§11), stream watchdog (§9.1) — các phase P1/P2 tiếp theo.
- Tool idempotency (§13.3), per-tool deadline — phase sau.

---

## 2. Thiết kế tổng thể

```
[Gateway]                                     [DB]
  chat.send ──create run record (agent_runs)──▶ agent_runs
  Loop.Run ──heartbeat tick──▶ update row every interval
  state transitions ──▶ update agent_runs.status
  WS events ──▶ EventFrame.Seq (per-connection monotonic) ──▶ event journal (run_timeline_items)
  GET /runs/{id} ──▶ RunStore.Get
  GET /runs/{id}/events?after=N ──▶ ReplayRunEvents
  stale recovery (startup + periodic) ──▶ mark hung runs failed
```

### Quyết định thiết kế + rationale

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | **Tạo bảng `agent_runs` riêng**, không extend `run_timeline_items` | `run_timeline_items` là journal display-safe append-only (item_type/status/preview). `agent_runs` là **run record**: 1 row/run, state machine, heartbeat. Tách mối lo (separation of concerns). `run_timeline_items` giữ nguyên làm event journal. |
| D2 | `EventFrame.Seq` = **per-connection monotonic int64** | An toàn nhất cho resync: client reconnect → nhận `lastSeq` từ `connect` response → request `/runs/{id}/events?after=lastSeq` (kể từ run record). Không đụng `stateVersion` (vốn dùng cho optimistic sync). |
| D3 | Fetch `PKG/protocol` mới method: `runs.get`, `runs.list`, `runs.events` | Follow pattern `run.timeline.get` hiện có (methods.go:44). Đặt trong `internal/gateway/methods/run_timeline.go` (đã có). |
| D4 | CLI `goclaw run <list|get|events|recover>` | Theo pattern `goclaw traces` (cmd/traces*.go). |
| D5 | Config `reliability.runs.*` | Root Config thêm field `Reliability ReliabilityConfig` (config.go:45). Keep backward-compat: field `omitempty`, defaults khi zero. |
| D6 | Heartbeat update coalesced (không 1 write/event) | Write mỗi `HeartbeatInterval` (default 10s) + trên terminal transition. Tránh DB write-storm. |
| D7 | Stale-recovery sweep chạy startup + periodic (mỗi `SweepInterval`) | `RecoverInterruptedRuns` hiện chỉ mark-failed ở startup (run_timeline_store.go:64-71). Thêm sweep định kỳ cho hung-at-runtime. |
| D8 | RunID vẫn là string, không đổi contract | Caller: `uuid.NewString()` ở chat.go:357, chat_runner.go:60, worker.go:857, chat_completions.go:158. **Ngoại lệ: heartbeat runs** dùng `run_id = "heartbeat:<agentKey>"` (ticker.go:288) — non-UUID. `run_id TEXT` + UNIQUE(tenant_id,run_id) xử lý tốt; nếu heartbeat run ghi `agent_runs`, replay/seq logic phải chấp nhận non-UUID. |
| D9 | **Fallback: nếu write run record thất bại (DB down), run vẫn chạy** | Non-fatal. Chỉ log + điều này confirm qua `AppendRunTimelineItem` non-fatal precedent. Không block agent execution vì DB lỗi tạm. |

---

## 3. Files to modify/create

### 3.1. Store layer

| File | Change |
|------|--------|
| **`internal/store/run_timeline_store.go`** | Thêm `AgentRun` struct + `AgentRunStatus` constants (`pending`, `running`, `compacting`, `completed`, `failed`, `cancelled`). Thêm `RunStore` interface: `CreateRun`, `UpdateRunStatus`, `HeartbeatRun`, `GetRun`, `ListRuns`, `RecoverStaleRuns`. |
| **`internal/store/pg/run_timeline.go`** | Implement `RunStore` methods (PG dialect, `$N` params). |
| **`internal/store/sqlitestore/schema.sql`** | Thêm `CREATE TABLE agent_runs (...)` vào fresh schema (desktop SQLite). |
| **`internal/store/sqlitestore/schema.go`** | Thêm incremental patch trong `migrations` map (59→60) + bump `SchemaVersion`. |
| **`internal/store/sqlitestore/run_timeline.go`** | Implement `RunStore` methods (SQLite dialect, `?` params). |
| **`migrations/000097_agent_runs.up.sql`** | PG table. |
| **`migrations/000097_agent_runs.down.sql`** | Drop table. |
| **`internal/upgrade/version.go`** | Bump `RequiredSchemaVersion` 96→97. |

`agent_runs` schema (PG + SQLite cùng shape, dùng `store/base` lifecycle):

```sql
CREATE TABLE agent_runs (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),  -- repo convention: v7 (18 migrations, GenNewID=uuid.NewV7). SQLite: TEXT PK
    tenant_id     UUID        NOT NULL,                          -- SQLite: TEXT
    run_id        TEXT        NOT NULL,
    session_key   TEXT        NOT NULL,
    agent_id      UUID,
    user_id       TEXT,
    channel       TEXT,
    chat_id       TEXT,
    status        TEXT        NOT NULL DEFAULT 'pending',  -- agent_runs status enum
    attempt       INT         NOT NULL DEFAULT 1,
    checkpoint    JSONB,                                    -- placeholder, phase sau §17
    heartbeat_at  TIMESTAMPTZ               DEFAULT now(),  -- last heartbeat
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ,
    error         TEXT,
    metadata      JSONB,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),       -- BuildMapUpdate auto-appends (query_builder.go:54)
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, run_id)
);
CREATE INDEX idx_agent_runs_tenant_status ON agent_runs (tenant_id, status);
CREATE INDEX idx_agent_runs_session ON agent_runs (session_key);
```

> **⚠️ Dual-DB note (CLAUDE.md convention):** PG dùng `uuid_generate_v7()` (repos convention, module `pg_catalog`/`uuidv7`); SQLite dùng TEXT với UUID string (không có hàm gen). Các cột nullable = `*uuid.UUID` / `*string` / `*time.Time`. Bắt buộc thêm `updated_at` vì `BuildMapUpdate` (query_builder.go:36→54) auto-append cho tables trong `TablesWithUpdatedAt` — nếu table có `updated_at` column, update sẽ không fail. Nếu `agent_runs` không `updated_at`, phải exclude khỏi `TablesWithUpdatedAt`.

### 3.2. Protocol layer

| File | Change |
|------|--------|
| **`pkg/protocol/methods.go`** | Thêm `MethodRunsGet = "runs.get"`, `MethodRunsList = "runs.list"`, `MethodRunsEvents = "runs.events"`. |
| **`pkg/protocol/frames.go`** | `NewEvent` thêm optional `seq` param? **NO** — giữ signature. Thêm fields mới: `EventFrame.Seq` đã tồn tại. Chỉ cần **populate** nó ở caller. |
| **`pkg/protocol/events.go`** | (nếu cần) thêm event name `run.recovering`, `run.checkpoint`. |

### 3.3. Gateway layer

| File | Change |
|------|--------|
| **`internal/gateway/client.go`** | `Client` thêm atomic `seq int64` theo connection. `SendEvent` → assign `Seq` trước khi marshal. `SendResponse` không touch. |
| **`internal/gateway/methods/chat.go`** | Ở `dispatchChatSends` (tạo run): gọi `RunStore.CreateRun` + gắn heartbeat goroutine/bộ tick. Terminal state (completed/failed/cancelled) → `UpdateRunStatus`. |
| **`internal/gateway/methods/run_timeline.go`** | Thêm handlers `runs.get`, `runs.list`, `runs.events`. |
| **`internal/gateway/server.go`** | BuildMux thêm `GET /runs/{id}` + `GET /runs/{id}/events` routes (HTTP API). |
| **`internal/gateway/router.go`** | `handleConnect` response thêm `lastSeq` (từ client seq counter) tùy chọn. |

### 3.4. Agent runtime

| File | Change |
|------|--------|
| **`internal/agent/loop_run.go`** | `Loop.Run`: sau `AgentEventRunStarted`, gọi `runs.CreateRun` (qua dependency). Trên exit paths (completed/failed/cancelled) → `UpdateRunStatus`. |
| **`internal/agent/router.go`** | `ActiveRun` thêm `Attempt int`, `RunStore *store.RunStore` (inject). `RegisterRun` tạo record; `UnregisterRun` cập nhật terminal. |
| **`internal/heartbeat/ticker.go`** | (read-only check) xem heartbeat app hiện có; run-level heartbeat là mới, tách biệt. |

### 3.5. Config

| File | Change |
|------|--------|
| **`internal/config/config.go`** | Thêm `Reliability ReliabilityConfig` field. Định nghĩa `ReliabilityConfig{ Runs RunsConfig }`. |
| **`internal/config/config.go`** | `RunsConfig{ HeartbeatIntervalMs int; StaleAfterMs int; SweepIntervalMs int; ExtensionBudgetMs int }` với defaults validated. |

### 3.6. CLI

| File | Change |
|------|--------|
| **`cmd/root.go`** | `rootCmd.AddCommand(runsCmd())` chỗ `tracesCmd()`. |
| **`cmd/runs.go`** (new) | `goclaw run list|get|events|recover`. Pattern: `cmd/traces*.go` vì cùng consume `RunTimelineStore`. |

### 3.7. Web UI parity (starter)

| File | Change |
|------|--------|
| **`ui/web/src/api/protocol.ts`** | Thêm event/method types nếu cần consumed. |
| **`ui/web/src/types/chat.ts`** | `Run` type sẵn có — xem có cần thêm `status` field tỷ lệ. |

> ⚠️ **Parity note:** Web UI consume `run.started/completed/failed/cancelled` tại `ui/web/src/pages/chat/hooks/use-chat-messages.ts:154,245,272,289`, `use-query-invalidation.ts:21-24`, `trace-detail-dialog.tsx:78` (paths đã fix theo audit). Change này KHÔNG break các event lobby hiện tại (giữ event names + `Seq` optional). Chỉ ADD: backend populate `Seq`, thêm method `runs.get` (WS) + HTTP `/runs/{id}`. UI không bắt buộc đổi để run vẫn hoạt động; UI resync là phase sau. **State parity: Backend + DB + API primary; Web UI N/A because event names unchanged + `seq?: number` optional (protocol.ts:26) nên không break TS parser.** Vẫn thêm error-handling nếu `runs.get` 404 → UI không hard-fail.

---

## 4. Implementation steps (ordering)

### Phase A — DB + store (nền, không break)
1. Migration PG `000097_agent_runs.up.sql` + `000097_agent_runs.down.sql`. Bump `RequiredSchemaVersion` 96→97.
2. SQLite: `schema.sql` (thêm table vào fresh schema) + `schema.go` patch (migrations map entry 59→60) + bump `SchemaVersion` 59→60.
3. `store/run_timeline_store.go`: thêm `AgentRun` struct + `AgentRunStatus` + `RunStore` interface.
4. `store/pg/run_timeline.go` + `store/sqlitestore/run_timeline.go`: implement `RunStore` theo `store/base` Dialect (BuildMapUpdate, BuildScopeClause).
5. **Verify:** dual-DB build (`go build ./...` + `-tags sqliteonly`) + unit test RunStore roundtrip.

### Phase B — Protocol + gateway
6. `pkg/protocol/methods.go`: thêm `runs.get/list/events`.
7. `pkg/protocol/frames.go`: (giữ st) — chỉ set up để populate.
8. `internal/gateway/client.go`: per-connection seq counter; `SendEvent` populate `Seq`.
9. `internal/gateway/methods/chat.go`: `CreateRun` + heartbeat tick + terminal `UpdateRunStatus`.
10. `internal/gateway/methods/run_timeline.go`: `runs.get/list/events` handlers.
11. `internal/gateway/server.go`: HTTP routes `/runs/{id}` + `/runs/{id}/events`.

### Phase C — Runtime + CLI
12. `internal/agent/loop_run.go` + `internal/agent/router.go`: wire RunStore, create/update on lifecycle.
13. `cmd/runs.go` + `cmd/root.go`: CLI.
14. **Verify:** go vet, dual build, unit + integration.

### Phase D — Config + docs
15. `internal/config/config.go`: `ReliabilityConfig{RunsConfig}` + defaults.
16. Docs `docs/` (nếu change user-visible) — per documentation-management rules.
17. **Verify:** `go test ./...` narrow, broader integration.

---

## 5. Acceptance criteria

1. `agent_runs` table tồn tại ở cả PG + SQLite với đúng schema + indices; migration up/down không vỡ.
2. Mỗi `chat.send` tạo 1 run record (state `pending`→`running`→terminal). Run record tồn tại sau run kết thúc.
3. `EventFrame.Seq` được populate mỗi event (monotonic per connection); client có thể theo dõi seq theo run.
4. `GET /runs/{id}` + `/runs/{id}/events?after=N` trả đúng dữ liệu (replay từ `run_timeline_items`).
5. `goclaw run list|get|events|recover` hoạt động (sửa DB trực tiếp khi local).
6. Config `reliability.runs.*` đọc được; defaults hợp lý.
7. **Stale-recovery**: run bị treo > `StaleAfterMs` bị mark `failed` ở startup/periodic sweep (không xóa, không resume).
8. Dual-DB build theo chuẩn: `go build ./...` + `go build -tags sqliteonly ./...`, vet clean, tests pass.
9. Non-fatal cho DB lỗi: run vẫn chạy nếu run-record write fail (log-only).
10. **No regression:** event names giữ nguyên; `run.timeline.get` không break (viewer role vẫn đọc được).

---

## 6. Risks & rollback

| Risk | Mitigation |
|------|-----------|
| **DB migration lỗi** (PG vs SQLite khác dialect) | Test cả 2 trên build tags. Rollback: `goose down` / xóa patch. |
| **Heartbeat write-storm** (mỗi event 1 write) | Coalesce mỗi `HeartbeatInterval`. D6. |
| **Run record lỗi → run chết** | Non-fatal (D9) — write fail chỉ log. |
| **Seq counter race** (concurrent SendEvent) | `atomic.Int64.Add` — thread-safe. |
| **`runs.get` 404 break UI** | Add-tolerant: UI đã xử lý 404 pattern (trace-detail dialog refs). |
| **Exists `RecoverInterruptedRuns` behavior đổi** | Table mới, method cũ của `RunTimelineStore` giữ nguyên. New method trên `RunStore`. |
| **Scope creep §17 checkpoint-resume** | Giữ placeholder `checkpoint` column chỉ; resume là phase sau. |

---

## 7. Tests

- **Unit:** `RunStore` roundtrip on `sqlmock`/fake (PG dialect), `Storage` full-schema for SQLite — tối thiểu 1 test cả 2.
- **Integration** (`-tags integration`, pgvector pg18): tạo run → terminal → `GetRun` trả đúng status; `RecoverStaleRuns` mark hung.
- **Protocol:** test `SendEvent` populate `Seq` (unit, atomic counter).
- **CLI:** test kết nối fake store, không cần gateway live.
- **Fixtures:** 1 byte-fix character test cho migration (`agent_runs` schema) BEFORE wiring run to gateway.

---

## 8. Cross-surface parity checklist (CLAUDE.md)

- [ ] **Gateway server:** store + protocol + client seq + handlers + heartbeat wiring.
- [ ] **DB migrations:** PG `migrations/*` + `internal/upgrade/version.go` + SQLite `schema.sql`/`schema.go` + `SchemaVersion`.
- [ ] **API contract:** `pkg/protocol/methods.go` + `frames.go` (Seq populate) + HTTP `/runs/{id}` + tests từ real shapes.
- [ ] **CLI:** `cmd/runs.go` + `cmd/root.go`.
- [ ] **Config:** `ReliabilityConfig{RunsConfig}`.
- [ ] **Web UI:** N/A for core event names (unchanged); `runs.get` 404 tolerance on existing trace/status consumers.

---

## 9. Open questions before implementation

1. **`RecoverStaleRuns` sweep interval** — default value? (đề xuất: heartbeat 10s, stale 60s, sweep 30s).
2. **`runs.events` RPC** có cần filter theo `item_type` + `since` timestamp, hay cứ `limit/offset` như `run.timeline.get`?
3. **Attempt_id ✕ duplicate detection**: phase này chỉ lưu `attempt` int trên run record; việc resume/retry thực (attempt++ + pending→running) có làm trong P0 hay để phase sau? (đề xuất: P0 chỉ record attempt, không tự retry.)