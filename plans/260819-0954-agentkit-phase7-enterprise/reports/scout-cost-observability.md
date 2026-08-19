# Scout: Cost Governance + Observability — Phase 7 Enterprise Gap Analysis

**Date:** 2026-08-19 | **Scoped to:** cost governance (LLM cost limits/budgets per tenant/agent/session) + observability (metrics, tracing, alerting)

---

## Status

- **Cost governance: largely BUILT** (PostgreSQL/Standard only). Mature: policies, windowed counters with reservation/reconcile, pricing catalog + overrides, enforcement gate in agent loop, HTTP + WS APIs, web UI. Gaps: SQLite/desktop parity, usage-event-based cost tracking vs trace-based, session-level budgets, threshold alerting, budget notification UX.
- **Observability: largely BUILT.** OTel tracing + reliability metrics are build-tag (`otel`) gated and opt-in via config; in-DB `traces`/`spans`/`usage_events`/`usage_snapshots`/`usage_event_rollups` are the primary store; web UI has a Usage analytics page. No Prometheus/`/metrics` endpoint — OTLP only. Alerting: webhook for SLO burn-rate + background-provider errors exists (config-gated), but the SLO tracker is not wired to the alert webhook in the code I traced — only provider-error webhooks via `bgalert`.

---

## 1. Existing State

### 1.1 Cost governance — what exists

**Pricing (per-million-token cost tables)**
- `internal/config/config.go:747-757` — `ModelPricing` struct (`input_per_million`, `output_per_million`, `cache_read_per_million`, `cache_create_per_million`, `reasoning_per_million`), under `telemetry.model_pricing` (`config.go:769`).
- `internal/usage/pricing/decimal.go:55-101` — `CostMicros()` computes exact micro-cost with `big.Rat`; `FromProviderUsage()` (`decimal.go:26-49`) maps `providers.Usage` → billable units (tokens, cache, reasoning, request, image, web search).
- `internal/tracing/cost.go:24-51` — `CalculateCost()` (config-pricing path); `CalculateCostFromUsagePricing()` (`cost.go:53-66`); `LookupPricing()` (`cost.go:70-81`).
- `migrations/000070_usage_caps_pricing.up.sql` — `usage_pricing_catalog` (OpenRouter catalog, `model_id UNIQUE`, `NUMERIC(30,18)` prices) + `usage_pricing_overrides` (tenant/provider/model, `UNIQUE (tenant_id, provider_id, model_id)`).
- `internal/store/pg/usage_pricing.go:25-72` — `UpsertPricingCatalog`; `ResolvePricing()` (`usage_pricing.go:179-207`) with precedence override → catalog; provider/model candidate prefixes (`usage_pricing.go:209-284`).
- `internal/usage/pricing/openrouter_sync.go:14-35` — auto-sync OpenRouter catalog daily (24h).

**Budget policies + enforcement**
- `migrations/000071_usage_cap_policies.up.sql` — `usage_cap_policies` (tenant/agent/provider/model scoped, `window_key IN ('hour','day','week','month')`, `max_tokens` / `max_cost_micros`), `usage_cap_counters` (windowed used/reserved), `usage_cap_reservations` (per-call reserve → reconcile), `usage_cap_events` (allow/block audit log).
- `migrations/000072_agent_budget_usage_cap_bridge.up.sql` — legacy `agents.budget_monthly_cents` (`migrations/000015_agent_budget.up.sql:1`) bridged into `usage_cap_policies` with `source='agent_budget_monthly_cents'`.
- `internal/store/usage_caps.go:185-202` — `UsageCapStore` interface (policies CRUD, `ReserveUsage`, `ReconcileUsage`, utilization, events).
- `internal/store/pg/usage_caps.go` — PG implementation.
- `internal/usage/caps/service.go:65-164` — `Preflight()` gate: resolves provider, loads policies, estimates input tokens (`EstimateInputTokens`, `service.go:274-286`), prices estimate, `ReserveUsage` → `ErrCapExceeded` block + `usage_caps.blocked` slog. `Reservation.Reconcile()` (`service.go:178-235`) charges actual usage after the call.
- Enforcement wiring in agent loop:
  - `internal/agent/usage_caps_runtime.go:13-43` — `reserveInternalLLMUsage` (compaction/summarization/memory flush internal calls); `callInternalLLMWithUsage` (`usage_caps_runtime.go:45-88`) with hard-ceiling guard per fallback candidate.
  - `internal/agent/loop_pipeline_callbacks.go:827-838` — main-loop `guardCompleteModelRequest` + `usageCaps.Preflight` reservation.
  - `internal/agent/loop_request_budget.go:49-99` — per-request context-window budget guard (tokens, not cost).
  - `cmd/gateway.go:477` — `usageCapSvc := usagecaps.NewService(pgStores.UsageCaps, pgStores.Providers)`.

