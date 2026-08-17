## What

Wires the reliability layer into the runtime so other workstreams (provider
wiring, health command, agent loop) can consume one shared, configured
instance.

- **`internal/reliability/singleton.go`** (new): package singleton bundling
  `CircuitBreaker`, `HealthRegistry` (sharing the same breaker),
  `RateLimitCoordinator`, and `Metrics`.
  - `Default()` — lazy-constructs the default bundle (breaker thresholds
    FailureThreshold 5 / DegradedThreshold 2 / Cooldown 30s / HalfOpenMax 1 /
    ProbeTimeout 30s, unlimited rate-limit pending, fresh metrics).
  - `Configure(opts CircuitOptions, maxPending int)` — rebuilds the bundle
    atomically from runtime config; safe to call multiple times (tests swap
    bundles between cases).
  - No DI framework — a plain package singleton. `sync.Once` is never reset,
    so `Default()` + `Configure()` cannot race.
- **`internal/config/config.go`**: new `reliability.circuit` block
  (`failure_threshold`, `degraded_threshold`, `cooldown_ms`, `half_open_max`,
  `probe_timeout_ms`, `rate_limit_max_pending` — 0 = unlimited) with defaults
  constants and `EffectiveCircuit()` / `EffectiveRateLimitMaxPending()`
  getters, following the existing `RunsConfig.Effective*` pattern. Config →
  reliability import only (no cycle; reliability stays self-contained).
- **`cmd/gateway.go`** (`runGateway`, the gateway runtime wiring point —
  `internal/gateway/runtime.go` does not exist in this repo): after config
  load + edition resolution, calls `reliability.Configure(...)` from
  `cfg.Reliability.Circuit` and logs the effective breaker thresholds at
  debug level. The desktop app reaches the same code via `cmd.RunGateway()`.
- Tests: `internal/reliability/singleton_test.go` (non-nil bundle, stable
  `Default()`, rebuild on `Configure()`, option application through the
  shared breaker, default bundle construction) and
  `internal/config/circuit_config_test.go` (defaults, overrides, zero/negative
  fallback, JSON round-trip).

No public contracts of existing reliability types changed.

## Verified

- `go build ./...` + `go vet ./...` + `go test ./internal/reliability/
  ./internal/config/ ./internal/gateway/ -count=1` + `go build -tags
  sqliteonly ./...` → all pass (Docker).
- `go test -race ./internal/reliability/` → pass.

## Notes

- Scope owned: `internal/config/config.go`, `cmd/gateway.go`,
  `internal/reliability/singleton.go` + tests. Cleaned-up review-only PR for
  W1 (runtime config + singleton); provider wiring and health command land in
  their own PRs.
- `reliability.Default()` returns the current bundle; consumers should not
  retain a reference across a `Configure()` call.