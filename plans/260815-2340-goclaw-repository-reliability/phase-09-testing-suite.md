# Phase 9 — Testing Suite

> Plan: `GoClaw_Upgrade_Improvement_Plan.md` §25 (Test matrix), §26 (Chaos testing),
> §27 (Regression tests), §29 (CLI/debug optional).
> Scout 2026-08-17 (2 Explore agents): full coverage map of existing test
> infrastructure. This phase adds only the confirmed gaps — it does NOT rebuild
> existing unit tests.

## Context & Requirements

### Outcome
Turn the reliability/weak-model features shipped in Phases 0–8 into a suite of
regression tests that (a) simulate provider chaos through a REAL HTTP LLM server,
(b) drive weak-model failure scenarios through the REAL pipeline loop, and
(c) wire the new suites into CI so they actually run on every push/PR.

### Non-goals (YAGNI / repo rule)
- No load/stress/benchmark tests (CLAUDE.md: skip throughput, p95/p99 latency,
  ReadMemStats memory tests — flaky on shared CI).
- No checkpoint/resume integration (deferred Phase 3/4).
- No new production code — tests + Makefile/CI wiring + docs only.
- Do not rewrite existing green unit tests; extend where the scout explicitly
  identified a gap.

### Acceptance criteria
- AC1: `internal/providers/` gains a scriptable fake-LLM HTTP server helper +
  tests driving retry/chaos + stream watchdog long-reasoning regression through
  a REAL httptest SSE endpoint (no injected closures for these).
- AC2: `internal/pipeline/` gains weak-model chaos regression tests through the
  REAL `NewDefaultPipeline().Run()` loop (extend the existing `PipelineDeps`
  harness) covering malformed tool call, invalid JSON, empty output, premature
  completion, repeated tool loop.
- AC3: `tests/integration/` gains at least one PG-backed run-lifecycle test
  asserting a run does NOT flip to FAILED when the provider stream disconnects
  mid-run (Case B at agent-loop level) + a stale-run recovery test exercising
  `RecoverStaleRuns` against the real store.
- AC4: `Makefile` + `.github/workflows/ci.yaml` wired so the new suites run in
  CI. No `-race` flake introduced; unit+integration stay green.
- AC5: `docs/` updated only where user-visible commands/CI behavior changed
  (README "Running" section test commands, if needed).
- AC6: Cross-surface parity: no API contract / WS frame / DB schema change —
  tests only. State N/A for web UI / CLI surfaces.

### Files to create / modify

Create:
- `internal/providers/chaos_harness_test.go` — scriptable fake-LLM server (SSE +
  non-stream), sequence of responses (429+Retry-After, 5xx, slow first token,
  random stream close), routes per scenario.
- `internal/providers/chaos_retry_test.go` — RetryDo through real HTTP with
  scripted 429 storm + Retry-After header parsed from response.
- `internal/providers/chaos_failover_test.go` — real fake-LLM server through
  `RunWithFailover` / `ModelFallbackProvider`: 429 primary→backup, random 5xx
  series, streamed-chunk error does NOT fallback.
- `internal/providers/stream_watchdog_reasoning_test.go` — long-reasoning
  regression (Case E): thinking_delta events at < idle interval for several
  seconds → watchdog MUST NOT fire; first text token arrives late.
- `internal/pipeline/weak_model_chaos_test.go` — weak-model scenarios through
  `NewDefaultPipeline().Run()`: malformed tool call, invalid JSON, empty output,
  premature completion (ContinuationGate), repeated tool loop. Asserts recovery
  path taken + run completes (not FAILED).
- `tests/integration/provider_stream_disconnect_test.go` — PG-backed: simulate
  stream disconnect mid-run at agent-loop level, assert run does NOT become
  FAILED (Case B); uses `testDB`/`seedTenantAgent` + a fake-LLM server.
- `tests/integration/stale_run_recovery_test.go` — PG-backed: create a run with
  stale heartbeat, call `PGRunStore.RecoverStaleRuns`, assert stale RUNNING →
  recovered status.
- `Makefile` — add targets `test-providers-chaos`, `test-pipeline-chaos`,
  `test-reliability-e2e`, wire into `test-critical` or a new `test-phase9`.
- `.github/workflows/ci.yaml` — add a "Reliability chaos tests (Phase 9)" step.

### Implementation steps (per module)

#### Module A — Provider chaos harness (chaos_harness_test.go + chaos_retry_test.go)
1. Build `fakeLLMServer` helper in `chaos_harness_test.go`:
   - `httptest.NewServer` serving OpenAI-compat `/v1/chat/completions`.
   - Mode switch: `script []responseStep` where each step = {status, headers,
     body, delayBefore, closeConn bool}. Server consumes steps in order, repeats
     last step if exhausted.
   - SSE mode: writes `data: {...}` frames with configurable inter-frame delay;
   supports `data: [DONE]`.
   - `ResetSteps()` for re-use across sub-tests.
2. `chaos_retry_test.go`:
   - `TestRetryDo_HTTP_429Storm_RespectsRetryAfter`: script [429+Retry-After:1,
     429+Retry-After:1, 200]. Drive `providers.RetryDo` with an fn that calls the
     server. Assert: 3 attempts, success, elapsed respects Retry-After.
   - `TestRetryDo_HTTP_5xxSeries_ExhaustsThenFails`: script [503,503,503,503]
     with maxAttempts=3 → error after 3, calls==3.
   - `TestRetryDo_HTTP_Timeout_NoFalseRetry`: server sleeps > client timeout →
     i/o timeout classified retryable, backoff not infinite.
   - Reuse existing `IsRetryableError`/`computeDelay` coverage — do not duplicate
     classification unit tests.

