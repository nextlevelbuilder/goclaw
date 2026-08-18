# Phase 2 — Durable Runtime

Scope: `§105` Phase 2 (Run State, Checkpoint, Resume, Event Store, Event Replay, Streaming recovery). Backend durable core only; UI reconnect replay deferred.

## Context (hiện trạng scout)

Nền durable-run đã có ~65% từ reliability Phase 4/9 (P0). Phase 2 lấp đầy các gap khiến resume chưa khả dụng:

- `agent_runs` table: PG `migrations/000097_agent_runs.up.sql` (checkpoint JSONB L16), SQLite `internal/store/sqlitestore/schema.sql:639` (checkpoint TEXT L650) + patch `schema.go:99` (version 59). `RequiredSchemaVersion=98` `internal/upgrade/version.go:5`, `SchemaVersion=61` `schema.go:19`.
- `RunsStore` interface `internal/store/run_timeline_store.go:129` (8 methods); `AgentRun.Checkpoint` L105 ghi rõ `// placeholder (resume = future phase)`. PG `PGRunStore` `pg/run_timeline.go:172`, SQLite `SQLiteRunStore` `sqlitestore/run_timeline.go:162`.
- `Loop.Run` `internal/agent/loop_run.go:18`: `startRunRecord` L48 (Status=running, Attempt=1), `runViaPipeline` L177, terminal L196/199/266. `run_record.go:33` tạo row + heartbeat 10s, KHÔNG ghi checkpoint.
- `runViaPipeline` `internal/agent/loop_pipeline_adapter.go:18`: `convertRunInput(&req)` L19 → `pipeline.NewRunState(input, nil, model, provider)` L42 → `p.Run(ctx, state)` L44. `PipelineConfig.CheckpointInterval: 5` L94 chỉ flush session messages.
- `CheckpointStage` `internal/pipeline/checkpoint_stage.go:24`: chỉ `FlushMessages` (session messages), KHÔNG durable checkpoint.
- `RunState` `internal/pipeline/run_state.go:16`: identity (Input/Workspace/Model/Provider), `Ctx`, `Messages *MessageBuffer`, substates (Context/Think/Prune/Tool/Observe/Compact/Evolution), `Iteration`, `RunID`, `Calls`. `NewRunState` L56. `BuildResult` L75.
- Substates: `internal/pipeline/substates.go` — `ThinkState` L31 (LastResponse/TotalUsage/LastUsage/StreamingActive/Tools), `ToolState` L61 (AllowedTools/TotalToolCalls/LoopKilled/Deliverables), `ObserveState` L74 (FinalContent/FinalThinking/BlockReplies/ContinuationGateFired), `CompactState` L107, `ContextState` L10.
- Stream chunks WS-only: `loop_pipeline_callbacks.go` `emitChunk` L460-478 → `ChatEventChunk`/`ChatEventThinking` (`pkg/protocol/events.go:155-157`). KHÔNG persist.
- `RunTimelineRecorder` `internal/agent/run_timeline_recorder.go:23`: `timelineKindForEvent` L125 chỉ persist terminal/activity/block.reply/tool.call/tool.result (chunks/thinking bị drop).
- Timeline upsert strip content: PG `pg/run_timeline.go:55` (`content = ''`), SQLite `sqlitestore/run_timeline.go:55`.
- Per-run WS seq: `Loop.emit` `internal/agent/loop_tracing.go:28` (atomic từ 1, clear khi terminal). Timeline seq: `RunTimelineRecorder.reserveSeq` `run_timeline_recorder.go:67`.
- Replay: WS `runs.events` `internal/gateway/methods/run_timeline.go:213` (afterSeq cursor), HTTP `GET /v1/runs/{runID}/events` `internal/http/traces.go:53` → `handleRunEvents` L431.
- Recovery: `RecoverStaleRuns` PG `pg/run_timeline.go:321` / SQLite `sqlitestore/run_timeline.go:333` — heartbeat hết hạn → `failed`. `RecoverInterruptedRuns` PG `pg/run_timeline.go:451` / SQLite `sqlitestore/run_timeline.go:410` — `started` không có terminal → `failed`. Startup wiring `cmd/gateway_managed.go:215-221`.

## Gaps cần đóng (G1–G6)

