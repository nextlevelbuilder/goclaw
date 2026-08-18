# AgentKit Deep Integration — Phase 6 Advanced

Nguồn vision: `plans/260815-2340-goclaw-repository-reliability/GoClaw_AgentKit_2026_Deep_Integration_Plan.md`
(`§16` hibernation; `§17-18` event-sourced runtime + time travel; `§65-66` mission mode + cron).

## Status

- **Phase 6 (Advanced):** `[ ]` — IN PROGRESS (2026-08-19).
- Phase 1 `/gc:` Foundation `[x]` SHIPPED (PR #8). Phase 2 Durable Runtime `[x]` (PR #10). Phase 3 Native Layer `[x]` (PR #11-14). Phase 4 Reliability = trùng reliability plan `[x]` (không làm lại). Phase 5 Multi-Agent `[x]` SHIPPED (PR #15-18).

## Quyết định đã chốt (controller scout, 2026-08-19)

1. **Reuse hạ tầng có sẵn, không làm lại:**
   - Durable checkpoint + crash-recovery resume: `Loop.ResumeRun` (`internal/agent/loop_run.go:329`), `runs.resume` (`pkg/protocol/methods.go:52`), `HTTP /v1/runs/{runID}/resume`, `UpdateRunCheckpoint` overwrite (PG `run_timeline.go:271`, SQLite `:261`), `RunTimelineStatusPaused` (`run_timeline_store.go:53`).
   - Event replay (full-content): `run.timeline.get` + `runs.events` + chunk/thinking/tool.started (`run_timeline_store.go:30-36`).
   - Cron scheduling machinery: DB-backed `store.CronStore` (`internal/store/cron_store.go:198`), `ComputeNextRun` (at/every/cron gronx), run logs. **LƯU Ý: file-backed `internal/cron` là legacy — DB-backed là wired (`cmd/gateway.go:628`).**
   - `/gc:` parser/registry/executor: `internal/commands/gc/{parser,registry,executor}.go`; loop intercept `applyGCCommand` (`internal/agent/gc_command.go:20`); registry wiring `cmd/gateway_managed.go:226-238`.
   - Task/artifact/subagent-task tables: `team_tasks`, `artifacts` (`000099`), `subagent_tasks` — reusable cho mission milestones/artifacts.
2. **4 khối genuinely mới** (vision chưa có nền): Hibernation, Time travel/checkpoint-history, Mission data model + `/gc:mission`, Mission workspace scope. Phase 6 làm **3 workstream backend-only** (WS-A Hibernation, WS-B Time Travel, WS-C Mission).
3. **Restrictive constraints đã xác định:**
   - Checkpoint overwrite-only + 200-msg cap → time travel cần table mới (append-only snapshot), không sửa `agent_runs`.
   - `MarshalCheckpoint` omits Provider/Ctx/exit code → rewind phải rebuild input/model từ run record (như `ResumeRun` tại `loop_run.go:360`).
   - Cron payload message-only → mission scheduler cần payload kind mới hoặc bảng/interface riêng; schedule machinery tái dùng.
   - Mission sub-workdirs chạm immutable `WorkspaceContext` contract (`workspace_context.go:20-42`) → WS-C chỉ thêm mission scope branch, không break existing scopes.
4. **Non-goals:** predictive failure (Phase 10 ✓), experience store (memory/consolidation ✓), self-improvement (self-evolution ✓), simulation/chaos (Phase 9 ✓), UI/web mới, `/gc:team` mới, hook system (§48 chốt Phase 3 ✓). Chỉ backend runtime + WS methods + protocol events + tools + DB.
5. **Public contract mới cần chọn từ đầu:** Phase 6 định nghĩa method names (`runs.pause`, `runs.wake`, `runs.checkpoints.list`, `/gc:mission` kind, mission WS methods) — vision chỉ mô tả kịch bản không có RPC.

## Phases

| Phase | Nội dung | Trạng thái |
|-------|----------|------------|
| 6 | Advanced (hibernation + time travel + mission mode) | `[ ]` |

## Acceptance criteria (Phase 6)

- [ ] **Hibernation** (WS-A): `runs.pause` ghi checkpoint + status `paused` on-demand (không chỉ crash-recovery); `runs.wake` reuses `runs.resume` path. Protocol methods + permissions (write) + i18n. Test pass.
- [ ] **Hibernation idle-timer** (WS-A): idle hibernate optional (config-gated) — run tự pause sau N phút không heartbeat. Test pass.
- [ ] **Time travel** (WS-B): bảng `run_checkpoint_snapshots` (append-only versioned) PG+SQLite, migration `000101` + `RequiredSchemaVersion` 101 + SQLite patch 63→64 + `SchemaVersion` 64. Test roundtrip + tenant scope.
- [ ] **Time travel replay** (WS-B): `runs.checkpoints.list` liệt kê snapshot; `runs.replay` rewind resume từ snapshot N (rebuild via `ResumeRun` path). Test pass.
- [ ] **Mission data model** (WS-C): bảng `missions` (goals/milestones/acceptance_criteria/status/checkpoint refs/tenant) + `MissionStore` PG+SQLite; `team_tasks`/`artifacts` linkable. Test pass.
- [ ] **/gc:mission** (WS-C): command kind `mission` + registry + skill; create/pause/resume mission qua loop. Test pass.
- [ ] **Mission scheduler** (WS-C): cron payload kind `mission` hoặc mission-specific job; `ComputeNextRun` reuse. Test pass.
- [ ] Dual-DB lockstep: PG migration + bump; SQLite schema.sql + patch + bump (cho cả snapshots + missions).
- [ ] Mỗi workstream: test → tự review → PR riêng → CI green.
- [ ] Regression: `go build ./...`, `go vet ./...`, `go build -tags sqliteonly ./...`, unit + integration xanh.

## Workstreams (disjoint, backend-only)

| WS | Nội dung | Files (exclusive) | Kiểu |
|----|----------|-------------------|------|
| **A** | Hibernation — intentional suspend/wake | `internal/gateway/methods/hibernate.go` + `internal/agent/hibernate.go` + `internal/store/run_hibernate.go`(?) + `cmd/gateway_hibernate.go` + `pkg/protocol/*.go` (methods/events) + `internal/permissions/policy.go` + `internal/i18n/*` + `*_test.go` | Runtime + protocol |
| **B** | Time travel — checkpoint history + replay | `internal/store/checkpoint_snapshot.go` (interface) + `internal/store/pg/checkpoint.go` + `internal/store/sqlitestore/checkpoint.go` + `migrations/000101_*.sql` + `internal/upgrade/version.go` + `schema.go`/`schema.sql` (snapshot section) + `internal/gateway/methods/time_travel.go` + `internal/agent/replay.go`(?) + `*_test.go` | DB + protocol |
| **C** | Mission mode — data model + /gc:mission + scheduler | `internal/store/mission_store.go` + `internal/store/pg/mission.go` + `internal/store/sqlitestore/mission.go` + `migrations/000101_*.sql` (append missions table) + `schema.go`/`schema.sql` (mission section) + `internal/commands/gc/parser.go` (KindMission) + `registry.go` + `internal/gateway/methods/mission.go` + `cmd/gateway_mission.go` + `*_test.go` | DB + runtime |

**Lưu ý dual-DB lockstep trùng ngầm:** WS-B và WS-C đều muốn bump `000101`/`RequiredSchemaVersion`/`SchemaVersion` → **phải phân tách migration**: WS-B sở hữu `000101_run_checkpoint_snapshots`, WS-C thêm table `missions` trong cùng migration NẾU merge cùng nhịp; nếu lệch → WS-C dùng `000102_missions`. Controller sẽ align số migration theo thứ tự merge.

## Execution detail

- Phase file: [`phase-06-advanced.md`](phase-06-advanced.md)
- Reports: [`reports/`](reports/)