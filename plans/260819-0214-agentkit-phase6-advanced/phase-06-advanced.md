# Phase 6 — AgentKit Advanced (Hibernation + Time Travel + Mission Mode)

Scope: `§16` hibernation, `§17-18` event-sourced runtime + time travel, `§65-66` mission mode + cron. Reuse durable-runtime nền Phase 2 + cron/gc command hạ tầng; làm mới 3 khối: intentional hibernation, checkpoint-history time travel, mission daemon/data-model.

## Context (scout 2026-08-19, controller + Explore agent + verify trực tiếp)

### Reuse (đọc-only)
- **Checkpoint + resume:** `internal/pipeline/run_state.go:125,205` `MarshalCheckpoint`/`RestoreCheckpoint` (cap `maxCheckpointMessages=200`); `CheckpointStage.maybeWriteDurable` `checkpoint_stage.go:68` (interval-driven, non-fatal, `DurableCheckpointInterval=5` wired `loop_pipeline_adapter.go:100-115`); `Loop.ResumeRun` `loop_run.go:329`; `runRecordUpdater.checkpoint` `run_record.go:117`; `makeRunResumer` `cmd/gateway_resume.go:26`; WS `runs.resume` `internal/gateway/methods/run_timeline.go:293`; HTTP `POST /v1/runs/{runID}/resume`; `RunTimelineStatusPaused` `run_timeline_store.go:53` (đã accept trong `ValidAgentRunStatus`).
- **Event replay full-content:** chunk/thinking/tool.started giữ nguyên content (`run_timeline_store.go:30-36`), `run.timeline.get` + `runs.events` với `AfterSeq` cursor.
- **Cron (DB-backed live):** `store.CronStore` `cron_store.go:198`; `CronJob` + `CronPayload{Kind: "agent_turn"/"command"}` + `CronSchedule` (at/every/cron gronx) + `ComputeNextRun`; handler `makeCronJobHandler` `cmd/gateway_cron.go:57` → luôn build `agent.RunRequest{Message: job.Payload.Message}` tại `:169-185`; wiring `SetOnJob` `cmd/gateway_heartbeat.go:78`; bảng `cron_jobs` JSONB payload `000001_init_schema.up.sql:267`. **Legacy file-backed `internal/cron` không được construct live trong `cmd/`, nhưng vẫn được `internal/store/{pg,sqlitestore}/cron*.go` + `internal/config` + `internal/cronexec` import cho schedule/retry helpers — KHÔNG xóa/đụng, chỉ mở rộng `store.CronPayload` (DB-backed).**
- **`/gc:` command:** `internal/commands/gc/{parser,registry,executor}.go` (kind consts `parser.go:23-33`, `knownKinds` `:36`); loop intercept `applyGCCommand` `internal/agent/gc_command.go:20`; registry wiring `cmd/gateway_managed.go:226-238`. Thêm kind mission = tăng `knownKinds` + registry slug + skill — không cần loop change.
- **Task/artifact/subagent-task tables:** `team_tasks` (`000003_agent_teams.up.sql:29`), `artifacts` (`000099_artifacts.up.sql`), `subagent_tasks` (schema.go:227) — reusable cho mission milestones/artifacts.

### Gap (Phase 6 làm mới)
1. **Intentional hibernation:** `paused` status chỉ tồn tại như crash-recovery (`RecoverStaleRuns`/`RecoverInterruptedRuns` set paused/failed). KHÔNG có RPC nào ghi checkpoint + set `paused` on-demand. Cần `runs.pause` (suspend) + wake path reuse `runs.resume`.
2. **Checkpoint history:** `UpdateRunCheckpoint` overwrite-only (PG `run_timeline.go:271/280`, SQLite `:261/270`) — rewind về iteration cũ là bất khả. Cần bảng append-only snapshot versioned + `runs.checkpoints.list` + `runs.replay` rewind resume.
3. **Mission data model:** `team_tasks`/`artifacts`/`subagent_tasks` reusable nhưng không có gì buộc chúng thành mission. Cần bảng `missions` (goals/milestones/acceptance/status/checkpoint refs) + `MissionStore` interface + `MissionMethods`.
4. **`/gc:mission`:** parser+registry+skill integration.
5. **Mission scheduler:** cron `Payload.Kind` thêm `"mission"` + `makeCronJobHandler` branch → schedule mission resume.
6. **Mission workspace scope:** resolver hiện 3 scope (personal/team/delegate, `workspace_context.go:12-16`); mission sub-workdir cần branch mới — KISS: WS-C thêm `ScopeMission` mới, không đổi existing.