| Gap | Nội dung | Nơi sửa |
|-----|----------|---------|
| G1 | Checkpoint writer — serialize `RunState` → `agent_runs.checkpoint` mỗi N iteration | `checkpoint_stage.go`, `run_record.go`, `RunsStore` + PG/SQLite |
| G2 | Resume entry — đọc run + checkpoint → rebuild `RunState` → chạy lại pipeline | `loop_run.go`, `runViaPipeline`, `loop_pipeline_adapter.go`, WS method + HTTP handler |
| G3 | Attempt advancement + `compacting` status khi retry | `run_record.go`, `RunsStore.UpdateRunStatus`, resume path |
| G4 | Stream chunk/thinking persist vào `run_timeline_items` (un-strip cho type mới) | `run_timeline_recorder.go`, `pg/run_timeline.go:55`, `sqlitestore/run_timeline.go:55` |
| G5 | Per-phase status enum (`thinking`/`waiting_tool`/`verifying`) | `run_timeline_store.go` consts, recorder, pipeline emit |
| G6 | `RecoverStaleRuns`/`RecoverInterruptedRuns` tôn trọng resume model | `pg/run_timeline.go:321,451`, `sqlitestore/run_timeline.go:333,410`, startup wiring |

## Files to modify

### Layer 1 — Store (interface + dual-DB)
- `internal/store/run_timeline_store.go` — thêm `UpdateRunCheckpoint(ctx, runID string, status string, checkpoint json.RawMessage) error` vào `RunsStore`; thêm const item type `chunk`/`thinking`/`tool.started`; thêm const status enum pha (`thinking`/`waiting_tool`/`verifying`); thêm `RunTimelineStatusPaused` nếu cần cho G6.
- `internal/store/pg/run_timeline.go` — implement `UpdateRunCheckpoint` (UPDATE agent_runs SET checkpoint=$3, status=$4, updated_at=NOW() WHERE run_id=$1 AND tenant_id=$2); bỏ strip `content=''` cho item_type IN (chunk, thinking) trong `AppendRunTimelineItem` L55; sửa `RecoverStaleRuns` L321 + `RecoverInterruptedRuns` L451.
- `internal/store/sqlitestore/run_timeline.go` — tương tự (chỗ `content=''` L55, `RecoverStaleRuns` L333, `RecoverInterruptedRuns` L410).

### Layer 2 — Pipeline checkpoint
- `internal/pipeline/deps.go` — thêm callback `WriteCheckpoint func(ctx, state *RunState) error` + config flag (ví dụ `DurableCheckpointInterval int`, mặc định = CheckpointInterval) trong `PipelineConfig`.
- `internal/pipeline/checkpoint_stage.go` — gọi `deps.WriteCheckpoint(ctx, state)` sau khi flush messages (giữ non-fatal).
- `internal/pipeline/run_state.go` — thêm helper `MarshalCheckpoint() (json.RawMessage, error)` + `RestoreCheckpoint(raw json.RawMessage, input *RunInput, ws, model, provider) (*RunState, error)`. Giới hạn payload size (tránh gửi toàn bộ MessageBuffer nếu quá lớn — ví dụ cap messages + substate đủ để rebuild).
- **Gate-premise (đã verify):** `ContextStage` (`internal/pipeline/context_stage.go:111-131`) khi chạy lại sẽ `SetHistory` (session history) + `BuildMessages` → **xóa trắng in-progress tool results/messages đã restore từ checkpoint**. Vì vậy resume KHÔNG được chạy lại setup stages. Design: thêm `RunState.Resuming bool` (set bởi `RestoreCheckpoint`) + `PipelineConfig.ResumeIteration int`; `Pipeline.Run` (`pipeline.go:60`) skip setup stages khi `Resuming`; vòng lặp iteration bắt đầu từ `state.Iteration = ResumeIteration` (thay vì 0 ở `pipeline.go:74`). `ContextStage` khi `Resuming` chỉ cần giữ `state.Ctx` + hook context, bỏ qua rebuild messages (guard từng bước 3/4/7/8).

