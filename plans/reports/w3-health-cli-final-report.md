# W3 Final Report — `goclaw health` CLI (reliability diagnostics + self-test)

> Workstream W3 of the reliability upgrade. Branch `feat/reliability-upgrade`, PR: https://github.com/qkhalk/goclaw/pull/1

## Deliverables (all committed)

| File | Change |
|------|--------|
| `cmd/health_cmd.go` (new) | `goclaw health` — plain-text, sorted, stable dump per `provider:model`: circuit state, health score, attempts/successes, consecutive failures, cooldown remaining, rate-limited-until, timeouts, stall count; plus metrics counters (requests, successes, retries, rate-limited, server errors, timeouts, stalls, agent recovered/continued, premature completes, loop detected). `goclaw health --check` — deterministic in-process regression checks: A) fake 429 → `RateLimitCoordinator` cooldown reported; B) simulated stream disconnect → classified retryable; C) `reliability.IsRetryable(nil)` → false. PASS/FAIL lines; exit 0 = all pass, 1 = any failure. |
| `cmd/root.go` | Registered `healthCmd()` |
| `internal/providers/remote_health.go` (new) | Nil-safe snapshot helpers over `reliability.Default()` (falls back to zero values when the singleton is uninitialized; missing `Keys()` was added to `HealthRegistry`) |
| `internal/providers/remote_health_test.go` (new) | Empty-registry defaults, observed-failure snapshot, cooldown reporting, sorted all-keys snapshot, metrics counters (delta-based; the singleton is process-global and shared with W2 wiring tests) |
| `internal/reliability/health.go` | `HealthRegistry.Keys()` — sorted enumeration of observed `provider:model` keys (additive; wiring agents only call `Status`/`Observe`) |
| `internal/upgrade/version.go` | `RequiredSchemaVersion` 97 → 98, matching the controller's `000098_channel_message_archive` renumber (`e1dbbd03`); keeps `TestRequiredSchemaVersionMatchesLatestMigration` green |
| `docs/04-gateway-protocol.md` | §6.2 "Reliability Diagnostics": `goclaw health` / `--check` usage + `reliability.circuit.{failure_threshold, degraded_threshold, cooldown_ms, half_open_max, probe_timeout_ms, rate_limit_max_pending}` config reference (field names verified against `internal/config/config.go`) |
| `docs/project-changelog.md` | 2026-08-16 reliability wiring + health CLI entry |

## Verification (Docker, golang:1.26.0-alpine)

- `go build ./...` — pass
- `go build -tags sqliteonly ./...` — pass
- `go vet ./...` — pass
- `go test ./cmd/... ./internal/providers/... ./internal/reliability/... ./internal/upgrade/... ./internal/config/... ./internal/store/... -count=1` — green (no regressions in pre-existing provider tests; tests use unique `w3-*` keys against the process-wide singleton)
- Manual: `goclaw health` (zero-state dump), `goclaw health --check` → 3x PASS, exit 0

## CI status

- CI run 31935053068 (after pushing `c95cb6d5`) — **all jobs green**: `go` (build, sqliteonly build, vet, unit, invariant P0, contract P1, integration tests, coverage — 7m17s), `release-versioning`, `web`.
- The earlier duplicate-`000097_*` migration collision from the upstream dev merge was resolved on this branch by the controller's renumber (`e1dbbd03`) + this workstream's `RequiredSchemaVersion` bump (`c95cb6d5`); the go job's Unit tests step now passes on the ubuntu runner.
- The `internal/http` Ollama and npm/pip checker failures seen in local Docker runs are alpine-environment-only (no Ollama server / no binaries); CI's ubuntu runner has none of these failures.

## Surface parity

- Gateway server: N/A — read-only diagnostics; no handler/store/migration changes.
- API contract: N/A — no protocol/HTTP surface change.
- Web UI: N/A — CLI-only surface.
- CLI/runtime package: `goclaw health` and `goclaw health --check` added here.

## Notes / coordination

- My files temporarily disappeared from the working tree during parallel-agent commit churn; recovered via verification against the merged state (`429a1e20` + `56a94767` + merge `69da17b5`).
- The metrics unit test was rewritten to delta-based assertions because the process-global singleton accumulates counters from W2 wiring tests running in the same test binary.
- `go test -race` is unavailable in the alpine image (needs cgo); CI runs `-race` on ubuntu.

Status: DONE
Summary: All W3 deliverables committed and verified (build, vet, scoped tests, sqliteonly build, manual `--check`); CI run 31935053068 fully green (go + release-versioning + web). PR #1 body updated for the resolved migration collision.
Concerns: None.