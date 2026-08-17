# P0 Durable Agent Run State Machine Shipped

**Date**: 2026-08-16
**Severity**: Low (completion entry — no incident)
**Component**: Store layer (`agent_runs`), agent loop runtime, run RPC/HTTP/CLI surfaces
**Status**: Resolved — P0 complete, commits NOT yet pushed

## What Happened

§7 of the reliability plan (Durable Agent Run) is done. Agent runs are now a
1-row-per-run durable record instead of depending on a single request/stream.
Previously there was no durable representation of a run outside the in-flight
request; a crash mid-run meant the run simply vanished with no way to observe,
list, or recover it.

Two commits on `feat/reliability-upgrade`:
- `56e435f0` feat(store): durable agent run records (PG + SQLite dual-DB)
- `bfc36d88` feat(runtime): wire durable run-record lifecycle into agent loop

## Technical Details

### Schema (dual-DB)
- PostgreSQL: `agent_runs` table via migration `000097`.
- SQLite: full-schema update + incremental patch in `schema.go` `migrations`
  map, plus `SchemaVersion` bump. This is the CLAUDE.md dual-DB rule: missing
  the SQLite patch crashes the desktop edition on startup, so both were done
  together.

### RunsStore interface
`CreateRun / UpdateRunStatus / UpdateRunTerminal / TouchHeartbeat /
GetRun / ListRuns / RecoverStaleRuns`, implemented for both `store/pg` and
`store/sqlitestore` using the shared `store/base` dialect pattern
(`BuildMapUpdate` / `BuildScopeClause`).

### Runtime lifecycle
- Non-blocking heartbeat ticker so live runs never get swept as stale.
- Panic safety-net finalizer.
- **Single choke point at `Loop.Run`**: every run origin — chat WS, cron,
  channels, heartbeat ticker, delegation, HTTP — creates/updates the record
  through one path. No per-origin divergent handling.
- `reliability.runs.*` config: heartbeat / stale-after / sweep /
  extension-budget.

### Surfaces
- `runs.get/list/events` RPC with viewer-scoped RBAC + status-enum validation.
- `/v1/runs` HTTP endpoints with resync cursor.
- `goclaw run list/get/events` CLI.
- i18n keys in all 5 catalogs.

### Scope honored (P0 only)
Records an `attempt` field. Auto-retry and resume are explicitly deferred to a
later phase — acceptance was to persist the durable record, not to add
recovery automation.

## Verification

- `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...` all pass
  (run in Docker `golang:1.26.0-alpine` via the persistent
  `goclaw-gomodcache` volume — there is no local Go on the Windows host).
- Affected package tests pass: `internal/agent`, config, store, store/pg,
  store/base, gateway/methods.
- A code-reviewer pass caught one issue: WS `runs.list` lacked status-enum
  validation. Fixed by adding a `store.ValidAgentRunStatus` helper and
  re-verified.

## Lessons Learned

1. **Single choke point beats per-origin handle.** Routing every run origin
   through `Loop.Run` avoids the classic drift where a new origin (heartbeat,
   delegation) forgets to create a record. Worth keeping for any future run
   state expansion.
2. **Dual-DB migrations are not optional.** The PG migration alone would leave
   the SQLite desktop build broken on startup. Do both in the same change or
   it ships with a landmine.
3. **Scope discipline paid off.** Recording `attempt` now but deferring
   auto-retry/resume kept the PR reviewable and shippable rather than ballooning
   across the whole reliability plan.

## Operational Note

The git-manager subagent was blocked this session because the Bash tool
returned no output. The commit was done directly via the PowerShell tool, which
works on this host. The two commits are **not pushed**.

## Next Steps

- Push `feat/reliability-upgrade` and open the PR.
- Later phases: auto-retry + resume using the `attempt` field.
- Invariant test for run lifetime/tenant isolation when P1 scenarios are
  built out.
