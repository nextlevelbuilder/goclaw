# Phase 9 Module A+B — Provider Chaos Test Report

> Work context: `plans/260815-2340-goclaw-repository-reliability/phase-09-testing-suite.md`
> Date: 2026-08-17 · Scope: Modules A + B (AC1)

## Summary

Added 4 NEW test files in `internal/providers/` (package `providers`) that drive
retry, failover, and the stream watchdog through a REAL HTTP fake-LLM server —
the confirmed test matrix gap. No production code was touched. All validation
passed in Docker (`golang:1.26.0`): `gofmt` clean, `go vet` clean, new tests
green with `-race`, 3x repeat no flake, full providers suite green, and the
`sqliteonly` build-tag test compile is clean.

## Files added

### `internal/providers/chaos_harness_test.go`
`fakeLLMServer` helper:
- `httptest.NewServer` serving OpenAI-compat chat completions (plain JSON body
  and SSE `data:` frames, including `data: [DONE]`).
- `script []responseStep` consumed in order; the LAST step repeats once the list
  is exhausted. Each `responseStep` = `{status, headers, body, delayBefore,
  closeConn, sseFrames, sseDone, sseFrameGap}`.
  - `closeConn` force-closes the socket via `http.Hijacker` (abrupt EOF
    mid-response).
  - HTTP errors write the status line + body; a 200 with frames streams SSE with
    configurable inter-frame delay.
- Concurrency-safe (`sync.Mutex`); `requestCount()` for attempt assertions,
  `LastRequest()` for path/auth assertions, `ResetSteps()` for re-use.
- Delay helpers: `httpErrorStep` (status + optional Retry-After seconds),
  `openAICompleteStep` (non-stream 200), and SSE frame builders
  (`openAIReasoningDelta`, `openAITextDelta`, `openAIStopDelta`).
- Delays are ms-scale only (10–200ms); no second-scale walls.

### `internal/providers/chaos_retry_test.go`
`RetryDo` through real HTTP with a scripted fake server:

| Test | Script | Assertions |
|------|--------|------------|
| `TestRetryDo_HTTP_429Storm_RespectsRetryAfter` | `[429+RA:1s, 429+RA:1s, 200]` | success on attempt 3, `requestCount == 3`, elapsed ≥ ~900ms (Retry-After honored, generous bound), `MinDelay=10ms/MaxDelay=50ms/Jitter=0` proving the server hint dominated over backoff |
| `TestRetryDo_HTTP_5xxSeries_ExhaustsThenFails` | `[503, 503, 503, 503]`, `Attempts: 3` | error surfaced, `requestCount == 3` (attempt budget capped), error is `*HTTPError` with Status 503 |
| `TestRetryDo_HTTP_Timeout_NoFalseRetry` | server delays 200ms > client `Timeout: 50ms` | timeouts classified retryable, `requestCount == cfg.Attempts`, bounded elapsed (no infinite loop), eventually errors |

The closure maps non-200 into `&HTTPError{Status, Body, RetryAfter:
ParseRetryAfter(...)}` — the same contract the provider HTTP layer uses.

### `internal/providers/chaos_failover_test.go`
`RunWithFailover` through real HTTP with two OpenAI profiles (key1 primary, key2
backup):

| Test | Script | Assertions |
|------|--------|------------|
| `TestFailover_HTTP_429Primary_BackupSucceeds` | primary 429, backup 200 | result from backup, `requestCount == 2`, ≥1 attempt classified `FailoverRateLimit` |
| `TestFailover_HTTP_5xxSeries_Rotates` | primary 503, backup 200 | result from backup, `requestCount == 2`, ≥1 attempt classified `FailoverServerError` |
| `TestFailover_HTTP_StreamedChunk_DoesNotFallback` | primary emits one SSE chunk then closes (no `[DONE]`, `Connection: close`) | `RunWithFailover` settles on `*FailoverStreamed`, partial text returned, `requestCount == 1` (backup never called), `len(attempts) == 0` |

The streamed runFn reads the real SSE stream; when output escaped before a clean
`[DONE]` it wraps the failure in `FailoverStreamed`, mirroring the provider
stream adapters' chunk-emitted guard.

### `internal/providers/stream_watchdog_reasoning_test.go`
`TestWatchdog_ThinkingDeltas_NoFalseStall` (Case E):
- Fake SSE server emits `reasoning_content` deltas every ~200ms for ~3s (14
  frames), then a text chunk + pause-reset stop + `[DONE]`.
- Watchdog armed `WithStreamTimeouts(ctx, 700ms, 0)` — idle window > inter-delta
  gap so deltas keep re-arming, but far below the 3s reasoning phase.
- Drives `p.ChatStream` on a real `NewOpenAIProvider` pointed at the fake server.
- Asserts: no error (no false stall), `LLMStreamStalls` delta == 0, final text
  token `"final answer"` delivered, elapsed ≥ 2500ms (survived the reasoning
  phase), chunks delivered.
- The inverse (silence > idle fires) is deliberately not duplicated — already
  covered by `TestStreamWatchdog_FiresOnIdle` / `TestOpenAIChatStream_WatchdogFires`.

## Validation results (Docker, `golang:1.26.0`)

```
gofmt -l <4 files>            → no output (all formatted)
go vet ./internal/providers/  → clean
go test -race -timeout=120s ./internal/providers/ -run "TestRetryDo_HTTP|TestWatchdog_Thinking|TestFailover_HTTP"
  → ok (all 7 new tests PASS; watchdog ~3.2s, 429 storm ~2.0s)
go test -race (full providers suite) → ok (81s)
go test -race -count=3 (new tests)   → ok, no flake
go test -tags sqliteonly (compile)   → ok
```

## Deviations

- **`golang:1.26.0-alpine` cannot run `-race`** (no gcc/cgo), so validation used
  the Debian `golang:1.26.0` image (`golang:1.26.0` was already present locally).
- The watchdog reasoning test uses **OpenAI-compat `reasoning_content` deltas**
  rather than Anthropic `thinking_delta`. Rationale: the OpenAI path exercises
  `ChaosHarness`'s existing SSE frame builders and `NewOpenAIProvider` (the same
  precedent as `stream_watchdog_test.go`'s OpenAI stall test); the `watchReset`-
  per-event behavior it guards is identical in the Anthropic stream
  (`anthropic_stream.go` line ~72). The OpenAI delta re-arms via the same
  per-event `watchReset()`, so the regression target (any provider stream loop
  that fails to reset on thinking) is fully exercised.
- Initial draft of the harness handler omitted the "write error status" branch
  (the error body was returned with a 200). Caught by the first validation run;
  fixed and re-validated.
- No Makefile/CI changes in this module (those are Module E of the phase, run by
  a separate lane).

## Cross-surface parity

- Gateway server: N/A — tests only, no handler/store change.
- API contract: N/A — no struct/wire change.
- Web UI / CLI: N/A — no UI or command change.

## Files touched

Modified: none (production code untouched).
Created: the 4 test files listed above only.