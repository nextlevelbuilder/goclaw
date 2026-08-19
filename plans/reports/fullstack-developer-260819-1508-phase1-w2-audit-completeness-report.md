# Phase 1 W2 — Audit Completeness — Implementation Report

**Date:** 2026-08-19
**Workstream:** Phase 1 W2 (plan `plans/260819-0954-agentkit-phase7-enterprise/phase-01-approval-audit.md`, W2 scope)
**Status:** DONE_WITH_CONCERNS

## Deliverables mapping

| # | Deliverable | Status |
|---|---|---|
| 1 | Login/logout audit (WS + HTTP) | Done. `auth.login` / `auth.login_failed` in WS connect (all paths) + HTTP `requireAuth`/`requireAuthBearer`. `auth.logout` emitted on WS disconnect for authenticated clients. |
| 2 | WS audit tenant_id | Done. `methods/audit.go` now has `emitAuditCtx` with ctx-tenant precedence; connect-path emits resolve tenant via `routerTenantID` (ctx → client → master). |
| 3 | SQLite audit persistence | Done. `wireAuditSubscriber` now uses `d.auditActivityStore()` which returns `pgStores.Activity` (SQLiteActivityStore in desktop builds). Comment documents backend-agnostic wiring. |
| 4 | Retention (`audit.retentionDays` + Prune + daily sweep) | Done. `Prune` on ActivityStore interface + PG + SQLite. Config `audit.retention_days` (default 0). Daily 24h-ticker sweep in `cmd/`. |
| 5 | Audit export (`GET /v1/activity/export`) | Done. CSV + JSONL, admin-gated, streamed in pages. |
| 6 | Unit tests | Done. WS tenant scoping, login/login_failed, Prune (PG + SQLite), SQLite persist, export endpoint, logout. |

## Files Modified/Created

### New files
| File | Purpose |
|---|---|
| `internal/gateway/methods/audit_test.go` | `emitAuditCtx` tenant precedence (ctx > client), nil-publisher no-op. Uses `gateway.NewCapturingTestClient`. |
| `internal/gateway/router_test.go` (augmented) | Added `TestHandleConnect_AuditsGatewayTokenLogin`, `TestHandleConnect_AuditsRejectedLogin`, `TestRouterTenantID_Ordering`. |
| `internal/gateway/auth_logout_test.go` | `unregisterClient` emits `auth.logout` for authenticated clients, none for unauthenticated. |
| `internal/http/auth_audit_test.go` | `requireAuth` emits `auth.login` (success) / `auth.login_failed` (failure), method detection `bearer`. |
| `internal/http/activity_export_test.go` | CSV/JSONL export, invalid format 400, admin-gated 403. |
| `internal/store/sqlitestore/activity_test.go` | SQLite `Prune` (old deleted / new kept, empty delete), `Log` persists tenant. |
| `internal/store/pg/activity_test.go` | PG `Prune`, `Log`+`List` (skips when `TEST_DATABASE_URL` unset). |

### Modified files
| File | Change |
|---|---|
| `internal/gateway/methods/audit.go` | `emitAuditCtx(pub, client, reqCtx, ...)` — ctx tenant overrides client tenant; `emitAudit` delegates. |
| `internal/gateway/router.go` | Connect auth audit emits in Path 1/1b/2/3a/4; `routerTenantID` helper; `bus` import. |
| `internal/gateway/server.go` | `EventPublisher()` accessor; `unregisterClient` emits `auth.logout`; `emitLogoutAudit`. |
| `internal/http/auth.go` | `pkgMsgBus` + `InitAuditBus`; `authMethod()`; `auditLogin()`; calls in `requireAuth`/`requireAuthBearer`. |
| `internal/http/activity.go` | `GET /v1/activity/export` route, `handleExport`, `writeExportHeader`, `writeExportRows`. |
| `internal/store/activity_store.go` | `Prune(ctx, before) (int64, error)` added to interface. |
| `internal/store/pg/activity_store.go` | `Prune` — batched DELETE `created_at < $1 LIMIT 1000`. |
| `internal/store/sqlitestore/activity.go` | `Prune` — batched DELETE `created_at < ? LIMIT 1000` (RFC3339Nano). |
| `internal/http/activity_aggregate_test.go` | Added `Prune` stub to `fakeActivityStore`. |
| `internal/config/config.go` | `Audit AuditConfig` field + `ReplaceFrom`. |
| `internal/config/config_channels.go` | `AuditConfig{RetentionDays int}`. |
| `internal/config/config_load.go` | `Default()` sets `Audit.RetentionDays: 0`. |
| `cmd/gateway.go` | `httpapi.InitAuditBus(msgBus)`; `deps.wireAuditRetentionSweep()`. |
| `cmd/gateway_events.go` | `auditActivityStore()` helper; `wireAuditSubscriber` uses it; `wireAuditRetentionSweep()` (24h ticker + stop channel). |
| `cmd/gateway_deps.go` | `auditRetentionStop func()` field. |
| `cmd/gateway_lifecycle.go` | Stops retention sweep on shutdown. |

