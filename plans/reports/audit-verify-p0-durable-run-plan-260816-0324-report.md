# Audit Report — P0 Durable Agent Run Execution Plan (verify pass)

**Date:** 2026-08-16 03:24 (Asia/Saigon)
**Auditor:** code-reviewer (a5544518e72c86a1a), read-only
**Plan audited:** `plans/reports/execution-plan-p0-durable-run-260816-0330-report.md`

## Verdict

**SAFE TO EXECUTE** — với 3 corrections bắt buộc đã baked vào plan. 14 claims verified: 8 VERIFIED, 2 wrong, 2 convention divergences. Không có fabricated identifiers.

## Claims verified (8/14)

| Verdict | Claim |
|---------|-------|
| ✅ | `RequiredSchemaVersion = 96` (version.go:5), highest PG migration `000096`, bump → 97 |
| ✅ | `SchemaVersion = 59` + migrations map (schema.go:19, :97) |
| ✅ | `store/base` : `Dialect`(:12), `BuildMapUpdate`(:36), `BuildScopeClause`(:100), `NilStr`(helpers.go:14) |
| ✅ | `PGRunTimelineStore` : constructor(:21) + Append/List/Recover(:25,:73,:176) + `toStore()`(:155) |
| ✅ | `NewEvent()` không populate `EventFrame.Seq` (frames.go:54, :88-94); `SendEvent` marshal as-is (client.go:192-193) |
| ✅ | `MethodRunTimelineGet = "run.timeline.get"` (methods.go:44); `runs.*` follow pattern |
| ✅ | run_timeline.go handler pattern: `Register(router *gateway.MethodRouter)`:25, `handleGet`:36, `filterRunTimelineItemsByUser`:83 |
| ✅ | Root `Config` (config.go:45-66) KHÔNG có `Reliability` field — add mới không conflict |
| ✅ | `ActiveRun` struct (router.go:253-264) fields đúng; `RegisterRun`:291/`UnregisterRun`:319 KHÔNG touch DB — run-record write là mới |
| ✅ | `Loop.Run` (loop_run.go:18) emits RunStarted:39-44, RunCancelled:187, RunFailed:189, RunCompleted:243 — wiring points đúng |
| ✅ | Migration numbering: next = `000097` |
| ✅ | TS `EventFrame.seq?: number` optional (protocol.ts:26) + `handleEvent` chỉ đọc event/payload (ws-client.ts:284-333) → Seq addition KHÔNG break web UI |
| ✅ | `RecoverInterruptedRuns` startup-only (gateway_managed.go:215); `RunStore.RecoverStaleRuns` mới tách bạch |
| ✅ | No heartbeat naming clash: `store.HeartbeatRunLog` (heartbeat_store.go:50-51) là struct, không method clash |

## Wrong claims (2) — đã fix

1. **D8 RunID caller cite sai** (BEFORE → AFTER): `ticker.go:288` thực tế dùng `run_id = "heartbeat:<agentKey>"`, KHÔNG phải `uuid.NewString()`. Bốn caller còn lại đúng. Fix: plan giờ ghi rõ heartbeat runs non-UUID + replay logic phải chấp nhận.

2. **Web UI paths stale**: plan cited `ui/web/src/hooks/use-chat-send.ts` → thật là `ui/web/src/pages/chat/hooks/use-chat-send.ts`. Fix đã áp.

## Convention divergences (2) — đã fix

3. **Migration UUID**: plan dùng `uuid_generate_v4()` → repo có 18 migrations dùng `uuid_generate_v7()` + `GenNewID()` = v7. **Fix: v7.**

4. **Missing `updated_at`**: `BuildMapUpdate` auto-append `updated_at` cho tables trong `TablesWithUpdatedAt` (query_builder.go:54). Nếu `agent_runs` không có column, update sẽ fail. **Fix: thêm `updated_at` column vào schema.**

## Secondary notes

- `RunTimelineStore` có đúng 3 methods (run_timeline_store.go:61-71); thêm `RunStore` riêng coherent với risk note "method cũ giữ nguyên".
- `RecoverInterruptedRuns` tồn tại cả PG(:176) + SQLite(:167) — startup-only uniform.
- Key files: `internal/upgrade/version.go`, `internal/store/base/query_builder.go`, `internal/store/pg/run_timeline.go`, `internal/agent/router.go`, `internal/agent/loop_run.go`, `pkg/protocol/frames.go`, `cmd/gateway_managed.go`, `internal/heartbeat/ticker.go`.