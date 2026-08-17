# Wire reliability layer into the provider hot-path (W2)

> Part of the reliability upgrade workstream. Depends on: reliability singleton + config knobs ([W1 commit `429a1e20`](https://github.com/qkhalk/goclaw/commit/429a1e20)).

## What this does

Routes every real provider request through the reliability layer so operator-visible circuit breaking, health scoring, rate-limit coordination, and metrics are backed by actual runtime signals instead of a dry-run layer.

Integration point: `internal/providers/reliability_wiring.go` — the **single** file where providers touch `internal/reliability` (import direction: providers → reliability, no cycles). Every entry is nil-safe and panic-safe (`safeRecord`), so a reliability-layer defect can never break a provider request.

## Changes

| File | Change |
|------|--------|
| `internal/providers/reliability_wiring.go` (new) | `observeSuccess` / `observeFailure` (classifies via `reliability.ClassifyError`, feeds Health + breaker + metrics counters), `circuitAllow` (non-blocking breaker gate; returns `*reliability.ReliabilityError` with RetryAfter when open), `waitRateLimit` (coordinator cooldown wait), `record429Cooldown`, `isRateLimitedErr` / `rateLimitRetryAfter` |
| `internal/providers/openai_chat.go` | `Chat` + `ChatStream`: `circuitAllow` → `waitRateLimit` → `RecordLLMRequest` → `RetryDo` → `observeSuccess`/`observeFailure` after the retry cycle settles (stream success recorded once the connection phase wins) |
| `internal/providers/anthropic.go` | `Chat`: same pattern |
| `internal/providers/ollama.go` | `Chat` + `ChatStream`: same pattern |
| `internal/providers/codex.go` | `ChatStream`: same pattern (per-attempt loop; success on completion, failure on non-retryable emit, ctx cancel and exhaustion) |
| `internal/providers/retry.go` | `RetryDo` records `LLMRetries` per retryable error and `LLMRateLimited` on 429s — public contract unchanged |
| `internal/providers/failover.go` | `RunWithFailover` observes success/failure per candidate and records coordinator 429 cooldowns (with Retry-After) on rate-limited errors |
| `internal/providers/cooldown.go` | `CooldownTracker` bridges rate-limit/overloaded failures to the process-wide `RateLimitCoordinator` (toggleable via `WithLocalBridge` for test isolation); `RecordSuccess` clears coordinator cooldowns |
| `internal/providers/model_fallback.go` | Fallback ordering prefers `HealthRegistry.Score` once a candidate has >5 observed attempts; primary always first; candidates without signal keep configured order |
| `internal/providers/reliability_wiring_test.go` (new) | circuitAllow healthy → nil; breaker opens after 5 consecutive failures and circuitAllow rejects with RetryAfter > 0; success closes it; observeFailure(429) → `LLMRateLimited`; coordinator cooldown wiring; `waitRateLimit` blocking + context-cancellation |
| `internal/providers/retry_test.go` | RetryDo metric counters (retries, 429 rate-limit) |

## Design notes

- **Breaker keying** = `provider:model`, matching `CooldownKey` — health observed failures flow to the breaker under the same key (see `HealthRegistry.ObserveFailure`).
- **429 flow**: failover records the coordinator cooldown on rate-limited errors; `waitRateLimit` at every wired Chat entry makes concurrent runs wait together instead of retry-storming. RetryDo additionally counts 429 retries for ops visibility.
- **Fallback health ordering** is conservative: no reorder until a candidate has measurable signal (>5 attempts), so fresh deployments keep configured order.

## Verification

- `go build ./...` and `go build -tags sqliteonly ./...` pass
- `go vet ./...` clean
- `go test ./internal/providers/ ./internal/reliability/ -count=1` green (including all pre-existing failover/cooldown/model-fallback tests — no regressions)
- Tests use unique `wiring-test:*` keys against the process-wide singleton, so no key collisions with existing provider tests

## Out of scope (stated for completeness)

- `cmd/gateway.go` / `internal/config` wiring of the singleton — W1 workstream (already landed on this branch)
- `goclaw health` CLI — W3 workstream (in progress on this branch)
- Not wired (no `RetryDo` hot-path or inherited via `OpenAIProvider.Chat`): Claude CLI provider, embeddings, image generation, AIMLAPI adapter