## Design decisions / deviations from plan wording

1. **SQLite persistence wiring (plan item 3).** The plan said "use `sqliteStores.Activity` when PG store nil". There is NO separate `sqliteStores` container in `gatewayDeps` — `setupStoresAndTracing` returns the same `*store.Stores` for all backends (PG, sqlite, sqliteonly), and `store.Stores.Activity` is set by each factory (PG `factory.go:50`, SQLite `factory.go:67`). So `d.pgStores.Activity` IS the SQLiteActivityStore on desktop builds; wiring was already backend-agnostic. I made it explicit and self-documenting via `auditActivityStore()`.
2. **`auth.logout` placement.** Emitted in `unregisterClient` (WS disconnect), gated on `client.authenticated`. No HTTP session-end logout was added — this is a stateless bearer/gateway-token surface; there is no HTTP session to tear down. WS disconnect is the authoritative session teardown point.
3. **Tenant for failures.** `auth.login_failed` events fall back to master tenant (no authenticated tenant exists at rejection time). This matches `store.MasterTenantID` convention for system-level logins.
4. **Export paging.** Reused the existing `List` store method with fixed 500-page/offset loop (no new cursor API).

## Key implementation details

- **WS connect audit emits** are placed AFTER tenant/userID resolution (not earlier draft position) so the tenant on the audit row is accurate — verified Path 1 at `router.go:182`, Path 1b at `router.go:233`, Path 2 at `router.go:245`, Path 3a at `router.go:304`, Path 4 at `router.go:343`.
- **HTTP login audit**: `requireAuth`/`requireAuthBearer` emit `auth.login` only after auth+role pass, `auth.login_failed` on either failure side.
- **Retention sweep** mirrors `internal/workstation/activity_sink.go` (24h ticker + stop channel). Disabled when `RetentionDays <= 0`. Reads `d.cfg.Audit.RetentionDays` per tick (fresh on hot-reload).
- **No new i18n keys** — reuses `i18n.MsgInvalidRequest` with args (verified existing pattern in `handleAggregate`).
- **No new WS methods** — only HTTP export route added; no `isReadMethod`/`isWriteMethod`/`isAdminMethod` changes needed.

## Tests

Write-only (controller runs Docker build/test). Not executed locally — Bash tool in this session fails with `ENAMETOOLONG: name too long, uv_spawn`, and no local Go toolchain per project memory (build/test runs in Docker).

- `internal/gateway/methods/audit_test.go` — monkey `EventPublisher` captures broadcast; asserts tenant precedence, actor, entity, nil-pub no-op.
- `internal/gateway/router_test.go` — WS connect emits `auth.login` (gateway_token → master tenant) + `auth.login_failed` (invalid_credentials); `routerTenantID` ordering.
- `internal/gateway/auth_logout_test.go` — disconnecting authenticated client emits `auth.logout` with tenant; unauthenticated client emits none.
- `internal/http/auth_audit_test.go` — HTTP login/login_failed via real `bus.MessageBus` subscriber; method detection (`bearer`).
- `internal/http/activity_export_test.go` — CSV header+rows, JSONL NDJSON lines, invalid format 400, non-admin 403.
- `internal/store/sqlitestore/activity_test.go` — Prune deletes old/misses new, empty no-op, Log persists tenant (FK-seeded tenants).
- `internal/store/pg/activity_test.go` — Prune + Log/List; skips when `TEST_DATABASE_URL` unset.

## Not touched (constraints preserved)

- W1 files: `internal/tools/exec_approval.go`, `internal/gateway/methods/exec_approval.go`, migration `000103*`.
- Phase 2 files: usage caps, `internal/reliability`, `otelexport`, `.github/workflows/ci.yaml`.
- No commits/pushes (write-only as instructed).

## Concerns

1. `unregisterClient` emits `auth.logout` before acquiring `s.mu` (deliberate — avoids emitting under lock; `eventPub` is immutable post-construction). Double-check for any caller that expects the unsubscribe ordering to differ — only `handleWebSocket` defer calls it.
2. HTTP stateless logout (session-end event) is not emitted — no HTTP session concept exists. Flagging so the controller can confirm this matches Phase 1 acceptance intent.
3. Export streams a possibly large tenant table in 500-row pages without a total count header; acceptable for admin export use, but a huge tenant may take many round-trips.

Status: DONE_WITH_CONCERNS
Summary: Phase 1 W2 audit completeness fully implemented (login/logout/login_failed events over WS+HTTP, WS audit tenant_id fix, SQLite audit persistence via unified stores container, retention with Prune + daily sweep, CSV/JSONL export endpoint, unit tests for all new paths). Controller should run Docker build/test (both std and `-tags sqliteonly`).
Concerns: HTTP logout session teardown N/A (stateless); export streams in pages without total; Bash tooling unavailable in this session so tests are write-only.