### Structural constraints (design-around)
- **Checkpoint overwrite + 200-msg cap** → snapshots table mới, không sửa `agent_runs`.
- **`MarshalCheckpoint` omits Provider/Ctx/exit code** (`run_state.go:120-124`) → rewind rebuild input/model từ run record + snapshot (pattern `ResumeRun` `loop_run.go:360`).
- **Migration numbering:** latest PG `000100_multi_agent_records` (`MIGRATION_LATEST` confirm = 100, `RequiredSchemaVersion = 100`); SQLite `SchemaVersion = 63`, patch map key `62:` → 63. Phase 6 bump phải align thứ tự merge giữa WS-B/WS-C (chúng đều thêm tables).

### Migration context (đã verify)
- `internal/upgrade/version.go:5` `RequiredSchemaVersion uint = 100`.
- `internal/store/sqlitestore/schema.go:19` `SchemaVersion = 63`; `migrations` map key `62:` → 63 (multi_agent_records) tại `:121`.
- Latest PG migration file: `migrations/000100_multi_agent_records.up.sql`.
- **Migration numbering (đã chốt sau verify gateway surface):** dispatch SERIAL → không race. WS-B sở hữu `000101_run_checkpoint_snapshots`.up/.down + `RequiredSchemaVersion` 101 + SQLite patch 63→64/`SchemaVersion` 64. WS-C merge sau (base đã có 101) → dùng `000102_missions` + `RequiredSchemaVersion` 102 + SQLite patch 64→65/`SchemaVersion` 65. WS-A không cần migration.

## 3 Workstream (disjoint, backend-only, 1 stage dispatch)

**File ownership — KHÔNG parallel edit cùng file.**

### WS-A — Hibernation (intentional suspend/wake)

Files (exclusive):
- `internal/agent/hibernate.go` (mới): `SuspendRun(ctx, runID) error` — viết checkpoint hiện tại + set status `paused` (không tạo goroutine mới); `WakeRun` wrapper trỏ lại `ResumeRun`. Giữ đồng bộ với `runRecordUpdater`/`Loop` run lifecycle.
- `internal/gateway/methods/hibernate.go` (mới): `HibernateMethods` — `runs.pause` (suspend) + `runs.wake` (resume) params `{runId}`; nil-safe khi store/router thiếu (mirror `RunTimelineMethods` pattern `run_timeline.go:43-49`).
- `cmd/gateway_hibernate.go` (mới): `makeRunSuspendResumer(agents, runs)` closure; wire vào `registerAllMethods` (sửa `cmd/gateway_methods.go:21` signature + call tại `cmd/gateway.go`).
- `pkg/protocol/methods.go`: thêm `MethodRunsPause = "runs.pause"` + `MethodRunsWake = "runs.wake"`.
- `pkg/protocol/events.go` (hoặc mới `run_events.go`): `EventRunPaused`/`EventRunWoken` payload (`run_id`, `iteration`, `checkpoint_seq`).
- `internal/permissions/policy.go`: `runs.pause`/`runs.wake` vào `isWriteMethod` (write cũng như `runs.resume`).
- `internal/i18n/keys.go` + catalog_{en,vi,zh,ko,ru}.go: thêm key mới (vd `MsgRunsPauseUnavailable`, `MsgRunsWakeUnavailable`).
- `*_test.go`: `internal/gateway/methods/hibernate_test.go` + `internal/agent/hibernate_test.go`.

**Design:** `runs.pause` = ghi checkpoint (cùng shape `MarshalCheckpoint`) + `UpdateRunCheckpoint(ctx, runID, "paused", raw)`; `runs.wake` = resolve agent via router → assert `loopResumer` → `ResumeRun`. Reuse hoàn toàn logic `makeRunResumer` (không duplicate). Idle-timer optional: config-gated `runs_idle_suspend_minutes` (default 0 = disabled) — Loop chạy heartbeat, nếu heartbeat quá lâu và config bật → tự suspend. KISS: idle-timer chỉ là tùy chọn, không phải core tiêu chí.

