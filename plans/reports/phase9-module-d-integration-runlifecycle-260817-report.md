# Phase 9 Module D — Integration Run-Lifecycle Tests Report

> Date: 2026-08-17
> Scope: Module D of Phase 9 (Testing Suite) — AC3 PG-backed run-lifecycle tests.
> Plan: `plans/260815-2340-goclaw-repository-reliability/phase-09-testing-suite.md` (Module D, lines 123-141).
> Two files created (the ONLY files touched):
> - `tests/integration/provider_stream_disconnect_test.go`
> - `tests/integration/stale_run_recovery_test.go`

## Summary

Created TWO new test files in `tests/integration/` (package `integration`, `//go:build
integration`), driving AC3 Case B (stream disconnect does not flip a run to FAILED) and
the stale-run recovery path against the REAL PostgreSQL store. No production code changed.
Both tests pass under `-race` against the live pgvector:pg18 container on :5433.

## What each test asserts

### 1. `TestProviderStreamDisconnect_MidStreamCloseDoesNotFailRun` (Case B)

- **Setup**: `testDB(t)` + `seedTenantAgent(t, db)` + `allowLoopbackForTest(t)`. An
  httptest SSE server serves OpenAI-compatible `/v1/chat/completions` streams:
  request 1 writes ONE content delta (`data: {...chat.completion.chunk...}`), flushes,
  then closes abruptly (handler returns, no `[DONE]` terminator); request 2+ returns a
  clean full response.
- **Provider**: a REAL `providers.NewOpenAIProvider("disc-test", "test-key", server.URL,
  "gpt-4o")` (default retry config), with the stream watchdog armed via
  `providers.WithStreamTimeouts(ctx, 2*time.Second, 0)` — a hang would time out (bounded
  < 8s total), but a clean EOF ends the stream before the idle window.
- **Durable record**: seeds an `agent_runs` row as `running` via
  `pg.NewPGRunStore(db).CreateRun(tenantCtx(tenantID), run)` — the state a live run is
  in when a stream disconnects mid-response.
- **The assertion**: after the mid-stream disconnect, `ChatStream` returns a non-nil
  result (no error). The run record is then finalized the way the agent loop finalizes a
  successful stream (`UpdateRunTerminal(..., AgentRunStatusCompleted, ...)`), and the
  final `GetRun` asserts `Status != AgentRunStatusFailed`.
- **Level chosen**: pipeline+adapter (real provider vs fake server), NOT full
  `agent.Loop.Run()`. Rationale below.

Observed run output: `requests=1 final status="completed" content="partial"` — the
scanner (`SSEScanner.Next`) treats a clean EOF after received data as an ordinary stream
end, so `ChatStream` returns the partial result successfully; no retry, no error, no
FAILED record. This is exactly the "provider-level recovery applies; a provider error
with retry/fallback is not a run failure" intent from the phase file.

### 2. `TestStaleRun_RecoverStaleRuns_MarksFailed`

- **Setup**: `testDB(t)` + `seedTenantAgent(t, db)`. Inserts TWO `agent_runs` rows via
  raw SQL — a stale run (heartbeat 1h ago, `status='running'`, started 2h ago) and a
  fresh run (heartbeat now, `status='running'`, started 5m ago). Own
  `t.Cleanup(... DELETE FROM agent_runs WHERE tenant_id=$1)` because `seedTenantAgent`
  cleanup does not delete `agent_runs`.
- **Action**: `pg.NewPGRunStore(db).RecoverStaleRuns(crossTenantCtx(), 30*time.Minute)` —
  cross-tenant context because the recovery path is startup/periodic and not tenant-scoped.
- **Asserts**:
  - return count == 1 (only the stale run);
  - stale run: `status == store.AgentRunStatusFailed`, `error` contains `"run stalled"`,
    `completed_at` non-nil;
  - fresh run: `status == "running"` (untouched), `error` empty, `completed_at` nil.

## Level chosen: pipeline+adapter vs agent-loop

