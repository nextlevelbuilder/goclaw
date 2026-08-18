# Phase 10 — WS-A report: SLO evaluator + OTel metrics sink

Status: DONE

Date: 2026-08-18
Owner: WS-A

## Scope

1. Config-driven SLO evaluator in `internal/reliability` (`SLOTracker`).
2. OTel metrics sink (`reliability.Sink`) with build-tag `otel` split
   (otel + noop) exporting counters prefix `goclaw.reliability.`.
3. OTLP meter provider (grpc/http) + gateway wiring file `wireReliabilityMetrics`
   (otel + noop). Gateway call-site wiring is owned by the controller.
4. `ReliabilityConfig.SLO` + `ReliabilityConfig.Alerts` config + defaults +
   Effective helpers + tests.

## Files changed / created

| File | Change | Type |
|------|--------|------|
| `internal/config/config.go` | Added `SLOConfig` + `AlertingConfig` structs to `ReliabilityConfig`; added defaults `DefaultSLOTargetPercent=0.99`, `DefaultSLOWindowSeconds=3600`, `DefaultAlertMinIntervalSeconds=60`; added `EffectiveSLOTarget()`, `EffectiveSLOWindow()`, `EffectiveAlertMinInterval()` helpers. | extend |
| `internal/config/config_load.go` | `Default()` `ReliabilityConfig` now seeds SLO + Alerts defaults (backward-compatible: zero-value configs fall back via Effective helpers). | extend |
| `internal/config/slo_alert_config_test.go` | Tests: defaults, overrides, zero/negative fallback, `Default()` seeding, JSON round-trip. | create |
| `internal/reliability/slo.go` | `SLOTracker` (mu + target + window + FIFO `[]sloSample{ok, at}`), `Observe(Snapshot)`, `Status() SLOStatus`, `Enabled()`. Nil-safe, no division-by-zero, no burn on idle. | create |
| `internal/reliability/slo_test.go` | Unit tests: empty/zero-window, idle no-burn, success rate, on-target, window pruning, all-dead, concurrent observe. | create |
| `internal/reliability/otelmetrics_sink_otel.go` | `//go:build otel` — `OTelSink` implements `Sink`, 13 counters + latency histogram, `NewOTelSink(*sdkmetric.MeterProvider)`, `NewOTelMeterProvider(ctx, OTelConfig)` with grpc/http OTLP exporters. | create |
| `internal/reliability/otelmetrics_sink_noop.go` | `//go:build !otel` — no-op `OTelSink`, `NewOTelSink()`, `Emit` nil-safe. No otel imports. | create |
| `cmd/gateway_reliability_metrics.go` | `//go:build otel` — `wireReliabilityMetrics(ctx, cfg) (func(), error)`: enabled+endpoint gate, meter provider, `RegisterSink`, 5s flush loop, idempotent stop (sync.Once). | create |
| `cmd/gateway_reliability_metrics_noop.go` | `//go:build !otel` — no-op returns `(nil, nil)`. | create |
| `go.mod` | Added requires (no go.sum — controller runs `go mod tidy`): `otlpmetric v1.40.0`, `otlpmetricgrpc v1.40.0`, `otlpmetrichttp v1.40.0`. | extend |

## Metric names (frozen contract for WS-C dashboard)

All counters prefix `goclaw.reliability.`. Dashboard reads these exact names.

| Metric | Snapshot source |
|--------|-----------------|
| `goclaw.reliability.requests` | `LLMRequests` |
| `goclaw.reliability.successes` | `LLMSuccesses` |
| `goclaw.reliability.retries` | `LLMRetries` |
| `goclaw.reliability.rate_limited` | `LLMRateLimited` |
| `goclaw.reliability.server_errors` | `LLMServerErrors` |
| `goclaw.reliability.timeouts` | `LLMTimeouts` |
| `goclaw.reliability.stream_stalls` | `LLMStreamStalls` |
| `goclaw.reliability.loop_detected` | `LLMLoop` (RecordLLMLoop — the wired producer) |
| `goclaw.reliability.repeated_tool_calls` | `LLMRepeatedToolCalls` |
| `goclaw.reliability.empty_outputs` | `LLMEmptyOutputs` |
| `goclaw.reliability.premature_completions` | `LLMPrematureCompletions` (RecordLLMPrematureCompletion — the wired producer) |
| `goclaw.reliability.agent_recovered` | `AgentRecovered` |
| `goclaw.reliability.agent_continued` | `AgentContinued` |
| `goclaw.reliability.llm_latency_ms` | histogram, avg ns/count converted to ms |

