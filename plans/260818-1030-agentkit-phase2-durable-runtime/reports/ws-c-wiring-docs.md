# WS-C Report — Phase 2 Durable Runtime (G6 wiring, version lockstep, docs)

**Date**: 2026-08-18
**Agent**: WS-C (wiring + version lockstep + docs)
**Scope**: `cmd/gateway_managed.go`, `cmd/gateway.go`, `internal/upgrade/version.go`,
`internal/store/sqlitestore/schema.go`/`schema.sql`, `docs/`. NO commit, NO Docker, NO store-impl edits.

## Summary

G6 startup recovery is already fully wired on BOTH gateway paths via the shared
`wireExtras` in `cmd/gateway_managed.go`. No new migration and no SQLite schema
change are needed (checkpoint column and all new string enums fit existing
columns/indexes). WS-B's resumer attack surface (`SetResumer`/`Resumer`/`Loop.ResumeRun`)
does NOT exist yet anywhere in the repo — resumer wiring is deferred. One
minimal doc update in `docs/04-gateway-protocol.md` §6.1 documenting the
paused/resumable recovery semantics.

---

## Task 1 — `cmd/gateway_managed.go`

### 1a. `timelineRecorder.Record(event)` (L337) — KEEP AS-IS
Present and wired inside `ResolverDeps.OnEvent`:

```go
timelineRecorder := agent.NewRunTimelineRecorder(stores.RunTimeline)  // L210
...
OnEvent: func(event agent.AgentEvent) {
    ... msgBus.Broadcast(...)
    timelineRecorder.Record(event)                                     // L337
}
```

No change needed.

### 1b. Startup recovery (L215-221) — KEEP AS-IS
`RecoverInterruptedRuns` runs once at startup inside `wireExtras`, cross-tenant.
No code change. WS-A owns the resume-aware store semantics (checkpoint → `paused`
instead of terminal-`failed`), which flows through this existing call. The
startup log will report the store's transition count once WS-A lands; adding a
code comment now would describe behavior not yet present in the store, so it is
documented here instead (see Concerns).

### 1c. Resumer wiring — DEFERRED (WS-B surface absent)
Grep across the entire repo for `SetResumer`, `resumer`, `Resumer`, `ResumeRun`:
**zero matches**. Specifically:
- `internal/gateway/methods/run_timeline.go` — `RunTimelineMethods` has NO
  resumer field/setter (methods: `timeline`, `runs`, `cfg`).
- `internal/http/traces.go` — `TracesHandler` has NO resumer field/setter
  (`runs`, `timeline` only).
- `internal/agent/loop_types.go` / `loop_run.go` — `Loop` has NO `ResumeRun`
  method; `agent.Agent` interface (`types.go:15`) has no resume entry.

Cannot wire a `func(ctx, runID) (*agent.RunResult, error)` → `Loop.ResumeRun`
closure because the target symbols do not exist; adding the wiring would break
the build. Per task instructions this is recorded for controller / follow-up
phase. When WS-B lands `SetResumer` on `RunTimelineMethods` and `TracesHandler`
plus `Loop.ResumeRun`, WS-C should wire the real closure via the agent router
(`agentRouter.Get(ctx, agentKey)` → `ResumeRun`); at that point the WS method /
HTTP handler should be left returning "resumer not wired" until the cmd wiring
lands.

---

## Task 2 — `cmd/gateway.go` (standard PG path)

Verified — **no change needed, no duplicate 1-shot recovery**:

| Concern | Status |
|---------|--------|
| `SetRunTimelineStore(pgStores.RunTimeline)` | Present L639-641 |
| `RunsStore` wired into WS methods | `registerAllMethods(... RunTimeline, Runs ...)` L823 → `gateway_methods.go:31-33` (`NewRunTimelineMethods` + `SetRunsStore`) |
| `RunsStore` wired into HTTP | `wireHTTP` → `gateway_http_handlers.go:64-66` (`tracesH.SetRunsStore`) |
| Startup `RecoverInterruptedRuns` | Present via shared `wireExtras` (gateway_managed.go L215-221, called from gateway.go L692) |
| Periodic `RecoverStaleRuns` sweep | Present via shared `wireExtras` (gateway_managed.go L664-670) |

Both gateway paths (`runGateway` PG standard, desktop SQLite via `cmd.RunGateway`)
share `wireExtras`, so recovery parity is automatic. Nothing to add.

**Concern (pre-existing, out of WS-C file scope):** the standard PG path also
starts a second stale-run sweep in `startCronAndHeartbeat`
(`cmd/gateway_heartbeat.go:110-116`), so two `runStaleRunsSweep` goroutines
tick on PG. They are idempotent (a run only transitions once when stale), so
this is wasteful, not harmful. `gateway_heartbeat.go` is NOT in WS-C's allowed
file list — flagging for controller: consider dropping the duplicate sweep in a
follow-up.