**Usage recording (where spend is captured)**
- `internal/agent/loop_pricing.go:16-45` — `calculateLLMCost()` resolves pricing per LLM call and returns USD.
- `internal/agent/usage_events.go:32-104` — `recordToolUsageEvent` (tool/skill/MCP/runtime usage events with `CostUSD`); `baseUsageEvent` (`usage_events.go:137-170`) tags tenant/agent/team/trace/run/session/channel. `insertUsageEventBestEffort` (`usage_events.go:180-203`) → collector buffer or direct insert.
- `internal/tracing/collector.go:539-557` — usage events flushed AFTER spans in same cycle (FK safety).
- `internal/agent/loop_tracing.go:236,373` — LLM spans carry `total_cost`; `internal/store/tracing_store.go:57,87` — `traces.total_cost`, `spans.total_cost`.
- `migrations/000080_usage_event_analytics.up.sql` — `usage_events` (per-call cost/tokens, `cost_usd DOUBLE PRECISION`) + `usage_event_rollups` (hourly aggregates, unique index on tenant/bucket/dims).
- `migrations/000016_usage_snapshots.up.sql:19-54` — `usage_snapshots` (hourly buckets by agent/provider/model/channel, `total_cost NUMERIC(12,6)`, cache/thinking tokens).
- `internal/tracing/snapshot_worker.go:15-33` — hourly aggregation from `traces`/`spans` → `usage_snapshots`, and `usage_events` → `usage_event_rollups`; catch-up + backfill (`snapshot_worker.go:146-230`).
- Startup cost backfill after pricing sync: `cmd/gateway.go:136-208` — `backfillTraceCostsAfterPricingSync` re-prices trace/span/snapshot/usage-event rows.
- Session-level token accumulation: `internal/store/session_store.go:41-42` (`sessions.input_tokens`/`output_tokens`), accumulated in agent loop; read via `internal/gateway/methods/usage.go:67-119`.

**APIs / UI**
- HTTP: `internal/http/usage_caps.go:32-44` — CRUD `/v1/usage-caps/policies`, `/v1/usage-caps/utilization`, `/v1/usage-caps/events`, `/v1/model-pricing*`. `internal/http/usage.go:27-34` — `/v1/usage/timeseries|breakdown|summary` + `/v1/usage/events/*` (from rollups).
- WS: `internal/gateway/methods/usage.go:47-50` — `usage.get` / `usage.summary`; `internal/gateway/methods/quota_methods.go:24-26` — `quota.usage`.
- Web UI: `ui/web/src/pages/usage/usage-page.tsx`, `usage-caps-panel.tsx`, `usage-event-analytics-panel.tsx`, hooks `use-usage-caps.ts` / `use-usage.ts` / `use-usage-event-analytics.ts`. Per-agent model-budget section `ui/web/src/pages/agents/agent-detail/overview-sections/model-budget-section.tsx`.
- Desktop UI: `ModelBudgetSection.tsx` (context-window budget only) — **no usage/cost analytics page**.

### 1.2 Observability — what exists

**Tracing**
- `internal/tracing/collector.go` — buffered batch collector: `CreateTrace`/`UpdateTrace`, `EmitSpan`, two-phase span updates, `FinishTrace`, retry queue (`collector.go:319-392`), stale-recovery (DISABLED, `collector.go:176-186`), prune >7d (`collector.go:19,466-479`), WS `trace.status` broadcast (`collector.go:588-605`).
- `internal/tracing/otelexport/exporter.go:38-99` — OTLP exporter (`grpc`/`http`), `ExportSpans` (`otelexport/exporter.go:103-224`) maps SpanData → OTel spans with `gen_ai.*` + `goclaw.*` attributes.
- Build-tag gating: `cmd/gateway_otel.go:1` (`//go:build otel`) + noop `cmd/gateway_otel_noop.go`. Wired `cmd/gateway.go:395` `initOTelExporter`. Config `internal/config/config.go:762-770` `TelemetryConfig` (`telemetry.enabled/endpoint/protocol/insecure/service_name/headers/model_pricing`).
- Docker OTel overlay: `docker-compose.otel.yml` (Jaeger + OTLP, `ENABLE_OTEL=true`).

