# Final Report — Phase 2 Durable Runtime (SHIPPED)

Date: 2026-08-18
PR: [#10](https://github.com/qkhalk/goclaw/pull/10) → dev, merged `74709556`

## Deliverables (G1–G6)

| Gap | Kết quả |
|-----|---------|
| G1 Checkpoint writer | `MarshalCheckpoint`/`RestoreCheckpoint` (`internal/pipeline/run_state.go`, cap 200 messages, giữ Images/RawAssistantContent). `UpdateRunCheckpoint` trên PG + SQLite. Checkpoint stage gọi `WriteCheckpoint` non-fatal. |
| G2 Resume entry | `Loop.ResumeRun(ctx, runID)` (`loop_run.go:329`): đọc run → restore checkpoint (corrupt → fresh fallback) → heartbeat-only updater (NO `CreateRun` → không clobber checkpoint) → tiếp tục pipeline, finalize `compacting`/`failed`/`completed`. API: WS `runs.resume` + HTTP `POST /v1/runs/{runID}/resume`. |
| G3 Attempt (deferred) | `UpdateRunStatus` không có attempt param; resume không reset attempt (no CreateRun). Ghi chú defer trong WS-B report. |
| G4 Event store | `run_timeline_recorder.go` map `chunk`/`thinking`/`tool.started` với FULL content (un-strip `content=''`). |
| G5 Phase status enum | Consts `thinking`/`waiting_tool`/`verifying`/`paused` + `ValidAgentRunStatus` mở rộng. |
| G6 Recovery reconcile | `RecoverStaleRuns`/`RecoverInterruptedRuns`: run có checkpoint → `paused` (resumable, `completed_at` NULL), không có → `failed` (terminal). |

## Controller patches

- **WS permission classification**: `runs.resume` vào `isWriteMethod` (operator+) — nếu không, `MethodRole` → `RoleNone` → fail-closed cho mọi client.
- **Resumer wiring**: `cmd/gateway_resume.go` `makeRunResumer` (runs.GetRun → agentRouter.Get → type-assert `loopResumer` → `ResumeRun`); wire vào WS methods + HTTP traces handler.

## Lỗi tìm trong gate (đã fix + test)

1. **JSONB `checkpoint <> ''` → SQLSTATE 22P02**: cột `agent_runs.checkpoint` là JSONB, so sánh với literal `''` buộc PG parse `''` thành json → fail. `UpdateRunCheckpoint` vốn normalize nil→NULL, nên absent = NULL. Fix: `checkpoint IS NOT NULL` (PG + SQLite parity). Bắt bởi integration `TestStaleRun_RecoverStaleRuns_MarksFailed` — chạy PASS.
2. **CI flake — JSONB key-order**: test PG roundtrip so byte-exact `string(got.Checkpoint) != string(cp)` — JSONB normalize key order không deterministic → CI fail `{"run_id":...} vs {"version":...}`. Fix: so semantic (unmarshal struct, so field). SQLite giữ raw text — không đổi.

## Verification

- `go build ./...` ✅, `go build -tags sqliteonly ./...` ✅, `go vet ./...` ✅ (Docker)
- Unit: pipeline, agent, gateway/methods, permissions, http(run/resume), store/pg (checkpoint+stale+interrupted), sqlitestore ✅
- Integration (pgvector pg18): run-lifecycle + stale-recovery + resume ✅
- CI PR #10: go, web, release-versioning — **3/3 green** ✅

## Cross-surface parity

- **Gateway server**: `runs.resume` WS + HTTP resume + stale/interrupted recovery ✅
- **API contract**: `pkg/protocol` `MethodRunsResume`, docs `04-gateway-protocol.md` §6.1 + `18-http-api.md` ✅
- **Web UI**: N/A — UI reconnect replay deferred (quyết định chốt trong plan, `runs.events` đã có `afterSeq` cursor sẵn cho phase sau).
- **CLI/runtime**: N/A — không có command mới; resume qua API.

## Ghi chú / deferred

- **Attempt++ (G3)**: defer — đổi `RunsStore.UpdateRunStatus` riêng ripple PG/SQLite + callers.
- **Chunk-flood coalescing**: defer (TODO trong recorder) — mỗi chunk 1 row hiện tại.
- **Duplicate `runStaleRunsSweep`**: pre-existing (gateway_managed.go:665 Phase 10 + gateway_heartbeat.go:110-116), idempotent — ghi chú cleanup tương lai.
- **Ollama HTTP tests fail trong Docker**: pre-existing env artifact (host.docker.internal rewrite), không phải regression Phase 2.

Status: SHIPPED
Summary: Phase 2 Durable Runtime hoàn tất qua PR #10 (74709556) — checkpoint + resume + event store + streaming recovery + resume-aware recovery, dual-DB parity, CI 3/3 green, integration gate xanh.