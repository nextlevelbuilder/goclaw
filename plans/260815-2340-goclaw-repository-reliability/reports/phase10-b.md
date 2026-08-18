# Phase 10 — WS-B report: managed stale-run sweep + bgalert webhook

Status: DONE

Date: 2026-08-18
Owner: WS-B

## Scope

1. Webhook alerting for `internal/bgalert` — best-effort HTTP POST on
   `ReportProviderError`.
2. Wire `runStaleRunsSweep` into the managed path (`cmd/gateway_managed.go`).

## Files changed / created

| File | Change | Type |
|------|--------|------|
| `internal/bgalert/report.go` | `AlertDeps` gains `WebhookURL string` + `MinIntervalSeconds int` (keyed-field construction → backward compatible with all existing callers). `ReportProviderError` calls `SendWebhook(ctx, deps, workerName, string(reason), err)` after the store write + WS broadcast when `WebhookURL != ""`. | extend |
| `internal/bgalert/webhook.go` | New HTTP webhook sender: `WebhookPayload` struct + `SendWebhook` (best-effort, 5s client timeout, sanitized message, cooldown). | create |
| `internal/bgalert/webhook_test.go` | httptest-based unit tests: payload shape/severity, empty URL no-send, server error best-effort, min-interval cooldown, severity mapping. | create |
| `cmd/gateway_managed.go` | `wireExtras` now starts `go runStaleRunsSweep(stores.Runs, appCfg.Reliability.Runs.EffectiveStaleAfter(), appCfg.Reliability.Runs.EffectiveSweepInterval())` guard `stores.Runs != nil`. | extend |

No external dependencies added. `net/http`, `io`, `sync`, `bytes` are stdlib.

## Webhook payload shape

```json
{
  "severity": "critical|warning",
  "title": "GoClaw background provider error",
  "message": "<sanitizeErrorMessage(err.Error())>",
  "worker": "<workerName>",
  "reason": "auth|billing|...",
  "timestamp": "<RFC3339 UTC>",
  "meta": {}
}
```

- Severity: `auth`, `auth_permanent`, `billing`, `model_not_found` → `critical`; everything else → `warning`.
- Message is always run through `sanitizeErrorMessage` (API-key mask + 200-rune truncation).
- Cooldown: package-level `sync.Mutex` + `lastSend time.Time`; with
  `MinIntervalSeconds > 0`, sends are throttled to at most one per interval.
  The timestamp refreshes only on a completed HTTP round-trip (not on
  transport failure), so a failing endpoint is not hammered.
- Never blocks: marshal / request / response errors are `slog.Warn` only.

## Sweep wiring location

`cmd/gateway_managed.go` `wireExtras`, right after the `go runEvolutionCron(...)`
block:

- Edited at lines 661–670 (insertion after old line 660).
- `appCfg *config.Config` was already in `wireExtras` scope (param at line 60);
  `stores *store.Stores` at line 47. No signature change.
- Signature confirmed against `cmd/gateway_heartbeat.go:29`
  `func runStaleRunsSweep(runs store.RunsStore, staleAfter, interval time.Duration)`.
  Not redefined.

## Verification notes (no local Go — static only, per task constraints)

- Compile risk review:
  - `gateway_managed.go` has no build tag (shared code); the sweep block
    references `store.RunsStore`, `config.Config.Reliability.Runs.Effective*`
    — all already imported/used in the file (`store`, `config` both imported at
    top). `runStaleRunsSweep` is in the same `cmd` package.
  - `bgalert` imports now: context, encoding/json, log/slog, regexp, time, bytes,
    io, net/http, sync. All stdlib. No build-tag-restricted imports.
  - `ReportProviderError` call at `cmd/gateway.go:478/:509` uses keyed
    `AlertDeps{SystemConfigs: ..., MsgBus: ...}` — unaffected by the two new
    fields.
- Test logic: tests use `httptest.Server` + `atomic.Int32` counter, no sleeps
  (the one `time.After(2s)` is a receive-failure watchdog only). `resetWebhookState()`
  clears the package cooldown between tests.
- `http.NewRequestWithContext` with caller ctx: if the caller's ctx is
  cancelled the request aborts immediately — acceptable for best-effort alerting.

## Concerns / notes for controller

1. **Duplicate sweep in PG build (pre-approved by plan).** `gateway_managed.go`
   is shared (no build tag) and `startCronAndHeartbeat`
   (`cmd/gateway_heartbeat.go:110-116`) already starts the sweep for the
   standard/lib builds. The new `wireExtras` sweep therefore starts a second
   sweep goroutine inside the same process in the PG build. `RecoverStaleRuns`
   is idempotent (marks rows `failed`, second invocation finds 0), so the only
   cost is two periodic queries. Plan Risk section explicitly records this as a
   non-issue. If the controller prefers a single sweep, the standard-path one
   (gateway_heartbeat.go:110) or the managed one could be dropped, but that
   touches WS-A-owned `gateway.go`/`gateway_heartbeat.go`.
2. `SendWebhook` guard: nil error and empty URL both no-op (defensive for
   direct callers). In `ReportProviderError` the error is always non-nil.