**Metrics**
- `internal/reliability/metrics.go:20-44` — atomic counters for requests/successes/retries/rate-limited/server-errors/timeouts/stream-stalls/loop/tool-loop/empty-output/premature + latency nanos; `Snapshot` (`metrics.go:108-149`), `Flush()` drain (`metrics.go:227-233`).
- `internal/reliability/otelmetrics_sink_otel.go:22-39` — frozen `goclaw.reliability.*` metric names (consumed by Phase 10 Grafana dashboard per comment); `OTelSink.Emit` (`otelmetrics_sink_otel.go:94-128`) records counters + `llm_latency_ms` histogram. Build-tag `otel`.
- Wiring: `cmd/gateway_reliability_metrics.go:20-75` — `wireReliabilityMetrics` starts 5s flush loop; noop `cmd/gateway_reliability_metrics_noop.go`.
- Producer side: `internal/providers/reliability_wiring.go:23-71` — `observeSuccess`/`observeFailure` record metrics + health + breaker; `internal/providers/retry.go:135` records retries; stream stall `reliability_wiring.go:502-515`.
- **No Prometheus endpoint** — no `promhttp`, no `/metrics` route found in grep. OTLP metrics only (requires `-tags otel` + `telemetry.enabled`).
- Reliability SLO evaluator: `internal/reliability/slo.go:36-127` — rolling-window success-rate + burn-rate (`SLOTracker`), `WithinBudget`. Config `reliability.slo` (`config.go:90-94`).

**Alerting**
- `internal/bgalert/webhook.go:53-102` — `SendWebhook` posts provider-error alerts to webhook (throttled by `MinIntervalSeconds`); severity mapping (`webhook.go:38-45`). `internal/bgalert/report.go` stores alerts in system config + `bgalert.ReportProviderError`.
- Config: `config.go:97-101` `AlertingConfig{enabled, webhook_url, min_interval_seconds}`; gated `cmd/gateway.go:67-72` `effectiveAlertWebhookURL`.
- Wired consumers: consolidation workers, vault enrich worker (`cmd/gateway.go:500-507,538-541`) for non-retryable provider errors.
- **SLO burn-rate → webhook NOT wired**: `NewSLOTracker` is only referenced in `internal/reliability/slo_test.go`; `SLOTracker.Observe/Status` has no production caller in my grep. `AlertingConfig` powers `bgalert` provider-error webhooks only.

**Logging**
- Structured `slog` throughout; `cmd/gateway.go:210-234` sets level (`GOCLAW_LOG_LEVEL`) + `GOCLAW_LOG_FILE` + `gateway.NewLogTee`. Security logs `slog.Warn("security.*")` per CLAUDE.md.

**CI/Makefile**
- `.github/workflows/ci.yaml:53-55` — `go build ./...`, `go build -tags sqliteonly ./...`, `go vet`; unit tests `-race`; integration/invariant/contract layers (P0/P1/P2); coverage. OTel build is NOT compiled in CI (no `-tags otel` step). `dev-beta-release.yaml:286-345` builds docker with `ENABLE_OTEL` matrix arg.
- `Makefile:46` — `docker-compose.otel.yml` overlay; `Makefile:101-109` — `test-contracts`/`test-scenarios`/`test-critical`.

---

## 2. Gap List (Build-Needed)

