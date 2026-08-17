# Phase 07 — Tool Reliability: Retry Classes + Deadlines + Progress Events + Idempotency/Side-Effect Safety

> **Status: DONE** — 2026-08-17. Branch `feat/phase7-tool-reliability` → PR #5 → merge dev (`83eef032`). Workstream: agents song song, file-ownership disjoint, **agents KHÔNG commit** — controller commit tuần tự + build/test chung.
> **Scope tickets:** plan §32 Phase 7 (tool retry classes, tool deadlines, tool progress, idempotency, side-effect safety) + §13.1/13.2/13.3 + §9.4 + §7.5.

## Context (scout 2026-08-17, verified directly against code)

**ĐÃ CÓ (KHÔNG xây lại):**
- **Tool call chain** (`internal/agent/loop_pipeline_tool_callbacks.go`): both paths emit `tool.call` then run blocking I/O:
  - Sequential `makeExecuteToolCall` (`:38-106`) — emits `AgentEventToolCall` `:46-51`, calls `executeToolForActor`, then `processToolResult`.
  - Parallel `makeExecuteToolRaw` (`:120-`) — parity fix so parallel path also emits `tool.call` (`:128-137`).
  - `tool.result` emitted in `internal/agent/loop_tools.go:85-90` with payload `{name, id, is_error, arguments, result}`.
- **JSON repair** (`internal/agent/json_repair.go`): pipeline `parse → repair → 1 compact-error retry → fail`, bounded. Cấp model output, not tool execution.
- **Loop detector** side-effect guard (`runState.loopDetector`): `record`/`recordResult`/`recordMutation` + `toolResultBreak` on critical loop (`loop_tools.go:47-50`, `:15-22`).
- **Idempotency cục bộ:** `credential_ephemeral.go` latch (idempotent cleanup), `async_completion_ledger.go` durable row (chống dup async delivery), `delegate_tool.go` publication cleanup.
- **Provider-level retry** (Phase 5): `RetryDo()` cho HTTP/SSE, endpoint `ErrRunDeadline`. KHÔNG áp cho tool execution.
- **Run-level timeouts:** `intentClassifyTimeout` 10s (intent_classify.go:24), `compactionTimeout` 120s (loop_compact.go:41).
- **Wire events cho tool:** `AgentEventToolCall="tool.call"` / `AgentEventToolResult="tool.result"` (`pkg/protocol/events.go:137-138`) as **agent-frame subtypes** (frame event name = `"agent"`).
- **UI consume:** `ui/web/src/pages/chat/hooks/use-chat-messages.ts:212-233` — `tool.call` → `ToolStreamEntry{phase:"calling"}`, `tool.result` → completed/error (match by `payload.id`).
- **WS bus path** (`internal/bus/`): `msgBus.Broadcast(bus.Event{...})` (`internal/gateway/gateway_managed.go:317-321`) → gateway `clientCanReceiveEvent` filter (`internal/gateway/event_filter.go:17-152`; **deny mặc định với non-admin**) → per-client `SendEvent`.
- **Precedent shape:** `workstation.exec.chunk/done` (`pkg/protocol/events.go:118-123`) = long-running thing emits chunked progress + completion. `team.task.progress` (`:47`).
- **Slow-tool watchdog:** `ToolTimingMap.StartSlowTimer` emits `AgentEventActivity{phase:"tool_slow"}` after threshold (`internal/agent/tool_timing.go:103-122`).
- **Tool_status preview:** `internal/channels/events.go` — friendly text ("🔌 Using external tool...") in streaming preview, gated by `ToolStatusEnabled`.

**NEEDS (thiếu, Phase 7 đưa vào):**
1. **Tool retry classes (§13.1)** — chưa có phân loại retry cho chính lời gọi tool: auto-retry (network/429/tmp 5xx) vs repair-then-retry (invalid args) vs never-retry (permission denied / destructive). `processToolResult` chỉ emit error, loop trả trực tiếp.
2. **Tool deadlines (§13.2)** — chưa có per-tool timeout/soft/hard deadline. Tool I/O blocking không deadline; `http.Client{}` "governed by chain context" mờ nhạt.
3. **Tool progress events (§13.3)** — chưa có `tool.started`/`tool.progress`/`tool.log`/`tool.completed` frame-level events. Tool dài khiến UI nghĩ agent treo (chỉ `tool_slow` sau threshold).
4. **Idempotency key atomic (§9.4)** — chưa có idempotency key `run_id+iteration+tool_call_id` cho external side effects; chỉ latch cục bộ.
5. **Side-effect classification (§7.5.1)** — chưa phân loại destructive/irreversible tool để chặn auto-retry lên tác vụ phá hoại.

