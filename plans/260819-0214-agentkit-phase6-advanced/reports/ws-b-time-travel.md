# WS-B Report — Time Travel (Checkpoint History + Replay Rewind)

## Scope

Implemented the append-only checkpoint-snapshot history + replay-rewind capability
for durable agent runs. A run's latest checkpoint lives in `agent_runs.checkpoint`
(overwrite-only); this work adds `run_checkpoint_snapshots` so an operator can list
a run's snapshot history (`runs.checkpoints.list`) and re-drive the run from any
earlier snapshot (`runs.replay`) without re-creating the run row.

## Files Modified/Created

| File | Action | Notes |
|------|--------|-------|
| `internal/store/checkpoint_snapshot.go` | created | `CheckpointSnapshot` struct, `CheckpointSnapshotStore` interface, status consts + `ValidCheckpointSnapshotStatus` |
| `internal/store/pg/checkpoint_snapshot.go` | created | PostgreSQL impl (JSONB snapshot, tenant fail-closed) |
| `internal/store/pg/checkpoint_snapshot_test.go` | created | 5 PG tests |
| `internal/store/sqlitestore/checkpoint_snapshot.go` | created | SQLite impl (TEXT snapshot, `-tags sqliteonly`) |
| `internal/store/sqlitestore/checkpoint_snapshot_test.go` | created | 5 SQLite tests |
| `migrations/000101_run_checkpoint_snapshots.up.sql` | created | PG migration |
| `migrations/000101_run_checkpoint_snapshots.down.sql` | created | PG rollback |
| `internal/upgrade/version.go` | modified | `RequiredSchemaVersion` 100 → 101 |
| `internal/store/sqlitestore/schema.go` | modified | `SchemaVersion` 63 → 64, migration patch `63:` |
| `internal/store/sqlitestore/schema.sql` | modified | Appended `run_checkpoint_snapshots` table + 2 indexes (lines 2480-2501) |
| `internal/store/stores.go` | modified | Added `CheckpointSnapshots CheckpointSnapshotStore` field |
| `internal/store/pg/factory.go` | modified | Wire `CheckpointSnapshots: NewPGCheckpointSnapshotStore(db)` |
| `internal/store/sqlitestore/factory.go` | modified | Wire `CheckpointSnapshots: NewSQLiteCheckpointSnapshotStore(db)` |
| `internal/agent/replay.go` | created | `ReplayRun` + `Loop.ResumeRunFrom` (replay/rewind engine) |
| `internal/agent/replay_test.go` | created | 9 agent tests (availability/not-found/dispatch) |
| `internal/gateway/methods/time_travel.go` | created | `TimeTravelMethods`: `runs.checkpoints.list` + `runs.replay` |
| `internal/gateway/methods/time_travel_test.go` | created | 9 handler tests |
| `cmd/gateway_replay.go` | created | `makeRunReplayer` closure (mirrors `makeRunResumer`) |
| `cmd/gateway_methods.go` | modified | Added `checkpointSnapshots` param + TimeTravelMethods wiring |
| `cmd/gateway.go` | modified | `registerAllMethods` call site: added `pgStores.CheckpointSnapshots` |
| `pkg/protocol/methods.go` | modified | `MethodRunsCheckpointsList = "runs.checkpoints.list"`, `MethodRunsReplay = "runs.replay"` |
| `internal/permissions/policy.go` | modified | Checkpoints-list → readMethods; replay → writeExact |
| `internal/i18n/keys.go` + 5 catalogs | modified | `MsgRunsCheckpointsUnavailable`, `MsgRunsReplayUnavailable`, `MsgRunsPauseUnavailable` (en/vi/zh/ko/ru) |

## Design Decisions

- **Tenant-scope fail-closed:** reads fail with ` WHERE 1=0` when the tenant is
  required but absent (both PG + SQLite). Inserts resolve tenant via
  `tenantIDForInsert` (falls back to master). `IsCrossTenant(ctx)` bypasses the
  predicate, matching `pg/contract.go` + `pg/artifact.go` conventions.
- **Snapshot as opaque JSONB/TEXT:** PG stores `snapshot` as JSONB with
  `$5::jsonb` on write and `snapshot::text AS snapshot` on read; SQLite stores
  TEXT bytes. The store layer never parses the pipeline checkpoint payload.
- **Newest-first ordering:** list reads order `seq DESC, id DESC` (PG) /
  `seq DESC, id DESC` (SQLite) — seq is the monotonic per-run snapshot sequence.