### Cost governance
- **G1. SQLite/desktop cost governance absent.** `internal/store/stores.go:74-75` explicitly: "UsageCaps is Standard/PostgreSQL only in the first budget-control rollout." `internal/store/sqlitestore/` has NO `usage_cap_policies`/`usage_cap_counters`/`usage_pricing_*` implementation (grep: no matches), and no usage-events cost backfill. Desktop (Lite edition, `sqliteonly`) ships with `budget_monthly_cents` on agents but no policy/counter/pricing.
- **G2. No per-session cost budget.** Policies scope to tenant/agent/provider/model (`migrations/000071_usage_cap_policies.up.sql:3-6`). No `session_key` dimension in `usage_cap_policies`; reservation keys are `purpose:agentUUID:uuid` (`usage_caps_runtime.go:39`) — per-session enforcement would need a policy dimension + reservation key carrying session.
- **G3. No tenant-level *daily* cost default / no pre-aggregated "spend to date in window" API for dashboards.** Utilization exists (`ListUsageCapUtilization`, `usage_caps.go:160-168`), but budget-vs-spend trend (window progress) for the UI is per-policy counter state, not a time-series. Web usage page shows historical cost from snapshots/rollups; live budget consumption is separate.
- **G4. Cost attribution dual-path.** Costs recorded in BOTH trace/span `total_cost` (via `loop_pricing.go`) AND `usage_events.cost_usd` (via `recordToolUsageEvent`). Snapshot aggregation reads from traces/spans (`snapshot_worker.go:283-375`); usage-event rollups read from `usage_events`. Two numbers can disagree (pricing source, timing). No reconciliation/consistency check.
- **G5. No cost-budget threshold alerts** (e.g., 80% of monthly budget). Only hard block at 100%. `usage_cap_events` has `decision` allow/block but no "warn" tier; no webhook on near-limit.
- **G6. Budget enforcement skips non-billable providers** (`ShouldEnforceProvider`, `service.go:253-260` excludes OAuth Claude CLI/Bailian/ACP/Ollama; requires API key). Providers without API keys or with no pricing row are silently skipped (`pricing_unknown` block only when a cost cap exists).
- **G7. `usage.get`/`usage.summary` WS methods are session-row based and in-memory paginated over 10k sessions** (`usage.go:67-70`) — not enterprise-scale; cost comes from `GetSessionCosts` (tracing store) only when present, else 0.
- **G8. Lite edition limits block usage analytics** (Lite limits in CLAUDE.md exclude KG/RBAC/multi-tenant; usage page/tracing store may be gated) — verify before claiming desktop observability.

### Observability
- **G9. No Prometheus `/metrics` endpoint.** All metrics are OTLP-only and require `-tags otel` + `telemetry.enabled`. Enterprise operators on standard Go stacks expect Prometheus scrape; also OTel tracing spans carry no cost attribute (`otelexport/exporter.go:126-170` has no `gen_ai.usage.*cost*`, no `goclaw.cost`).
- **G10. SLO burn-rate alerting not wired.** `AlertingConfig` exists + `bgalert` webhook works, but `SLOTracker` is test-only; no production loop observes snapshots into it or fires on `!WithinBudget`. The "alerts for SLO burn-rate" config is effectively dead until wired.
- **G11. OTel exporter drops cost + some metrics.** `otelexport/exporter.go` exports token attrs but not cost; reliability OTel sink covers only reliability counters, not per-tenant/agent LLM cost/latency by model (that lives in PG `usage_snapshots`/`usage_event_rollups` only). No OTLP metric export for cost/tokens per agent/model.
- **G12. OTel build not exercised in CI.** `ci.yaml:53-55` builds default + `sqliteonly`, never `-tags otel`, so the OTel exporter/sink compile independently of CI. No test job covers the OTLP path.
- **G13. Trace ingestion is best-effort async with drop risk.** `EmitSpan` drops on full buffer (`collector.go:225-231`); usage events dropped on full channel (`collector.go:252-261`); retry queue drop-oldest (`collector.go:319-338`). For billing-grade cost data, silent drops matter.
- **G14. No centralized alert rules/dashboard config in repo.** Grafana dashboard is referenced in a comment (`otelmetrics_sink_otel.go:20-21`) but no dashboard JSON/alert rules shipped. No runbook in repo for OTel/budget.
- **G15. `recoverStaleOnce` stale-trace recovery disabled** (`collector.go:176-186`) — crashed/orphaned runs may stay `running` in DB; acceptable per comment but a monitoring blind spot.

---

## 3. Suggested Approach Per Gap

### Cost governance
- **G1 — SQLite/desktop cost governance:** Implement `UsageCapStore` in `internal/store/sqlitestore/` (tables in `schema.go` full schema + incremental patch in `schema.go` migrations map per CLAUDE.md dual-DB rule; bump `SchemaVersion`). Gate policy CRUD by edition if needed. Reuse `internal/usage/caps/service.go` unchanged (store interface). Scope per plan — do NOT ship if desktop cost enforcement is out of Phase 7 scope; at minimum ship read-only budget columns + counters.
- **G2 — Session budgets:** Add optional `session_key` column to `usage_cap_policies` + include session in `UsageCapScope` and reservation key. Alternatively treat session as a sub-resource of agent budget (reservation per session, reconcile against agent counter) — cheaper, no schema change.
- **G3 — Spend-to-date window API:** Add `GetBudgetUsage(ctx, tenantID, window)` returning per-policy `used/limit/percent` by querying `usage_cap_counters` joined to policies; expose `/v1/usage-caps/overview` and surface in web Usage page. Low risk, high operator value.
- **G4 — Single cost source of truth:** Make `usage_events.cost_usd` (or a new `cost_usd` on snapshots) the billing truth; keep trace `total_cost` for per-call drill-down but reconcile once (extend `backfillTraceCostsAfterPricingSync` to also reconcile usage_events vs spans by trace/span id, or drop the span cost path and compute rollups from usage_events only).
- **G5 — Threshold alerts:** Add `warn_at_percent` (or `alert_threshold`) to `usage_cap_policies`; when reconcile crosses it, insert a `usage_cap_events` row with `decision='warn'` and fire a webhook via `bgalert` (extend payload with `goclaw.budget` reason). Reuses existing webhook infra.
- **G6 — Unpriced/non-billable providers:** Keep skip (KISS) but log `slog.Warn("usage_caps.skip_non_billable", ...)` and surface skipped events in `usage_cap_events` (already inserted at `service.go:79-84`). Document in config. Do not force-enforce where provider cannot report cost.
- **G7 — WS usage methods:** Replace in-memory 10k-session pagination with SQL-backed paging (LIMIT/OFFSET over `sessions` where `input_tokens+output_tokens>0`), or move cost into `usage_event_rollups` query with session dimension. Keep response shape stable.
- **G8 — Lite gating:** Audit `edition` gates before claiming desktop parity; document which cost surfaces are Standard-only.