**Kiến trúc chọn:**
- **Carrier cho progress events = `internal/bus/`** (WS broadcast, parse-driven, không dedup/retry). `internal/eventbus/` là **Sai carrier** — dedup collapse progress stream, retry double-emit side-effect handlers, không có WS bridge (agent 3 verified).
- **Frame-level vs subtype:** chọn **agent-frame subtype** (`tool.started`/`tool.progress`/`tool.log`/`tool.completed` nằm trong `event.type` của frame `"agent"`) để **không cần thêm filter branch** trong `event_filter.go` (deny mặc định), UI đã switch `event.type` sẵn. Đúng parity với `workstation.exec.chunk` về tinh thần, nhưng không tạo frame mới.
- **Cheap first:** Phase 7 = **per-tool deadline + retry classification sao cho an toàn** + **tool.started/log/progress/completed events** (UI giữ như ToolStreamEntry, thêm phases). Idempotency/side-effect: **classification-based gate** (đánh dấu tool "no-auto-retry" khi destructive) — không triển khai key-chain đầy đủ.

## Contracts (C1-C6)

- **C1 — `ToolExecutionSpec`**: cấu trúc per-tool: `Classification` (retry: auto/repair/never), `Deadline` (soft/hard), `Progress` (emit or not). Default: auto=false, deadline=vô hạn (bảo toàn hành vi hiện tại), progress=false (chỉ tool dài/đánh dấu) — zero-config khỏi đổi hành vi.
- **C2 — Retry chỉ khi `Classification == auto` VÀ tool không destructive**: network/429/5xx → retry tối đa N lần backoff; `repair` → 1 lần studio sửa args; `never` → không retry. Never retry side-effect tool.
- **C3 — Tool deadline**: mặc định không giới hạn; cấu hình soft/hard trên spec. Hard deadline hủy context; soft deadline chỉ emitted progress/warning.
- **C4 — Tool events**: `tool.started` + `tool.progress` + `tool.log` + `tool.completed` (agent-frame subtypes). Payload phải mang `runId` + tool `id`/`name` để UI correlate với `ToolStreamEntry.toolCallId`. Emit **cả 2 path** sequential+parallel. Default **off** (opt-in per spec) — không bùng nổ event cho mọi tool.
- **C5 — Side-effect safety**: không auto-retry tool có side effect (mutating). Loop detector vẫn là guard chính. Retry spec phải khai báo tường minh.
- **C6 — Backward compat**: zero config = hành vi hiện tại y hệt. Không đổi `tool.call`/`tool.result` wire shape. Khi `Progress==false`, không emit thêm gì.

## Files to modify/create

**Module A — Tool execution spec + retry/deadline wrapper (agent loop + tools):**
- `internal/agent/loop_pipeline_tool_callbacks.go` — thêm spec resolution; bọc `executeToolForActor` với deadline+retry khi spec yêu cầu.
- `internal/agent/loop_tools.go` (phần `processToolResult`) — classification hook; không emit mới ngoài retry request.
- `internal/tools/registry.go` hoặc file mới `internal/tools/spec.go` — `ToolExecutionSpec` type + default builder + lookup.
- Test mới: retry classification (auto/repair/never, destructive), deadline hủy context, zero-config backward compat.

**Module B — Tool progress/log events (agent + protocol + UI):**
- `pkg/protocol/events.go` — thêm `AgentEventToolStarted` `tool.started` = `"tool.started"`, `"tool.progress"`, `"tool.log"`, `"tool.completed"` constants.
- `internal/agent/loop_pipeline_tool_callbacks.go` — emit started (trước exec), completed (trong processToolResult đã emit tool.result — thêm phase completed nếu spec.Progress).
- `internal/agent/tool_progress.go` (NEW) — helper: async progress emitter (goroutine + channel → `emitRun`), log chunking.
- `ui/web/src/pages/chat/hooks/use-chat-messages.ts` — handle tool.started/progress/log/completed → ToolStreamEntry phases (thêm `running`/`output` states).
- Test: emit cả 2 path, payload mang runId+tool id, UI type.

## Không làm trong Phase 7
- Không đưa tool events qua `internal/eventbus/` (sai carrier — agent 3 verified).
- Không đổi `tool.call`/`tool.result` wire shape hiện có.
- Không triển khai idempotency key-chain (`run_id+iteration+tool_call_id`) đầy đủ — chỉ classification gate chống retry dangerous. Deferred: sau telemetry production.
- Không tự động bật progress cho mọi tool.
- Không đổi channels `tool_status` streaming preview.

## Validation
- `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` + unit tests (Docker golang:1.26-alpine).
- Retry classification: unit test mô phỏng 429/tmp-5xx → retry; permission → no retry; destructive → no retry.
- Deadline: tool vượt soft/hard deadline → context cancel, run tiếp.
- Events: sequential+parallel đều emit started/completed khi opt-in; payload có runId+tool id.

## Risks / Rollback
- Retry lên tool side-effect = nguy hiểm nhất → chỉ retry khi `Classification==auto` AND not destructive; default off.
- Progress event bùng nổ → opt-in, chunked log có cap.
- UI ToolStreamEntry mới phase phải hợp type existing (`tool.result` match by `payload.id`).
- Rollback: revert commit module, wire shape không đổi nên UI cũ vẫn chạy.