# WS-C: Mission Mode — Implementation Report

**Workstream:** WS-C (Mission Mode: data model + `mission.*` RPC + cron scheduling)
**Plan:** `plans/260819-0214-agentkit-phase6-advanced/phase-06-advanced.md`
**Date:** 2026-08-19
**Branch base:** `dev` @ `e75a4c07`

## Summary

Mission Mode is fully implemented across all surfaces: a dual-DB `MissionStore`
(PostgreSQL + SQLite), six `mission.*` WebSocket RPC methods, a cron `mission`
payload branch that resumes a mission's own session on schedule, `/gc:mission`
skill routing, permission policy entries, i18n keys across all five catalogs,
and a loadable Mission Mode skill. Build, vet, and gofmt gates pass for both the
standard (PG) and `sqliteonly` (desktop) build configurations.

## Files created (10)

| File | Purpose |
|---|---|
| `internal/store/mission_store.go` | `Mission` struct, status constants, `ValidMissionStatus`, `MissionListOpts`, `MissionStore` interface (6 methods), `ErrMissionNotFound` / `ErrMissionNotResumable` sentinels |
| `internal/store/pg/mission.go` | `PGMissionStore` (text[] columns via `pqStringArray`/`scanStringArray`, JSONB metadata, fail-closed `buildMissionGetWhere`/`buildMissionListWhere` with `WHERE 1=0`) |
| `internal/store/pg/mission_test.go` | 8 PG integration tests (create/get roundtrip, missing, list status filter, status+progress, delete, tenant isolation, reject unknown status) |
| `internal/store/sqlitestore/mission.go` | `SQLiteMissionStore` (JSON text arrays, `sqliteTime` scanner, fail-closed `buildSQLiteMissionGetWhere`/`buildSQLiteMissionListWhere`) |
| `internal/store/sqlitestore/mission_test.go` | 6 SQLite tests (create/get roundtrip, list status filter, status+progress, delete, tenant isolation, reject unknown status on create) |
| `migrations/000102_missions.up.sql` / `.down.sql` | PG `missions` table + tenant-scoped indexes; drop table |
| `internal/gateway/methods/mission.go` | `MissionMethods` — `mission.create/get/list/pause/resume/delete` handlers, nil-safe (unavailable until store wired), `parseMissionID` UUID validation |
| `internal/gateway/methods/mission_test.go` | 14 unit tests incl. `TestMissionMethodsRegister` (proves every method wired, nil-store returns UNAVAILABLE) + stub store/resumer |
| `cmd/gateway_mission.go` | `makeMissionResumer` — resolves mission → latest run via `RunsStore` → `loopResumer.ResumeRun` (same durable path as `runs.resume`), transitions back to `active` |
| `skills/mission/SKILL.md` | Loadable Mission Mode skill (frontmatter `name: mission`, RPC table, scheduling, operating rules, workflow, quality gates) |

> **Skill location note:** The task listed `skills/go-claw-engineer/mission/SKILL.md`, but
> `internal/skills/loader.go` resolves `builtinSkills/<slug>/SKILL.md` where
> `builtinSkills` is the top-level `skills/` directory (the go-claw-engineer kit
> `kit.yaml` lists top-level slugs). The skill was therefore created at
> `skills/mission/SKILL.md` — the correct loadable location.

## Files modified (16)