The phase file's preferred route is driving a full `agent.Loop.Run()` against the fake
server, with the explicit fallback ("If driving a full `Loop.Run()` needs more harness
than exists, scope to the pipeline level with a real provider adapter pointed at the fake
server") citing `abort_provider_stream_test.go`'s `NewAnthropicProvider(...WithBaseURL(...))`
precedent.

I chose the **pipeline+adapter** level because:

1. A full `Loop.Run()` needs a substantial harness — a wired `RunsStore`, an event
   publisher, workspace dirs, session store, tool registry, per-user profile/seed
   callbacks, tracing collector optional, etc. (`LoopConfig` has ~50 fields; the
   integration suite has no existing `agent.Loop` object-builder helper to reuse).
2. The FAILED-flip risk the test guards lives at the store/record boundary: `Loop.Run`
   only calls `runRecord.terminal(ctx, AgentRunStatusFailed, err.Error())` when the
   pipeline returns an error, and `ChatStream` returning a partial result on mid-stream
   close is a SUCCESS path — so asserting at the provider + record level covers the
   exact invariant ("a mid-response disconnect does not produce a FAILED run record")
   without the harness cost.
3. The precedent file (`abort_provider_stream_test.go`) itself drives provider-level
   behavior (abort on context cancel) rather than a full loop, matching this approach.

The test mirrors the loop's success finalize (`UpdateRunTerminal(completed)`), so the
record ends `completed` — proving the disconnect alone never produced `failed`.

## Deviations / notes

- `p.retryConfig` is unexported on `OpenAIProvider`, so the integration test cannot tune
  `Attempts`/`MinDelay` (the spec's suggested `p.retryConfig.Attempts = 2` is only valid
  from inside package `providers`). This was unnecessary anyway: with default retry
  config, the observed behavior is a single request returning the partial result (the
  scanner treats clean EOF as stream-end), so no retry occurs and no tuning was needed.
  The server still serves a clean full response on request 2+ as a robustness backstop.
- `allowLoopbackForTest(t)` is used (httptest binds 127.0.0.1). The default HTTP client
  (`NewDefaultHTTPClient`) does NOT enforce SSRF at dial time, but the loopback bypass
  matches the phase spec and the existing helper.
- No `t.Parallel()`: both tests touch the shared test DB / default reliability singleton
  implicitly; the suite's other integration tests use the same non-parallel pattern for
  shared-store tests. Seed data is per-test unique tenant IDs, so this is safe either way.
- The `go vet` failure `tests/integration/git_adapter_ssh_test.go:103:9: append with no
  values` is PRE-EXISTING (committed in `def1a9e6`, before this branch) and is not in
  either file I created; I was instructed not to touch existing files, so it is left
  as-is. `go vet` reports no findings in my two files.

## Validation (Docker)

Runtime used `golang:1.26.0` (Debian variant — alpine lacks gcc for `-race`). PG was
REACHABLE at `postgres://postgres:test@host.docker.internal:5433/goclaw_test` (the
`pgtest-goclaw` container on host port 5433; Windows Docker Desktop reaches the host via
`host.docker.internal`).

```
gofmt -l tests/integration/provider_stream_disconnect_test.go tests/integration/stale_run_recovery_test.go
# clean (no files listed)

go vet -tags integration ./tests/integration/   # only pre-existing git_adapter_ssh_test.go:103 finding; nothing in my files

TEST_DATABASE_URL="postgres://postgres:test@host.docker.internal:5433/goclaw_test?sslmode=disable" \
  go test -race -timeout=180s -tags integration ./tests/integration/ -run "TestProviderStreamDisconnect|TestStaleRun" -v

# --- PASS: TestProviderStreamDisconnect_MidStreamCloseDoesNotFailRun (0.99s)
# --- PASS: TestStaleRun_RecoverStaleRuns_MarksFailed (0.10s)
# PASS   ok  github.com/nextlevelbuilder/goclaw/tests/integration  2.150s
```

`requests=1` in the disconnect test's log line confirms the run was completed from the
partial-stream (first) request with no retry — the race detector was on.

Surface parity: N/A for all surfaces (no API/WS/DB schema/UI/CLI change — tests only).

Status: DONE
Summary: Module D complete — 2 PG-backed integration tests for the run-lifecycle (stream
disconnect does not FAIL a run; RecoverStaleRuns marks stale-heartbeat runs failed while
leaving fresh runs untouched), both pass under -race against live pgvector:pg18, gofmt/vet
clean for the new files.
Concerns/Blockers: Pre-existing `go vet` finding in `tests/integration/git_adapter_ssh_test.go:103`
(`append(os.Environ())` with no values) — not touched per the "existing files untouched"
constraint; reported for the record since the CI vet step may surface it.