**Quy tắc:** Không sửa `internal/pipeline/*`, `internal/store/{pg,sqlitestore}/run_timeline*.go` (chỉ thêm WS method surface + agent helper). Không đụng `internal/store/run_timeline_store.go` interface (giữ `RunsStore` nguyên — suspend dùng `UpdateRunCheckpoint` có sẵn).

### WS-B — Time Travel (checkpoint history + replay)

Files (exclusive):
- `internal/store/checkpoint_snapshot.go` (mới, package `store`): `CheckpointSnapshot` struct + `CheckpointSnapshotStore` interface:
  ```go
  type CheckpointSnapshot struct {
    ID          uuid.UUID, TenantID, RunID string, Seq int,
    Status      string,      // "paused" | "running" | "compacting" snapshot
    Snapshot    json.RawMessage, // MarshalCheckpoint output
    Iteration   int,
    CreatedAt   time.Time,
  }
  type CheckpointSnapshotStore interface {
    AppendCheckpointSnapshot(ctx, snap *CheckpointSnapshot) error
    ListCheckpointSnapshots(ctx, opts CheckpointSnapshotListOpts) ([]CheckpointSnapshot, error) // newest first
    GetCheckpointSnapshot(ctx, runID string, seq int) (*CheckpointSnapshot, error)
  }
  ```
- `internal/store/pg/checkpoint_snapshot.go` (mới) + `_test.go`. Tenant-scope fail-closed (`WHERE 1=0` khi không tenant — khớp `pg/contract.go`).
- `internal/store/sqlitestore/checkpoint_snapshot.go` (mới) + `_test.go` (TEXT snapshot, IDF/seq, `-tags sqliteonly`).
- `migrations/000101_run_checkpoint_snapshots.up.sql` + `.down.sql` (mới): bảng `run_checkpoint_snapshots` (id UUIDv7, tenant_id, run_id, seq INT, snapshot JSONB, iteration INT, status, created_at) + index `(tenant_id, run_id, seq DESC)` + `(tenant_id, created_at DESC)`. **WS-B sở hữu migration 000101.**
- `internal/upgrade/version.go`: `RequiredSchemaVersion = 101`.
- `internal/store/sqlitestore/schema.go`: patch key `63:` (63→64) append bảng `run_checkpoint_snapshots` + `SchemaVersion = 64`.
- `internal/store/sqlitestore/schema.sql`: append bảng.
- `internal/gateway/methods/time_travel.go` (mới): `runs.checkpoints.list` (list snapshots theo runId) + `runs.replay` (rewind resume từ seq N). Replay = lấy snapshot N → build `PipelineDeps` → `Loop.ResumeRun` từ snapshot đó (reuse `RestoreCheckpoint` path, nhưng nạp snapshot thay vì run.Checkpoint mới nhất).
- `internal/agent/replay.go` (mới): `ReplayRun(ctx, runID string, seq int)` — resolve agent, assert `loopResumer`, drives `RestoreCheckpoint(snapshot)` → run pipeline. **Lưu ý: `RestoreCheckpoint` trả `*RunState` với `Resuming()=true`; rebuild input từ snapshot + run record (pattern `loop_run.go:360`).**
- `pkg/protocol/methods.go`: `MethodRunsCheckpointsList = "runs.checkpoints.list"` + `MethodRunsReplay = "runs.replay"`.
- `internal/permissions/policy.go`: `runs.checkpoints.list` → read, `runs.replay` → write (đi lại agent loop).
- `internal/i18n/*`: key mới cho unavailable errors.
- `*_test.go`: store roundtrip + tenant scope + replay handler (mocked runner).

**Quy tắc:** WS-B sở hữu migration `000101` + `RequiredSchemaVersion 101` + `SchemaVersion 64` + schema section snapshots. KHÔNG đụng `internal/store/run_timeline_store.go` (RunsStore giữ nguyên), `internal/pipeline/*` (chỉ đọc MarshalCheckpoint/RestoreCheckpoint). Replay tái dùng `RestoreCheckpoint` — không duplicate.

