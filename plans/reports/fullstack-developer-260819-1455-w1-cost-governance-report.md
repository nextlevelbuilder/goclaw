# Phase 2 W1 — Cost Governance Gaps — Implementation Report

**Date:** 2026-08-19
**Workstream:** Phase 2 W1 (cost governance)
**Status:** DONE_WITH_CONCERNS

## Deliverables mapping

- **G1 SQLite UsageCapStore:** Done. Six tables added to `internal/store/sqlitestore/schema.sql` (fresh DBs) + `usageCapTablesMigration` as patch 66→67 in `schema.go` migrations map. Full `SQLiteUsageCapStore` in `internal/store/sqlitestore/usage_caps.go` mirrors PG (17 interface methods + `BudgetWindowWarned`). Factory wires `UsageCaps` for the sqliteonly (desktop) path so Lite enforces budgets. `SchemaVersion` bumped 65→67. Patch 66 (approval, Phase 1) untouched.
- **G5 warn_at_percent:** Done. PG migration `000104_usage_cap_warn.{up,down}.sql` (additive `ALTER TABLE ... ADD COLUMN warn_at_percent NUMERIC(5,2)` + partial index). SQLite parity in schema.sql + patch 67. `Reconcile` → `checkBudgetThresholds` fires exactly one `decision='warn'` `usage_cap_events` row per (policy, window_start), then fires a best-effort webhook via `bgalert.SendWebhook` (reason `goclaw.budget`). Dedup via `BudgetWindowWarned` (metadata `window_start` lookup). Config-gated: `cmd/gateway.go` wires `SetAlertWebhook(effectiveAlertWebhookURL(cfg), EffectiveAlertMinInterval()/time.Second)`.
- **G3 Spend-to-date window API:** Done. `store.GetBudgetUsage` (interface + PG + SQLite), `GET /v1/usage-caps/overview?window=hour|day|week|month` (empty → per-policy window). UI wired minimally: `useBudgetOverview` hook + `BudgetUsageRow` type + `warn_at_percent` on policy type + overview query key.
- **G7 WS usage pagination:** Done. Replaced in-memory 10k-session scan in `internal/gateway/methods/usage.go` with SQL-backed `TokenFilter` (new `SessionListOpts.TokenFilter bool`, pushed into SQL WHERE `(input_tokens > 0 OR output_tokens > 0)`) + existing LIMIT/OFFSET. Response shape unchanged (`records`/`total`/`limit`/`offset`). Backward-compatible: all existing `SessionListOpts` callers unaffected.
- **G4 Cost source of truth:** Done. `usage_event_cost_backfill.go` CTE now reconciles ALL diverging `usage_events.cost_usd` vs `spans.total_cost` (removed the `COALESCE(e.cost_usd,0)=0` zero-only guard). Comment documents the decision: `usage_events.cost_usd` is billing truth for rollups, spans are the originating measure, span cost retained. No `span` column dropped.
- **G2 Session budget:** Skipped (optional). Out of scope of the blocking deliverable set; not blocking any other work. Noted as deferred.

## Files Modified/Created

### New files
| File | Purpose |
|---|---|
| `migrations/000104_usage_cap_warn.up.sql` | Additive: `warn_at_percent NUMERIC(5,2) CHECK (0..100)` + partial index. |
| `migrations/000104_usage_cap_warn.down.sql` | Drop index + column. |
| `internal/store/sqlitestore/usage_caps.go` | Full SQLite UsageCapStore: pricing catalog/overrides, policy CRUD, ReserveUsage/ReconcileUsage (inline cap check via RowsAffected), utilization, events, GetBudgetUsage, BudgetWindowWarned. TEXT UUIDs, `?` placeholders, TEXT RFC3339Nano times, TEXT price affinity with CAST CHECKs. |
| `internal/store/sqlitestore/usage_caps_test.go` | SQLite store unit tests: policy CRUD, reserve idempotency + reconcile reserved→used, over-budget rejection, GetBudgetUsage math (cost-limit-wins 0.3), warn dedup per window, pricing resolve (catalog + override + missing), cross-tenant agent rejection, events newest-first. |