- **Replay does NOT re-create the run row:** `ReplayRun` resolves the run
  record + requested snapshot, resolves the owning agent via the router, asserts
  the concrete `Loop` implements `loopReplayer` (a mirror of cmd's
  `loopResumer`), then calls `Loop.ResumeRunFrom(ctx, runID, snapshot)`.
  `ResumeRunFrom` mirrors `Loop.ResumeRun` (loop_run.go:329) except the pipeline
  state comes from the given historical checkpoint: restore pipeline state,
  rebuild `RunRequest` via `runRequestFromRunRecord(run, checkpointRunInput(...))`
  (the checkpoint JSON's `input` field, since `RestoreCheckpoint` deliberately
  drops Input), start a heartbeat updater via `newRunRecordUpdater` on the
  EXISTING row, run via `runViaPipeline` with a durable checkpoint writer, and
  finalize completed/compacting/failed. A corrupt/empty checkpoint falls back
  to a fresh start (non-fatal, matching `ResumeRun`).
- **Nil-safe surface:** every handler reports `UNAVAILABLE` when its store or
  the replay entrypoint is absent; `makeRunReplayer` returns nil when any of
  router/runs/snapshot-store is nil.
- **Viewer scoping:** `runs.replay` does the same viewer-ownership check as
  `runs.resume` (`canSeeAll` + `run.UserID != client.UserID()` → `NOT_FOUND`).
- **Error mapping:** snapshot-not-found / no-rows surface as `NOT_FOUND`;
  capability-missing surfaces as `UNAVAILABLE`; other store errors → `INTERNAL`.

## Tests (5 store per backend, 9 agent, 9 handler)

PG (`internal/store/pg/checkpoint_snapshot_test.go`, `hooksTestDB` + `seedTenantAndAgent`):
1. `TestPGCheckpointSnapshotRoundtrip` — ID/tenant assigned, status preserved, snapshot roundtrip (semantic JSON compare).
2. `TestPGCheckpointSnapshotListNewestFirst` — 4 seqs list 4,3,2,1.
3. `TestPGCheckpointSnapshotListRunScoped` — separate run list isolation.
4. `TestPGCheckpointSnapshotDefaultStatus` — empty status defaults `paused`; unknown status rejected.
5. `TestPGCheckpointSnapshotTenantScope` — tenant B + no-tenant reads fail closed; cross-tenant list empty.

SQLite (`internal/store/sqlitestore/checkpoint_snapshot_test.go`, `openTestDB` + `EnsureSchema` + `seedSQLiteRunTimelineTenant`):
Same 5 scenarios as PG, exact string snapshot compare (TEXT preserves bytes).

Agent (`internal/agent/replay_test.go`):
1. `TestReplayRunUnavailableWhenCapabilitiesMissing` — nil router/runs/snaps → `ErrRunReplayUnavailable`.
2. `TestReplayRunRequiresRunID` — empty run_id rejected.
3. `TestReplayRunNotFoundWithoutRun` — nil run → `ErrRunReplayNotFound`.
4. `TestReplayRunNotFoundWhenSnapshotMissing` — snapshot get error → `ErrRunReplayNotFound`.
5. `TestReplayRunDispatchesToOwnerLoop` — router resolves stub, `ResumeRunFrom` receives the requested checkpoint, result returned.
6. `TestResumeRunFromUnavailableWithoutStore` — nil `runsStore` → `ErrRunResumeUnavailable`.
7. `TestResumeRunFromNotFound` — nil run → `ErrRunResumeNotFound`.
8. `TestResumeRunFromRequiresRunID` — empty run_id rejected.
9. `TestResumeRunFromPropagatesStoreError` — store error propagates raw.

Handler (`internal/gateway/methods/time_travel_test.go`):
1. `TestRunsCheckpointsListUnavailableWithoutStore` — nil store → `UNAVAILABLE`.
2. `TestRunsCheckpointsListSuccess` — runId + items payload.
3. `TestRunsCheckpointsListMissingRunID` — `INVALID_REQUEST`.
4. `TestRunsCheckpointsListRejectsNegativeOffset` — `INVALID_REQUEST`.
5. `TestRunsReplayUnavailableWithoutReplayer` — nil replay → `UNAVAILABLE`.
6. `TestRunsReplaySuccess` — runId/seq/result payload.
7. `TestRunsReplayMissingParams` — missing runId/seq → `INVALID_REQUEST`.
8. `TestRunsReplayNotFoundError` — `ErrRunReplayNotFound` → `NOT_FOUND`.
9. `TestRunsReplayViewerScopedToOwnRun` — viewer + other user's run → `NOT_FOUND`.