---

## Task 3 — `internal/upgrade/version.go`

**No migration needed — keep `RequiredSchemaVersion = 98`.**

Verified:
- `agent_runs.checkpoint` JSONB exists from migration `000097_agent_runs.up.sql` (L16).
- New item types (`chunk`/`thinking`/`tool.started`) and status (`paused`)
  are string values that fit existing columns:
  - `run_timeline_items.item_type VARCHAR(40)` (000074 L11), SQLite `TEXT`.
  - `agent_runs.status VARCHAR(40)` (000097 L14), SQLite `TEXT`.
- No index added by this phase (see Task 4).

---

## Task 4 — SQLite schema (`schema.go` + `schema.sql`)

**No schema change needed — keep `SchemaVersion = 61`.**

Verified:
- `agent_runs.checkpoint TEXT` already present in `schema.sql:650` (fresh DBs)
  and patch migration `59` in `schema.go:110` (incremental upgrade).
- `run_timeline_items.item_type TEXT` (schema.sql:610) already.
- Recovery queries and their index needs:
  - `RecoverStaleRuns` — `(status IN (...), heartbeat_at < deadline)`; covered by
    `idx_agent_runs_tenant_status`. Cross-tenant startup/periodic on a small
    table; a `(heartbeat_at)` index adds little.
  - `RecoverInterruptedRuns` — groups over `run_timeline_items` by `run_id`;
    covered by `idx_run_timeline_run_seq`.
  - WS-A's resume-aware query (checkpoint IS NOT NULL detection) filters on
    `checkpoint` + status; no index is warranted for a startup reconciliation
    query. If a later phase adds frequent `WHERE checkpoint IS NOT NULL`
    list queries, add `idx_agent_runs_checkpoint` then — not now.

---

## Task 5 — Docs

**Updated**: `docs/04-gateway-protocol.md` §6.1 Durable Run Records.

Added two sentences documenting the Phase 2 resume semantics (checkpoint →
`paused`, resumable via `runs.resume` WS + `POST /v1/runs/{run_id}/resume`),
matching the accepted plan contract
(`RunTimelineStatusPaused = "paused"`, G6). This is the smallest owning docs
surface; `docs/06-store-data-model.md` has no `agent_runs` section to update.
The endpoint tables in §6 / §6.1 RPC table were NOT touched (WS-B owns the
`runs.resume` method + HTTP route registration and their doc rows).

---

## Files Modified

| File | Change |
|------|--------|
| `docs/04-gateway-protocol.md` | §6.1 end: added checkpoint/paused/resume recovery semantics (3 sentences) |

**Not modified** (verified no change justified):
- `cmd/gateway_managed.go`, `cmd/gateway.go`
- `internal/upgrade/version.go`
- `internal/store/sqlitestore/schema.go`, `schema.sql`
- `migrations/*`

---

## Validation Notes

No Go compiler available on host (per memory: build in Docker — not run per
constraint). Verification done by close code reading:
- `grep -rn 'Resumer\|ResumeRun'` → 0 matches (WS-B surface absent).
- Recovery wiring traced through `wireExtras` shared by both gateway paths.
- Schema/column types confirmed against PG migration `000097` + `000074` and
  SQLite `schema.sql` + `schema.go` patch `59`.

---

## Next Steps (for controller / follow-up)

1. After WS-A lands resume-aware `RecoverStaleRuns`/`RecoverInterruptedRuns`:
   consider updating the startup log wording in `wireExtras` (gateway_managed.go
   L219) from "marked interrupted runs as failed" to distinguish `paused`.
2. After WS-B exposes `SetResumer` on `RunTimelineMethods`/`TracesHandler` and
   `Loop.ResumeRun`: wire the real resumer closure in `wireExtras` (needs an
   `agentRouter` handle — available via closure since resolver is built there).
3. Investigate the duplicate `runStaleRunsSweep` on the standard PG path
   (`wireExtras` + `startCronAndHeartbeat`); consider keeping only one.

---

Status: DONE_WITH_CONCERNS
Summary: G6 startup recovery already wired on both gateway paths via shared wireExtras;
resumer wiring deferred because WS-B's SetResumer/ResumeRun surface is absent from the repo;
no migration/schema change needed; minimal docs update in 04-gateway-protocol.md.
Concerns/Blockers: (1) resumer wiring blocked on WS-B landing SetResumer/Loop.ResumeRun —
calling code would not compile today; (2) duplicate periodic stale-run sweep on standard PG
path (pre-existing, in gateway_heartbeat.go, outside WS-C file scope) — flag for cleanup;
(3) store-implementation recovery semantics (checkpoint → paused) depend on WS-A and were not
touched here.