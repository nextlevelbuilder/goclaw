# Phase 2 — Cost Governance Gaps + Observability Gaps

**Depends on:** none (but share `cmd/gateway.go` with Phase 1快 W2 audit wiring — coordinate ordering; Phase 1 W2 touches `cmd/gateway_events.go`, this touches `cmd/gateway.go` startup. Keep separate files.)
**Files in this phase:** cost (`internal/store/sqlitestore/`, `internal/usage/caps/service.go`, `internal/store/pg/usage_caps.go`, `internal/http/usage_caps.go`), observability (`internal/reliability/slo.go`, `cmd/gateway.go`, `internal/tracing/otelexport/exporter.go`, `internal/reliability/otelmetrics_sink_otel.go`, `.github/workflows/ci.yaml`)
**Ownership:** W1 = cost gaps, W2 = observability gaps. Distinct files; big-risk on `cmd/gateway.go` — W2 only.

## Context / verified baseline

Cost + observability LARGELY BUILT (scout report). Baseline was verified independently: `internal/config/config.go:747-757` ModelPricing, `migrations/000070-000072` (pricing/cap policies/bridge), `internal/usage/caps/service.go:65-164` Preflight→Reserve→Reconcile, `internal/tracing/collector.go`, OTel exporter `//go:build otel`, `cmd/gateway_reliability_metrics.go`. Migration baseline: PG `000102`, SQLite `SchemaVersion=65`. No new migration needed unless SQLite cost tables added.

## Scope (from scout gaps)

### W1 — Cost governance gaps (scout G1, G3, G5, G7 + optional G2)
1. **SQLite/desktop UsageCapStore (G1):** Add `usage_cap_policies`, `usage_cap_counters`, `usage_cap_reservations`, `usage_cap_events` tables to SQLite (`schema.go` patch 66 + `schema.sql`). Implement `UsageCapStore` in `internal/store/sqlitestore/usage_caps.go` (mirror PG `internal/store/pg/usage_caps.go` interface `internal/store/usage_caps.go:185-202`). Wire `usagecaps.NewService` in sqliteonly path (`cmd/gateway_stores_sqliteonly.go`) so desktop enforces budgets. Pricing catalog: reuse `internal/usage/pricing` (uses decimal, DB-agnostic) — add SQLite `usage_pricing_catalog`/`overrides` tables.
2. **Spend-to-date window API (G3):** `UsageCapStore.GetBudgetUsage(ctx, tenantID, window)` returning per-policy `used/limit/percent` from `usage_cap_counters` join policies. Expose `GET /v1/usage-caps/overview`. Web UI usage page already reads utilization; wire new endpoint into `use-usage-caps.ts`.
3. **Budget threshold alerts (G5):** add `warn_at_percent` column to `usage_cap_policies` (PG migration `000104` + SQLite). On `Reconcile` crossing `warn_at_percent`, insert `usage_cap_events` row `decision='warn'` + fire `bgalert.SendWebhook` (reuse provider-error infra) with reason `goclaw.budget`. Config-gated like existing alerts.
4. **WS usage method pagination (G7):** replace in-memory 10k-session pagination in `internal/gateway/methods/usage.go:67-119` with SQL-backed LIMIT/OFFSET query on sessions where `input_tokens+output_tokens>0`. Keep response shape stable.
5. **Cost source of truth (G4):** make `usage_events.cost_usd` the billing truth for rollups; reconcile span `total_cost` vs `usage_events` in `backfillTraceCostsAfterPricingSync` (extend to also check usage_events vs spans). Document decision in code comment. Do NOT drop span cost (drill-down still uses it).
6. (Optional, G2 session budget — decide at dispatch: include `session_key` in policy SCOPE check but as sub-resource of agent counter, no schema change. Default: skip per YAGNI unless cheap.)

### W2 — Observability gaps (scout G9, G10, G11, G12)
1. **Prometheus `/metrics` (G9):** add `-tags prometheus` endpoint exposing `promhttp.Handler()` (feeding `reliability.Metrics.Snapshot` + per-tenant cost gauge from `usage_event_rollups`). New file `cmd/gateway_prometheus_*` (otel-tag-style: active + noop). Port/config: reuse `telemetry` config; add `telemetry.prometheus_enabled` + port. Keep OTLP intact.
2. **Wire SLO alerting (G10):** construct `SLOTracker` from `cfg.Reliability.SLO` in `cmd/gateway.go` startup; in the existing 5s `wireReliabilityMetrics` flush loop, `Observe(snapshot)`; on `!WithinBudget` call `bgalert.SendWebhook` with reason `slo_burn_rate`. Config already `reliability.slo` + `AlertingConfig`.
3. **Cost in OTel (G11):** add `attribute.Float64("goclaw.llm.cost_usd", s.TotalCost)` to `otelexport/exporter.go:126-170`. Also add per-(tenant,agent,model) cost/token counters to OTel metrics sink if a store read is cheap (else defer, ship span attr first — KISS).
4. **OTel build in CI (G12):** add `go build -tags otel ./...` + `go vet -tags otel ./...` to `ci.yaml`.
5. Tests: I/O cost calc, SLO tracker observe→burn-rate, prometheus handler smoke, SQLite UsageCapStore CRUD.

## Verification steps
- `go build ./...` + `go build -tags sqliteonly ./...` + `go build -tags otel ./...` + `go vet ./...` (controller Docker; note both tags).
- Unit: SQLite UsageCapStore, threshold-warn reconcile, SLO burn alert, prom handler, OTel span attr.
- Report in `reports/phase-02-cost-observability.md`.

## Risks / rollback
- Prometheus endpoint is NEW (additive). No conflict with existing router if bound to separate port.
- SQLite cost tables additive (schema patch 66 — coordinate with Phase 1 patch number; Phase 1 W1 adds approval table patch 66 too → Coordinate: Phase 1 uses patch 66 for `approval_requests`, Phase 2 uses patch 67. Controller enforces numbering to avoid collision.)
- SLO alerting: `SLOTracker` currently no production caller — verify `WithinBudget` semantics in `slo.go` before wiring; keep config-gated so off by default.