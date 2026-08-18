# AgentKit Deep Integration — Phase 2 Durable Runtime

Nguồn vision: `plans/260815-2340-goclaw-repository-reliability/GoClaw_AgentKit_2026_Deep_Integration_Plan.md`
(`§105` Phase 2: Run State / Checkpoint / Resume / Event Store / Event Replay / Streaming recovery;
`§107` Core Data Model: WorkflowRun → RunEvent + Checkpoint).

## Status

- **Phase 2 (Durable Runtime):** `[x]` SHIPPED (PR #10, merged 2026-08-18, commit `74709556`). Checkpoint + resume + event store + streaming recovery + resume-aware stale recovery. Nền đã có từ reliability Phase 4/9 (`agent_runs`, `run_timeline_items`, heartbeat, stale recovery).
- Phase 1 `/gc:` Foundation: `[x]` SHIPPED (PR #8). Phase 3–7: deferred.

## Quyết định đã chốt (user, 2026-08-18)

1. **Cross-restart resume:** dùng `agent_runs.checkpoint` (JSONB column, đã tồn tại nhưng chưa bao giờ ghi). Không tạo bảng checkpoint riêng.
2. **Event store:** repurpose `run_timeline_items` — mở rộng `item_type` với `chunk`/`thinking`/`tool.started`/`status`, bỏ strip content (`content=''`) cho các type mới. **Không migration mới** cho event store.
3. **UI reconnect replay:** DEFERRED. Backend durable core thôi (phase này), UI `runs.events` đã có `afterSeq` cursor sẵn cho phase sau.
4. Phase 4 Reliability (~90% trùng reliability plan đã merge) — **không làm lại**. Phase này chỉ làm durable core.

## Phases

| Phase | Nội dung | Files chính | Deps | Trạng thái |
|-------|----------|-------------|------|------------|
| 2 | Durable Runtime (checkpoint + resume + event store + streaming recovery) | `internal/pipeline/`, `internal/agent/`, `internal/store/`, `internal/gateway/`, `internal/http/`, `cmd/` | Phase 1 / reliability P0-P9 | `[x]` PR #10 (`74709556`) |

## Acceptance criteria (Phase 2)

- [x] `agent_runs.checkpoint` được ghi (JSONB, chứa `pipeline.RunState` — identity, substate, iteration, messages, cap 200).
- [x] New store method `UpdateRunCheckpoint(ctx, runID, checkpoint, status)` trên cả PG + SQLite; `compacting` status dùng khi tiến vào checkpoint.
- [x] Resume entry: `runs.resume` WS method + `POST /v1/runs/{runID}/resume` HTTP — đọc run record + checkpoint → rebuild `RunState` → chạy lại pipeline từ checkpoint.
- [x] Stream chunks & thinking được persist vào `run_timeline_items` (item_type `chunk`/`thinking`, content KHÔNG strip cho type mới).
- [x] Per-phase status enum (`thinking`/`waiting_tool`/`verifying`...) ghi vào `run.status` timeline item.
- [x] `RecoverStaleRuns` / `RecoverInterruptedRuns` tôn trọng resume model: interrupted run có checkpoint → `paused`/resumable, không bị terminal-failed.
- [x] Dual-DB lockstep: không cần migration (cột checkpoint đã tồn tại) — `RequiredSchemaVersion=98`, `SchemaVersion=61` giữ nguyên.
- [x] Regression: `go build ./...`, `go vet ./...`, unit + integration tests xanh trong Docker/CI.

## Execution detail

- Phase file: [`phase-02-durable-runtime.md`](phase-02-durable-runtime.md)
- Reports: [`reports/`](reports/)