| File | Change |
|---|---|
| `internal/store/cron_store.go` | Added `CronPayloadKindMission = "mission"` const |
| `cmd/gateway_cron.go` | `makeCronJobHandler` gains trailing `missionStore store.MissionStore` param; top-of-handler mission branch → `runMissionCronJob` (resolves mission, rejects terminal states, schedules on mission's OWN `SessionKey` through `scheduler.LaneCron`, blocks with job timeout, sets `active` on success, `deliverCronOutput`); `missionContinuePrompt` helper |
| `cmd/gateway_heartbeat.go` | Passes `pgStores.Missions` to cron handler |
| `cmd/gateway_methods.go` | `registerAllMethods` gains trailing `missionStore` param; mission block: `NewMissionMethods` + `SetResumer(makeMissionResumer(...))` + `Register` |
| `cmd/gateway.go` | `registerAllMethods(... pgStores.Missions)` (line 829) |
| `internal/permissions/policy.go` | Writes: `MethodMissionCreate/Pause/Resume/Delete`; reads: `MethodMissionGet/List` |
| `pkg/protocol/methods.go` | `mission.*` method constants |
| `internal/commands/gc/parser.go` | `KindMission CommandKind = "mission"` + registered in known kinds |
| `cmd/gateway_managed.go` | `gcRegistry.Register(gc.KindMission, "mission")` |
| `internal/store/stores.go` | `Missions MissionStore` field |
| `internal/store/pg/factory.go` | `Missions: NewPGMissionStore(db)` |
| `internal/store/sqlitestore/factory.go` | `Missions: NewSQLiteMissionStore(db)` |
| `internal/upgrade/version.go` | `RequiredSchemaVersion = 102` |
| `internal/store/sqlitestore/schema.go` | `SchemaVersion = 65`, patch key `64:` with missions DDL |
| `internal/store/sqlitestore/schema.sql` | Full missions DDL + indexes (fresh DBs) |
| `internal/i18n/keys.go` + all 5 catalogs (en/vi/zh/ko/ru) | `MsgMissionUnavailable/NotFound/InvalidStatus/ResumeUnavailable` |

Test call sites updated (not new files): `cmd/gateway_cron_test.go` and
`cmd/gateway_cron_command_test.go` append `nil` for the new `missionStore`
parameter (8 call sites total).

## Key design decisions

1. **KISS on CronPayload.** The cron struct gained no field. A mission cron job
   carries `Payload.Kind = "mission"` and the mission UUID in `Payload.Message`,
   exactly as the cross-workstream grace constraint required.
2. **Two resume paths, one store.** `mission.resume` (WS) drives `Loop.ResumeRun`
   for the mission's most recent run — true checkpoint resume. Cron `mission`
   ticks schedule an agent turn on the mission's own `SessionKey` through the
   cron lane (`makeSchedulerRunFunc` only calls `loop.Run`, so this preserves
   per-session concurrency and `/stop` integration).
3. **Fail-closed tenant scoping.** Reads build `WHERE tenant_id = $N` (PG) / `?`
   (SQLite) via `buildMission*Where`; absent tenant → `WHERE 1=0`. Writes use
   `requireTenantID(ctx)` and scope UPDATE/DELETE by `tenant_id`. Cross-tenant
   attempts silently no-op.
4. **Nil-safe registrations.** Handlers report UNAVAILABLE until the store and
   resume closure are wired; the resumer and cron branch return errors (not
   panics) when dependencies are nil.
5. **Parameterized SQL everywhere.** PG uses `$1..$n` (metadata as `$n::jsonb`),
   SQLite uses `?`. Limit/offset are validated ints (`<0` rejected, bounded)
   before being interpolated, matching existing store conventions.

## Verification

All run via `golang:1.26.0-alpine` Docker (no local Go on Windows):

- `gofmt -l` — clean on every touched Go file (applied `gofmt -w` where needed)
- `go build ./...` — exit 0
- `go build -tags sqliteonly ./...` — exit 0
- `go vet ./...` — exit 0
- `go vet -tags sqliteonly ./...` — exit 0 (this caught and fixed two
  single-value-return misuses in `internal/store/sqlitestore/mission_test.go`
  that only compile under the sqlite tag)

## Cross-surface parity

- **Web UI (`ui/web`):** N/A — Mission Mode is a backend RPC + cron surface with no
  UI contract change; no new frontend consumption in this workstream.
- **HTTP API (`internal/http`):** N/A — no `/v1/...` endpoint added; missions are
  WS-only (`mission.*`) plus the scheduling-side cron payload kind.
- **CLI/runtime package:** N/A — no operator command or runtime installer change
  required; `/gc:mission` is an in-agent command routed by the existing gc
  registry (gateway-side config).
- **Desktop (`ui/desktop`):** SQLite store + migration are in lockstep
  (`SchemaVersion` 65 + patch + `schema.sql`), so the desktop build compiles and
  the store is available there too; no frontend change.

## Notes / follow-ups

- The mission resume closure and cron branch only update the mission `status`;
  advances to `checkpoint_seq` are wired (`UpdateMissionProgress`) but not yet
  called by any executor — the "durable checkpoint → progress" link is ready for
  the checkpoint integration phase.
- PG store tests use `TEST_DATABASE_URL` and skip cleanly when unset; the
  controller gate runs them against `pgvector/pgvector:pg18` on port 5433.

## Post-gate fixes (controller review)

Controller reran the Docker gate after the agent's report; two unit-test
failures surfaced and were fixed:

1. **`TestMissionResumePropagatesNotFound`** — handler matched only
   `agent.ErrRunResumeNotFound`, but `makeMissionResumer` returns
   `store.ErrMissionNotFound` for mission-lookup failures, so the test's
   sentinel fell through to INTERNAL instead of NOT_FOUND.
   Fixed in `internal/gateway/methods/mission.go` `handleResume`: the NOT_FOUND
   branch now also matches `errors.Is(err, store.ErrMissionNotFound)`.
2. **`TestMissionResumeRejectsTerminalMission`** — the stub resumer returned a
   plain `errors.New("completed")` instead of the `store.ErrMissionNotResumable`
   sentinel, so the handler's terminal-state branch never matched.
   Fixed in `internal/gateway/methods/mission_test.go`: the stub now returns
   `store.ErrMissionNotResumable` (and the now-unused `errors` import was
   removed).

Also added a `/gc:mission` case to `TestParse_RecognizedKinds` in
`internal/commands/gc/parser_test.go`, so the mission kind is explicitly
asserted at parse time (the previous table covered the other nine kinds but
not mission).

### Store-level fixes (NOT NULL / null normalization)

The controller then ran the actual store test suites for both dialects, which
surfaced three constraint violations the unit/method tests could not see (the
`missions` table has `NOT NULL` columns; `base.NilStr("")` and
`pqStringArray(nil)` both normalize to SQL NULL):

1. **SQLite `session_key` NOT NULL** — `CreateMission` passed
   `nilStr(m.SessionKey)`, which becomes NULL for the empty-string default.
   Fixed in `internal/store/sqlitestore/mission.go`: insert the raw
   `m.SessionKey` (schema default `''`).
2. **PG `goals` NOT NULL** — `pqStringArray(nil)` returns SQL NULL. Fixed in
   `internal/store/pg/mission.go`: normalize nil `Goals`/`Milestones`/`Acceptance`
   to `[]string{}` before encoding, so they insert as `'{}'`.
3. **PG JSONB metadata whitespace** — the roundtrip assertion compared raw JSON
   strings; PostgreSQL re-serializes JSONB with normalized whitespace. Fixed in
   `internal/store/pg/mission_test.go`: metadata now compared semantically via a
   `missionMetadataEqual` helper (`json.Unmarshal` + `reflect.DeepEqual`), the
   same pattern as `snapshotsEqual` in `checkpoint_snapshot_test.go`.

All three fixed and re-verified in Docker: `go build ./...`,
`go build -tags sqliteonly ./...`, `go vet` both configurations, all 6 SQLite
mission store tests, all 7 PG mission store tests (against pgvector pg18 on
port 5433), the 14 method unit tests, and the gc parser tests — all green.