### WS-C — Mission Mode (data model + /gc:mission + scheduler)

Files (exclusive):
- `internal/store/mission_store.go` (mới, package `store`): `Mission` struct + `MissionStore` interface:
  ```go
  type Mission struct {
    ID uuid.UUID, TenantID uuid.UUID,
    Name string, Goals []string, Milestones []string,
    Acceptance []string,
    Status       string,   // "active" | "paused" | "completed" | "failed" | "cancelled"
    AgentID      *uuid.UUID,
    SessionKey   string,
    CheckpointSeq int,     // latest saved checkpoint snapshot ref
    CreatedAt, UpdatedAt time.Time,
    Metadata json.RawMessage,
  }
  type MissionStore interface {
    CreateMission(ctx, m *Mission) error
    GetMission(ctx, id) (*Mission, error)
    ListMissions(ctx, opts MissionListOpts) ([]Mission, error)
    UpdateMissionStatus(ctx, id, status) error
    UpdateMissionProgress(ctx, id, checkpointSeq int) error  // snapshot ref link
    DeleteMission(ctx, id) error
  }
  ```
- `internal/store/pg/mission.go` (mới) + `_test.go`.
- `internal/store/sqlitestore/mission.go` (mới) + `_test.go`.
- **Migration cho missions:** phụ thuộc thứ tự merge. Nếu WS-B (000101) merge chưa xong khi WS-C PR → ail WS-C dùng `000102_missions` + `RequiredSchemaVersion 102`... **Controller sẽ align: để WS-B mở migration 000101 và WS-C dùng 000102 theo thứ tự squash-merge.** Phase file note này dự phòng.
- `internal/commands/gc/parser.go`: thêm `KindMission CommandKind = "mission"` + vào `knownKinds`.
- `internal/commands/gc/registry.go`: `cmd/gateway_managed.go` register `mission` → skill slug (vd `mission` skill trong go-claw-engineer kit).
- `internal/gateway/methods/mission.go` (mới): `MissionMethods` — `mission.create` `mission.get` `mission.list` `mission.pause` `mission.resume` `mission.delete` (params `{missionId}`). Nil-safe khi store thiếu (mirror `RunTimelineMethods`).
- `cmd/gateway_mission.go` (mới): `makeMissionResumer(agents, missions, runs)` closure + wire `registerAllMethods`.
- `cmd/gateway_cron.go`: thêm branch `if job.Payload.Kind == store.CronPayloadKindMission` → schedule mission resume thay vì `agent.RunRequest{Message}`.
- `internal/store/cron_store.go`: thêm `CronPayloadKindMission = "mission"` + `MissionID string` field trong `CronPayload` (hoặc reuse `Message` làm mission id — KISS: dùng `Message` chứa mission ID, tránh đổi struct).
- `pkg/protocol/methods.go`: `mission.*` consts (create/get/list/pause/resume/delete).
- `internal/permissions/policy.go`: mission writes → `isWriteMethod`; mission reads → `isReadMethod`.
- `internal/i18n/*`: key mới cho mission errors + `/gc:mission` responses.
- Skill: `skills/go-claw-engineer/mission/SKILL.md` (mới) OR reuse existing skill — WS-C chọn, KISS.
- `*_test.go`: store roundtrip + tenant scope + mission methods + parser (KindMission parse).

**Quy tắc:** WS-C KHÔNG đụng migration `000101`/`RequiredSchemaVersion 101`/`SchemaVersion 64` (WS-B sở hữu). Nếu WS-C cần schema, dùng `000102` + bump riêng. KHÔNG đụng `internal/cron/*` legacy (file-backed dead code). Cron Payload mở rộng = thêm const + field, không đổi struct tồn tại.

## Cross-workstream contracts

