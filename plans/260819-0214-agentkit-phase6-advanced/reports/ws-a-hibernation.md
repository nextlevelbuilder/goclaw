# WS-A Hibernation — Implementation Report

**Status: DONE_WITH_CONCERNS**

**Date:** 2026-08-19
**Worktree:** `C:\Users\DORA\Downloads\goclaw-mod\.claude\worktrees\phase6-ws-a-hibernation` (branch `worktree-phase6-ws-a-hibernation`, fork from `dev` @ af487052)
**Scope:** Intentional suspend/wake (hibernation) — `runs.pause` writes checkpoint + paused on-demand, `runs.wake` reuses the existing resume path.

---

## Summary

All WS-A source deliverables are implemented, verified statically against the live
codebase, and written into the worktree. The Docker build/test gate and the git
commits **could not be executed** because the harness cannot spawn ANY shell
process in this worktree (Bash tool returns `ENAMETOOLONG: name too long,
uv_spawn` before running even `echo`; retried via subagents, `git -C`, `cmd.exe`,
and sandbox-disabled — all fail identically at spawn level). This is an
environment constraint, not a code issue. The gate + commits must be re-run from
a session whose CWD is shorter (e.g. the main repo root).

## Acceptance criteria verification

| Criterion | State | Evidence |
|---|---|---|
| `runs.pause` writes checkpoint + sets `paused` on-demand | ✓ implemented | `internal/agent/hibernate.go:101-142` (`Loop.SuspendRun` → `UpdateRunCheckpoint(runID, "paused", run.Checkpoint)`) |
| `runs.wake` reuses existing resume path, no duplicate resume logic | ✓ implemented | `cmd/gateway_hibernate.go:20-25` (`makeRunSuspendResumer` returns `makeRunResumer` for wake) |
| Protocol consts | ✓ | `pkg/protocol/methods.go:58-59` (`MethodRunsPause`/`MethodRunsWake`), `pkg/protocol/run_events.go` (new) |
| Permissions `isWriteMethod` | ✓ | `internal/permissions/policy.go:368-369` |
| i18n keys in all 5 catalogs | ✓ | `internal/i18n/keys.go:87-88` + `catalog_{en,vi,zh,ko,ru}.go` |
| Nil-safe wiring | ✓ | `cmd/gateway_methods.go:38-46` — nil closures report unavailable |
| Builds/tests green | ⚠ BLOCKED | Bash spawn unusable in worktree (env) |
| No store-layer (WS-B) or pipeline modifications | ✓ | Only the 15 owned files touched |

## Files modified (exact)

```
pkg/protocol/methods.go                 +MethodRunsPause/MethodRunsWake
pkg/protocol/events.go                  +AgentEventRunPaused/AgentEventRunWoken
pkg/protocol/run_events.go              NEW (EventRunPaused/EventRunWoken + payloads)
internal/permissions/policy.go          +runs.pause/runs.wake → isWriteMethod
internal/i18n/keys.go                   +MsgRunsPauseUnavailable/MsgRunsWakeUnavailable
internal/i18n/catalog_en.go             +2 keys
internal/i18n/catalog_vi.go             +2 keys
internal/i18n/catalog_zh.go             +2 keys
internal/i18n/catalog_ko.go             +2 keys
internal/i18n/catalog_ru.go             +2 keys
internal/agent/hibernate.go             NEW (SuspendRun/WakeRun/Loop.SuspendRun)
internal/agent/hibernate_test.go        NEW
internal/gateway/methods/hibernate.go   NEW (HibernateMethods)
internal/gateway/methods/hibernate_test.go NEW
cmd/gateway_hibernate.go                NEW (makeRunSuspendResumer)
cmd/gateway_methods.go                  +HibernateMethods registration
```

**Out of scope — NOT touched:** `internal/store/**` (WS-B owns),
`internal/pipeline/**` (read-only), `internal/agent/replay.go` (WS-B), mission files (WS-C).

## Implementation notes (design decisions)

1. **Pause writes the STORED durable checkpoint, not a live in-memory marshal.**
   The pipeline's live `RunState` is inaccessible from the suspend path (inside
   `runViaPipeline` scope) and `internal/pipeline/*` cannot be modified. The
   durable checkpoint written by CheckpointStage every N iterations is the
   newest recoverable snapshot, so `Loop.SuspendRun` re-reads it and rewrites it
   with status `paused` — the identical store method `runRecordUpdater.checkpoint`
   drives. Nil checkpoint → status `paused` with nil → `ResumeRun` falls back to
   a fresh start (same non-fatal recovery as a corrupt checkpoint). Confirmed via
   `run_state.go:125-149` (`MarshalCheckpoint` shape) and `loop_run.go:344-358`.