### Layer 3 — Agent loop wiring
- `internal/agent/run_record.go` — `startRunRecord` nhận thêm checkpoint writer callback; thêm `recordCheckpoint(ctx, state)` gọi `runsStore.UpdateRunCheckpoint` (non-fatal); giữ heartbeat.
- `internal/agent/loop_run.go` — sau `runViaPipeline` khi lỗi và `!ctx.Err()` (không phải cancel) → chuyển `compacting` + attempt++ trước khi trả lỗi; resume entry mới `ResumeRun(ctx, runID) (*RunResult, error)`.
- `internal/agent/loop_pipeline_adapter.go` — `runViaPipeline` chấp nhận `RunState` đã restore (nếu resume) thay vì luôn `NewRunState` L42; wire `WriteCheckpoint` callback vào `buildPipelineDeps`.
- `internal/agent/loop_tracing.go` — không đổi (seq đã đúng).

### Layer 4 — Timeline event store (G4/G5)
- `internal/agent/run_timeline_recorder.go` — `timelineKindForEvent` L125 thêm mapping: `ChatEventChunk` → `chunk`, `ChatEventThinking` → `thinking`, `AgentEventToolStarted` → `tool.started`; persist content đầy đủ (không preview-truncate cho chunk/thinking hoặc cap cao hơn).
- `internal/agent/loop_pipeline_callbacks.go` — `emitChunk` L460: khi persist, gộp chunk theo batch/iteration để tránh 1 item/1 chunk (bão DB); ví dụ gộp per-iteration vào 1 timeline item `chunk` với content nối dồn, hoặc dùng interval.

### Layer 5 — Resume API (G2)
- `internal/gateway/methods/run_timeline.go` — thêm `handleRunsResume` (`runs.resume`, param `runId`) + register.
- `internal/http/traces.go` — thêm `POST /v1/runs/{runID}/resume` handler (authMiddleware, same pattern handleRunGet L401).
- `pkg/protocol/methods.go` — thêm const `MethodRunsResume`.

### Layer 6 — Startup recovery (G6)
- `cmd/gateway_managed.go:215-221` — `RecoverInterruptedRuns` → nếu run có checkpoint → status `paused`/resumable (không terminal-failed); `RecoverStaleRuns` tương tự.

## Implementation steps

1. **Store layer trước** (contract): thêm `UpdateRunCheckpoint` vào interface + cả 2 impl; thêm item_type + phase-status const. Kèm unit test cho `UpdateRunCheckpoint` (PG dùng testdb fixture, SQLite in-memory).
2. **Pipeline checkpoint**: thêm `WriteCheckpoint` callback + `MarshalCheckpoint`/`RestoreCheckpoint` trong `run_state.go` (có cap size).
3. **Agent wiring**: `startRunRecord` nhận callback → `recordCheckpoint`; `runViaPipeline` nhận `*RunState` optional.
4. **Resume API**: `runs.resume` WS + `POST /v1/runs/{runID}/resume` HTTP → `Loop.ResumeRun`.
5. **Event store**: recorder mapping chunk/thinking/tool.started + un-strip content; gộp chunk theo batch.
6. **Recovery reconcile**: `RecoverStaleRuns`/`RecoverInterruptedRuns` — run có checkpoint → `paused` (resumable), chỉ terminal-failed khi heartbeat hết hạn VÀ không có checkpoint hợp lệ.
7. **Dual-DB lockstep** + `RequiredSchemaVersion`/`SchemaVersion` bump nếu migration thêm.
8. **Tests**: unit (checkpoint marshal/restore roundtrip, recorder mapping, recovery resume-aware) + integration PG (resume end-to-end).

## Validation

- `go build ./...` (PG), `go build -tags sqliteonly ./...` (SQLite), `go vet ./...`.
- `go test ./internal/agent/ ./internal/pipeline/ ./internal/store/... ./internal/gateway/methods/ ./internal/http/` trong Docker.
- Integration `go test -tags integration ./tests/integration/` với pgvector PG (port 5433).

## Risks & rollback

- **Checkpoint size:** `RunState.Messages` có thể lớn → cap marshaled size, lưu substate + messages gọn. Nếu checkpoint JSON không parse được khi resume → fallback chạy lại từ đầu (log warn, không crash).
- **Chunk flood:** persist mỗi chunk = 1 row → gộp theo iteration/batch, cap preview.
- **Recovery thay đổi semantics:** run có checkpoint không còn bị failed khi restart → đây là mục đích, nhưng cần test kỹ để không để rò rỉ run "paused" vĩnh viễn (thêm cấu hình max paused age).
- Rollback: tất cả thay đổi nằm trong binary (checkpoint ghi JSONB — DB không đổi shape ngoài nội dung). Bỏ resume path = run mới chạy lại từ đầu.

