# Controller Review — WS-A (Store + Pipeline Checkpoint)

Reviewed 2026-08-18 (controller session, after WS-A self-verify).

Status: APPROVED (structure review — compile gate deferred to Docker).

## Files reviewed

### Store layer — `internal/store/run_timeline_store.go`
- `RunsStore.UpdateRunCheckpoint(ctx, runID, status, checkpoint json.RawMessage) error` (L170) — khớp contract phase file.
- Item-type consts `chunk`/`thinking`/`tool.started` (L21-23) + `RunTimelineItemContentPersisted` (L30-36).
- Phase-status consts `thinking`/`waiting_tool`/`verifying` + `RunTimelineStatusPaused` (L48-53).
- `ValidAgentRunStatus` includes `paused` (L116); `AgentRunTerminal` unchanged (paused NOT terminal).

### Store layer — `internal/store/pg/run_timeline.go`
- `UpdateRunCheckpoint` (L271-284): `UPDATE agent_runs SET checkpoint=$3, status=$4, updated_at=$5 WHERE run_id=$1 AND tenant_id=$2`. Tenant-scoped, non-fatal semantics documented.
- `AppendRunTimelineItem`: contentKeeper un-strip (content=EXCLUDED.content cho keeper types; content='' cho legacy) — đúng G4. Item.Content reset về "" sau write cho non-keeper.
- `RecoverStaleRuns` (L346-376): CASE WHEN checkpoint IS NOT NULL → `paused` + completed_at giữ NULL + error "run paused: heartbeat expired, checkpoint available"; else `failed` + completed_at stamp.
- `RecoverInterruptedRuns` (L492+): has_checkpoint join agent_runs; paused item (metadata `run.paused` + preview) vs failed; `has_term` includes 'paused' → idempotent re-run.

### Store layer — `internal/store/sqlitestore/run_timeline.go`
- Cùng parity PG (UpdateRunCheckpoint L260-273, contentKeeper L41-49, RecoverStaleRuns L363-394, RecoverInterruptedRuns L467-500 has_checkpoint CASE + paused item L531-538).

### Pipeline layer
- `deps.go` — `WriteCheckpoint func(ctx, *RunState) error` (L116), `DurableCheckpointInterval int` (L163). Comment "Intentionally distinct" correct (grep `\` là rendering artifact, đã xác nhận bằng Read trực tiếp).
- `checkpoint_stage.go` — `maybeWriteDurable` (L68-82) gọi từ mọi nhánh early-return của Execute (L33-61); check `WriteCheckpoint != nil` + interval>0 + Iteration%interval==0; non-fatal (log warn).
- `run_state.go` — `Resuming() bool` (L66-71) reads private field; `checkpointMessage` projection (L84-97) giữ Images/RawAssistantContent, bỏ Videos/Transient; `runStateCheckpoint` (L99-116); `MarshalCheckpoint` (L125-149) cap `maxCheckpointMessages=200` giữ system + tail; `RestoreCheckpoint` (L205-233) set `resuming: true`, không restore Input/Model/Provider (caller set).
- `message_buffer.go` — `Restore` (L58-72) layout [system, ...history], pending=nil (CheckpointStage flush trước khi persist). Đúng.
- `pipeline.go` — skip setup khi `!state.Resuming()` (L63-69); `state.Iteration = 0` chỉ khi không resuming (L80-83); loop từ `state.Iteration` (L83). Đúng gate-premise.
- `context_stage.go` — L35-38: `if state.Resuming() { state.Ctx = ctx; return nil }`. Đúng.

## Tests (new)
- `pipeline/run_state_checkpoint_test.go` — roundtrip giữ Images/RawAssistantContent; omits Provider/Ctx; cap 200 giữ system; invalid JSON error; MessageBuffer.Restore shape.
- `pipeline/pipeline_resume_test.go` — resume skips setup + iterations 6..9; fresh state vẫn đầy đủ; ContextStage resume-only-sets-ctx giữ message (t.Fatal nếu BuildMessages/ResolveWorkspace chạy).
- `pipeline/run_state_calls_test.go` (WS-A báo, chưa xuất hiện trong git status lúc review) — AppendCall race + BuildResult.
- `pg/run_timeline_checkpoint_test.go` — roundtrip, tenant-scope fail-closed, content persist, RecoverInterruptedPaused + idempotent, stale resume-aware + empty-checkpoint → failed.
- `sqlitestore/run_timeline_checkpoint_test.go` — tương tự SQLite.

## Concerns for downstream

1. **Compile gate chưa chạy** — cần Docker gate (`go build ./...`, `-tags sqliteonly`, `go vet`, `go test ./internal/pipeline/ ./internal/store/...`) trước commit.
2. **WS-B còn trong flight** — ResumeRun sẽ gọi `pipeline.RestoreCheckpoint` + re-attach identity; contract sẵn sàng.
3. **`ValidAgentRunStatus` + paused** — `runs.list` validation accept `paused` status (WS-B đang xử lý message error text nếu cần).
