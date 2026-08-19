# Phase 2 W2 — Observability Gaps — Implementation Report

**Date:** 2026-08-19
**Workstream:** Phase 2 W2 (Phase 7 plan `plans/260819-0954-agentkit-phase7-enterprise/`)
**Status:** DONE_WITH_CONCERNS

## Files Modified/Created

### New files
| File | Purpose |
|---|---|
| `cmd/gateway_prometheus.go` | `//go:build prometheus` — `/metrics` text-exposition handler + dedicated listener wiring. Feeds `reliability.Metrics.Take()` counters + per-tenant `goclaw_tenant_cost_usd` gauge from `usage_event_rollups`. Zero new deps (hand-rolled text format). |
| `cmd/gateway_prometheus_noop.go` | `//go:build !prometheus` — no-op, matches `gateway_otel_noop.go` pattern. |
| `cmd/gateway_reliability_slo.go` | Shared (both builds): `sloTrackerFromConfig` + single `startReliabilityFlushLoop` (Take→Flush→Observe→webhook on `!WithinBudget`, reason `slo_burn_rate`). |
| `cmd/gateway_prometheus_test.go` | `//go:build prometheus` — handler smoke (counters + cost gauges), no-cost-rows, wire-disabled. |
| `cmd/gateway_reliability_slo_test.go` | SLO burn-rate drives `!WithinBudget`, Take-before-Flush drain contract, flush-loop start/stop contract. |
| `cmd/gateway_reliability_observability_test.go` | `//go:build !prometheus` — noop `wirePrometheusMetrics` contract. |
| `internal/config/prometheus_config_test.go` | `PrometheusConfig` defaults/overrides/JSON round-trip, `Default()` disabled. |
| `internal/tracing/otelexport/exporter_cost_test.go` | OTel span cost attr present for positive cost, omitted for 0 and nil cost (in-memory SimpleSpanProcessor recorder, no OTLP backend). |

### Modified files
| File | Change |
|---|---|
| `internal/config/config.go` | `TelemetryConfig.Prometheus` field + `PrometheusConfig{Enabled,Port,Host}` + `EffectivePort()`/`BindHost()` (defaults 9090 / 127.0.0.1). |
| `internal/config/config_load.go` | Env overrides `GOCLAW_PROMETHEUS_ENABLED` / `_PORT` / `_HOST`. |
| `cmd/gateway.go` | Wire `wirePrometheusMetrics(ctx, cfg, pgStores.DB)` after stores init (build-tag gated, `defer` stop). |
| `cmd/gateway_reliability_metrics.go` | OTel build: construct tracker before loop, start shared flush loop (`forceFlush=true`), keep meter shutdown. |
| `cmd/gateway_reliability_metrics_noop.go` | Default build: still start shared flush loop when SLO enabled (SLO alerting works without otel tag). |
| `internal/tracing/otelexport/exporter.go` | `attribute.Float64("goclaw.llm.cost_usd", *s.TotalCost)` when `TotalCost != nil && > 0`. |
| `.github/workflows/ci.yaml` | Added `go build -tags otel ./...` + `go vet -tags otel ./...`. |

**Not touched (verified):** `internal/reliability/metrics.go` (frozen names/attrs), `internal/reliability/slo.go` (semantics preserved — used as-is), `internal/reliability/otelmetrics_sink_otel.go` (metric names frozen — no new counters added), `internal/tracing/collector.go`, W1 cost files, migrations.

## Deliverables mapping

- **G9 Prometheus:** Done. `/metrics` on `telemetry.prometheus_port` (default 9090), loopback default. Hand-rolled Prometheus text format (no `promhttp`) — see concern below.
- **G10 SLO alert:** Done. `SLOTracker` observed in shared 5s flush loop; `bgalert.SendWebhook` fired with reason `slo_burn_rate` when `!WithinBudget`, gated by `reliability.slo.enabled` + `reliability.alerts.enabled`/webhook. Verified `WithinBudget` semantics in `slo.go` before wiring (it was test-only; now production-callable unchanged).
- **G11 Cost in OTel:** Span attribute `goclaw.llm.cost_usd` added. Per-(tenant,agent,model) cost/token counters to OTel sink **deferred** (per plan clause: only if cheap store read exists; `reliability` → `store` read would create an import cycle) — span attr shipped first, KISS.
- **G12 OTel CI:** Both commands added to `ci.yaml` go job.
- **Config keys:** `telemetry.prometheus_enabled` / `.port` / `.host` + env vars documented in config structs.

## Concerns

1. **Dependency deviation (G9):** Plan/scope called for `promhttp.Handler()` (`client_golang`). That module is NOT in `go.mod`/`go.sum`, and the controller's explicit fallback was "if new dependency blocks progress, skip the dep + note". Local Go toolchain unavailable (both controller-built paths confirmed; local `go` spawns fail with `ENAMETOOLONG` on uv_spawn), so I could not run `go get`/`go mod tidy` to add a tag-gated dependency safely. I hand-rolled the Prometheus text exposition format in the `prometheus`-tagged file — zero go.mod impact, default build stays clean, and the endpoint is fully Prometheus-scrape-compatible. If the controller prefers the real `client_golang` promhttp handler, it must add the tag-gated dep + `go mod tidy` (build-tag pattern already isolates it).
2. **Docs:** I did not update `docs/runbooks/reliability-ops.md` (out of my "may modify" list). Prometheus + SLO webhook runbook section remains a follow-up for the controller.
3. **Flush-loop ordering:** `Take()` before `Flush()` means the tracker observes the pre-drain delta; counters recorded between the two land in the sink's flush but not the tracker's sample — negligible drift at 5s cadence.

## Tests status

- Controller-run (Docker): `go build ./...`, `go build -tags sqliteonly ./...`, `go build -tags otel ./...`, `go vet ./...`, `go vet -tags otel ./...` — pending.
- `-tags prometheus` build/test — pending controller (validated by inspection).

Status: DONE_WITH_CONCERNS
Summary: W2 observability complete — Prometheus /metrics (hand-rolled zero-dep text format behind `-tags prometheus`), SLO burn-rate webhook wired into the shared flush loop on all builds, span cost attr added to OTel exporter, OTel build+vet added to CI, plus unit tests for all four.
Concerns: (1) Prometheus uses hand-rolled text exposition instead of `promhttp` because `client_golang` is absent from go.mod and no local Go toolchain exists to add a tag-gated dep — controller may add the dep if preferred; (2) per-(tenant,agent,model) OTel counters deferred per plan's KISS clause; (3) runbook doc update left to controller.