## Files mới

- `internal/pipeline/checkpoint.go` (marshal/restore helper) — nếu tách khỏi run_state.go.
- Test files kèm mỗi layer.

## Contract thống nhất (chốt trước để agents song song dùng chung)

```go
// store/run_timeline_store.go — RunsStore interface, thêm 1 method:
UpdateRunCheckpoint(ctx context.Context, runID string, status string, checkpoint json.RawMessage) error
// UPDATE agent_runs SET checkpoint=$3, status=$4, updated_at=NOW() WHERE run_id=$1 AND tenant_id=$2 (PG)
// tương tự ? cho SQLite. Non-fatal ở caller.

// store — item_type consts mới (WS-A define, WS-B use):
RunTimelineItemTypeChunk        = "chunk"
RunTimelineItemTypeThinking     = "thinking"
RunTimelineItemTypeToolStarted  = "tool.started"
// store — phase status consts mới:
RunTimelineStatusThinking    = "thinking"
RunTimelineStatusWaitingTool = "waiting_tool"
RunTimelineStatusVerifying   = "verifying"
RunTimelineStatusPaused      = "paused" // G6: interrupted run có checkpoint, resumable

// pipeline/deps.go — thêm:
type PipelineDeps struct { /* ... */
    WriteCheckpoint func(ctx context.Context, state *RunState) error // nil = disabled
}
type PipelineConfig struct { /* ... */
    DurableCheckpointInterval int // ghi durable checkpoint mỗi N iteration (0 = disable, default theo CheckpointInterval)
}

// pipeline/run_state.go — thêm:
func (rs *RunState) MarshalCheckpoint() (json.RawMessage, error)          // cap payload, giữ Images/RawAssistantContent
func RestoreCheckpoint(raw json.RawMessage) (*RunState, error)            // identity do caller set sau
func (rs *RunState) Resuming() bool                                        // true khi restore (skip setup)
// MessageBuffer: thêm Restore(msgs []providers.Message) (SetSystem+SetHistory+SetPending) để dựng lại từ checkpoint.

// agent/loop_pipeline_adapter.go:
func (l *Loop) runViaPipeline(ctx context.Context, req RunRequest, resume *pipeline.RunState) (*RunResult, error)
// resume != nil → dùng state đã restore + Iteration offset; nil → NewRunState như hiện tại.

// agent/loop_run.go — thêm:
func (l *Loop) ResumeRun(ctx context.Context, runID string) (*RunResult, error) // đọc run record → RestoreCheckpoint → runViaPipeline
```

`providers.Message` (`internal/providers/types.go:144`) có `json:"-"` trên `Images`/`Videos`/`RawAssistantContent`/`Transient` — `MarshalCheckpoint` phải custom-marshal giữ `Images` + `RawAssistantContent` (nếu cần cho tool loop tiếp), bỏ `Videos`/`Transient`. Khi `RestoreCheckpoint`, `Resuming()` = true → `Pipeline.Run` skip setup stages + bắt đầu iteration từ checkpoint.

## Ownership (parallel agents, disjoint)

- **WS-A (Store + pipeline):** `internal/store/run_timeline_store.go`, `internal/store/pg/run_timeline.go`, `internal/store/sqlitestore/run_timeline.go`, `internal/pipeline/{deps,checkpoint_stage,run_state,checkpoint}.go` + test.
- **WS-B (Agent loop + resume API):** `internal/agent/{run_record,loop_run,loop_pipeline_adapter,loop_pipeline_callbacks,run_timeline_recorder}.go`, `internal/gateway/methods/run_timeline.go`, `internal/http/traces.go`, `pkg/protocol/methods.go` + test.
- **WS-C (Recovery + wiring + docs):** `cmd/gateway_managed.go`, `cmd/gateway.go`, `internal/upgrade/version.go`, `internal/store/sqlitestore/schema.go`/`schema.sql`, `migrations/`, docs.
