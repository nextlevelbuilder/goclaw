# `goclaw health` CLI — reliability diagnostics + operator self-test (W3)

> Part of the reliability upgrade workstream. Depends on: reliability singleton + config knobs ([W1 commit `429a1e20`](https://github.com/qkhalk/goclaw/commit/429a1e20)) and provider hot-path wiring ([W2 `56a94767`](https://github.com/qkhalk/goclaw/commit/56a94767)).

## What this does

Adds the operator-facing surface for the reliability layer: a `goclaw health` command that dumps live circuit/health/rate-limit/metrics state, and a `goclaw health --check` mode running deterministic in-process regression checks mirroring plan §27 cases A/B/C — no network, no database.

## Changes

| File | Change |
|------|--------|
| `cmd/health_cmd.go` (new) | `goclaw health` — plain-text, sorted, stable dump per `provider:model` (circuit state, health score, attempts/successes, consecutive failures, cooldown remaining, rate-limited-until, timeouts, stall count) plus metrics counters. `goclaw health --check` — runs case A (fake 429 → coordinator cooldown reported), case B (simulated stream disconnect → classified retryable), case C (`IsRetryable(nil)` → false); prints PASS/FAIL lines and exits 0/1. |
| `cmd/root.go` | Register `healthCmd()` |
| `internal/providers/remote_health.go` (new) | Snapshot helpers reading the process-wide reliability singleton via `reliability.Default()` — nil-safe (fresh process returns zero values, never panics) |
| `internal/providers/remote_health_test.go` (new) | Empty-registry defaults, observed-failure snapshot, cooldown reporting, sorted all-keys snapshot, metrics counters (delta-based — the singleton is process-global and shared with W2 wiring tests) |
| `internal/reliability/health.go` | `HealthRegistry.Keys()` — sorted enumeration of observed `provider:model` keys so operator tooling can enumerate entries without touching the internal map |
| `docs/04-gateway-protocol.md` | New §6.2 "Reliability Diagnostics": `goclaw health` / `--check` usage + `reliability.circuit.*` config reference |
| `docs/project-changelog.md` | 2026-08-16 entry for reliability wiring + health CLI |

## Design notes

- **Nil-safe everywhere**: `remote_health.go` and the CLI tolerate a not-yet-initialized singleton; the singleton is created lazily on first use, so a gateway that never wires it still gets a valid zero-state dump.
- **`Keys()`** is additive and non-conflicting with the wiring agents (they only call `Status`/`Observe`); it is the only way to enumerate registry entries from outside the package.
- **Regression checks are deterministic**: no sleeps, no flaky timers — the 429 cooldown asserts a 5s registered cooldown, the stream-disconnect check classifies a transport EOF, the nil check asserts the false-error guard.
- `--check` exits 0 when all PASS, 1 on any FAIL — scriptable for CI/runbooks.

## Verification

- `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...` all pass (Docker, golang:1.26.0-alpine)
- `go test ./cmd/... ./internal/providers/... ./internal/reliability/... -count=1` green, including all pre-existing provider tests (no regressions); tests use unique `w3-*` keys against the process-wide singleton
- Manual: `goclaw health` (zero-state dump) and `goclaw health --check` → 3× PASS, exit 0

## Surface parity

- **Gateway server:** N/A — read-only diagnostics; no handler/store/migration changes
- **API contract:** N/A — no protocol/HTTP surface change
- **Web UI:** N/A — CLI-only surface
- **CLI/runtime package:** `goclaw health` and `goclaw health --check` added here

## Out of scope (completeness note)

- Full-suite `go test ./...` currently has pre-existing failures unrelated to this work: `TestNewMigrationSource_LoadsMigrations` (duplicate `000097_*` migration files from the upstream dev merge `69da17b5`), `internal/http` Ollama integration tests (need a reachable Ollama server; fail inside Docker), and npm/pip checker tests (no npm/pip in the alpine container). None touch files changed here, all fail on the parent commit as well.