2. **Wake = the exact `makeRunResumer` closure.** `makeRunSuspendResumer`
   (`cmd/gateway_hibernate.go`) builds a suspend closure that delegates to
   `agent.SuspendRun` (which resolves the owning agent via router + asserts the
   narrow `loopSuspender` interface, mirroring `cmd/gateway_resume.go`'s
   `loopResumer`) and returns `makeRunResumer(...)` verbatim for wake. Zero
   resume logic duplicated. `agent.WakeRun` is a tiny named delegating wrapper.

3. **Pause event goes through `l.emit`** so it stamps the per-run WS Seq and is
   broadcast through the same channel as `run.started`/`run.completed`
   (verified against `loop_tracing.go:28-46`). `run.paused` is NOT in
   `isTerminalRunTimelineEvent`, so the per-run seq counter survives into the
   resumed execution. `timelineKindForEvent` returns false for it (no timeline
   item; pause is not terminal and must not clear the run journal).

4. **RBAC + drift test.** Both methods added to `isWriteMethod` →
   `MethodRole` = RoleOperator, satisfying
   `TestMethodRole_DriftCoverage_AllProtocolMethodsClassified`
   (`internal/permissions/policy_test.go:354`) which parses methods.go at test
   time and fails on any `Method*` const resolving to RoleNone.

5. **i18n.** All 5 catalogs (`en/vi/zh/ko/ru`) have both keys; the ko catalog
   anchor was `MsgRunsUnavailable` (it lacks `MsgRunTimelineUnavailable`).
   No git-key parity test is affected (`git_keys_parity_test.go` is scoped to
   `MsgGitCred*`).

## Tests added

- `internal/agent/hibernate_test.go` — Loop.SuspendRun: unavailable w/o store,
  empty runID, not-found, checkpoint+paused write (asserts recorded store call +
  emitted AgentEventRunPaused with iteration), idempotent on paused, terminal
  runs untouched, store-error propagation; package SuspendRun: router resolution
  via UUID→resolver, unavailable w/o wiring, agent without suspend capability;
  WakeRun delegate + nil-resume; checkpointIteration edge cases.
- `internal/gateway/methods/hibernate_test.go` — handlers: unavailable without
  suspend/resumer, missing runId → INVALID_REQUEST, success (paused status echo;
  wake runId echo), viewer-scoped-to-own-run NOT_FOUND, pause/wake error mapping.

## Docker gate result

**NOT RUN — environment blocked.** The required command is:

```bash
MSYS_NO_PATHCONV=1 docker run --rm \
  -v /c/Users/DORA/Downloads/goclaw-mod/.claude/worktrees/phase6-ws-a-hibernation:/src \
  -w /src -v goclaw-gomodcache:/go/pkg/mod \
  golang:1.26.0 sh -c "go build ./... && go vet ./... && go build -tags sqliteonly ./... && go test ./internal/agent/... ./internal/gateway/methods/... ./pkg/protocol/... ./internal/permissions/... ./internal/i18n/..."
```

Every shell attempt (Bash, subagent Bash, `git -C`, `cmd.exe`, sandbox-disabled)
failed identically with `ENAMETOOLONG: name too long, uv_spawn` before any
command ran. **Recommended:** run the gate and commits from the controller's
session (shorter CWD) or after restarting the runtime, as per the git-manager
report.

## Git commits

**NOT created — environment blocked.** Intended commits (conventional, no AI refs):

1. `feat(agent): add intentional suspend/wake (hibernation) run lifecycle`
   (pkg/protocol/*, internal/permissions/policy.go, internal/i18n/*,
   internal/agent/hibernate.go, internal/agent/hibernate_test.go)
2. `feat(gateway): wire runs.pause/runs.wake WS methods`
   (internal/gateway/methods/hibernate.go, hibernate_test.go,
   cmd/gateway_hibernate.go, cmd/gateway_methods.go)

## Concerns / Blockers

- **Env:** Bash spawn is broken for this worktree path; build/test gate + commit
  must be executed from a shorter-CWD session or the main repo.
- **Static-only verification:** source compiles by construction (all referenced
  symbols verified against live code), but `go build/vet/test` has not executed
  in this worktree.
- **Idle-timer:** optional, config-gated `runs_idle_suspend_minutes` — NOT
  implemented; deduped from active scope per plan §acceptance (optional
  criterion). Surface parity: WS/HTTP/CLI — new methods are WS-only by design;
  HTTP/CLI resume endpoints remain untouched.

---

```
Status: DONE_WITH_CONCERNS
Summary: WS-A hibernation fully implemented in worktree (protocol consts, permissions, i18n x5, agent SuspendRun/WakeRun, HibernateMethods handlers, cmd wiring, 2 test files). Build/test gate and git commits BLOCKED by environment (Bash spawn ENAMETOOLONG on the long worktree CWD).
Concerns/Blockers: Run the Docker gate + commits from a shorter-CWD session; static verification only until then.
```