#### Module B — Stream watchdog long-reasoning regression (stream_watchdog_reasoning_test.go)
3. `TestWatchdog_ThinkingDeltas_NoFalseStall` (Case E):
   - Fake-LLM SSE server emits `thinking_delta` events every ~200ms for 3s
     (below idle timeout), then a final text chunk.
   - Assert: watchdog does NOT report stall, stream completes, first text token
     delivered. Mirrors anthropic_stream.go watchReset-per-event behavior.
   - Also assert the inverse (no events for > idle → fires) already covered by
     existing idle test — do not duplicate.

#### Module C — Weak-model chaos through pipeline (weak_model_chaos_test.go)
4. Extend the `PipelineDeps` harness (`pipeline_test.go`/`stages_test.go`
   `defaultState()`/`mockTokenCounter`). Each scenario drives the REAL loop:
   - `TestWeakModel_MalformedToolCall_RepairsAndContinues`: CallLLM returns
     malformed tool call JSON; assert repair path invoked (RepairToolCall /
     repairJSON), loop continues, run completes with ExitCode != AbortRun.
   - `TestWeakModel_EmptyOutput_Recovers`: first response empty → recovery
     (retry/continuation), second returns content → completes.
   - `TestWeakModel_PrematureCompletion_GateForcesContinue`: model says done but
     ContinuationGate sees unmet criteria → CONTINUE, missing work completed.
   - `TestWeakModel_RepeatedToolLoop_DetectedNotInfinite`: same tool+args
     repeated → loop detector triggers, loop bounded.
   - `TestWeakModel_InvalidJSON_RepairThenContinue`: invalid JSON response →
     repairJSON → valid tool call → continues.
   - Assert reliability counters (LLMRetries, loop metrics) where the feature
     exposes them.
   - NOTE: pipeline loop may need a fake `ExecuteToolCall` / tool registry to
     let tool-using scenarios run — reuse existing test doubles, do not add
     production code.

#### Module D — Integration run-lifecycle (tests/integration/*)
5. `provider_stream_disconnect_test.go` (Case B):
   - Reuse `testDB`, `seedTenantAgent`, `tenantCtx` from tests/integration.
   - Build a fake-LLM server (SSE) that starts a stream then closes the
     connection mid-response (no terminal frame).
   - Drive the agent/pipeline run against it; assert the run status does NOT
     become FAILED solely from the disconnect (provider-level recovery applies;
     per `loop_pipeline_adapter` wiring, a provider error with retry/fallback is
     not a run failure).
   - If driving a full `Loop.Run()` needs more harness than exists, scope to the
     pipeline level with a real provider adapter pointed at the fake server
     (precedent: `abort_provider_stream_test.go` uses
     `providers.NewAnthropicProvider(...WithBaseURL(server.URL))`).
6. `stale_run_recovery_test.go`:
   - `TestStaleRun_RecoverStaleRuns_MarksRecovered`: insert agent_run with old
     heartbeat via `PGRunStore`, call `RecoverStaleRuns(ctx, staleAfter)`, assert
     returned count and status transition. Precedent: `store/pg/run_store_test.go`
     already unit-tests this — keep the integration one light (real DB, real
     store, one happy path + one "fresh run untouched").

#### Module E — Makefile + CI wiring
7. `Makefile`: add:
   ```make
   test-providers-chaos:
   	go test -race -timeout=120s ./internal/providers/ -run "TestRetryDo_HTTP|TestWatchdog_Thinking|TestChaos"
   test-pipeline-chaos:
   	go test -race -timeout=120s ./internal/pipeline/ -run "TestWeakModel"
   test-phase9: test-providers-chaos test-pipeline-chaos
   ```
   Note: these are pure Go unit tests (no DB) — do NOT add the `integration`
   tag. The integration cases in Module D run under the existing
   `go test -tags integration ./tests/integration/`.
8. `.github/workflows/ci.yaml`: after the "Integration tests" step, add
   `- name: Reliability chaos tests (Phase 9)` running
   `make test-phase9` (or the two inline commands). Keep `-timeout` bounded.
9. Ensure no new flake: chaos server delays must use short ms-scale values and
   be bounded; tests must not depend on wall-clock except with generous margins.

### Validation
- Docker (no local Go):
  `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`
  `go test -race ./internal/providers/ -run "TestRetryDo_HTTP|TestWatchdog_Thinking|TestChaos"`
  `go test -race ./internal/pipeline/ -run "TestWeakModel"`
  `go test -race -timeout=180s -tags integration ./tests/integration/ -run "TestProviderStreamDisconnect|TestStaleRun"`
- Full `go test ./internal/...` still green.

### Risks / rollback
| Risk | Mitigation | Rollback |
|------|-----------|----------|
| Chaos server timing flake | ms-scale delays, generous margins, bounded timeouts | remove flaky test, keep others |
| Weak-model test needs production change | reuse existing doubles; do not add prod code | skip scenario, note in report |
| CI step too slow | -run scoping + -timeout bounded | revert ci.yaml hunk |
| Integration disconnect test hard to drive | fall back to pipeline-level with real adapter (abort_provider_stream precedent) | keep unit-level version |

### Surface parity
- Gateway server: N/A (no handler/store change).
- API contract: N/A (no struct/wire change).
- Web UI: N/A (no UI change).
- CLI/runtime: N/A (no command change; Makefile targets are dev/CI tooling).