The Snapshot also exposes legacy `LoopDetected` / `PrematureCompleted` counters;
they are wired nowhere in production (`grep RecordLoopDetected|RecordPrematureCompletion`
= 0 non-test hits), so the sink records the LLM-prefixed fields only to avoid
double-counting. The metric names follow the spec exactly.

## Config structure (backward-compatible)

```json
{
  "reliability": {
    "slo":   { "enabled": true, "target_percent": 0.99, "window_seconds": 3600 },
    "alerts": { "enabled": true, "webhook_url": "...", "min_interval_seconds": 60 }
  }
}
```

- Zero-value config → defaults via Effective helpers (`Default()` also seeds them).
- `webhook_url` may carry secrets → loaded from env override by the controller's
  wiring (field already exists in config, env overlay is a controller/WS-B concern).
- Existing configs parse unchanged (all new fields `omitempty`).

## Verification notes (no local Go — static only, per task constraints)

- **No import cycle:** `reliability` cannot import `internal/tracing/otelexport`
  because `otelexport → store → config → reliability`. Confirmed via grep.
  So `NewOTelMeterProvider` takes a local `OTelConfig` struct (same fields as the
  tracing `otelexport.Config`) instead of importing it. No semconv import —
  resource attributes use literal keys `service.name` / `service.version`
  (per controller guidance; semconv is only an indirect dep).
- **Build tags:** otel file imports only otlpmetric*/sdk/metric/metric/attribute/
  resource/stdlib. Noop file imports only stdlib. Both `wireReliabilityMetrics`
  signatures match across build tags.
- **`go vet` risks:** `sync.Once` `once` var is function-scoped in
  `wireReliabilityMetrics` (declared before the stop closure, captured once);
  `time.Duration` conversions verified. `math` import removed after aligning
  BurnRate semantics (no Inf). `takeSnapshot` helper in `slo_test.go` is used
  throughout (no unused funcs).
- **Constants alignment** in the metric block manually verified to gofmt width
  (longest name 26 chars: `MetricPrematureCompletions`).
- **Config tests:** existing `config_load_test.go`/`config_test.go` check
  specific fields only — adding SLO/Alerts defaults does not break them.

## Decisions

1. **BurnRate when successRate == 0:** kept at `0` (not `math.Inf`). The spec
   says compute `target/successRate` only when `successRate > 0`; 0 keeps
   `SLOStatus` JSON-marshalable for webhook/dashboard consumers. The
   `SuccessRate==0` + `WithinBudget=false` fields carry the outage signal.
2. **Interval-level ok semantics:** a flush delta counts as one sample; `ok` is
   true only when every request in the delta succeeded (matches the spec sample
   `{ok bool; at time.Time}`).
3. **Prune boundary:** samples `at < now-window` are dropped; the newest sample
   never expires (cutoff anchored to the newest sample's timestamp).
4. **OTelSink noop signature differs by build tag:** noop `NewOTelSink()` takes no
   args (no otel import in the default build); otel `NewOTelSink(*sdkmetric.MeterProvider)`.
   The controller's gateway.go wiring (otel build) passes the provider; the noop
   build is never called with one.
5. **Flush cadence 5s** lives in `wireReliabilityMetrics` (matches the tracing
   exporter batch window). `NewOTelMeterProvider` uses a 5s PeriodicReader
   interval too — double cadence is harmless (flush drains counters; reader
   exports accumulated values).

## Concerns / notes for controller

1. The gateway call-site wiring of `wireReliabilityMetrics` (go.mod build-tag
   split already done) is controller-owned per plan. Ensure the returned `stop`
   is invoked on graceful shutdown in both standard and managed paths.
2. `go mod tidy` must run in the container to populate go.sum for the three new
   otlpmetric requires. `otel/sdk/metric` and `otlpmetric` base are already in
   go.sum as transitive entries; the new direct requires will resolve to the same
   versions.
3. WS-B's `AlertingConfig` consumption reads `cfg.Reliability.Alerts.WebhookURL`
   + `EffectiveAlertMinInterval()` — the config shape here matches the WS-B
   payload/cooldown contract (best-effort POST, min-interval throttle).
4. Metric names are a hard contract with WS-C's Grafana dashboard — rename only
   with a coordinated dashboard change.