### Observability
- **G9 — Prometheus endpoint:** Add `-tags prometheus` (or reuse `otel` tag) exposing `promhttp.Handler()` on a `/metrics` port, feeding from `reliability.Metrics.Snapshot` + a new per-tenant cost gauge from `usage_event_rollups` (or `usage_snapshots`). Keep OTLP path intact. Verify port/config naming against existing gateway config conventions.
- **G10 — Wire SLO alerting:** In `cmd/gateway.go` startup, construct `SLOTracker` from `cfg.Reliability.SLO`, `Observe` each reliability `Metrics.Flush` snapshot (hook into the existing 5s loop or a new one), and on `!WithinBudget` call `bgalert.SendWebhook` with reason `slo_burn_rate`. Config-gated (already exists).
- **G11 — Cost in OTel:** Add `attribute.Float64("goclaw.llm.cost_usd", s.TotalCost)` (and `goclaw.agent_id` already present) to `otelexport/exporter.go:126-170`. Add a per-(tenant,agent,model) cost/token counter to the OTel metrics sink reading from usage rollups (needs a store — heavier; consider deferring, ship span cost attr first).
- **G12 — CI OTel build:** Add `go build -tags otel ./...` and `go vet -tags otel ./...` to `ci.yaml` (fast, catches tag-gated compile rot).
- **G13 — Drop risk:** Bump buffer sizes configurable; log + count drops (`tracing.span_dropped` counter already logs Warn); for billing-grade, add a durable fallback insert path for usage events (e.g., direct `InsertEvents` retry with backoff rather than drop) — align with Phase "record-only" P0 decision.
- **G14 — Dashboard/alert rules:** Ship a `deploy/grafana/` folder with a dashboard JSON from `goclaw.reliability.*` + usage metrics + example alert rules + runbook snippet. Link from `docs/`.
- **G15 — Stale recovery:** Re-enable `staleRecoveryLoop` after adding `last_span_at` (per `collector.go:176-186` note) OR expose "running traces older than N" as a dashboard query + alert rule so operators see orphans.

---

## Unresolved Questions

1. Does Phase 7 include SQLite/desktop cost governance (G1), or is Standard/PG-only acceptable? `stores.go:74-75` documents PG-only as the deliberate first rollout.
2. Which is the billing source of truth — `usage_events` or trace spans (G4)? Choose one before building reports/alerts on it.
3. Is Prometheus (G9) required, or is OTLP + existing Jaeger overlay sufficient for Phase 7?
4. Is per-session cost budgeting (G2) actually a Phase 7 requirement, or is agent/tenant granularity enough?

Status: DONE_WITH_CONCERNS
Summary: Cost governance and observability are both largely built (usage cap policies with reservation/reconcile + pricing catalog; OTel tracing/metrics + in-DB usage analytics + webhook alerting), but enterprise gaps remain: SQLite/desktop has no UsageCapStore, no Prometheus endpoint, SLO burn-rate alerting is unwired, cost data flows through two un-reconciled paths, and no budget-threshold alerts exist.
Concerns/Blockers: The SLO tracker (`SLOTracker`) appears to have no production caller — verify during planning before claiming SLO alerting works. OTel build is not compiled in CI, so G12 is a cheap high-value fix. The `usage.get` WS method is O(n=10k) in-memory — fine for small deployments, not enterprise scale.