### Modified files
| File | Change |
|---|---|
| `internal/store/sqlitestore/schema.sql` | Six usage-cap/pricing tables + indexes after usage_event_rollups block. |
| `internal/store/sqlitestore/schema.go` | `SchemaVersion` 65→67; `66: usageCapTablesMigration` (patch 66 is Phase 1 approval — not touched); const body mirroring schema.sql. |
| `internal/store/sqlitestore/factory.go` | `UsageCaps: NewSQLiteUsageCapStore(db)`. |
| `internal/store/usage_caps.go` | `UsageCapEventWarn` const; `UsageCapPolicy.WarnAtPercent`; `UsageCapPolicyPatch.WarnAtPercent **float64`; `BudgetUsageWindow`/`BudgetUsageRow` types; `GetBudgetUsage` interface method; `UsageCapBudgetWarnStore` optional interface; `UsageReservationResult.TenantID` field. |
| `internal/store/pg/usage_caps.go` | warn_at_percent in create/update/scan; `GetBudgetUsage`; `BudgetWindowWarned` + private `budgetWindowWarned`; `TenantID` populated in ReserveUsage results. |
| `internal/usage/caps/service.go` | `checkBudgetThresholds` (warn event + webhook, once per window); `SetAlertWebhook`; bgalert import; webhook fields on Service. |
| `internal/usage/caps/service_test.go` | fake store now implements `GetBudgetUsage` + `BudgetWindowWarned`; new tests: warn fires exactly once per window, warn event metadata present when webhook configured. |
| `cmd/gateway.go` | `usageCapSvc.SetAlertWebhook(effectiveAlertWebhookURL(cfg), int(cfg.Reliability.Alerts.EffectiveAlertMinInterval()/time.Second))`. |
| `internal/http/usage_caps.go` | `GET /v1/usage-caps/overview` route + `handleBudgetOverview`; `warn_at_percent` on policy body/patch (incl. JSON-null handling). |
| `internal/i18n/keys.go` | `MsgUsageCapsOverviewFailed = "usage_caps.overview_failed"`. |
| `internal/i18n/catalog_{en,vi,zh,ru}.go` | Overview-failed translations in all 4 catalogs. |
| `internal/store/session_store.go` | `SessionListOpts.TokenFilter bool db:"-"`. |
| `internal/store/pg/sessions_list.go` | `(input_tokens > 0 OR output_tokens > 0)` SQL filter. |
| `internal/store/sqlitestore/sessions_list.go` | Same filter for SQLite. |
| `internal/gateway/methods/usage.go` | SQL-backed pagination via `ListPagedRich` + TokenFilter; dropped in-memory zero-token scan. |
| `internal/store/pg/usage_event_cost_backfill.go` | Reconcile all diverging event-vs-span costs (not just zero); cost-source-of-truth comment. |
| `internal/upgrade/version.go` | `RequiredSchemaVersion` 102→104. |
| `internal/store/stores.go` | Updated `UsageCaps` field comment (now SQLite-backed too). |
| `ui/web/src/types/usage-caps.ts` | `warn_at_percent` on policy; `BudgetUsageRow` type. |
| `ui/web/src/lib/query-keys.ts` | `usage.caps.overview(window)` key. |
| `ui/web/src/pages/usage/hooks/use-usage-caps.ts` | `useBudgetOverview` exported hook; overview key invalidated on refresh. |

**Not touched (verified):** Phase 1 files (approval store, migration 000103, patch 65→66 approval), `internal/reliability/`, `internal/tracing/otelexport/`, `.github/workflows/ci.yaml`, `internal/gateway/methods/exec_approval.go`, `internal/tools/exec_approval.go`, `cmd/gateway_events.go`, `cmd/gateway_prometheus_*`.

## Compile fixes made during verification

- `UsageReservationResult` lacked `TenantID`; `service.go` referenced `r.result.TenantID`. Added the field + populated it in PG + SQLite `ReserveUsage` (both return sites) + fake in tests.
- `sqlitestore` referenced undefined `nullStatus` (PG-only helper). Added SQLite equivalent.
- `sqlitestore.ResolvePricing` referenced undefined `usagePricingModelCandidates`/`openRouterProviderPrefixes`/`openRouterModelPrefixes`/`normalizeProviderAlias`/`appendUniqueString`. Added mirrors from PG.
- `fakeUsageCapStore` in `service_test.go` lacked `GetBudgetUsage` (new interface method) — added + `BudgetWindowWarned`.

## Tests Status

- Type check / build: not run locally (no local Go toolchain; controller runs Docker build). Code verified by manual interface/method trace.
- Unit tests added:
  - `internal/store/sqlitestore/usage_caps_test.go` — CRUD, reserve/reconcile, over-budget, GetBudgetUsage math, warn dedup, pricing resolve, cross-tenant, events order.
  - `internal/usage/caps/service_test.go` — warn-once-per-window, warn-event metadata.
- Integration tests: none run (require PG + server, controller handles).

## Issues Encountered / Concerns

1. **G2 session budget skipped** (optional deliverable). Not blocking.
2. **Webhook test uses `http://localhost:1/alert`** — `bgalert.SendWebhook` performs a real HTTP POST that will fail fast (connection refused). Best-effort + swallowed errors, so no flake risk, but it does exercise the network path. Could be gated by making the webhook send injectable, but that expands scope — left as-is.
3. **Cost-source-of-truth backfill now overwrites ALL diverging event costs** (not just zero). Intended per G4, but worth noting it changes existing non-zero event rows on next backfill run.
4. **`warn_at_percent` CHECK bound 0..100** enforced in both PG and SQLite; the service skips `<= 0` thresholds, so 0 effectively disables.

## Next Steps

- Controller: Docker `go build ./...` + `go build -tags sqliteonly ./...`, `go vet`, run the new unit tests (`go test -tags sqlite ./internal/store/sqlitestore/` + `go test ./internal/usage/caps/`).
- Consider wiring the overview endpoint into the usage-caps panel UI (currently hook-only, minimal).

Status: DONE_WITH_CONCERNS
Summary: All blocking Phase 2 W1 deliverables implemented (SQLite UsageCapStore, warn_at_percent + webhook, budget overview API + UI hook, WS SQL pagination, cost source of truth). Optional G2 skipped. Compile issues found during self-verification fixed. Tests written; build not run locally.
Concerns/Blockers: G2 deferred; webhook test performs a real (failing) HTTP POST; cost backfill now overwrites all diverging event costs (intended).