No benchmark/load tests were added.

## Dual-DB Lockstep Verification

| Check | Status |
|-------|--------|
| `migrations/000101_run_checkpoint_snapshots.{up,down}.sql` exist | PASS |
| `internal/upgrade/version.go`: `RequiredSchemaVersion uint = 101` | PASS |
| `internal/store/sqlitestore/schema.go`: `SchemaVersion = 64`, migration patch `63:` present | PASS |
| `internal/store/sqlitestore/schema.sql`: `run_checkpoint_snapshots` table + indexes appended | PASS |
| Migration name carries no phase ID (`run_checkpoint_snapshots`) | PASS |
| `store.Stores` exposes `CheckpointSnapshots`; both factories wired | PASS |

## RBAC / Protocol / i18n Consistency

- `runs.checkpoints.list` → `isReadMethod`; `runs.replay` → `isWriteMethod`.
- Both methods resolve to non-`RoleNone` (drift test
  `TestMethodRole_DriftCoverage_AllProtocolMethodsClassified` will pass).
- i18n keys use the `error.runs_*` slug prefix; no overlap with existing keys.
  (WS-A also intends to add pause/hibernate keys; controller coordinates merge.)

## Safety Constraints

- Did NOT touch `internal/pipeline/*` (read-only `MarshalCheckpoint`/
  `RestoreCheckpoint`), `internal/store/run_timeline_store.go` (interface
  unchanged), `internal/store/pg/run_timeline.go` / `sqlitestore/run_timeline.go`
  (interface behavior unchanged), `cmd/gateway_resume.go`, WS-A hibernate files,
  or WS-C mission files.
- No fake data/mocks in production code; only test stubs.
- All user-input SQL parameterized (`$N` PG, `?` SQLite) — no string
  concatenation of user input.
- `context.WithoutCancel` used for heartbeat/terminal writes, matching the
  existing run-record updater pattern.
- Replay reuses `RestoreCheckpoint` — no duplicated checkpoint logic.
- No phase IDs in code comments; comments describe behavior.

## Validation Pending

No local Go toolchain available and the Bash tool is broken (ENAMETOOLONG on
every spawn, including `echo`), so the Docker gate could not be executed in this
session. The controller must run:
`go build ./...`, `go vet ./...`, `go build -tags sqliteonly ./...`,
`go test ./internal/store/pg/... ./internal/store/sqlitestore/...`,
`go test ./internal/agent/... ./internal/gateway/methods/...`.

A full static compile-review of every changed file was completed (all referenced
symbols verified against the codebase: store helper functions, Loop fields,
pipeline checkpoint APIs, protocol frame helpers, permissions lists, i18n keys).

## Files Not Owned (Read-Only Reference)

Read-only references used for patterns only: `pg/contract.go`, `pg/artifact.go`,
`pg/helpers.go`, `pkgSqlxDB` alias, `sqlitestore/helpers.go`, `scan_time.go`,
`run_timeline_test.go`, `schema_migration_test.go`, `migrations/000100_*`,
`internal/agent/loop_run.go`, `loop_types.go`, `router.go`, `run_record.go`,
`loop_pipeline_adapter.go`, `cmd/gateway_resume.go`, `run_timeline.go`
(handler reference for viewer scoping + resume parity), `sessions_test.go`
(`sessionReqFrame`), `hooks_test.go` (`hooksTestDB`, `seedTenantAndAgent`).

Status: DONE
Summary: WS-B time travel implemented end-to-end — append-only checkpoint-snapshot history (PG 000101 + SQLite 63→64, dual-DB lockstep) with tenant fail-closed store, `runs.checkpoints.list` + `runs.replay` WS handlers wired via cmd, and replay that re-drives the owning Loop from a chosen snapshot without re-creating the run row. Tests: 5 store per backend + 9 agent + 9 handler. RBAC read/write classification + drift test covered; i18n keys added to all 5 catalogs.
Concerns/Blockers: Docker build/test gate not executed — no local Go and the Bash tool is broken (ENAMETOOLONG). Static review passed for every file. WS-A runs in parallel on shared peripheral files (permissions, i18n); the controller coordinates serial squash-merge (WS-B first per phase file line 139).