- **WS-A** sở hữu `internal/agent/hibernate.go` + `internal/gateway/methods/hibernate.go` + `cmd/gateway_hibernate.go` + `pkg/protocol` (pause/wake methods/events) + `permissions` + `i18n`.
- **WS-B** sở hữu `internal/store/checkpoint_snapshot*.go` + `internal/store/{pg,sqlitestore}/checkpoint_snapshot*.go` + `migrations/000101_*` + `version.go` + `schema.go`/`schema.sql` (snapshots) + `internal/gateway/methods/time_travel.go` + `internal/agent/replay.go`.
- **WS-C** sở hữu `internal/store/mission_store.go` + `internal/store/{pg,sqlitestore}/mission*.go` + `migrations/000102_*` + `internal/commands/gc/parser.go` (add KindMission) + `cmd/gateway_managed.go` (register mission skill) + `internal/gateway/methods/mission.go` + `cmd/gateway_mission.go` + `cmd/gateway_cron.go` (mission branch) + `internal/store/cron_store.go` (add kind const + field).
- **SHARED gateway surface (MUST be edit-sequentially, KHÔNG parallel):**
  `cmd/gateway_methods.go` (`registerAllMethods` signature `:21` + body calls), `cmd/gateway.go` (call site `:829` — 31 positional args), `internal/permissions/policy.go` (`isWriteMethod`/`isReadMethod` fail-closed slices), `pkg/protocol/methods.go` + `events.go`, `internal/i18n/keys.go` + 5 catalog files. CẢ 3 WS đều chạm peripheral files này (consts/keys/permissions), không chỉ WS-C. → **Không thể song song bất kỳ 2 WS nào mà không conflict.**

## Implementation steps

1. **Dispatch SERIAL (không song song):** WS-B (Time Travel) TRƯỚC; merge; sau đó WS-A (Hibernation), merge; sau đó WS-C (Mission), merge. Lý do: cả 3 cùng chạm `cmd/gateway_methods.go` + `cmd/gateway.go` + `permissions/policy.go` + `pkg/protocol/methods.go` + `internal/i18n`. Serial đảm bảo mỗi PR CI-green độc lập, đúng ràng buộc "tránh parallel edits cùng file".
2. Thứ tự serial cũng chốt migration numbering: B (101) → A (no schema) → C (102).
3. Mỗi WS: implement → Docker gate (build/vet/sqliteonly/unit) → self-review → PR → theo dõi CI → controller review → merge.
4. Final verify: full `go build ./...` + `go vet ./...` + `-tags sqliteonly` + unit + integration.
5. Plan tick + report.

## Validation

- Mỗi WS: `go build ./...`, `go vet ./...`, `go test ./internal/<pkg>/` trong Docker (`golang:1.26.0`, mount `C:/Users/DORA/Downloads/goclaw-mod:/src`, volume `goclaw-gomodcache`, `MSYS_NO_PATHCONV=1`).
- WS-B/WS-C: `-tags sqliteonly` build + SQLite store test + PG store test (`TEST_DATABASE_URL` port 5433).
- Integration: WS-A wake/replay chạy qua mocked runner; WS-C mission create/resume qua loop.
- CI/CD: mỗi WS PR riêng, theo dõi `go`/`web`/`release-versioning` green.

## Risks & rollback

- **Migration numbering race** (B vs C): controller align thứ tự squash-merge; nếu lệch, WS-C dùng 000102 + bump 102.
- **Replay correctness:** `RestoreCheckpoint` không khôi phục `Input`; rewind phải rebuild từ snapshot + run record (pattern `loop_run.go:360`). Rollback: log warn + fallthrough fresh start (khớp `ResumeRun` non-fatal).
- **Checkpoint [200-msg cap]:** snapshot substring may mất context first — replay only recent, không hoàn hảo. Document như limitation.
- **Workspace mission scope:** thêm `ScopeMission` mới — KHÔNG đổi existing scopes/params (WS-C); nếu mission cần sub-workdir, dùng `ScopeDelegate`-like branch.
- **Cron payload mở rộng:** thêm const `mission` + field `MissionID` trong `CronPayload` — không đổi struct hiện tại (backward compatible).
- **i18n:** mỗi user-facing error mới → key + catalogs (en/vi/zh) — WS-A/WS-C đều thêm keys; không override keys lẫn nhau (dùng slug prefix).

## Ownership summary

- **WS-A:** hibernate (agent + methods + cmd + protocol + permissions + i18n)
- **WS-B:** checkpoint_snapshot store (interface + pg + sqlite + migration 000101 + version bump) + time_travel methods + replay
- **WS-C:** mission store (pg + sqlite + migration 000102 + bump) + mission methods + /gc:mission parser/registry/skill + cron mission branch